# CONTRACT — practice run: inventory reservation service

Single source of truth for the implementer, verifier, adversary and integrator.
Owner: Planner. Every change is logged in `DECISIONS.md` and announced in the room.

- Specification: the task brief "Task brief — practice run" (room message `9717d99e-67b6-4827-8c73-001ae0b4e8b5`; same text as `factory/examples/practice-brief.md`).
- `S-*` entries quote the brief verbatim. `C-*` entries are the contract. Each `C-*` entry cites the `S-*` text it implements and, where the brief is silent, the `D-*` decision in `DECISIONS.md` that fixed the detail.
- Milestone tag: **[M1]** or **[M2]**. M2 includes everything in M1 (S-13).

---

## 1. Specification, verbatim

| id | Brief section | Text (verbatim) |
|---|---|---|
| S-1 | Locations | Working directory: `practice/service/` |
| S-2 | Locations | Acceptance directory: `practice/acceptance/` |
| S-3 | Locations | Delivery folders: `practice/milestone-1/`, `practice/milestone-2/` |
| S-4 | Stack and constraints | Go standard library HTTP server, SQLite through a pure-Go driver, all dependencies vendored. |
| S-5 | Stack and constraints | Must build and run with no network access: `docker build --network=none -t practice .` and `docker run --network=none --cpus=1 --memory=512m -p 8080:8080 practice` |
| S-6 | Milestone 1 — API | Create an item with a name and a stock count; returns the item with an id. |
| S-7 | Milestone 1 — API | Get an item, including its available stock. |
| S-8 | Milestone 1 — API | Reserve N units of an item; returns a reservation id. |
| S-9 | Milestone 1 — API | Cancel a reservation; the units become available again. |
| S-10 | Milestone 1 — API | Invalid input returns HTTP 400 with a JSON body `{"error": "<message>"}`. Unknown ids return 404 with the same body shape. Never 500. |
| S-11 | Milestone 2 — Correctness under load | Available stock never goes below zero, even when 50 reserve requests for the same item arrive at once. |
| S-12 | Milestone 2 — Correctness under load | A request carrying an `Idempotency-Key` header that was already used returns the original response and does not reserve again. The same key with a different body returns 409. |
| S-13 | Milestone 2 — Correctness under load | Milestone 1 behaviour must keep passing. |
| S-14 | Done | Both milestones released: every item accepted by the verifier, no break found by the adversary, and each delivery folder built and tested by the integrator under the constraints above. |

---

## 2. Build and runtime constraints

| id | M | Entry | Refs |
|---|---|---|---|
| C-BUILD-1 | M1 | Service source lives in `practice/service/`. Its root contains the `Dockerfile`, `go.mod`, `go.sum` and `vendor/`. | S-1, S-4 |
| C-BUILD-2 | M1 | Built exactly with `docker build --network=none -t practice .` run from inside the service directory (`practice/service/`, and later each delivery folder). The build context is that directory alone; nothing outside it is referenced. | S-3, S-5, D-14 |
| C-BUILD-3 | M1 | No step of the build downloads anything: every Go dependency is in `vendor/` and compiled with `-mod=vendor`; the Go toolchain is never auto-downloaded (`GOTOOLCHAIN=local`). Base images are pinned by tag and may be pre-pulled into the local Docker cache. | S-4, S-5, D-14 |
| C-BUILD-4 | M1 | HTTP is served by Go's standard library `net/http`. No third-party HTTP router or framework. | S-4 |
| C-BUILD-5 | M1 | Storage is SQLite accessed through a pure-Go driver. The binary is built with `CGO_ENABLED=0`. | S-4 |
| C-RUN-1 | M1 | Runs exactly with `docker run --network=none --cpus=1 --memory=512m -p 8080:8080 practice`: no extra flags, environment variables, volumes or arguments are needed. A test harness may add only `-d`, `--rm` and `--name <name>`, which change neither resources nor network. | S-5, D-21 |
| C-RUN-2 | M1 | Listens on TCP port `8080` on all interfaces inside the container, and answers `GET http://127.0.0.1:8080/health` from within the container's network namespace within 10 seconds of container start. | S-5, D-12, D-20 |
| C-RUN-3 | M1 | Starts from an empty database on every fresh container. Durability across container restarts is not required. | D-13 |
| C-RUN-4 | M1 | How clients reach the service. Under `--network=none` the container has only a loopback interface, so `-p 8080:8080` publishes nothing and the host cannot connect (confirmed 2026-09-25). Every black-box client — verifier, adversary, integrator — starts the service with the C-RUN-1 command and connects to `http://127.0.0.1:8080` from a client container that joins the service's network namespace: `docker run --rm --network container:<name> <client image> …`. The client also has no network, so its image and everything it runs must already be local. | S-5, S-14, D-20 |
| C-RUN-5 | M1 | Image tag. The release build by the integrator uses exactly `-t practice`. Other seats may substitute a seat-specific tag (e.g. `practice-verifier`, `practice-adversary`) with otherwise identical build and run flags, so that seats sharing one Docker daemon never test each other's image. | S-5, D-21 |

