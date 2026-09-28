#!/usr/bin/env bash
# Adversary harness. Implements the contract's run and reach rules:
#   C-RUN-1  docker run --network=none --cpus=1 --memory=512m -p 8080:8080 <image>
#            plus only -d, --rm and --name (allowed by D-21).
#   C-RUN-4  every client joins the service's network namespace with
#            --network container:<name> and speaks to http://127.0.0.1:8080.
#   C-RUN-5  the adversary builds its own tag, never `practice`.
#
# Nothing here may change the run command's resources or network mode.

set -euo pipefail

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
adv_tool() {
  docker run --rm --network "container:$ADV_NAME" "$ADV_TOOL_TAG" "$@"
}
