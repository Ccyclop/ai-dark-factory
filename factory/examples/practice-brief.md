# Task brief — practice run

@planner Build a small inventory reservation service as a JSON API.

## Locations
- Working directory: `practice/service/`
- Acceptance directory: `practice/acceptance/`
- Delivery folders: `practice/milestone-1/`, `practice/milestone-2/`

## Stack and constraints
- Go standard library HTTP server, SQLite through a pure-Go driver, all dependencies vendored.
- Must build and run with no network access:
  `docker build --network=none -t practice .` and
  `docker run --network=none --cpus=1 --memory=512m -p 8080:8080 practice`

## Milestone 1 — API
- Create an item with a name and a stock count; returns the item with an id.
- Get an item, including its available stock.
- Reserve N units of an item; returns a reservation id.
- Cancel a reservation; the units become available again.
- Invalid input returns HTTP 400 with a JSON body `{"error": "<message>"}`. Unknown ids return 404 with the same body shape. Never 500.

## Milestone 2 — Correctness under load
- Available stock never goes below zero, even when 50 reserve requests for the same item arrive at once.
- A request carrying an `Idempotency-Key` header that was already used returns the original response and does not reserve again. The same key with a different body returns 409.
- Milestone 1 behaviour must keep passing.

## Done
Both milestones released: every item accepted by the verifier, no break found by the adversary, and each delivery folder built and tested by the integrator under the constraints above.
