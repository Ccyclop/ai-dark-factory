# DECISIONS — practice run

Every interpretation of the brief that `CONTRACT.md` depends on, with the reason. Owner: Planner.
Format: id, decision, reason, contract entries affected. Changes to the contract are appended here, never rewritten.

Questions asked to the human so far: none. No part of the brief contradicts another, and none of the choices below is irreversible.

---

**D-1 — Specification source.** The task brief is the whole specification; it references no other documents. Every detail the brief does not state is fixed by a decision in this file.
*Reason:* the brief names no API document, so the contract must fix routes, field names and status codes itself, or the implementer and verifier would each invent their own.
*Affects:* all `C-*`.

**D-2 — JSON naming.** Field names are lowercase snake_case. Requests and responses are JSON objects.
*Reason:* consistent, conventional for JSON APIs; the brief's only literal field is `"error"`, which fits.
*Affects:* C-REP-*.

**D-3 — Ids are positive integers.** Item ids and reservation ids are server-assigned JSON integers ≥ 1, two separate sequences, never reused. Both representations call their own id `"id"`.
*Reason:* maps directly to SQLite `INTEGER PRIMARY KEY`; "returns the item with an id" (S-6) and "returns a reservation id" (S-8) are both satisfied by the `"id"` field of the returned object, which keeps items and reservations symmetric.
*Affects:* C-ID-1, C-REP-ITEM, C-REP-RES.

**D-4 — Item fields `stock` and `available`.** Create takes `"name"` and `"stock"`. The item carries `"stock"` (the count given at creation, never changed) and `"available"` (stock minus active reservations).
*Reason:* S-6 says "a stock count", S-7 says "available stock"; exposing both lets any client check C-INV-1 and C-INV-2 without a second endpoint. There is no endpoint that changes `stock`, because the brief defines none.
*Affects:* C-REP-ITEM, C-OP-CREATE, C-INV-2.

**D-5 — Reserve is `POST /reservations` with `{"item_id", "quantity"}`.** N units (S-8) is the field `"quantity"`. The reservation carries `"status"` (`"active"` / `"cancelled"`).
*Reason:* putting the item id in the body rather than the path means the whole request is in the body, so S-12's "same key with a different body returns 409" covers every way two reserve requests can differ. With the item id in the path, a replay aimed at a different item but with an identical body would, read literally, have to return the first item's reservation. `"status"` makes a cancel observable in its own response.
*Affects:* C-OP-RESERVE, C-REP-RES, C-IDEM-2, C-IDEM-3.

**D-6 — Cancel is `DELETE /reservations/{id}` and is idempotent.** It returns `200` with the reservation (`"status": "cancelled"`). Cancelling an already-cancelled reservation returns `200` with the same body and releases nothing.
*Reason:* S-9 requires the units to come back; the property that matters is that they come back exactly once (C-INV-3). The brief defines no error for a repeated cancel: `404` would be wrong (the id is known), and `409` is the brief's code for idempotency key reuse. DELETE is idempotent by HTTP semantics, so a client retry after a lost response is safe.
*Affects:* C-OP-CANCEL, C-CAN-2, C-INV-3.

**D-7 — Input validation ranges.** `name`: a JSON string with at least one non-whitespace character and at most 200 Unicode code points, stored and returned exactly as sent (not trimmed). `stock`: JSON integer 0…1000000000 (zero allowed). `quantity`: JSON integer 1…1000000000. `item_id`: JSON integer in the signed 64-bit range. Integers must be written without fraction or exponent (`5.0`, `1e3` are `400`). `null` counts as missing. Unknown extra fields are ignored.
*Reason:* S-10 requires `400` for invalid input but does not define "valid". Upper bounds keep every sum within 64-bit integers so no overflow can reach a 500; a stock of zero is a meaningful item that can never be reserved; a reservation of zero units is meaningless. Ignoring unknown fields avoids rejecting harmless extra client data.
*Affects:* C-CREATE-2, C-CREATE-3, C-RES-2, C-RES-3, C-ERR-400.

