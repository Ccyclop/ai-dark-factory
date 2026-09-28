#!/usr/bin/env bash
# Adversary harness. Implements the contract's run and reach rules:
#   C-RUN-1  docker run --network=none --cpus=1 --memory=512m -p 8080:8080 <image>
#            plus only -d, --rm and --name (allowed by D-21).
#   C-RUN-4  every client joins the service's network namespace with
#            --network container:<name> and speaks to http://127.0.0.1:8080.
#   C-RUN-5  the adversary builds its own tag, never `practice`.
#
# Nothing here may change the run command's resources or network mode.
#
# Deliberately no `set -e`: this file is sourced, and a campaign that aborts on
# the first non-zero exit produces a silently truncated report, which is worse
# than a failed check. Every check must run, report and be counted. A docker
# daemon hiccup must never be reported as a contract break either, which is what
# adv_tool's retry is for.

set -uo pipefail

ADV_TAG="${ADV_TAG:-practice-adversary}"     # C-RUN-5: never `practice`
ADV_TOOL_TAG="${ADV_TOOL_TAG:-adversary-tool:local}"
ADV_NAME="${ADV_NAME:-adv-svc}"
ADV_CLIENT="${ADV_CLIENT:-curlimages/curl:8.11.1}"   # cached locally, D-23
ADV_URL="${ADV_URL:-http://127.0.0.1:8080}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVICE_DIR="$REPO_ROOT/practice/service"

log() { printf '[adv] %s\n' "$*" >&2; }
die() { printf '[adv] FATAL: %s\n' "$*" >&2; exit 1; }

# Preconditions: every image used must already be in the local cache, because
# C-RUN-4's client has no network either.
adv_require_images() {
  local img
  for img in "$ADV_CLIENT" "$ADV_TOOL_TAG"; do
    docker image inspect "$img" >/dev/null 2>&1 \
      || die "image not in local cache: $img (no network to pull it)"
  done
}

# C-BUILD-2/C-BUILD-3 with C-RUN-5's own tag.
adv_build_service() {
  log "docker build --network=none -t $ADV_TAG .  (in practice/service)"
  ( cd "$SERVICE_DIR" && docker build --network=none -t "$ADV_TAG" . ) >&2
}

adv_build_tool() {
  [ -f "$REPO_ROOT/adversary/tool/Dockerfile" ] || return 0
  log "building attack tool image $ADV_TOOL_TAG"
  ( cd "$REPO_ROOT/adversary/tool" && docker build --network=none -t "$ADV_TOOL_TAG" . ) >&2
}

# Exactly the C-RUN-1 command. Only -d, --rm and --name are added.
adv_run_command() {
  printf 'docker run -d --rm --name %s --network=none --cpus=1 --memory=512m -p 8080:8080 %s' \
    "$ADV_NAME" "$ADV_TAG"
}

adv_up() {
  adv_down || true
  log "$(adv_run_command)"
  docker run -d --rm --name "$ADV_NAME" \
    --network=none --cpus=1 --memory=512m -p 8080:8080 "$ADV_TAG" >/dev/null
}

adv_down() { docker rm -f "$ADV_NAME" >/dev/null 2>&1 || true; }

# adv_run <tool args...>: runs the tool, always returns 0, leaves the output in
# ADV_OUT and the tool's exit code in ADV_RC. Use where a check must be able to
# report a client-side failure as a check failure instead of losing the output.
adv_run() {
  ADV_OUT="$(adv_tool "$@" 2>&1)"
  ADV_RC=$?
  return 0
}

# --- reading the tool's output ----------------------------------------------
# The tool prints one KEY=value pair per line. These read one field back without
# re-parsing, so a campaign script stays a list of attacks.

adv_status()   { printf '%s' "$1" | sed -n 's/^STATUS=//p'; }
adv_body()     { printf '%s' "$1" | sed -n 's/^BODY=//p'; }
adv_complete() { printf '%s' "$1" | sed -n 's/^COMPLETE=//p'; }
adv_note()     { printf '%s' "$1" | sed -n 's/^NOTE=//p'; }
adv_hist()     { printf '%s' "$1" | sed -n 's/^STATUS_HIST //p'; }
adv_distinct() { printf '%s' "$1" | sed -n 's/^DISTINCT_RESPONSES=//p'; }
adv_sample()   { printf '%s' "$1" | sed -n 's/^SAMPLE=//p'; }
adv_incomplete() { printf '%s' "$1" | sed -n 's/^INCOMPLETE=//p'; }
adv_fivexx()   { printf '%s' "$1" | sed -n 's/^FIVE_XX=//p'; }
adv_elapsed()  { printf '%s' "$1" | sed -n 's/.*elapsed_ms=//p'; }
# adv_hdr <output> <name> -> header value, matched case-insensitively.
adv_hdr() {
  printf '%s' "$1" | sed -n 's/^HDR //p' | grep -i "^$2: " | head -1 | sed 's/^[^:]*: //'
}

# Compact-JSON field readers. Every contracted body is a single flat object with
# uniquely named fields, so one greedy match per field is unambiguous. Numbers
# come back as integers, strings without their quotes.
jnum() { printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\(-*[0-9][0-9]*\).*/\1/p"; }
jstr() { printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"; }

# C-RUN-2: /health must answer at 127.0.0.1:8080 inside the namespace within 10s.
adv_wait_ready() {
  local budget="${1:-10}" waited=0
  while [ "$waited" -lt "$budget" ]; do
    if adv_curl -fsS --max-time 2 "$ADV_URL/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2; waited=$((waited + 1))
  done
  return 1
}

# C-RUN-4: a single curl, inside the service's network namespace.
adv_curl() {
  docker run --rm --network "container:$ADV_NAME" "$ADV_CLIENT" "$@"
}

# C-RUN-4: the compiled attack tool, inside the service's network namespace.
#
# Exit codes 125..127 come from docker itself, never from the tool (which exits
# 0, 1 or 2), and they mean the client container never ran. That is a fact about
# the daemon, not about the service, so it is retried once instead of being
# reported as a breach: an adversary that blames the service for its own tooling
# loses the room's trust and the implementer's time.
adv_tool() {
  local out rc
  out="$(docker run --rm --network "container:$ADV_NAME" "$ADV_TOOL_TAG" "$@" 2>&1)"; rc=$?
  if [ "$rc" -ge 125 ] && ! printf '%s' "$out" | grep -q '^STATUS='; then
    log "client container failed to start (docker exit $rc), retrying once"
    out="$(docker run --rm --network "container:$ADV_NAME" "$ADV_TOOL_TAG" "$@" 2>&1)"; rc=$?
  fi
  printf '%s\n' "$out"
  return $rc
}
