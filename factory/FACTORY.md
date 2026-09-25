# The Factory

A five-seat software factory running in BAND Desktop across three model families. It turns a written task brief into a verbatim contract, builds against it, verifies independently, attacks what was accepted, and releases frozen, clean-built milestone snapshots. A human is involved only for genuine specification conflicts.

## Seats

| Seat | Runtime | Model | Workspace | Mandate |
|---|---|---|---|---|
| Planner | Claude Code | Claude | main clone | `mandates/planner.md` |
| Implementer | Claude Code | Claude | main clone | `mandates/implementer.md` |
| Verifier | OpenCode | GLM-5.2 via Featherless | own clone | `mandates/verifier.md` |
| Adversary | OpenCode | third model family via Featherless (exact id in `COST.md`) | own clone | `mandates/adversary.md` |
| Integrator | Claude Code | Claude | fresh temp clone per release | `mandates/integrator.md` |

## Key design decisions

**1. Independence by construction.** The verifier and the adversary run on model families different from the implementer's, in physically separate clones, and derive their tests from the contract without reading the implementer's reasoning. Agent teams most often accept broken work because author and checker share blind spots; separating model, workspace and information removes all three shared sources of error.

**2. Contract first.** Before any code, the planner copies every externally visible name, shape, error condition and invariant from the specification, verbatim, into `CONTRACT.md`. Every work item, test and attack cites contract ids. Graders test exact names and shapes; paraphrase is the cheapest way to lose points and this removes it.

**3. Every break becomes a permanent test.** The adversary attacks accepted work for concurrency, replay, malformed input and state violations. Each BREAK becomes a fix item whose break case the verifier adds to the acceptance suite, so the suite only ratchets up and later milestones cannot regress.

**4. The factory measures itself.** Every release reports first-pass acceptance rate, REJECTs, BREAKs, fix cycles, human questions and wall time in `METRICS.md`.

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

GitHub `origin/main` is the hub. Every seat pulls with rebase before starting, commits only the paths it owns, pushes immediately, and never force-pushes. Because ownership never overlaps, conflicts do not arise.

## Ownership: who may write where

| Path | Owner |
|---|---|
| `CONTRACT.md`, `DECISIONS.md` | Planner |
| Working service directory (named in the brief) | Implementer |
| Acceptance directory (`acceptance/` by default) | Verifier |
| `adversary/` | Adversary |
| Delivery folders (named in the brief), `METRICS.md` | Integrator (delivery folders write-once) |
| `factory/` | Human only |

## Room conventions

Every handoff message starts with its type tag and id and mentions the receiving seat: `WORK-ITEM`, `EVIDENCE`, `ACCEPT`, `REJECT`, `MILESTONE-CANDIDATE`, `BREAK`, `NO-BREAK`, `MILESTONE-COMPLETE`, `RELEASE`. The tags make the room export an audit log that the integrator can compute metrics from.

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

To be filled after the run.