**D-8 — A path id that matches nothing is `404`, whatever its form.** `/items/abc`, `/items/0`, `/items/-1`, `/items/007`, `/items/99999999999999999999` all return `404`, as do the same forms under `/reservations/`.
*Reason:* S-10: "Unknown ids return 404". A path segment names a resource; if no resource has that name it is unknown. A `400` here would make the answer depend on the id's spelling, which clients cannot know is wrong in advance. A body field of the wrong JSON type is different: it is malformed input, so `400` (D-7).
*Affects:* C-GET-2, C-CAN-3, C-ERR-404.

**D-9 — Every response is JSON, including routing errors.** Paths that are not routes give `404` and wrong methods give `405` (with `Allow`), both with the `{"error": ...}` body. A request body over 1048576 bytes is `400`.
*Reason:* S-10 fixes the error body shape; Go's default mux answers `404`/`405` in plain text, which would break that shape. The brief names only 400, 404 and 409 as error codes; an oversized body is invalid input, so `400` rather than `413`.
*Affects:* C-REP-HDR, C-ERR-400, C-ERR-404, C-ERR-405.

**D-10 — Insufficient stock is `409`; error precedence is 400 → 404 → 409.** Reserving more than `available` returns `409` and changes nothing.
*Reason:* S-11 implies some reserve requests must be refused, but the brief gives no code. It is not invalid input (the same request can succeed later, after a cancel), so not `400`; `409 Conflict` is the conventional code for a request that conflicts with current state, and it is already in the brief's vocabulary. Validation is checked before lookup, and lookup before stock, so the answer does not depend on database state when the input is malformed.
*Affects:* C-RES-5, C-ERR-409, C-ERR-PREC.

**D-11 — No implicit de-duplication.** Without an `Idempotency-Key` header, identical requests are independent operations.
*Reason:* S-12 ties replay protection to the header; de-duplicating without it would refuse legitimate repeated orders.
*Affects:* C-CREATE-4, C-RES-7.

**D-12 — `GET /health` → `200 {"status": "ok"}`.** Added for the walking skeleton and for readiness checks by the verifier, adversary and integrator.
*Reason:* the planner mandate requires a first item that answers a trivial request; a dedicated route lets every seat wait for readiness without creating data.
*Affects:* C-OP-HEALTH, C-RUN-2.

**D-13 — Fresh database per container.** The SQLite file lives inside the container; there is no volume.
*Reason:* the run command in S-5 mounts no volume and passes no environment, so data cannot outlive the container anyway. Each test run starts clean.
*Affects:* C-RUN-3.

