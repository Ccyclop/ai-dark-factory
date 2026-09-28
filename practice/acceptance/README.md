# Acceptance suite — practice inventory reservation service

Black-box acceptance tests written from `CONTRACT.md` (the contract) and the
task brief. The verifier owns this directory. Nothing here imports or reads
the service: every test talks to a running instance over HTTP only, and the
build/container checks in `contract.sh` observe only the image, the
container, the listening socket and the binary's own build information.

## The one command

```sh
sh practice/acceptance/run.sh
```

Run it from a fresh clone, on a machine with Docker and the pinned base
images (`golang:1.25.14-alpine3.24`, `alpine:3.24.1`) in the local cache. It
does three things, in order, and stops at the first failure:

1. `contract.sh` — checks the build and runtime contract (C-BUILD-1..5,
   C-RUN-1..3): builds the image with the exact `docker build --network=none
   -t practice .` command from inside `practice/service/`, rebuilds it
   `--no-cache` from a git archive of `practice/service/` alone to prove no
   step downloads anything and nothing outside the directory is referenced,
   reads the binary's build information (CGO_ENABLED=0, the vendored pure-Go
   SQLite driver, no third-party HTTP router), runs a container with the
   exact `docker run --network=none --cpus=1 --memory=512m -p 8080:8080
   practice` command, and checks readiness within 10 s, the wildcard bind on
   port 8080, and a fresh database on every fresh container.
2. starts the service with that exact command (plus `-d` and `--name`, which
   C-RUN-1 allows a harness) and waits up to 10 s for `GET /health`.
3. runs the black-box HTTP suite (`api/`) in a sidecar container attached
   to the service container's network namespace, so the suite reaches the
   service at `http://localhost:8080` exactly as the contract asks
   (C-RUN-4: under `--network=none` the host cannot connect; the sidecar is
   the only way in).

The suite runs offline end to end: the build needs no network, and the Go
toolchain for the test run is the build stage's own pinned base image.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `PRACTICE_BASE_URL` | unset | Point the suite at an already-running service (e.g. `http://host.docker.internal:8080`) and skip steps 2-3's own container. Use this when someone else started the service with a published port. |
| `PRACTICE_IMAGE` | `practice` | Image tag to build and run. Seats other than the integrator use their own tag (`practice-verifier`, `practice-adversary`) per C-RUN-5, so seats sharing one Docker daemon never test each other's image. |
| `PRACTICE_COMMIT` | `HEAD` | Git revision to export for the isolated-context build check. Set it to the commit under test (e.g. the one named in EVIDENCE). |
| `PRACTICE_GO_TESTS` | `-count=1 -v -timeout 900s` | Flags for `go test` in step 3. |

Example — verify a specific commit against a service you started yourself:

```sh
PRACTICE_COMMIT=9db2c84a89b68404927e569281f933f527d1e2bc \
PRACTICE_IMAGE=practice-verifier \
sh practice/acceptance/run.sh
```

## Layout

- `run.sh` — the one command above.
- `contract.sh` — build and runtime contract checks (C-BUILD-1..5, C-RUN-1..3).
- `api/` — the black-box HTTP suite, by contract area:
  - `harness_test.go` — the HTTP harness and the contract assertions
    (C-REP-HDR, C-REP-ERR, C-ERR-500, C-ERR-405's Allow header).
  - `health_test.go` — C-OP-HEALTH (D-12).
  - `notfound_test.go` — C-ERR-404.
  - `method_test.go` — C-ERR-405.
  - `protocol_test.go` — C-ERR-500 and the C-REP-HDR scope boundary (D-25).
  - `robustness_test.go` — C-ERR-500 under malformed, oversized and framed input.
  - `concurrency_test.go` — C-ERR-500 under simultaneous load.

Every break case ever cited against the service is kept here as a permanent
test.
