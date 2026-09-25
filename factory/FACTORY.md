# The Factory

A four-seat software factory running in BAND Desktop. It takes a written task brief, plans it, builds it, verifies it independently and releases frozen, clean-built milestone snapshots. A human is involved only for genuine specification conflicts.

## Seats

| Seat | Runtime | Model | Mandate |
|---|---|---|---|
| Planner | Claude Code | Claude | `mandates/planner.md` |
| Implementer | Claude Code | Claude | `mandates/implementer.md` |
| Verifier | OpenCode | GLM-5.2 via Featherless | `mandates/verifier.md` |
| Integrator | Claude Code | Claude | `mandates/integrator.md` |

## Key design decision: cross-model, cross-workspace verification

The verifier runs on a different model family from the implementer, in a separate clean checkout, and writes its tests from the specification without reading the implementer's reasoning. Agent teams most often accept broken work because author and checker share blind spots. Separating model, workspace and information removes all three shared sources of error.

## Flow

```
task brief ─► Planner ──WORK-ITEM──► Implementer ──EVIDENCE──► Verifier
                 ▲                        ▲                        │
                 │                        └────────REJECT──────────┤
                 │                                                 │
                 └──────────────────────ACCEPT─────────────────────┘

all items ACCEPT ─► Planner: MILESTONE-COMPLETE ─► Integrator
                  ─► RELEASE PASS ─► frozen snapshot ─► next milestone
```

## Room conventions

Every handoff message starts with its type tag (`WORK-ITEM`, `EVIDENCE`, `ACCEPT`, `REJECT`, `MILESTONE-COMPLETE`, `RELEASE`) and the item or milestone id, and mentions the receiving seat. The tags make the room export readable as an audit log.

## Ownership: who may write where

| Path | Owner |
|---|---|
| Working service directory (named in the brief) | Implementer |
| Acceptance directory (`acceptance/` by default) | Verifier |
| `DECISIONS.md` | Planner |
| Delivery folders (named in the brief) | Integrator, write-once |
| `factory/` | Human only |

## Gates

1. No item is done without a verifier ACCEPT on a named commit.
2. No milestone is released without a clean build under the brief's exact constraints and a full regression run.
3. Delivery snapshots are write-once.

## Escalation

- Three REJECTs on one item: the planner re-scopes it and logs the reason in `DECISIONS.md`.
- Contradiction in the specification or an irreversible choice: the planner asks the human one question with a proposed default, and proceeds with the default if unanswered.

## Standing this factory up for a different task

1. Create four agents in BAND Desktop with the runtimes above and give each its mandate as its standing instruction.
2. Put all four in one room.
3. Paste a task brief into the room naming the specification, the working directory, the delivery folders and the build and run constraints.

The mandates refer to no particular product, so nothing in them changes between tasks.

## Results

To be filled after the run: per milestone, the number of items, REJECTs, fix cycles, wall time and cost. See `COST.md`.

## Limitations

To be filled after the run.
