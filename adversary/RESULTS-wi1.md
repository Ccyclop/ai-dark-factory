# Adversary results — WI-1 (commit 9db2c84)

Campaign run: 2026-09-28, this seat's clone, contract revision `fb56d88` (C-RUN-4 and
C-RUN-5 in force). Reviewed afterwards against `20efb24` (D-24, D-25): the service is
unchanged (still `9db2c84`) and D-24 confirms the reach model this campaign used, so
every result below stands under the current contract text. See "Effect of D-25" for
the one place where the contract text itself moved.

```
NO-BREAK WI-1
Commit attacked: 9db2c84
Attacks run: 94 checks, 0 failed, 3 skipped (not attackable yet), 3 observations
Script: adversary/attack-wi1.sh   (re-runnable, exit 0)
Raw log: adversary/results/wi1-9db2c84.log
Invariants checked: C-RUN-1, C-RUN-2, C-RUN-3 (not observable), C-RUN-4, C-RUN-5,
  C-BUILD-2/3, C-OP-HEALTH, C-REP-HDR, C-ERR-404, C-ERR-405, C-ERR-500
```

Status of this campaign: WI-1 had no ACCEPT yet when it ran, so this is a
pre-emptive campaign on the evidence commit, not a gated verdict. It will be re-run
when the milestone-candidate gate opens.

## How the service was reached (C-RUN-4, C-RUN-5)

Built with the adversary's own tag and started with the exact run command plus only
the three permitted harness flags:

```
docker build --network=none -t practice-adversary .        # in practice/service
docker run -d --rm --name adv-svc --network=none --cpus=1 --memory=512m \
  -p 8080:8080 practice-adversary
```

Every client was a container joined to that namespace:
`docker run --rm --network container:adv-svc <image> …` against `http://127.0.0.1:8080`.
Client images are already cached (`curlimages/curl:8.11.1`, `adversary-tool:local`,
both built from `golang:1.25.14-alpine3.24` / `alpine:3.24.1`), so nothing needs a pull.

The harness proves the C-RUN-4 premise on every run rather than assuming it:

- `docker port adv-svc` is empty — `-p 8080:8080` publishes nothing under `--network=none`.
- The host cannot reach `127.0.0.1:8080`; neither can a container in its own namespace.
- `docker inspect` reports `NetworkMode=none NanoCpus=1000000000 Memory=536870912`,
  so the harness changes no resource and no network setting.
