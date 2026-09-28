# Harness self-test — the state attack set, before the routes exist

Date: 2026-09-28. Seat: adversary. No verdict is claimed here: WI-1 has no ACCEPT
and the state routes do not exist yet. This records that the *tooling* is proven
before its first real use.

## Why

`adversary/attack-state.sh` attacks C-CREATE-4, C-RES-5/6/7, C-CAN-1..4,
C-INV-1/2/3/4, C-ERR-PREC, C-IDEM-1..7 and C-RUN-3 — roughly 80 checks, none of
which can run against the walking skeleton. Shipping an attack script whose only
ever execution is its first real one at a gate means any quoting, JSON-reading or
arithmetic bug in it arrives wearing the costume of a contract break, and the room
spends a cycle on the implementer's code instead of on my script. So the script is
self-tested first.

## Method

`adversary/dryrun/` is a throwaway, contract-shaped stub service: the four
state-changing routes, one mutex, no database, no idempotency store beyond a map.
It is built only for the self-test and is never used for a verdict. The campaign
script then runs against it with `--stub`, which swaps the image and prints
"results are NOT a verdict" in the header.

```
docker build --network=none -t adversary-stub:local adversary/dryrun
adversary/attack-state.sh --stub          # 84 checks, 84 pass, 0 fail
```

Against the real walking skeleton the same script gates out cleanly:

```
adversary/attack-state.sh                 # 4 harness checks pass, 26 contract ids skipped, exit 0
```

## What the self-test caught

1. **A wrong expectation of mine, not a service bug.** `GET /reservations/1` on a
   fresh container returns `405` (the route exists, the method does not), not
   `404`. The "the old reservations are gone" check now issues `DELETE` and expects
   `404`, which is what C-CAN-3 actually says. Had this shipped, the first real run
   would have reported a C-RUN-3 break against a correct implementation.
2. **`lib.sh` was imposing `set -e` on every script that sources it.** A Docker-level
   failure (exit 125, the client container never started) killed the campaign
   mid-run and left a truncated report that looked complete. `lib.sh` now sets only
   `-u -o pipefail`, with the reason in the file. This was not theoretical: it
   happened during the first self-test run.
3. **A daemon hiccup could have been reported as a contract break.** `adv_tool` now
   retries once when Docker itself fails (exit 125..127 with no status line, which
   the tool never produces) and the output says so on stderr. Blaming the service
   for my own tooling is the one mistake this seat cannot afford.

After the fixes, the WI-1 set reproduces its recorded result exactly — 94 pass, 0
fail, 3 skip, 3 watch — on the same commit, with the corrected harness.

## Log

`adversary/results/selftest-stub.log` — the full 84-check self-test run.
