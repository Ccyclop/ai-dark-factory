# Cost and run log

Record at the end of every milestone. Sources: BAND Desktop Analytics, the Featherless Subscription page (itemised per request), and `/cost` in each Claude Code session. Factory metrics (REJECTs, BREAKs, first-pass rate) are in `METRICS.md`.

## Models used

| Seat | Exact model id | Billing |
|---|---|---|
| Planner | claude-opus-5-5[1m] | Claude Max subscription |
| Implementer | claude-opus-5-5[1m] | Claude Max subscription |
| Integrator | sonnet alias (record the resolved id from BAND's runtime check) | Claude Max subscription |
| Verifier | moonshotai/Kimi-K2.7-Code | Featherless per-request credits |
| Adversary | zai-org/GLM-5.2 | Featherless per-request credits |

Measured during setup (Featherless, input tokens): Kimi K2.7 Code about $0.83 per million with prompt caching cutting cached input roughly fourfold; GLM-5.2 about $1.45 per million. Every OpenCode request carries a baseline of roughly 7–8k input tokens.

## Per milestone and seat

| Milestone | Seat | Wall time | Input tokens | Output tokens | Cost ($) | Notes |
|---|---|---|---|---|---|---|
| | Planner | | | | | |
| | Implementer | | | | | |
| | Verifier | | | | | |
| | Adversary | | | | | |
| | Integrator | | | | | |

## Totals

| Milestone | Wall time | Cost ($) | Human interventions |
|---|---|---|---|
