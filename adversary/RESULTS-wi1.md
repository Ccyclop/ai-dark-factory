# Adversary results — WI-1 (commit 9db2c84)

Campaign run: 2026-09-28, this seat's clone, contract revision `fb56d88` (C-RUN-4 and
C-RUN-5 in force). Re-run and re-recorded after the contract moved to D-24…D-27; the
service commit is the same throughout.

```
NO-BREAK WI-1
Commit attacked: 9db2c84
Attacks run: 105 checks, 0 failed, 3 skipped (not attackable yet), 0 open questions
Script: adversary/attack-wi1.sh   (re-runnable, exit 0)
Raw log: adversary/results/wi1-9db2c84.log
Invariants checked: C-RUN-1, C-RUN-2, C-RUN-3 (not observable), C-RUN-4, C-RUN-5,
  C-BUILD-2/3, C-OP-HEALTH, C-REP-HDR, C-ERR-404, C-ERR-405, C-ERR-500, C-PATH-1
```

Status of this campaign: WI-1 had no ACCEPT yet when it ran, so this is a
pre-emptive campaign on the evidence commit, not a gated verdict. It will be re-run
when the milestone-candidate gate opens.

Contract revision: the first run of this campaign was against `fb56d88` (94 checks)
and the recorded result is from `HEAD` at contract revision `60cab97`/D-26 and
`1a42e11`/D-27 (105 checks). The service is unchanged in both, and D-24 confirmed the
reach model this campaign used, so the verdict is the same under the current contract
text. See "What D-25, D-26 and D-27 changed" below.

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
| Path matching | 13 C-PATH-1 cases: encoded unreserved characters in a route (`/%68ealth`, `/he%61lth`, `/%68ea%6ct%68`, with and without a query), `%2F` in a segment, a trailing `%2F`, a dot segment, an empty segment, a trailing slash, case sensitivity, an extra segment, and invalid percent-encoding `/%zz` | the encoded spellings are the route (200); everything else is 404; `/%zz` is refused by `net/http` before routing |
| State | nothing to attack: WI-1 has no state-changing endpoint (3 skips) | C-RUN-3, C-INV-1, C-INV-2 and all of C-IDEM-* deferred to WI-2+ |

Notable negatives worth keeping in the permanent suite, because they are the
boundaries a later milestone can rot:

- A body larger than the server's 64 MiB drain cap (64 MiB+1, 96 MiB, 128 MiB) still
  produced a complete `404`. The response is written before the body is drained, so the
  connection closing with data in flight does not cost the client its response.
- A 16 MiB request line is answered `431` and a 1 MiB one is answered `404`; both complete.
- A client that half-closes mid-body gets `405`/`404`, not a reset.

## What D-25, D-26 and D-27 changed for this campaign

**D-25 and D-27 — `net/http`'s pre-handler responses.** About 20 of the checks record
plain-text `400`s and two record `431`s that Go's server sends itself, before any
handler runs. Under the original C-REP-HDR wording ("every response, success or error,
carries `Content-Type: application/json` and a body that is a single JSON object")
those were reportable breaks with no in-contract fix. D-25 excluded them and D-27
generalised the rule: any response `net/http` produces before a handler runs is
excluded, whatever its status, and only `500` stays forbidden for every request. The
script now implements exactly that — `500` fails, any other `5xx` from a pre-handler
rejection is recorded as a watch, and a dropped connection still fails. Nothing in this
run produced a `5xx`, so there is nothing to report.

**D-26 / C-PATH-1 — the open question this campaign raised, now decided.** The three
observations in the first run (`GET /%68ealth`, `/he%61lth` and `/%68ea%6ct%68` all
answering `200`) are now contracted behaviour: the path is split as sent, each segment
is decoded on its own, an encoded unreserved character is the same route, `%2F` is
never a separator, and nothing else is normalised. Those three cases moved from
observations to passing checks, and eleven more C-PATH-1 cases joined them.

## Latent C-PATH-1 non-conformance, not yet observable (prediction, not a break)

`9db2c84` routes on `r.URL.Path` (`practice/service/server.go`, `s.route(r.URL.Path)`),
which Go has already decoded in full. For `/health` that is indistinguishable from
C-PATH-1, which is why every C-PATH-1 case in this campaign passes. It stops being
equivalent the moment a route has a second segment: `/items%2F1` decodes to `/items/1`,
so an implementation matching on the decoded path would serve item 1 where C-PATH-1
requires `404`. D-26 names this exact trap.

This is code reading, not a reproduction, and at WI-1 no request can distinguish the
two: with one single-segment route there is no target whose decoded and per-segment
forms differ observably. So it is not reported as a break. The check is armed —
`adversary/attack-state.sh` asserts `/items%2F<id>` is `404` for a real id, and that
section runs as soon as items exist. The fix is to split `r.URL.EscapedPath()` on `/`
and decode each segment, which is what `adversary/dryrun` does.

## Harness defects found and fixed during this campaign

1. `adversary/tool/main.go` had a duplicated `const chunk` / `pad` pair in
   `cmdBigHeader`, so the tool no longer compiled and only a cached image kept it
   running — the script merely checked that the image existed. It now builds the tool
   itself, and adds `bigline` (request line of n bytes), `abort` (declare a body, send
   half, close write) and `seq` (n identical requests, reporting the response histogram
   and the number of distinct responses, which is how replay and idempotency are
   judged).
2. `adversary/lib.sh` set `set -euo pipefail` while being sourced, so it imposed
   `set -e` on both campaign scripts. A Docker-level failure (exit 125: the client
   container never started) aborted the campaign mid-run and left a truncated report
   that looked complete. This happened for real during the state-campaign self-test.
   `lib.sh` now sets only `-u -o pipefail`.
3. `adv_tool` now retries once when Docker itself fails (exit 125..127 with no status
   line, which the tool never produces) and says so on stderr, so a daemon hiccup can
   never be recorded as a contract break.
4. `adversary/attack-state.sh --stub` only checked that the stub image existed, so a
   stale stub was self-tested for a whole run; it now rebuilds it. The stub also used
   `http.ServeMux`, which cleans paths and answers with a `301` — a C-PATH-1 violation
   the new checks caught immediately. The real service routes by hand for the same
   reason.
5. `sed 's/./%&/g'` was used to spell an id in percent-encoded form and produced `%1%4`
   for `14`, not `%31%34`, so the request was invalid percent-encoding and `net/http`
   refused it. Replaced with a real byte encoder, `pct_encode` in `lib.sh`.
