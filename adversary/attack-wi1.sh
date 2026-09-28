#!/usr/bin/env bash
# Adversary attack set against the WI-1 walking skeleton.
#
# Scope note: at WI-1 the only implemented route is GET /health, so the
# state-changing attack categories (reserve/cancel/idempotency) have no
# endpoint to attack yet. What is attackable now is everything the contract
# says about a *running* service: the run and reach rules, readiness, the
# health contract, routing errors and the never-500 / never-drop-a-connection
# guarantee -- which is exactly the surface most likely to rot by WI-5.
#
# Usage: adversary/attack-wi1.sh [--keep]
#   Re-runnable. Exits non-zero if any check fails.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# shellcheck source=lib.sh
source adversary/lib.sh

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

PASS=0; FAIL=0; SKIP=0
declare -a FAILURES=()
declare -a WATCHES=()

hex() { printf '%s' "$1" | od -An -tx1 -v | tr -d ' \n'; }

ok()   { PASS=$((PASS+1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); FAILURES+=("$1"); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP+1)); printf '  \033[33mSKIP\033[0m %s\n' "$1"; }
# A reproducible observation that the contract does not settle unambiguously.
# Noted, never counted as a break: the planner rules on it.
watch() { WATCHES+=("$1"); printf '  \033[33mWATCH\033[0m %s\n' "$1"; }
head_() { printf '\n\033[1m== %s\033[0m\n' "$1"; }

# check_status <label> <expected-status> <method> <path> [extra args...]
check_status() {
  local label="$1" want="$2" method="$3" path="$4"; shift 4
  local out; out="$(adv_tool req "$method" "$path" "$@" 2>&1)"
  local got; got="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
  local comp; comp="$(printf '%s' "$out" | sed -n 's/^COMPLETE=//p')"
  if [ "$comp" != "true" ]; then
    bad "$label (incomplete response: $(printf '%s' "$out" | sed -n 's/^NOTE=//p'))"; return
  fi
  if [ "$got" = "$want" ]; then ok "$label -> $got"; else bad "$label expected $want got ${got:-none}"; fi
}

# check_json_err <label> <expected-status> <method> <path> [extra args...]
# C-REP-ERR: exactly one field "error", non-empty; C-REP-HDR: JSON content type.
check_json_err() {
  local label="$1" want="$2" method="$3" path="$4"; shift 4
  local out; out="$(adv_tool req "$method" "$path" "$@" 2>&1)"
  local got ct body
  got="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
  ct="$(printf '%s' "$out" | sed -n 's/^HDR content-type: //p')"
  body="$(printf '%s' "$out" | sed -n 's/^BODY=//p')"
  if [ "$(printf '%s' "$out" | sed -n 's/^COMPLETE=//p')" != "true" ]; then
    bad "$label (incomplete response)"; return
  fi
  [ "$got" = "$want" ] || { bad "$label expected $want got ${got:-none}"; return; }
  case "$ct" in application/json*) ;; *) bad "$label content-type=$ct"; return ;; esac
  printf '%s' "$body" | grep -Eq '^\{"error":".+"\}$' \
    && ok "$label -> $got $body" \
    || bad "$label body not {\"error\":\"<non-empty>\"}: $body"
}

# check_raw_complete <label> <hex-payload>  -- C-ERR-500: a complete response,
# and never a 5xx, however malformed the request was.
check_raw_complete() {
  local label="$1" payload="$2"
  local out; out="$(adv_tool raw "$payload" 2>&1)"
  local st comp note
  st="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
  comp="$(printf '%s' "$out" | sed -n 's/^COMPLETE=//p')"
  note="$(printf '%s' "$out" | sed -n 's/^NOTE=//p')"
  if [ -n "$st" ] && [ "${st%%0}" != "" ] && [ "$st" -ge 500 ] 2>/dev/null; then
    bad "$label returned $st (C-ERR-500)"; return
  fi
  if [ "$comp" = "true" ]; then ok "$label -> ${st:-conn closed cleanly} ${note:+($note)}"
  else bad "$label no complete response: ${note:-dropped connection} (C-ERR-500)"; fi
}

# ---------------------------------------------------------------- harness
head_ "Harness: C-RUN-4 / C-RUN-5 reachability preconditions"
adv_build_tool
adv_require_images
adv_build_service
ok "service built offline with tag $ADV_TAG (C-RUN-5)"

adv_up
if docker port "$ADV_NAME" 2>&1 | grep -q .; then
  bad "docker port published something under --network=none (C-RUN-4 premise)"
else
  ok "docker port is empty: -p 8080:8080 publishes nothing (C-RUN-4 premise)"
fi

