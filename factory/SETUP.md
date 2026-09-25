# Standing up the factory

About 20 minutes on a machine that already has the prerequisites.

## Prerequisites

- macOS or Linux, Docker running, git with a GitHub remote named `origin`, python3
- BAND Desktop, signed in, readiness checks passing
- Claude Code, signed in (a Max plan comfortably carries three Claude seats)
- OpenCode (`npm i -g @opencode/cli` or `opencode-ai`)
- An OpenAI-compatible provider key for the two open-model seats (this factory used Featherless) exported as `FEATHERLESS_API_KEY`

## 1. Claude Code: no inherited connectors

Seats would otherwise inherit every claude.ai connector on your account (mail, drive, calendar …). BAND's runtime probe starts outside the repository, so the repository's own `.claude/settings.json` is not enough; set it at user level for the duration of the run:

```bash
[ -f ~/.claude/settings.json ] && cp ~/.claude/settings.json ~/.claude/settings.json.bak
python3 -c 'import json,os;p=os.path.expanduser("~/.claude/settings.json");d=json.load(open(p)) if os.path.exists(p) else {};d["disableClaudeAiConnectors"]=True;json.dump(d,open(p,"w"),indent=2)'
```
Restore afterwards from the `.bak` file.

## 2. OpenCode: provider and guardrails, outside the repository

```bash
FEATHERLESS_API_KEY=... ./factory/opencode-config.sh
```
It writes `~/.config/opencode/opencode.json` (provider, models, and permissions: edits and shell allowed, `sudo` and force-push denied, paths outside the workspace denied except temp) and, on macOS, exports `OPENCODE_CONFIG` to GUI apps. Then quit BAND Desktop completely, run `pkill -x jamd`, and reopen BAND. Repeat after every reboot.

## 3. Workspaces

```bash
./factory/setup.sh
```
It checks everything above, creates or updates two independent sibling clones (`<repo>-verify`, `<repo>-adversary`), writes each clone's git-ignored `opencode.json` with that seat's model and its mandate as OpenCode `instructions`, and prints the agent table with your absolute paths. Override models with `VERIFIER_MODEL=… ADVERSARY_MODEL=…`.

## 4. Create five agents in BAND Desktop

Agents → Create your own. Leave tags empty. Role: **Choose role file** → `factory/mandates/<agent>.md`.

### Claude Code seats

| Field | planner | implementer | integrator |
|---|---|---|---|
| Working directory | repository | repository | repository |
| Model | `opus[1m]` | `opus[1m]` | `sonnet` |
| Reasoning effort | xhigh | high | medium |
| Permission mode | Auto | Auto | Auto |
| Command / Arguments | `claude` / empty | `claude` / empty | `claude` / empty |
| Session name | `planner` | `implementer` | `integrator` |
| Claude customizations | Use my Claude setup | Use my Claude setup | Use my Claude setup |
| Environment allowlist | `PATH,HOME,SSH_AUTH_SOCK` | same | same |

In **Test runtime → Details** expect exactly one MCP server (`jam`) and the intended model. "Minimal — credentials only" does not work with a subscription login: it runs Claude Code in bare mode, which reads only API keys.

### OpenCode seats (ACP agent tab)

| Field | verifier | adversary |
|---|---|---|
| Starting point | Custom command | Custom command |
| Command | absolute path of `opencode` | absolute path of `opencode` |
| Arguments | `acp` | `acp` |
| Working directory | `<repo>-verify` | `<repo>-adversary` |
| Approval policy | Allow automatically | Allow automatically |
| Session name | `verifier` | `adversary` |
| Environment allowlist | `PATH,HOME,SSH_AUTH_SOCK,OPENCODE_CONFIG` | same |

The model and the mandate both come from each clone's `opencode.json`; still choose the role file in BAND so the agent's profile matches. BAND's native OpenCode tab was not used; see Limitations in `FACTORY.md`.

To confirm a seat really runs under its mandate, ask it something only the mandate answers, without reading files — for example the first line of its report format.

### Check each seat

Assign work to each agent:
```
Run `pwd`, `git remote -v`, `git branch --show-current` and `docker info --format '{{.ServerVersion}}'`, then reply with the exact output. Do not change anything.
```
Expect its own workspace, your remote, `main`, and a Docker version.

## 5. Team, room, brief

Group the five agents into one team, open a room with it, and paste a task brief that names:

- **Specification:** the documents to build against
- **Working directory:** where the implementer writes the service
- **Acceptance directory:** optional, defaults to `acceptance/`
- **Delivery folders:** one per milestone
- **Milestones:** what each one covers
- **Build and run constraints:** exact commands, resource limits, network rules
- **Validation tool:** optional repository checker to run before release
- **Done:** the observable end state

Address the brief to the planner. See `examples/practice-brief.md` for a complete example.

## 6. Watch; answer only what is asked

The planner asks you only about genuine contradictions, each time with a proposed default.
