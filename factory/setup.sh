#!/usr/bin/env bash
# Stand up the factory's local workspaces and check prerequisites.
# Run from anywhere inside the repository: ./factory/setup.sh
set -uo pipefail

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
for t in git docker claude opencode; do
  if command -v "$t" >/dev/null 2>&1; then ok "$t  ($(command -v "$t"))"; else bad "$t not found on PATH"; fi
done
if command -v band >/dev/null 2>&1; then ok "band CLI"; else warn "band CLI not on PATH — install or repair it from BAND Desktop"; fi
if docker info >/dev/null 2>&1; then ok "Docker daemon running"; else bad "Docker daemon not running — start Docker Desktop"; fi
if [ -n "${FEATHERLESS_API_KEY:-}" ]; then ok "FEATHERLESS_API_KEY set"; else warn "FEATHERLESS_API_KEY not set — needed only if OpenCode seats use Featherless via env"; fi

echo "Remote"
if git ls-remote --exit-code --heads origin main >/dev/null 2>&1; then
  ok "origin/main exists"
else
  bad "origin/main missing — run: git branch -M main && git push -u origin main"
fi

echo "Secrets check (tracked files)"
if git ls-files -z | xargs -0 grep -nIE '(fw-[A-Za-z0-9]{16,}|sk-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16})' 2>/dev/null; then
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
      if git clone --quiet "$ORIGIN" "$dir"; then ok "$dir  (cloned)"; else bad "$dir  (clone failed)"; fi
    fi
  done
else
  warn "skipped until the problems above are fixed"
fi

cat <<TABLE

Create these agents in BAND Desktop (Agents → Create an agent):

  agent        runtime       working directory
  planner      Claude Code   $ROOT
  implementer  Claude Code   $ROOT
  verifier     OpenCode      $PARENT/$NAME-verify
  adversary    OpenCode      $PARENT/$NAME-adversary
  integrator   Claude Code   $ROOT

Standing instruction for each: the contents of factory/mandates/<agent>.md
TABLE

if [ "$FAIL" -ne 0 ]; then echo; echo "Setup incomplete — fix the ✘ items and run again."; fi
exit "$FAIL"