---

## 3. Resources and representations

| id | M | Entry | Refs |
|---|---|---|---|
| C-REP-ITEM | M1 | Item JSON object with exactly these fields: `"id"` (JSON integer ≥ 1), `"name"` (JSON string, exactly as submitted), `"stock"` (JSON integer, the stock count given at creation; never changes), `"available"` (JSON integer, see C-INV-2). | S-6, S-7, D-3, D-4 |
| C-REP-RES | M1 | Reservation JSON object with exactly these fields: `"id"` (JSON integer ≥ 1, the reservation id), `"item_id"` (JSON integer), `"quantity"` (JSON integer), `"status"` (JSON string, `"active"` or `"cancelled"`). | S-8, S-9, D-3, D-5 |
| C-REP-ERR | M1 | Error JSON object with exactly one field: `"error"` (non-empty JSON string, human-readable; exact wording is not contracted). Shape: `{"error": "<message>"}`. | S-10 |
| C-REP-HDR | M1 | Every response, success or error, carries `Content-Type: application/json` and a body that is a single JSON object (responses to `HEAD` carry no body, per HTTP). | S-10, D-9 |
| C-ID-1 | M1 | Item ids and reservation ids are separate sequences of positive integers assigned by the server. An id is never reused. | S-6, S-8, D-3 |

---

## 4. Operations

All routes are exact paths. `{id}` is a path segment.

| id | M | Method and path | Success | Refs |
|---|---|---|---|---|
| C-OP-HEALTH | M1 | `GET /health` | `200` body `{"status": "ok"}` | D-12 |
| C-OP-CREATE | M1 | `POST /items` | `201`, body C-REP-ITEM | S-6 |
| C-OP-GET | M1 | `GET /items/{id}` | `200`, body C-REP-ITEM | S-7 |
| C-OP-RESERVE | M1 | `POST /reservations` | `201`, body C-REP-RES with `"status": "active"` | S-8, D-5 |
| C-OP-CANCEL | M1 | `DELETE /reservations/{id}` | `200`, body C-REP-RES with `"status": "cancelled"` | S-9, D-6 |

### C-OP-CREATE — `POST /items` [M1]
- Request body: JSON object `{"name": <string>, "stock": <integer>}`.
- C-CREATE-1: On success returns `201` with the new item; `"available"` equals `"stock"`; `"name"` and `"stock"` equal the submitted values.
- C-CREATE-2: `"name"` must be a JSON string containing at least one non-whitespace character and at most 200 characters (Unicode code points). Otherwise `400`. (D-7)
- C-CREATE-3: `"stock"` must be a JSON integer with 0 ≤ `stock` ≤ 1000000000. Otherwise `400`. (D-7)
- C-CREATE-4: Without an `Idempotency-Key` header, two identical requests create two distinct items with different ids. (D-11)

### C-OP-GET — `GET /items/{id}` [M1]
- C-GET-1: Returns `200` with the item; `"available"` reflects every reservation and cancellation completed before the request (C-INV-2).
- C-GET-2: If `{id}` does not identify an existing item — including non-numeric, zero, negative, leading-zero or out-of-range values — returns `404` with C-REP-ERR. (D-8)

### C-OP-RESERVE — `POST /reservations` [M1]
- Request body: JSON object `{"item_id": <integer>, "quantity": <integer>}`. (D-5)
- C-RES-1: On success returns `201` with the reservation (`"status": "active"`, `"item_id"` and `"quantity"` as submitted), and the item's `"available"` decreases by exactly `"quantity"`.
- C-RES-2: `"item_id"` must be a JSON integer in the signed 64-bit range. Otherwise `400`. (D-7)
- C-RES-3: `"quantity"` must be a JSON integer with 1 ≤ `quantity` ≤ 1000000000. Otherwise `400`. (D-7)
- C-RES-4: A valid `"item_id"` that does not identify an existing item returns `404`. (S-10)
- C-RES-5: If `"quantity"` is greater than the item's current `"available"`, returns `409` with C-REP-ERR and changes nothing. (D-10)
- C-RES-6: The availability check and the write are one atomic step: however many reserve requests run at once, the sum of quantities of successful reservations never exceeds the stock available before them. (S-11, D-15)
- C-RES-7: Without an `Idempotency-Key` header, two identical requests are two reservations (each succeeds or fails on its own under C-RES-5). (D-11)