**D-14 — Meaning of "no network access" for the build.** `--network=none` removes the network from `RUN` steps. Therefore: all Go modules are vendored and built with `-mod=vendor`; `GOTOOLCHAIN=local` so a `go`/`toolchain` line in `go.mod` never triggers a toolchain download; base images are pinned by tag and may be pre-pulled into the local Docker cache (the fresh-clone rule for the integrator covers the git checkout, not Docker's image cache). The build context is the service directory alone, so each delivery folder builds on its own.
*Reason:* S-4 and S-5 together. Observed on 2026-09-25: the host has no Go toolchain and no `golang` image cached, so the implementer must pull the chosen base images once and do dependency vendoring inside a container while network is still allowed for development.
*Affects:* C-BUILD-2, C-BUILD-3.

**D-15 — Atomic reserve from Milestone 1.** The reserve check-and-write is atomic from the first reserve item; Milestone 2 load-tests it at 50 concurrent requests under the S-5 resource limits and hardens it (for example, no "database is locked" error may surface as a 5xx).
*Reason:* the implementer mandate requires atomic check-and-write for every state change, and building reserve non-atomically in M1 would force rework in M2.
*Affects:* C-RES-6, C-INV-1.

**D-16 — `Idempotency-Key` applies to every state-changing route and is ignored on `GET`.** Honoured on `POST /items`, `POST /reservations`, `DELETE /reservations/{id}`.
*Reason:* S-12 says "A request carrying an `Idempotency-Key` header", not only a reserve request. Replaying a `GET` from a stored response would return stale stock, which contradicts S-7; `GET` is already safe to repeat.
*Affects:* C-IDEM-1.

**D-17 — What "the same request" means for a key.** A request matches the stored one when method, path and raw body bytes are all identical. Any difference is `409`. Keys are global and never expire while the container runs.
*Reason:* S-12 compares "body"; byte equality is exact and cannot be argued about. Method and path are included because otherwise one key could be replayed against a different route and return an unrelated response. The service has no clients or authentication, so there is no narrower scope for keys.
*Affects:* C-IDEM-2, C-IDEM-3, C-IDEM-7.

**D-18 — The first response for a key is stored, whatever its status.** A replay returns the stored `201`, `200`, `400`, `404` or insufficient-stock `409`. The key-reuse `409` and the invalid-key `400` are not stored.
*Reason:* S-12: "returns the original response". Storing only successes would let a replay return something other than the original response. The two excluded cases never executed the operation, so there is nothing original to store.
*Affects:* C-IDEM-4.

**D-19 — Key value limits.** 1–255 bytes; an empty value, a longer value, or more than one `Idempotency-Key` header is `400`.
*Reason:* bounds storage per key and removes ambiguity about which of two headers counts.
*Affects:* C-IDEM-5.

---

## Changes after the first contract (0d3f957)

**D-20 — Black-box access to a `--network=none` container** (2026-09-25, raised by the implementer while WI-1 was in progress; confirmed by the planner).
Finding: with `docker run --network=none … -p 8080:8080`, Docker publishes no port (`docker port` is empty) and the host cannot connect to `localhost:8080`, whatever the service does. S-5's run command therefore cannot be reached from the host, which conflicts with S-14 ("built and tested … under the constraints above").
Decision (default, pending the human's answer): keep S-5's run command exactly and reach the service from inside its network namespace. Every client runs in a container started with `--network container:<service-container-name>` and connects to `http://127.0.0.1:8080`. The rejected alternative was to drop `--network=none` from the run step during tests.
*Reason:* this is the only reading that keeps every constraint in S-5 and S-14 while tests run. Dropping `--network=none` would test the service under conditions the brief forbids. If the human chooses the alternative, only the harness changes, not the service.
*Affects:* C-RUN-2 (amended), C-RUN-4 (new), WI-1 criterion 3 (amended, see D-22). The human was asked.

**D-21 — Harness flags and per-seat image tags.** A harness may add `-d`, `--rm` and `--name` to the run command. Seats other than the integrator may build with a seat-specific tag instead of `practice`.
*Reason:* several seats share one Docker daemon. If they all build `-t practice` from different commits, one seat can test another seat's image. These flags and tags change neither resources nor network. The integrator's release build keeps the exact tag.
*Affects:* C-RUN-1 (amended), C-RUN-5 (new).

**D-22 — WI-1 criterion 3 amended while in progress.** It said "`GET http://localhost:8080/health`", meaning from the host. It now reads: "`GET http://127.0.0.1:8080/health` from a container joined to the service's network namespace (C-RUN-4)". Nothing else in WI-1 changes. The implementer and verifier were notified in the room.
*Reason:* the original wording cannot be met by any service (D-20).

**D-23 — Build facts recorded from WI-1 (commit 9db2c84).** The base images are `golang:1.25.14-alpine3.24` (build) and `alpine:3.24.1` (runtime), both now in the local Docker cache. The build needs Go ≥ 1.25 because `modernc.org/sqlite` v1.59.0 requires it. `vendor/` is about 136 MB in about 2,000 files, none over 20 MB. `curlimages/curl:8.11.1` is also cached and can be used as a client image for C-RUN-4.
*Reason:* the integrator's fresh clone does not include Docker's image cache (D-14). These images must stay cached on the release machine, or be pulled once, before an offline build.
*Affects:* C-BUILD-3 (no text change).