if curl -sS -m 5 -o /dev/null "http://127.0.0.1:8080/health" 2>/dev/null; then
  bad "host reached 127.0.0.1:8080; C-RUN-4 assumes it cannot"
else
  ok "host cannot reach 127.0.0.1:8080 (C-RUN-4 premise holds)"
fi

if docker run --rm "$ADV_CLIENT" -sS -m 5 -o /dev/null "http://127.0.0.1:8080/health" 2>/dev/null; then
  bad "a container in its own namespace reached the service"
else
  ok "a detached-namespace container cannot reach it either"
fi

res="$(docker inspect "$ADV_NAME" --format '{{.HostConfig.NetworkMode}} {{.HostConfig.NanoCpus}} {{.HostConfig.Memory}}')"
if [ "$res" = "none 1000000000 536870912" ]; then
  ok "run limits intact: NetworkMode=none NanoCpus=1 Memory=512m (C-RUN-1)"
else
  bad "run limits wrong: $res"
fi

# ---------------------------------------------------------------- C-RUN-2
head_ "C-RUN-2: /health answers at 127.0.0.1:8080 within 10s of container start"
for i in 1 2 3; do
  adv_down; adv_up
  if out="$(adv_tool ready 12 2>&1)"; then
    ok "cold start $i: ${out%%$'\n'*}"
  else
    bad "cold start $i: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done
adv_down; adv_up

# ---------------------------------------------------------------- C-OP-HEALTH
head_ "C-OP-HEALTH: GET /health -> 200 {\"status\": \"ok\"}"
out="$(adv_tool req GET /health 2>&1)"
st="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
body="$(printf '%s' "$out" | sed -n 's/^BODY=//p')"
[ "$st" = "200" ] && ok "status 200" || bad "status ${st:-none}"
[ "$body" = '{"status": "ok"}' ] && ok "body exactly {\"status\": \"ok\"}" || bad "body=$body"
printf '%s' "$out" | grep -q '^HDR content-type: application/json' \
  && ok "Content-Type: application/json (C-REP-HDR)" || bad "Content-Type wrong (C-REP-HDR)"

# ---------------------------------------------------------------- C-ERR-404
head_ "C-ERR-404: every non-route path is 404 with C-REP-ERR"
for p in / /items /items/1 /items/abc /reservations /reservations/1 \
         /health/ /health/extra /HEALTH /healthz /api/items //health /./health; do
  check_json_err "GET $p" 404 GET "$p"
done

# A query string does not change the path, so /health?x=1 is still the route.
check_status "GET /health?probe=1 (query is not the path)" 200 GET "/health?probe=1"

# ---------------------------------------------------------------- path handling
head_ "C-ERR-404: traversal, separators and control bytes are not routes"
for p in /items/../health /items/..%2fhealth /health/..%2f..%2fhealth /health/. \
         /health%2f /health%2F%2e /%2e/health /health%00 /health%20; do
  check_json_err "GET $p" 404 GET "$p"
done

head_ "WATCH: percent-encoded spellings of the /health route"
# C-ERR-404 says any path that is not a route is 404, and section 4 says routes
# are exact paths. A percent-encoded spelling is the same resource under
# RFC 3986 6.2.2, so both answers are defensible; report, do not fail.
for p in /%68ealth /he%61lth /%68ea%6ct%68 /health%3f /HEALTH; do
  out="$(adv_tool req GET "$p" 2>&1)"
  st="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
  case "$st" in
    200) watch "GET $p -> 200 (percent-decoded to the route /health)" ;;
    404) ok "GET $p -> 404" ;;
    *)   watch "GET $p -> ${st:-no status}: $(printf '%s' "$out" | sed -n 's/^NOTE=//p')" ;;
  esac
done

# ---------------------------------------------------------------- C-ERR-405
head_ "C-ERR-405: wrong method on a route is 405 + Allow + C-REP-ERR"
for m in POST PUT DELETE PATCH OPTIONS TRACE CONNECT; do
  check_json_err "$m /health" 405 "$m" /health
done
out="$(adv_tool req POST /health 2>&1)"
printf '%s' "$out" | grep -qi '^HDR allow: GET' \
  && ok "Allow header present on 405 (C-ERR-405): $(printf '%s' "$out" | sed -n 's/^HDR allow: //p')" \
  || bad "Allow header missing on 405 (C-ERR-405)"

# ---------------------------------------------------------------- HEAD
head_ "C-REP-HDR: HEAD /health is 200 with no body"
out="$(adv_tool req HEAD /health 2>&1)"
st="$(printf '%s' "$out" | sed -n 's/^STATUS=//p')"
bd="$(printf '%s' "$out" | sed -n 's/^BODY=//p')"
[ "$st" = "200" ] && ok "HEAD status 200" || bad "HEAD status ${st:-none} (got a complete response? $(printf '%s' "$out" | sed -n 's/^COMPLETE=//p'))"
[ -z "$bd" ] && ok "HEAD body empty" || bad "HEAD returned a body: $bd"

