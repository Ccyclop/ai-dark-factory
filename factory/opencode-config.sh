#!/usr/bin/env bash
# Writes the global OpenCode config (Featherless provider + unattended permission guardrails)
# to ~/.config/opencode/opencode.json — outside the repository — and, on macOS, exposes its
# path to GUI-launched apps such as BAND Desktop.
# Requires FEATHERLESS_API_KEY in the environment. The key is never written inside the repository.
set -euo pipefail

python3 - << 'PYEOF'
import json, os, sys
key = os.environ.get("FEATHERLESS_API_KEY", "")
if not key:
    sys.exit("FEATHERLESS_API_KEY is not set in this shell.")
cfg = {
  "$schema": "https://opencode.ai/config.json",
  "provider": {"featherless": {
    "npm": "@ai-sdk/openai-compatible",
    "name": "Featherless",
    "options": {"baseURL": "https://api.featherless.ai/v1", "apiKey": key},
    "models": {
      "zai-org/GLM-5.2": {"name": "GLM 5.2"},
      "moonshotai/Kimi-K2.7-Code": {"name": "Kimi K2.7 Code"},
      "deepseek-ai/DeepSeek-V4-Flash": {"name": "DeepSeek V4 Flash"}}}},
  "permission": {
    "edit": "allow",
    "bash": {"*": "allow", "sudo *": "deny",
             "git push --force*": "deny", "git push -f*": "deny",
             "git push * --force*": "deny", "git push * -f*": "deny"},
    "external_directory": {"*": "deny",
             "/tmp/**": "allow", "/private/tmp/**": "allow",
             "/var/folders/**": "allow", "/private/var/folders/**": "allow"},
    "doom_loop": "deny"}
}
p = os.path.expanduser("~/.config/opencode/opencode.json")
os.makedirs(os.path.dirname(p), exist_ok=True)
with open(p, "w") as f:
    json.dump(cfg, f, indent=2)
os.chmod(p, 0o600)
print("wrote " + p)
PYEOF

if [ "$(uname)" = "Darwin" ]; then
  launchctl setenv OPENCODE_CONFIG "$HOME/.config/opencode/opencode.json"
  echo "OPENCODE_CONFIG exported to GUI apps (lasts until reboot)."
  echo "Now quit BAND Desktop completely and run: pkill -x jamd   — then reopen BAND."
fi
