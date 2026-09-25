# Standing up the factory

Time needed: about 10 minutes once the prerequisites are installed.

## Prerequisites

- macOS or Linux
- BAND Desktop, signed in, with its readiness checks passing
- Claude Code, signed in
- OpenCode, with an OpenAI-compatible provider connected (this factory used Featherless: base URL `https://api.featherless.ai/v1`)
- Docker running
- git and a GitHub remote named `origin`

## 1. Prepare workspaces

From the repository root:

```bash
./factory/setup.sh
```

It checks the tools, confirms `origin/main` exists, and creates or updates two independent sibling clones: `<repo>-verify` and `<repo>-adversary`. At the end it prints the agent table below with your absolute paths.

## 2. Create five agents in BAND Desktop

| Agent | Runtime | Model | Working directory | Standing instruction |
|---|---|---|---|---|
| planner | Claude Code | default | repository | contents of `factory/mandates/planner.md` |
| implementer | Claude Code | default | repository | contents of `factory/mandates/implementer.md` |
| verifier | OpenCode | a model family different from the implementer's | `<repo>-verify` | contents of `factory/mandates/verifier.md` |
| adversary | OpenCode | a third model family | `<repo>-adversary` | contents of `factory/mandates/adversary.md` |
| integrator | Claude Code | default | repository | contents of `factory/mandates/integrator.md` |

## 3. Put them in one room and paste a task brief

A task brief must name:

- **Specification:** the documents to build against
- **Working directory:** where the implementer writes the service
- **Acceptance directory:** optional, defaults to `acceptance/`
- **Delivery folders:** one per milestone
- **Milestones:** what each one covers
- **Build and run constraints:** exact commands, resource limits, network rules
- **Validation tool:** optional repository checker to run before release
- **Done:** the observable end state

See `examples/practice-brief.md` for a complete example.

## 4. Watch, answer only what is asked

The planner will ask you only about genuine contradictions, each time with a proposed default. Everything else runs without you.