# ---------------------------------------------------------------- C-ERR-500 raw
head_ "C-ERR-500: malformed requests get a complete response, never 5xx"
CR=$'\r'
LF=$'\n'
TAB=$'\t'
NUL=$'\x00'
# hex() is printf '%s', which does not expand escapes: control bytes must be
# put in the argument with $'...' or the test would send the literal text.
check_raw_complete "bare LF request line"      "$(hex "GET /health HTTP/1.1${CR}")"
check_raw_complete "no Host header (HTTP/1.1)" "$(hex "GET /health HTTP/1.1${CR}${CR}")"
check_raw_complete "bad HTTP version"          "$(hex "GET /health HTTP/9.9${CR}Host: x${CR}${CR}")"
check_raw_complete "non-numeric status target" "$(hex "GET /health HTTP/one.two${CR}Host: x${CR}${CR}")"
check_raw_complete "negative Content-Length"   "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Content-Length: -5${CR}${CR}")"
check_raw_complete "non-numeric Content-Length" "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Content-Length: abc${CR}${CR}")"
check_raw_complete "header without colon"      "$(hex "GET /health HTTP/1.1${CR}Host: x${CR}BadHeader${CR}${CR}")"
check_raw_complete "space in header name"      "$(hex "GET /health HTTP/1.1${CR}Host: x${CR}Bad Header: v${CR}${CR}")"
check_raw_complete "absolute-form target"      "$(hex "GET http://127.0.0.1:8080/health HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "HTTP/1.0 no Host"          "$(hex "GET /health HTTP/1.0${CR}${CR}")"
check_raw_complete "garbage bytes as a line"   "$(hex $'\x01\x02\x03\x04\x05')"
check_raw_complete "Content-Length + chunked"  "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Content-Length: 3${CR}Transfer-Encoding: chunked${CR}${CR}0${CR}${CR}")"
check_raw_complete "Expect: 100-continue"       "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Expect: 100-continue${CR}Content-Length: 4${CR}${CR}abcd")"
check_raw_complete "duplicate Host headers"    "$(hex "GET /health HTTP/1.1${CR}Host: a${CR}Host: b${CR}${CR}")"
check_raw_complete "NUL in header value"       "$(hex "GET /health HTTP/1.1${CR}Host: x${CR}X-Bad: a${NUL}b${CR}${CR}")"
check_raw_complete "NUL in request target"     "$(hex "GET /health${NUL}x HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "LF-only, no CR at all"     "$(hex "GET /health HTTP/1.1${LF}Host: x${LF}${LF}")"

head_ "C-ERR-500: header block past MaxHeaderBytes (1 MiB)"
for n in 1048577 8388608; do
  if out="$(adv_tool bigheader "$n" 2>&1)"; then
    ok "headers of ${n} bytes -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
  else
    bad "headers of ${n} bytes: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done

# ---------------------------------------------------------------- C-ERR-500 body size
head_ "C-ERR-500 / C-ERR-400: oversized bodies still get a complete response"
for n in 1048577 8388608 67108864; do
  if out="$(adv_tool bigbody "$n" POST /health 2>&1)"; then
    ok "POST /health with ${n}-byte body -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
  else
    bad "POST /health with ${n}-byte body: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done
if out="$(adv_tool bigbody 4194304 GET /health 2>&1)"; then
  ok "GET /health with 4 MiB body -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
else
  bad "GET /health with 4 MiB body: $(printf '%s' "$out" | tr '\n' ' ')"
fi

# The service drains an unread body up to a cap before closing the connection.
# A body larger than that cap is the interesting boundary: the response is
# written first, but the connection closes with data still in flight, so the
# client can lose it (C-ERR-500 / C-ERR-400 need a complete response anyway).
head_ "C-ERR-500: bodies past the 64 MiB drain cap (67108864 = 64 MiB)"
for n in 67108865 100663296 134217728; do
  if out="$(adv_tool bigbody "$n" POST /nope 2>&1)"; then
    ok "POST /nope with ${n}-byte body -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
  else
    bad "POST /nope with ${n}-byte body: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done