### C-OP-CANCEL — `DELETE /reservations/{id}` [M1]
- No request body is read.
- C-CAN-1: Cancelling an `"active"` reservation returns `200` with the reservation now `"status": "cancelled"`, and the item's `"available"` increases by exactly its `"quantity"`.
- C-CAN-2: Cancelling an already `"cancelled"` reservation returns `200` with the same body and changes nothing; the units are released only once, including when several cancels of the same reservation run at once. (D-6)
- C-CAN-3: If `{id}` does not identify an existing reservation (same rule as C-GET-2), returns `404`. (D-8)
- C-CAN-4: Units released by a cancel can be reserved again.

---

## 5. Errors (all operations)

| id | M | Entry | Refs |
|---|---|---|---|
| C-ERR-400 | M1 | Invalid input returns `400` with C-REP-ERR. Invalid input includes: empty body; body that is not valid JSON; JSON that is not a single object (array, string, number, `null`); trailing content after the object; a required field missing or `null`; a field of the wrong JSON type; a number with a fraction or exponent where an integer is required (`5.0`, `1e3`); an integer outside the stated range; a request body larger than 1048576 bytes. Unknown extra fields are ignored. | S-10, D-7, D-9 |
| C-ERR-404 | M1 | Unknown ids return `404` with C-REP-ERR. Any path that is not a route in section 4 also returns `404` with C-REP-ERR. | S-10, D-8, D-9 |
| C-ERR-405 | M1 | A route path used with a method it does not support returns `405` with C-REP-ERR and an `Allow` header. | D-9 |
| C-ERR-409 | M1/M2 | `409` with C-REP-ERR for insufficient stock (C-RES-5) and for idempotency key reuse with a different request (C-IDEM-3). | S-12, D-10 |
| C-ERR-PREC | M1 | When several errors apply, the first in this order wins: `400` (header or body validation) → `404` (unknown id) → `409`. | D-10 |
| C-ERR-500 | M1 | Never 500: no request, however malformed, oversized or concurrent, gets any `5xx` status, and every request receives a complete HTTP response (no dropped connection). | S-10 |

---

## 6. Invariants

| id | M | Entry | Refs |
|---|---|---|---|
| C-INV-1 | M1, load-tested M2 | Available stock never goes below zero: for every item, 0 ≤ `available` ≤ `stock` at all times, including when 50 reserve requests for the same item arrive at once. | S-11 |
| C-INV-2 | M1 | For every item, `available` = `stock` − (sum of `quantity` over its reservations whose `status` is `"active"`), observable via `GET /items/{id}` once all in-flight requests have completed. | S-7, S-9 |
| C-INV-3 | M1 | A reservation's units are released at most once. | S-9, D-6 |
| C-INV-4 | M2 | A given `Idempotency-Key` causes at most one execution of an operation, however many requests carry it, sequentially or at once. | S-12 |
| C-INV-5 | M2 | All M1 contract entries keep holding. | S-13 |

---

## 7. Idempotency [M2]

| id | M | Entry | Refs |
|---|---|---|---|
| C-IDEM-1 | M2 | The request header `Idempotency-Key` (header name matched case-insensitively) is honoured on `POST /items`, `POST /reservations` and `DELETE /reservations/{id}`. It is ignored on `GET` routes. Requests without it behave exactly as in M1. | S-12, D-16 |
| C-IDEM-2 | M2 | A request carrying a key that was already used, with the same method, path and byte-identical body, returns the original response — same status code and byte-identical body — and does not execute again (no new item, no new reservation, no change to `available`). | S-12, D-17 |
| C-IDEM-3 | M2 | A request carrying a key that was already used with a different body, or with a different method or path, returns `409` with C-REP-ERR and changes nothing. The stored original response is kept. | S-12, D-17 |
| C-IDEM-4 | M2 | The original response is the response produced by the first request that carried the key, whatever its status (`201`, `200`, `400`, `404` or `409` for insufficient stock). A `409` produced by C-IDEM-3 and a `400` produced by C-IDEM-5 are not stored. | S-12, D-18 |
| C-IDEM-5 | M2 | The key value must be 1 to 255 bytes. An empty or longer value, or more than one `Idempotency-Key` header, returns `400` with C-REP-ERR. | D-19 |
| C-IDEM-6 | M2 | When several requests carrying the same new key arrive at once, exactly one executes; each of the others returns either the same response (C-IDEM-2) or `409` (C-IDEM-3) according to its own body. | S-12, C-INV-4 |
| C-IDEM-7 | M2 | Keys are global (not per client or per route) and do not expire during the life of the container. | D-17 |
