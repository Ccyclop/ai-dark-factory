#!/usr/bin/env bash
# Adversary attack set for the state-changing surface: items, reservations,
# cancellations, the stock invariants and idempotency.
#
# The WI-1 set (attack-wi1.sh) can only attack a *running* service. This set
# attacks everything that changes state, which is where the contract's real
# invariants live: C-CREATE-4, C-RES-5/6/7, C-CAN-1..4, C-INV-1/2/3, C-IDEM-1..7,
# C-RUN-3, C-ID-1, C-ERR-PREC.
#
# Every state check is gated: while the routes do not exist yet the check is
# recorded as SKIP with the reason and the script still exits 0. When they do
# exist, each check is a self-contained attack with its own item, so the expected
# arithmetic never depends on another section's leftovers.
#
# Usage:
#   adversary/attack-state.sh            attack the service built from practice/service
#   adversary/attack-state.sh --stub     self-test against adversary/dryrun (NOT a verdict)
#   adversary/attack-state.sh --keep     leave the service container running afterwards
#
# Exit code is non-zero only if a check failed. Skips never fail the run.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# shellcheck source=lib.sh
source adversary/lib.sh

STUB=0
KEEP=0
for arg in "$@"; do
  case "$arg" in
    --stub) STUB=1 ;;
    --keep) KEEP=1 ;;
    *) printf 'usage: %s [--stub] [--keep]\n' "$0" >&2; exit 2 ;;
  esac
done

PASS=0; FAIL=0; SKIP=0
declare -a FAILURES=()
declare -a SKIPS=()
declare -a WATCHES=()

ok()   { PASS=$((PASS+1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); FAILURES+=("$1"); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP+1)); SKIPS+=("$1"); printf '  \033[33mSKIP\033[0m %s\n' "$1"; }
watch(){ WATCHES+=("$1"); printf '  \033[33mWATCH\033[0m %s\n' "$1"; }
head_() { printf '\n\033[1m== %s\033[0m\n' "$1"; }

# eq <label> <expected> <actual>
eq() { if [ "$2" = "$3" ]; then ok "$1 -> $2"; else bad "$1: expected [$2] got [$3]"; fi; }

# Skips a whole section with one line per contract id it would have covered.
skip_section() {
  local reason="$1"; shift
  for id in "$@"; do skip "$id ($reason)"; done
}

# ---------------------------------------------------------------- report
# Defined before first use: the gate leaves early through it.
report_and_exit() {
  if [ "$KEEP" = "0" ]; then adv_down; fi
  printf '\n\033[1m== Result\033[0m  pass=%d fail=%d skip=%d watch=%d\n' "$PASS" "$FAIL" "$SKIP" "${#WATCHES[@]}"
  if [ "${#WATCHES[@]}" -gt 0 ]; then
    printf '\nObservations (not breaks):\n'
    for w in "${WATCHES[@]}"; do printf '  - %s\n' "$w"; done
  fi
  if [ "$SKIP" -gt 0 ]; then
    printf '\nSkipped (not attackable yet):\n'
    for s in "${SKIPS[@]}"; do printf '  - %s\n' "$s"; done
  fi
  if [ "$FAIL" -gt 0 ]; then
    printf '\nFailed checks:\n'
    for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
    exit 1
  fi
  exit 0
}

# ---------------------------------------------------------------- requests
# new_item <name> <stock>: creates an item and sets ITEM_ID / ITEM_STOCK.
new_item() {
  local out st body
  out="$(adv_tool req POST /items --body "{\"name\":\"$1\",\"stock\":$2}" 2>&1)"
  st="$(adv_status "$out")"; body="$(adv_body "$out")"
  ITEM_ID="$(jnum "$body" id)"; ITEM_STOCK="$2"
  case "$ITEM_ID" in
    ''|*[!0-9]*) ITEM_ID=""; return 1 ;;
  esac
  [ "$st" = "201" ]
}

