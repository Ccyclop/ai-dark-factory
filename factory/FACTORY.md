# The Factory

A five-seat software factory running in BAND Desktop across three model families. It turns a written task brief into a verbatim contract, builds against it, verifies independently, attacks what was accepted, and releases frozen, clean-built milestone snapshots. A human is involved only for genuine specification conflicts.

## Seats

| Seat | Runtime | Model | Reasoning | Workspace | Mandate |
|---|---|---|---|---|---|
| Planner | Claude Code | Claude Opus 5.5, 1M context | xhigh | main clone | `mandates/planner.md` |
| Implementer | Claude Code | Claude Opus 5.5, 1M context | high | main clone | `mandates/implementer.md` |
| Verifier | OpenCode via ACP | Kimi K2.7 Code (Moonshot) via Featherless | provider default | own clone | `mandates/verifier.md` |
| Adversary | OpenCode via ACP | GLM-5.2 (Z.ai) via Featherless | provider default | own clone | `mandates/adversary.md` |
| Integrator | Claude Code | Claude Sonnet 5 | medium | fresh temp clone per release | `mandates/integrator.md` |

The seat that runs most often (the verifier, once per work item) sits on the cheapest capable model; the seat that runs least often (the adversary, once per milestone) sits on the more expensive open model.

## Key design decisions

**1. Independent verification.** The verifier and the adversary run on model families different from the implementer's and from each other's, each in its own clone, and derive their tests from the contract rather than from the implementer's reasoning. Agent teams most often accept broken work because author and checker share blind spots; separating model, workspace and information attacks all three.

**2. Contract first.** Before any code, the planner copies every externally visible name, shape, error condition and invariant from the specification, verbatim, into `CONTRACT.md`. Every work item, test and attack cites contract ids. Graders test exact names and shapes; paraphrase is the cheapest way to lose points and this removes it.

**3. Every break becomes a permanent test.** The adversary attacks accepted work for concurrency, replay, malformed input and state violations. Each BREAK becomes a fix item whose break case the verifier adds to the acceptance suite, so the suite only ratchets up and later milestones cannot regress.

**4. The factory measures itself.** Every release reports first-pass acceptance rate, REJECTs, BREAKs, fix cycles, human questions and wall time in `METRICS.md`; cost per seat is logged in `COST.md`.

**5. Guardrails in the runtime, not only in the prompt.** Force-push and `sudo` are denied by the OpenCode permission config, not just forbidden in the mandates; Claude seats run with no inherited connectors, hooks or skills beyond what the room needs.

## Flow

```
brief ─► Planner: CONTRACT.md ─► WORK-ITEM ─► Implementer ─► EVIDENCE ─► Verifier
                                    ▲              ▲                        │
                                    │              └───────── REJECT ───────┤
                                    │                                       │
                                    └──────────────── ACCEPT ───────────────┘

all items ACCEPT ─► MILESTONE-CANDIDATE ─► Adversary
      BREAK    ─► fix WORK-ITEM (break case = acceptance criterion) ─► loop
      NO-BREAK ─► MILESTONE-COMPLETE ─► Integrator ─► RELEASE PASS
               ─► frozen snapshot + METRICS.md ─► next milestone
```

## Repository sync

GitHub `origin/main` is the hub. Every seat pulls with rebase before starting, commits only the paths it owns, pushes immediately, never force-pushes, and ends each commit message with a `Seat: <name>` trailer, so `git log` shows which seat produced every change.

## Ownership: who may write where

| Path | Owner |
|---|---|
| `CONTRACT.md`, `DECISIONS.md` | Planner |
| Working service directory (named in the brief) | Implementer |
| Acceptance directory (`acceptance/` by default) | Verifier |
| `adversary/` | Adversary |
| Delivery folders (named in the brief), `METRICS.md` | Integrator (delivery folders write-once) |
| `factory/`, `.claude/` | Human only |
| `/opencode.json` in each seat clone | Local, git-ignored; holds only that seat's model id |

## Room conventions

Every handoff message starts with its type tag and id and mentions the receiving seat: `WORK-ITEM`, `EVIDENCE`, `ACCEPT`, `REJECT`, `MILESTONE-CANDIDATE`, `BREAK`, `NO-BREAK`, `MILESTONE-COMPLETE`, `RELEASE`. Seats also keep the room's work board current. The tags make the room export an audit log that the integrator computes metrics from.

## Gates

1. No item is done without a verifier ACCEPT on a named, pushed commit.
2. No milestone is complete without an adversary NO-BREAK on its final commit.
3. No milestone is released without a fresh-clone build under the brief's exact constraints and a full regression run.
4. Delivery snapshots are write-once.

## Escalation

- Three REJECTs on one item: the planner re-scopes it and logs the reason in `DECISIONS.md`.
- Contradiction in the specification or an irreversible choice: the planner asks the human one question with a proposed default and proceeds with the default if unanswered.

## Standing this factory up for a different task

See `SETUP.md`. The mandates refer to no particular product; only the task brief changes between tasks.

## Results

To be filled after the run from `METRICS.md` and `COST.md`.

## Limitations

1. **Filesystem boundaries between seats are advisory.** OpenCode's `external_directory` rule blocks file tools but not shell commands: during setup, both OpenCode seats could list a sibling seat's clone with `ls`. Independence therefore rests on separate model families, separate clones and the mandates. Full isolation would need one Docker sandbox per seat.
2. **OpenCode seats run through ACP.** BAND Desktop's native OpenCode runtime intermittently failed to offer a custom OpenAI-compatible provider at startup (the model check raced the provider load), so the verifier and adversary run as ACP agents (`opencode acp`), each selecting its model through a git-ignored `opencode.json` in its own clone. That file also loads the seat's mandate through OpenCode's own `instructions` setting, because we could not confirm that ACP agents receive BAND's role text: after a mandate update, the Claude Code seats quoted the new section verbatim while the ACP seat reported not having it.
3. **Claude seats inherit the operator's Claude Code login.** claude.ai connectors are disabled at user level because BAND's runtime probe starts outside the repository, where the project-level setting does not apply.
4. To be completed after the run.