- Readiness measured from container start: 1 ms, 3 ms, 0 ms on three cold starts
  (C-RUN-2's 10 s budget).

## Attacks by category

| Category | Attacks | Result |
|---|---|---|
| Concurrency | 50 and 200 simultaneous `GET /health`, 50 simultaneous `POST /health`; 15 s x 16 workers on `/health` (341 437 responses) and 10 s x 16 on `/nope` (238 751 responses) | 0 incomplete, 0 5xx, statuses only 200/404/405 |
| Repetition | the identical request replayed 341 437 times in one connection-per-request loop; three full container restarts; `POST` of an unchanged body | no drift, no 5xx, no dropped connection |
| Input | 15 byte-level malformed requests (bare LF, no Host, bad version, negative/overflow/non-numeric `Content-Length`, header without colon, space in header name, absolute-form target, garbage bytes, `Content-Length` + `Transfer-Encoding`, `Expect: 100-continue`, duplicate `Host`, NUL in header value, NUL in target, LF-only line endings); 11 further parser edges (duplicate `Content-Length`, obs-fold, double space, tab, bare CR, `OPTIONS *`, lowercase method, `Transfer-Encoding: gzip`, bad chunk size, uppercase chunk size); header blocks of 1 MiB+1, 8 MiB; request lines of 64 KiB, 1 MiB, 16 MiB; bodies of 1 MiB+1, 4, 8, 64, 64 MiB+1, 96 and 128 MiB; three truncated-body aborts (client stops mid-body, then closes the write side) | every one got a complete response; nothing 5xx |
| Routing | 14 non-route paths, 9 traversal/separator/control-byte paths, query string on a route, 7 wrong methods, `Allow` header, `HEAD` body suppression | 404 `{"error":…}` everywhere a path is not a route; 405 with `Allow: GET, HEAD`; `HEAD /health` 200 with empty body |
| State | nothing to attack: WI-1 has no state-changing endpoint (3 skips) | C-RUN-3, C-INV-1, C-INV-2 and all of C-IDEM-* deferred to WI-2+ |

Notable negatives worth keeping in the permanent suite, because they are the
boundaries a later milestone can rot:

- A body larger than the server's 64 MiB drain cap (64 MiB+1, 96 MiB, 128 MiB) still
  produced a complete `404`. The response is written before the body is drained, so the
  connection closing with data in flight does not cost the client its response.
- A 16 MiB request line is answered `431` and a 1 MiB one is answered `404`; both complete.
- A client that half-closes mid-body gets `405`/`404`, not a reset.

## Effect of D-25 on this campaign

D-25 landed as `20efb24`, after the run. It excludes `net/http`'s protocol-level
rejections from C-REP-HDR and allows exactly one `5xx` (the `505` for an unsupported HTTP
major version). That matters for how this log should be read: roughly 20 of the checks
record plain-text `400`s and two record `431`s from `net/http` itself, before any handler
runs. Under the pre-D-25 wording of C-REP-HDR ("every response, success or error, carries
`Content-Type: application/json` and a body that is a single JSON object") those would
have been reportable C-REP-HDR breaks, and no in-contract fix could have removed them —
which is the situation D-25 correctly describes. So:

- Under D-25, this campaign is a NO-BREAK with no qualifications.
- If D-25 is ever reverted, the same log becomes a C-REP-HDR break report with 22
  reproductions (`adversary/results/wi1-9db2c84.log`, the `400`/`431` lines), and the
  only in-contract remedy would be a custom connection layer, i.e. outside the brief.

No check in this campaign expected a `5xx` from any other cause, and none occurred.

## Observations for the planner (not breaks — reported, not counted)

1. **Percent-encoded spellings of a route are served as the route.**
   `GET /%68ealth`, `GET /he%61lth` and `GET /%68ea%6ct%68` all return `200` with the
   health body, because the request target is matched after percent-decoding. C-ERR-404
   says any path that is not a route returns 404, and section 4 says routes are exact
   paths, so this is genuinely ambiguous rather than a violation. It becomes material
   at WI-4: under this behaviour `/reservations/%31` is reservation 1.
   *Recommendation:* make it a decision now (the next free `D-*` number) and pin it in the
   verifier's suite — either "the target is compared after percent-decoding, so equivalent
   encodings are the same route" or "only the exact spelling is the route, everything else
   is 404". Leaving it unstated means the answer can change between milestones without
   breaking any criterion, which is exactly the kind of drift this room is meant to catch.
2. **Two parser leniencies, no contract entry involved.** Go accepts bare-LF request
   lines (`GET /health HTTP/1.1\nHost: x\n\n` returns `200`) and accepts a 1 048 577-byte
   header block (its cap is `MaxHeaderBytes` plus 4 KiB of slack; 8 MiB gives `431`).
   Neither violates any `C-*` entry — the contract only requires a complete, non-5xx
   response — but both are places where a stricter proxy in front of the service could
   disagree. No action needed unless the planner wants C-ERR-500 to state an intent here.

## Harness defect found and fixed during this campaign

`adversary/tool/main.go` had a duplicated `const chunk` / `pad` pair in `cmdBigHeader`, so
the tool no longer compiled and the stale `adversary-tool:local` image was being used
without anyone noticing: the attack script only checked that the image *existed*. The
script now builds the tool image itself before the campaign, so a broken tool can never
be masked by a cached image. Two subcommands were added for the boundary probes above:
`bigline` (request line of n bytes) and `abort` (declare a body, send half, close write).
