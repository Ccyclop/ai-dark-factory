#!/usr/bin/env bash
# Stand up the factory's local workspaces and check prerequisites.
# Run from anywhere inside the repository: ./factory/setup.sh
# Optional overrides: VERIFIER_MODEL=... ADVERSARY_MODEL=... ./factory/setup.sh
set -uo pipefail

VERIFIER_MODEL="${VERIFIER_MODEL:-featherless/moonshotai/Kimi-K2.7-Code}"
ADVERSARY_MODEL="${ADVERSARY_MODEL:-featherless/zai-org/GLM-5.2}"

ok()   { printf '  \033[32m✔\033[0m %s\n' "$1"; }
bad()  { printf '  \033[31m✘\033[0m %s\n' "$1"; FAIL=1; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
FAIL=0

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || { echo "Run this inside the git repository."; exit 1; }
cd "$ROOT"
NAME="$(basename "$ROOT")"
PARENT="$(dirname "$ROOT")"
ORIGIN="$(git remote get-url origin 2>/dev/null)" || { echo "No 'origin' remote. Create and push the repository first."; exit 1; }

echo "Tools"
for t in git docker claude opencode python3; do
  if command -v "$t" >/dev/null 2>&1; then ok "$t  ($(command -v "$t"))"; else bad "$t not found on PATH"; fi
done
if command -v band >/dev/null 2>&1; then ok "band CLI"; else warn "band CLI not on PATH — install or repair it from BAND Desktop"; fi
if docker info >/dev/null 2>&1; then ok "Docker daemon running"; else bad "Docker daemon not running — start Docker Desktop"; fi

echo "Claude Code seats"
if python3 -c 'import json,os,sys; d=json.load(open(os.path.expanduser("~/.claude/settings.json"))); sys.exit(0 if d.get("disableClaudeAiConnectors") is True else 1)' 2>/dev/null; then
  ok "claude.ai connectors disabled at user level"
else
  warn "claude.ai connectors not disabled in ~/.claude/settings.json — seats would inherit them (see SETUP.md)"
fi

echo "OpenCode seats"
OC_CFG="$HOME/.config/opencode/opencode.json"
if python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if "featherless" in d.get("provider",{}) else 1)' "$OC_CFG" 2>/dev/null; then
  ok "global OpenCode config has the featherless provider"
else
  bad "global OpenCode config missing or without provider — run: FEATHERLESS_API_KEY=... ./factory/opencode-config.sh"
fi
if [ "$(uname)" = "Darwin" ]; then
  if [ "$(launchctl getenv OPENCODE_CONFIG 2>/dev/null)" = "$OC_CFG" ]; then ok "OPENCODE_CONFIG visible to GUI apps"
  else warn "OPENCODE_CONFIG not exported to GUI apps (resets on reboot) — run ./factory/opencode-config.sh"; fi
fi

echo "Remote"
if git ls-remote --exit-code --heads origin main >/dev/null 2>&1; then
  ok "origin/main exists"
else
  bad "origin/main missing — run: git branch -M main && git push -u origin main"
fi

echo "Secrets check (tracked files)"
if git ls-files -z | xargs -0 grep -nIE '(rc_[A-Za-z0-9]{16,}|fw-[A-Za-z0-9]{16,}|sk-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16})' 2>/dev/null; then
  bad "possible credential found in tracked files (see above)"
else
  ok "no obvious credentials in tracked files"
fi

echo "Independent clones"
if [ "$FAIL" -eq 0 ]; then
  for role in verify adversary; do
    dir="$PARENT/$NAME-$role"
    if [ -d "$dir/.git" ]; then
      if git -C "$dir" pull --rebase --quiet origin main; then ok "$dir  (updated)"; else bad "$dir  (pull failed)"; fi
    else
      if git clone --quiet "$ORIGIN" "$dir"; then ok "$dir  (cloned)"; else bad "$dir  (clone failed)"; continue; fi
    fi
    model="$VERIFIER_MODEL"; [ "$role" = "adversary" ] && model="$ADVERSARY_MODEL"
    printf '{\n  "$schema": "https://opencode.ai/config.json",\n  "model": "%s"\n}\n' "$model" > "$dir/opencode.json"
    if git -C "$dir" status --porcelain -- opencode.json | grep -q .; then
      bad "$dir/opencode.json is not git-ignored"
    else
      ok "$dir/opencode.json → $model (git-ignored)"
    fi
  done
else
  warn "skipped until the problems above are fixed"
fi

cat <<TABLE

Create these agents in BAND Desktop (Agents → Create your own; no tags needed):

  agent        runtime                         working directory                     model / effort
  planner      Claude Code                     $ROOT        opus[1m] / xhigh
  implementer  Claude Code                     $ROOT        opus[1m] / high
  integrator   Claude Code                     $ROOT        sonnet / medium
  verifier     ACP agent: $(command -v opencode) acp   $PARENT/$NAME-verify      from opencode.json
  adversary    ACP agent: $(command -v opencode) acp   $PARENT/$NAME-adversary   from opencode.json

Role file for each: $ROOT/factory/mandates/<agent>.md
Full field-by-field settings: factory/SETUP.md
TABLE

if [ "$FAIL" -ne 0 ]; then echo; echo "Setup incomplete — fix the ✘ items and run again."; fi
exit "$FAIL"