# ---------------------------------------------------------------- C-ERR-500 parser edges
head_ "C-ERR-500: request-parser edges still get a complete response"
check_raw_complete "duplicate Content-Length (3 and 4)"   "$(hex "POST /nope HTTP/1.1${CR}Host: x${CR}Content-Length: 3${CR}Content-Length: 4${CR}${CR}abc")"
check_raw_complete "Content-Length int64 overflow"        "$(hex "POST /nope HTTP/1.1${CR}Host: x${CR}Content-Length: 99999999999999999999${CR}${CR}")"
check_raw_complete "obs-fold continuation line"           "$(hex "GET /health HTTP/1.1${CR}Host: x${CR}X-Fold: a${CR} b${CR}${CR}")"
check_raw_complete "double space in request line"         "$(hex "GET  /health  HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "tab in request line"                  "$(hex "GET${CR}${TAB}/health HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "bare CR inside header value"          "$(hex "GET /health HTTP/1.1${CR}Host: x${CR}X-Bad: a${CR}b${CR}${CR}")"
check_raw_complete "OPTIONS * (general handler disabled)" "$(hex "OPTIONS * HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "lowercase method"                     "$(hex "get /health HTTP/1.1${CR}Host: x${CR}${CR}")"
check_raw_complete "Transfer-Encoding: gzip"              "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Transfer-Encoding: gzip${CR}${CR}")"
check_raw_complete "bad chunk size"                       "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Transfer-Encoding: chunked${CR}${CR}zz${CR}${LF}${CR}")"
check_raw_complete "chunked with uppercase hex size"      "$(hex "POST /health HTTP/1.1${CR}Host: x${CR}Transfer-Encoding: chunked${CR}${CR}A${CR}bcdefghij${CR}0${CR}${CR}")"

head_ "C-ERR-500: request line far past any header cap"
for n in 65536 1048576 16777216; do
  if out="$(adv_tool bigline "$n" 2>&1)"; then
    ok "${n}-byte request line -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
  else
    bad "${n}-byte request line: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done

head_ "C-ERR-500: client stops sending mid-body"
for spec in "POST /health 1000000" "POST /nope 1000000" "POST /nope 4"; do
  # shellcheck disable=SC2086
  set -- $spec
  if out="$(adv_tool abort "$1" "$2" "$3" 2>&1)"; then
    ok "$1 $2 (declared $3, half sent) -> $(printf '%s' "$out" | sed -n 's/^STATUS=//p') complete"
  else
    bad "$1 $2 declared $3: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done

# ---------------------------------------------------------------- concurrency
head_ "C-ERR-500 under simultaneity (S-11 shape, 50 at once)"
for n in 50 200; do
  if out="$(adv_tool burst "$n" GET /health 2>&1)"; then
    ok "burst $n GET /health: $(printf '%s' "$out" | sed -n 's/^STATUS_HIST //p')"
  else
    bad "burst $n GET /health: $(printf '%s' "$out" | tr '\n' ' ')"
  fi
done
if out="$(adv_tool burst 50 POST /health 2>&1)"; then
  ok "burst 50 POST /health: $(printf '%s' "$out" | sed -n 's/^STATUS_HIST //p')"
else
  bad "burst 50 POST /health: $(printf '%s' "$out" | tr '\n' ' ')"
fi

# ---------------------------------------------------------------- sustained load
head_ "C-ERR-500 under sustained load: 15s, mixed routes and methods"
if out="$(adv_tool load 15 16 /health GET 2>&1)"; then
  ok "load /health: $(printf '%s' "$out" | sed -n 's/^STATUS_HIST //p') total=$(printf '%s' "$out" | sed -n 's/.*total=//p')"
else
  bad "load /health: $(printf '%s' "$out" | tr '\n' ' ')"
fi
if out="$(adv_tool load 10 16 /nope GET 2>&1)"; then
  ok "load /nope: $(printf '%s' "$out" | sed -n 's/^STATUS_HIST //p')"
else
  bad "load /nope: $(printf '%s' "$out" | tr '\n' ' ')"
fi

# ---------------------------------------------------------------- state
head_ "C-RUN-3 / C-INV-1 / C-INV-2: not observable at WI-1"
skip "C-RUN-3 fresh database: no endpoint stores anything yet"
skip "C-INV-1/C-INV-2 stock arithmetic: no item or reservation endpoint yet"
skip "C-IDEM-*: Milestone 2"

# ---------------------------------------------------------------- report
if [ "$KEEP" = "0" ]; then adv_down; fi
printf '\n\033[1m== Result\033[0m  pass=%d fail=%d skip=%d watch=%d\n' "$PASS" "$FAIL" "$SKIP" "${#WATCHES[@]}"
if [ "${#WATCHES[@]}" -gt 0 ]; then
  printf '\nObservations for the planner (not breaks):\n'
  for w in "${WATCHES[@]}"; do printf '  - %s\n' "$w"; done
fi
if [ "$FAIL" -gt 0 ]; then
  printf '\nFailed checks:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
exit 0