# avail_of <id>: prints the item's available count, or nothing if not readable.
avail_of() {
  local out; out="$(adv_tool req GET "/items/$1" 2>&1)"
  [ "$(adv_status "$out")" = "200" ] || return 1
  jnum "$(adv_body "$out")" available
}

# resv <item_id> <quantity>: prints the tool output of one reserve.
resv() { adv_tool req POST /reservations --body "{\"item_id\":$1,\"quantity\":$2}" 2>&1; }

# reserve_ok <item_id> <quantity>: reserves and prints the new reservation id.
reserve_ok() {
  local out body; out="$(resv "$1" "$2")"
  [ "$(adv_status "$out")" = "201" ] || return 1
  body="$(adv_body "$out")"
  jnum "$body" id
}

# ---------------------------------------------------------------- harness
head_ "Harness: C-RUN-4 / C-RUN-5 preconditions"
if [ "$STUB" = "1" ]; then
  docker image inspect adversary-stub:local >/dev/null 2>&1 \
    || die "build it first: docker build --network=none -t adversary-stub:local adversary/dryrun"
  ADV_TAG=adversary-stub:local
  printf '  self-test mode: stub image, results are NOT a verdict\n'
else
  adv_build_tool
  adv_require_images
  adv_build_service
  ok "service built offline with tag $ADV_TAG (C-RUN-5)"
fi

adv_up
if docker port "$ADV_NAME" 2>&1 | grep -q .; then
  bad "docker port published something under --network=none (C-RUN-4 premise)"
else
  ok "docker port is empty: -p 8080:8080 publishes nothing (C-RUN-4 premise)"
fi
res="$(docker inspect "$ADV_NAME" --format '{{.HostConfig.NetworkMode}} {{.HostConfig.NanoCpus}} {{.HostConfig.Memory}}')"
eq "run limits intact (C-RUN-1)" "none 1000000000 536870912" "$res"
if adv_tool ready 10 >/dev/null 2>&1; then
  ok "ready within 10s (C-RUN-2)"
else
  bad "not ready within 10s (C-RUN-2)"
fi

# ---------------------------------------------------------------- gate
head_ "Gate: are the state routes implemented?"
GATE_OUT="$(adv_tool req POST /items --body '{"name":"adv-gate","stock":1}' 2>&1)"
GATE_ST="$(adv_status "$GATE_OUT")"
printf '  POST /items -> %s\n' "${GATE_ST:-no response}"
STATE_LIVE=0
[ "$GATE_ST" = "201" ] && STATE_LIVE=1
ITEM_ID=""; ITEM_STOCK=""; RID=""

if [ "$STATE_LIVE" = "0" ]; then
  skip_section "route not implemented yet: POST /items -> ${GATE_ST:-none}" \
    C-CREATE-1 C-CREATE-2 C-CREATE-3 C-CREATE-4 C-ID-1 \
    C-RES-1 C-RES-5 C-RES-6 C-RES-7 \
    C-CAN-1 C-CAN-2 C-CAN-3 C-CAN-4 \
    C-INV-1 C-INV-2 C-INV-3 C-ERR-PREC \
    C-IDEM-1 C-IDEM-2 C-IDEM-3 C-IDEM-4 C-IDEM-5 C-IDEM-6 C-IDEM-7 C-INV-4 \
    C-RUN-3
  report_and_exit
fi

# ---------------------------------------------------------------- C-CREATE
head_ "C-CREATE-4: identical creates without a key are independent items"
out="$(adv_tool seq 5 POST /items --body '{"name":"adv-repeat","stock":7}' 2>&1)"
eq "5 identical creates -> 5 x 201" "201=5" "$(adv_hist "$out")"
eq "5 distinct bodies, so 5 distinct items" "5" "$(adv_distinct "$out")"
eq "no dropped connection" "0" "$(adv_incomplete "$out")"

