#!/bin/sh
# run.sh — the one command that runs the whole acceptance suite for the practice
# inventory reservation service, from a fresh clone, under the exact constraints
# the task brief fixes.
#
#   sh practice/acceptance/run.sh
#
# It does three things, in order, and stops at the first failure:
#   1. sh practice/acceptance/contract.sh
#      builds the image with C-BUILD-2's exact command and checks C-BUILD-1..5
#      and C-RUN-1..3 against the image and a container started with C-RUN-1's
#      exact command.
#   2. starts the service with C-RUN-1's exact command, detached only so this
#      script can go on, and waits up to 10 s for GET /health (C-RUN-2).
#   3. runs the black-box HTTP suite in a sidecar container attached to the
#      service container's network namespace, so the suite reaches the service at
#      http://localhost:8080 exactly as the plan asks. --network=none leaves the
#      service container with nothing but its own loopback, so the host cannot
#      reach it and the sidecar is the only way in.
#
# Env:
#   PRACTICE_BASE_URL  skip steps 2-3's own container and point the suite at this
#                      URL instead (for an instance someone else started, e.g.
#                      `docker run -d -p 8080:8080 practice` and
#                      PRACTICE_BASE_URL=http://host.docker.internal:8080).
#   PRACTICE_IMAGE     image tag to build and run (default: practice)
#   PRACTICE_COMMIT    revision to export for the isolated-context build
#                      (default: HEAD)
#   PRACTICE_GO_TESTS  go test flags (default: -count=1 -v -timeout 900s)

set -u

ACC_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$ACC_DIR/../.." && pwd)
IMAGE=${PRACTICE_IMAGE:-practice}
GO_TESTS=${PRACTICE_GO_TESTS:--count=1 -v -timeout 900s}
RC=0
SVC=""

cleanup() {
	if [ -n "$SVC" ]; then
		docker rm -f "$SVC" >/dev/null 2>&1
	fi
}
trap cleanup EXIT INT TERM

printf '########## 1/3  build and runtime contract ##########\n\n'
RC_CONTRACT=0
sh "$ACC_DIR/contract.sh" || RC_CONTRACT=1
[ "$RC_CONTRACT" -eq 0 ] || { printf '\ncontract checks failed; not starting the service\n'; exit 1; }

printf '\n########## 2/3  start the service with the C-RUN-1 command ##########\n\n'
if [ -n "${PRACTICE_BASE_URL:-}" ]; then
	URL=$PRACTICE_BASE_URL
	printf 'PRACTICE_BASE_URL is set: using %s and not starting a container.\n' "$URL"
else
	SVC=$(docker run -d --network=none --cpus=1 --memory=512m -p 8080:8080 "$IMAGE")
	URL=http://localhost:8080
	printf 'container %s started with:\n  docker run -d --network=none --cpus=1 --memory=512m -p 8080:8080 %s\n' \
		"$SVC" "$IMAGE"
	START=$(date +%s)
	READY=no
	while [ $(( $(date +%s) - START )) -le 10 ]; do
		if [ "$(docker exec "$SVC" wget -q -O- "$URL/health" 2>/dev/null)" = '{"status": "ok"}' ]; then
			READY=yes
			break
		fi
		sleep 0.2
	done
	if [ "$READY" != yes ]; then
		printf 'the service did not answer GET /health within 10 s (C-RUN-2)\n'
		exit 1
	fi
	printf 'GET /health answered %ss after start (C-RUN-2 allows 10s)\n' "$(($(date +%s) - START))"
fi

printf '\n########## 3/3  black-box HTTP suite ##########\n\n'
# The Go toolchain is the build stage's own pinned base image, so it is already
# in the local cache and needs no network.
GO_IMAGE=$(awk '/^FROM/ {print $2; exit}' "$ROOT/practice/service/Dockerfile")
if [ -z "${PRACTICE_BASE_URL:-}" ]; then
	NETARG="--network container:$SVC"
else
	NETARG=""
fi
# shellcheck disable=SC2086
docker run --rm $NETARG \
	-e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-mod=mod \
	-e PRACTICE_BASE_URL="$URL" \
	-v "$ACC_DIR":/src -w /src "$GO_IMAGE" \
	go test $GO_TESTS ./api/ || RC=1

printf '\n== acceptance suite: contract checks %s, HTTP suite %s ==\n' \
	"$([ "$RC" -eq 0 ] && echo pass || echo FAIL)" "$([ "$RC" -eq 0 ] && echo pass || echo FAIL)"
exit "$RC"