head_ "C-CREATE-2/3: boundary values for name and stock"
out="$(adv_tool req POST /items --body '{"name":"zero","stock":0}' 2>&1)"
eq "stock 0 accepted" "201" "$(adv_status "$out")"
eq "stock 0 -> available 0" "0" "$(jnum "$(adv_body "$out")" available)"
out="$(adv_tool req POST /items --body '{"name":"max","stock":1000000000}' 2>&1)"
eq "stock 1000000000 accepted" "201" "$(adv_status "$out")"
for v in -1 1000000001 5.0 1e3 '"5"' true null '"abc"'; do
  out="$(adv_tool req POST /items --body "{\"name\":\"edge\",\"stock\":$v}" 2>&1)"
  eq "stock $v rejected (C-ERR-400)" "400" "$(adv_status "$out")"
done

NAME200="$(printf 'é%.0s' {1..100})$(printf 'x%.0s' {1..100})"
NAME201="$(printf 'é%.0s' {1..100})$(printf 'x%.0s' {1..101})"
out="$(adv_tool req POST /items --body "{\"name\":\"$NAME200\",\"stock\":1}" 2>&1)"
eq "name of exactly 200 code points accepted" "201" "$(adv_status "$out")"
eq "200-code-point name returned unchanged" "$NAME200" "$(jstr "$(adv_body "$out")" name)"
out="$(adv_tool req POST /items --body "{\"name\":\"$NAME201\",\"stock\":1}" 2>&1)"
eq "name of 201 code points rejected" "400" "$(adv_status "$out")"
out="$(adv_tool req POST /items --body '{"name":"   ","stock":1}' 2>&1)"
eq "whitespace-only name rejected" "400" "$(adv_status "$out")"
out="$(adv_tool bigbody 1048577 POST /items 2>&1)"
eq "body of 1048577 bytes rejected (C-ERR-400)" "400" "$(adv_status "$out")"
out="$(adv_tool req POST /items --body '{"name":"x","stock":1} {"name":"y","stock":2}' 2>&1)"
eq "trailing content after the object rejected" "400" "$(adv_status "$out")"

# ---------------------------------------------------------------- C-RES under simultaneity
head_ "C-RES-6 / C-INV-1: 50 simultaneous reserves on one item (S-11)"
new_item storm 5 || bad "could not create the item for the reserve storm"
if [ -n "${ITEM_ID:-}" ]; then
  out="$(adv_tool burst 50 POST /reservations --body "{\"item_id\":$ITEM_ID,\"quantity\":1}" 2>&1)"
  eq "50 x 1 unit on stock 5 -> 5 x 201, 45 x 409" "201=5 409=45" "$(adv_hist "$out")"
  eq "no dropped connection (C-ERR-500)" "0" "$(adv_incomplete "$out")"
  eq "available is 0 (C-INV-1, C-INV-2)" "0" "$(avail_of "$ITEM_ID")"

  new_item storm2 3 || bad "could not create the item for the second reserve storm"
  out="$(adv_tool burst 50 POST /reservations --body "{\"item_id\":$ITEM_ID,\"quantity\":2}" 2>&1)"
  eq "50 x 2 units on stock 3 -> 1 x 201, 49 x 409" "201=1 409=49" "$(adv_hist "$out")"
  eq "available is 1: the leftover unit is not lost (C-INV-2)" "1" "$(avail_of "$ITEM_ID")"
  out="$(adv_tool burst 50 POST /reservations --body "{\"item_id\":$ITEM_ID,\"quantity\":3}" 2>&1)"
  eq "50 x 3 units on the leftover 1 -> 0 x 201, 50 x 409" "409=50" "$(adv_hist "$out")"
  eq "available still 1 (C-RES-5 changes nothing)" "1" "$(avail_of "$ITEM_ID")"
fi

# ---------------------------------------------------------------- C-RES-5, C-ERR-PREC
head_ "C-RES-5: insufficient stock is 409 and changes nothing"
new_item exact 10 || bad "could not create the item for the exact-reserve check"
if [ -n "${ITEM_ID:-}" ]; then
  EXACT_ID="$ITEM_ID"
  out="$(resv "$EXACT_ID" 10)"
  eq "reserve of exactly available succeeds" "201" "$(adv_status "$out")"
  eq "available is 0 afterwards" "0" "$(avail_of "$EXACT_ID")"
  before="$(avail_of "$EXACT_ID")"
  out="$(resv "$EXACT_ID" 1)"
  eq "one more unit is 409" "409" "$(adv_status "$out")"
  eq "the 409 body is C-REP-ERR" "1" \
     "$(printf '%s' "$(adv_body "$out")" | grep -Ec '^\{"error":".+"\}$')"
  eq "available unchanged by the 409" "$before" "$(avail_of "$EXACT_ID")"

  for q in 0 -1 1000000001 1.5 '"3"'; do
    out="$(resv "$EXACT_ID" "$q")"
    eq "quantity $q rejected (C-RES-3)" "400" "$(adv_status "$out")"
  done
  for id in 999999 0 -1; do
    out="$(resv "$id" 1)"
    eq "item_id $id is 404 (C-RES-4)" "404" "$(adv_status "$out")"
  done
  out="$(adv_tool req POST /reservations --body "{\"item_id\":999999,\"quantity\":0}" 2>&1)"
  eq "400 beats 404 (C-ERR-PREC)" "400" "$(adv_status "$out")"
  out="$(resv 999999 1)"
  eq "404 beats 409 for an unknown item (C-ERR-PREC)" "404" "$(adv_status "$out")"
  out="$(adv_tool req POST /reservations --body '{"item_id":1}' 2>&1)"
  eq "missing quantity is 400" "400" "$(adv_status "$out")"
  out="$(adv_tool req POST /reservations --body '[{"item_id":1,"quantity":1}]' 2>&1)"
  eq "array instead of object is 400" "400" "$(adv_status "$out")"
fi

# ---------------------------------------------------------------- C-CAN
head_ "C-CAN-1/2/3/4: cancel restores, repeats change nothing, ids are validated"
new_item cancel 10 || bad "could not create the item for the cancel checks"
if [ -n "${ITEM_ID:-}" ]; then
  CAN_ID="$ITEM_ID"
  RID="$(reserve_ok "$CAN_ID" 4)" || bad "reserve of 4 failed before the cancel checks"
  eq "available is 6 after reserving 4" "6" "$(avail_of "$CAN_ID")"
  first="$(adv_tool req DELETE "/reservations/$RID" 2>&1)"
  eq "cancel returns 200" "200" "$(adv_status "$first")"
  eq "status is cancelled (C-CAN-1)" "cancelled" "$(jstr "$(adv_body "$first")" status)"
  eq "available is 10 after the cancel" "10" "$(avail_of "$CAN_ID")"
  second="$(adv_tool req DELETE "/reservations/$RID" 2>&1)"
  eq "cancelling again returns 200" "200" "$(adv_status "$second")"
  eq "the second body is byte-identical (C-CAN-2)" "$(adv_body "$first")" "$(adv_body "$second")"
  eq "and nothing is released twice" "10" "$(avail_of "$CAN_ID")"
  for id in 999999 abc 0 -1 01; do
    out="$(adv_tool req DELETE "/reservations/$id" 2>&1)"
    eq "cancel $id is 404 (C-CAN-3)" "404" "$(adv_status "$out")"
  done
  out="$(resv "$CAN_ID" 10)"
  eq "released units can be reserved again (C-CAN-4)" "201" "$(adv_status "$out")"
fi

# ---------------------------------------------------------------- C-INV-3 simultaneity
head_ "C-INV-3: 20 simultaneous cancels of one reservation release once"
new_item once 100 || bad "could not create the item for the cancel storm"
if [ -n "${ITEM_ID:-}" ]; then
  ONCE_ID="$ITEM_ID"
  RID="$(reserve_ok "$ONCE_ID" 7)" || bad "reserve of 7 failed before the cancel storm"
  eq "available is 93 after reserving 7" "93" "$(avail_of "$ONCE_ID")"
  out="$(adv_tool burst 20 DELETE "/reservations/$RID" 2>&1)"
  eq "every concurrent cancel answers 200 (C-CAN-2)" "200=20" "$(adv_hist "$out")"
  eq "7 units are released exactly once (C-INV-3)" "100" "$(avail_of "$ONCE_ID")"
fi

# ---------------------------------------------------------------- mixed race
head_ "C-INV-1/C-INV-2: reserves racing cancels, checked by exact arithmetic"
new_item mix 30 || bad "could not create the item for the mixed race"
if [ -n "${ITEM_ID:-}" ]; then
  MIX_ID="$ITEM_ID"
  out="$(adv_tool seq 20 POST /reservations --body "{\"item_id\":$MIX_ID,\"quantity\":1}" 2>&1)"
  eq "20 sequential 1-unit reserves all succeed" "201=20" "$(adv_hist "$out")"
  eq "available is 10 afterwards" "10" "$(avail_of "$MIX_ID")"
  # The first reservation's id is in the tool's sample line; cancelling it 20
  # times concurrently must release exactly one unit, while 30 more reserves
  # compete for the remaining 10.
  SAMPLE_RID="$(jnum "$(adv_sample "$out")" id)"
  M_RESV="$(mktemp -t adv-resv.XXXXXX)"; M_CAN="$(mktemp -t adv-can.XXXXXX)"
  ( adv_tool burst 30 POST /reservations --body "{\"item_id\":$MIX_ID,\"quantity\":1}" >"$M_RESV" 2>&1 ) &
  RESV_PID=$!
  ( adv_tool burst 20 DELETE "/reservations/$SAMPLE_RID" >"$M_CAN" 2>&1 ) &
  CAN_PID=$!
  wait $RESV_PID $CAN_PID
  rout="$(cat "$M_RESV")"; cout="$(cat "$M_CAN")"
  rm -f "$M_RESV" "$M_CAN"
  k="$(printf '%s' "$rout" | sed -n 's/.*STATUS_HIST 201=\([0-9]*\).*/\1/p')"
  k="${k:-0}"
  canok="$(adv_hist "$cout")"
  eq "every concurrent cancel still answers 200" "200=20" "$canok"
  eq "the 409s are exactly the ones that lost the race" "$((30 - k))" \
     "$(printf '%s' "$rout" | sed -n 's/.*STATUS_HIST .*409=\([0-9]*\).*/\1/p' | head -1)"
  # 10 available, 1 unit released by the cancel storm, k units taken by the
  # reserves that won: available must be exactly 11 - k.
  want=$((11 - k))
  got="$(avail_of "$MIX_ID")"
  eq "available is exactly $want (C-INV-2)" "$want" "$got"
  if [ -n "$got" ] && [ "$got" -ge 0 ] 2>/dev/null && [ "$got" -le 30 ]; then
    ok "available stayed within 0..stock (C-INV-1): $got"
  else
    bad "available left 0..stock: got ${got:-none}"
  fi
  eq "no 5xx during the race (C-ERR-500)" "0" "$(adv_fivexx "$rout")"
fi

# ---------------------------------------------------------------- C-IDEM (M2)
head_ "C-IDEM-*: idempotency keys (Milestone 2)"
new_item idem 100 || bad "could not create the item for the idempotency checks"
if [ -n "${ITEM_ID:-}" ]; then
  IDEM_ID="$ITEM_ID"
  BODY="{\"item_id\":$IDEM_ID,\"quantity\":3}"
  out="$(adv_tool seq 3 POST /reservations -H 'Idempotency-Key: adv-seq-1' --body "$BODY" 2>&1)"
  if [ "$(adv_hist "$out")" = "201=3" ] && [ "$(adv_distinct "$out")" = "1" ]; then
    ok "3 replays of one key returned 1 distinct response (C-IDEM-2)"
    eq "and the units were taken exactly once (C-INV-4)" "97" "$(avail_of "$IDEM_ID")"
  else
    skip_section "no idempotency support yet (replays answered: $(adv_hist "$out"))" \
      C-IDEM-2 C-IDEM-3 C-IDEM-4 C-IDEM-5 C-IDEM-6 C-IDEM-7 C-INV-4
  fi

  if [ "$(adv_hist "$out")" = "201=3" ] && [ "$(adv_distinct "$out")" = "1" ]; then
    before="$(avail_of "$IDEM_ID")"
    out="$(adv_tool req POST /reservations -H 'Idempotency-Key: adv-seq-1' --body "{\"item_id\":$IDEM_ID,\"quantity\":4}" 2>&1)"
    eq "same key, different body -> 409 (C-IDEM-3)" "409" "$(adv_status "$out")"
    eq "and nothing changed" "$before" "$(avail_of "$IDEM_ID")"
    out="$(adv_tool req POST /reservations -H 'Idempotency-Key: adv-seq-1' --body "$BODY" 2>&1)"
    eq "the original response survives the 409 (C-IDEM-4)" "201" "$(adv_status "$out")"
    if [ -n "$RID" ]; then
      out="$(adv_tool req DELETE "/reservations/$RID" -H 'Idempotency-Key: adv-seq-1' 2>&1)"
      eq "same key, different path -> 409 (C-IDEM-3)" "409" "$(adv_status "$out")"
    else
      skip "same key, different path: no reservation id to hand (C-IDEM-3)"
    fi

    out="$(adv_tool burst 50 POST /reservations -H 'Idempotency-Key: adv-storm-1' --body "{\"item_id\":$IDEM_ID,\"quantity\":1}" 2>&1)"
    eq "50 simultaneous requests with one new key -> 1 distinct response (C-IDEM-6)" "1" "$(adv_distinct "$out")"
    eq "exactly one of them executed (C-INV-4)" "96" "$(avail_of "$IDEM_ID")"

    long="$(printf 'k%.0s' {1..255})"
    out="$(adv_tool req POST /reservations -H "Idempotency-Key: $long" --body "{\"item_id\":$IDEM_ID,\"quantity\":1}" 2>&1)"
    if [ "$(adv_status "$out")" = "201" ] || [ "$(adv_status "$out")" = "409" ]; then
      ok "255-byte key accepted (C-IDEM-5)"
    else
      bad "255-byte key -> $(adv_status "$out") (C-IDEM-5)"
    fi
    out="$(adv_tool req POST /reservations -H 'Idempotency-Key: adv-ok' -H 'Idempotency-Key: adv-dup' --body "{\"item_id\":$IDEM_ID,\"quantity\":1}" 2>&1)"
    eq "two Idempotency-Key headers -> 400 (C-IDEM-5)" "400" "$(adv_status "$out")"
    out="$(adv_tool req GET "/items/$IDEM_ID" -H 'Idempotency-Key: adv-get-1' 2>&1)"
    eq "the key is ignored on GET (C-IDEM-1)" "200" "$(adv_status "$out")"
  fi
fi

# ---------------------------------------------------------------- C-RUN-3
head_ "C-RUN-3: a fresh container starts from an empty database"
if new_item fresh 5; then
  FRESH_ID="$ITEM_ID"
  adv_down; adv_up
  adv_tool ready 10 >/dev/null 2>&1 || bad "not ready after restart (C-RUN-2)"
  out="$(adv_tool req GET "/items/$FRESH_ID" 2>&1)"
  eq "an item from the previous container life is gone (C-RUN-3)" "404" "$(adv_status "$out")"
  out="$(adv_tool req DELETE /reservations/1 2>&1)"
  eq "its reservations are gone too (C-CAN-3 on a fresh database)" "404" "$(adv_status "$out")"
  out="$(adv_tool seq 3 POST /items --body '{"name":"after-restart","stock":1}' 2>&1)"
  eq "creates work again after the restart" "201=3" "$(adv_hist "$out")"
fi

# ---------------------------------------------------------------- report
report_and_exit
