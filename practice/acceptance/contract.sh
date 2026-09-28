#!/bin/sh
# contract.sh — the acceptance checks that are about the build and the container,
# not about HTTP: C-BUILD-1..5 and C-RUN-1..3 of CONTRACT.md.
#
# Everything here is observed from outside the service: the image, the container,
# the socket it listens on, and the answers it gives. No file of the service is
# read for its own sake — the Dockerfile is read only for the two facts the
# contract fixes about it (pinned base image, and which Go toolchain to use for
# `go version -m`), and the binary is inspected through the toolchain's own
# build-information reader.
#
# Usage:  sh practice/acceptance/contract.sh
# Env:    PRACTICE_IMAGE   image tag to build and run (default: practice)
#         PRACTICE_COMMIT  git revision to export for the isolated-context build
#                          (default: HEAD)

set -u

IMAGE=${PRACTICE_IMAGE:-practice}
ACC_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$ACC_DIR/../.." && pwd)
SERVICE=$ROOT/practice/service
COMMIT=${PRACTICE_COMMIT:-HEAD}

PASS=0
FAIL=0
WORK=$(mktemp -d "${TMPDIR:-/tmp}/practice-acceptance.XXXXXX")
CONTAINERS=""

pass() { PASS=$((PASS + 1)); printf '  PASS  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  FAIL  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }
check() { # check <contract id> <what> ; body on stdin, non-zero exit fails
	id=$1; what=$2
	if out=$(sh -c "$(cat)" 2>&1); then
		pass "$id  $what"
	else
		fail "$id  $what" "$(echo "$out" | tail -5)"
	fi
}

cleanup() {
	for c in $CONTAINERS; do
		docker rm -f "$c" >/dev/null 2>&1
	done
	docker network rm practice-acceptance-net >/dev/null 2>&1
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM
new_container() { # new_container <image> <extra docker run flags...>
	img=$1; shift
	c=$(docker run -d "$@" "$img" 2>&1) || { printf 'docker run failed: %s\n' "$c" >&2; return 1; }
	CONTAINERS="$CONTAINERS $c"
	printf '%s' "$c"
}
# netns_get runs wget inside the container's own network namespace, which is the
# only place the service is reachable under --network=none.
netns_get() { docker exec "$1" wget -q -O- "http://127.0.0.1:8080$2" 2>/dev/null; }
netns_reachable() { docker exec "$1" wget -q -O /dev/null "http://127.0.0.1:8080/health" >/dev/null 2>&1; }

printf '== practice acceptance: build and runtime contract ==\n'
printf 'image tag: %s\nrevision:  %s\n\n' "$IMAGE" "$COMMIT"

# ---------------------------------------------------------------- C-BUILD-1
printf 'C-BUILD-1  service source lives in practice/service/ with Dockerfile, go.mod, go.sum, vendor/\n'
missing=
for f in Dockerfile go.mod go.sum; do
	[ -s "$SERVICE/$f" ] || missing="$missing $f"
done
[ -d "$SERVICE/vendor" ] || missing="$missing vendor/"
if [ -n "$missing" ]; then
	fail "C-BUILD-1  Dockerfile, go.mod, go.sum, vendor/ present and non-empty" "missing:$missing"
else
	pass "C-BUILD-1  Dockerfile, go.mod, go.sum, vendor/ present and non-empty"
fi

# C-BUILD-3 also fixes that base images are pinned by tag, and the pinned tag of
# the first stage is the Go toolchain this script uses to read the binary's build
# information (the same image the build itself needs, so it is always cached).
GO_IMAGE=$(awk '/^FROM/ {print $2; exit}' "$SERVICE/Dockerfile" 2>/dev/null)
FROM_LINES=$(grep '^FROM' "$SERVICE/Dockerfile" 2>/dev/null | tr '\n' ';')
if [ -z "$GO_IMAGE" ]; then
	fail "C-BUILD-3  base images pinned by tag" "no FROM line in the Dockerfile"
else
	bad=
	printf '%s' "$FROM_LINES" | tr ';' '\n' | while read -r line; do
		[ -n "$line" ] || continue
		ref=$(echo "$line" | awk '{print $2}')
		case "$ref" in
		*:*|*@*) : ;;
		*) echo "unpinned: $ref" ;;
		esac
		case "$ref" in
		*:latest) echo ":latest: $ref" ;;
		esac
	done >"$WORK/unpinned.txt"
	bad=$(cat "$WORK/unpinned.txt")
	if [ -n "$bad" ]; then
		fail "C-BUILD-3  base images pinned by tag" "$bad"
	else
		pass "C-BUILD-3  base images pinned by tag ($FROM_LINES)"
	fi
fi

# ---------------------------------------------------------------- C-BUILD-2
# The exact build command of C-BUILD-2, from inside the service directory.
printf 'C-BUILD-2  docker build --network=none -t %s . from practice/service/\n' "$IMAGE"
if (cd "$SERVICE" && docker build --network=none -t "$IMAGE" . ) >"$WORK/build.log" 2>&1; then
	pass "C-BUILD-2  builds with the exact C-BUILD-2 command"
else
	fail "C-BUILD-2  builds with the exact C-BUILD-2 command" "$(tail -5 "$WORK/build.log")"
fi

# ---------------------------------------------------------------- C-BUILD-3
# No step may download anything. --network=none removes the network from every
# RUN step, and --no-cache re-runs every step from the pinned, pre-pulled base
# image, so a successful build proves nothing was fetched.
printf 'C-BUILD-3  no step of the build downloads anything\n'
ISOLATED=$WORK/isolated
mkdir -p "$ISOLATED"
if (cd "$ROOT" && git archive "$COMMIT" practice/service) | tar -x -C "$ISOLATED" 2>/dev/null; then
	if [ -d "$ISOLATED/practice/service" ]; then
		pass "C-BUILD-2  build context is the service directory alone (git archive $COMMIT)"
		if (cd "$ISOLATED/practice/service" &&
			docker build --network=none --no-cache -t "$IMAGE-isolated" .) >"$WORK/build-isolated.log" 2>&1; then
			pass "C-BUILD-3  builds with --network=none --no-cache from the service directory alone"
			if grep -qiE 'go: downloading|Downloading |attempt to download|pulling from' "$WORK/build-isolated.log"; then
				fail "C-BUILD-3  no download during the build" "$(grep -iE 'download' "$WORK/build-isolated.log" | head -3)"
			else
				pass "C-BUILD-3  no download during the build"
			fi
		else
			fail "C-BUILD-3  builds with --network=none --no-cache from the service directory alone" \
				"$(tail -5 "$WORK/build-isolated.log")"
		fi
	else
		fail "C-BUILD-2  build context is the service directory alone" "practice/service missing from git archive $COMMIT"
	fi
else
	fail "C-BUILD-2  build context is the service directory alone" "git archive $COMMIT failed"
fi
if [ -s "$SERVICE/vendor/modules.txt" ]; then
	pass "C-BUILD-3  vendor/modules.txt present ($(grep -c '^# ' "$SERVICE/vendor/modules.txt") modules)"
else
	fail "C-BUILD-3  vendor/modules.txt present" "vendor/modules.txt missing or empty"
fi

# ------------------------------------------------- C-BUILD-4 and C-BUILD-5
# The binary's own build information is the only place the dependency list and the
# CGO setting are observable from outside the service.
printf 'C-BUILD-4  HTTP served by net/http, no third-party router or framework\n'
printf 'C-BUILD-5  SQLite through a pure-Go driver, built with CGO_ENABLED=0\n'
mkdir -p "$WORK/bin"
EXTRACTED=""
if CID=$(docker create "$IMAGE" 2>/dev/null); then
	CONTAINERS="$CONTAINERS $CID"
	# The binary's path is the implementer's choice, so find the executable
	# rather than assume one. busybox find counts 512-byte blocks.
	CANDIDATES=$(docker run --rm --network=none --entrypoint sh "$IMAGE" -c \
		'find / -xdev -type f -size +2048 2>/dev/null' 2>/dev/null)
	for c in $CANDIDATES; do
		base=$(basename "$c")
		if docker cp "$CID:$c" "$WORK/bin/$base" >/dev/null 2>&1 &&
			docker run --rm --network=none -v "$WORK/bin":/out:ro -w /out "$GO_IMAGE" \
				go version -m "/out/$base" >"$WORK/buildinfo.txt" 2>/dev/null; then
			EXTRACTED=$base
			break
		fi
	done
	docker rm "$CID" >/dev/null 2>&1
	CONTAINERS=$(echo $CONTAINERS | sed "s/ $CID//")
fi
if [ -z "$EXTRACTED" ]; then
	fail "C-BUILD-4  binary build information readable" "no executable Go binary found in the image"
	fail "C-BUILD-5  CGO_ENABLED=0 and a pure-Go SQLite driver" "no executable Go binary found in the image"
else
	pass "C-BUILD-4  binary build information readable ($EXTRACTED)"
	# The standard library's own symbols are in the binary whatever the build
	# flags, so their presence is evidence that net/http is linked in.
	NETHTTP=$(docker run --rm --network=none -v "$WORK/bin":/out:ro -w /out "$GO_IMAGE" \
		sh -c "grep -ao 'net/http\\.[A-Za-z0-9_.()*]*' /out/$EXTRACTED | sort -u | wc -l" 2>/dev/null | tr -d ' ')
	if [ "${NETHTTP:-0}" -gt 0 ]; then
		pass "C-BUILD-4  net/http is linked into the binary ($NETHTTP distinct net/http symbols)"
	else
		fail "C-BUILD-4  net/http is linked into the binary" "no net/http symbols found"
	fi
	ROUTERS=$(grep '^dep' "$WORK/buildinfo.txt" | awk '{print $2}' |
		grep -E 'gin-gonic|labstack|gorilla|go-chi/chi|gofiber|httprouter|julienschmidt|go-zero|kratos|go-restful|unrolled|valyala|fasthttp|revel|beego|echo\.v[0-9]|httprouter\.v[0-9]' || true)
	if [ -n "$ROUTERS" ]; then
		fail "C-BUILD-4  no third-party HTTP router or framework in the dependency list" "$ROUTERS"
	else
		pass "C-BUILD-4  no third-party HTTP router or framework in the dependency list"
	fi
	VENDORED=$(find "$SERVICE/vendor" -type d 2>/dev/null |
		grep -E '/(gin|echo|chi|fiber|gorilla|httprouter|router|mux)(/|$)' || true)
	if [ -n "$VENDORED" ]; then
		fail "C-BUILD-4  no router package vendored" "$VENDORED"
	else
		pass "C-BUILD-4  no router package vendored"
	fi
	if grep -q 'CGO_ENABLED=0' "$WORK/buildinfo.txt"; then
		pass "C-BUILD-5  built with CGO_ENABLED=0"
	else
		fail "C-BUILD-5  built with CGO_ENABLED=0" "$(grep '^build' "$WORK/buildinfo.txt" | tr '\n' ' ')"
	fi
	if grep -q 'modernc.org/sqlite' "$WORK/buildinfo.txt" &&
		! grep -q 'mattn/go-sqlite3' "$WORK/buildinfo.txt"; then
		pass "C-BUILD-5  SQLite through modernc.org/sqlite (pure Go), no mattn/go-sqlite3"
	else
		fail "C-BUILD-5  SQLite through modernc.org/sqlite (pure Go), no mattn/go-sqlite3" \
			"$(grep '^dep' "$WORK/buildinfo.txt" | tr '\n' ' ')"
	fi
fi

# ---------------------------------------------------------------- C-RUN-1
# The exact run command of C-RUN-1, detached only so this script can go on.
printf 'C-RUN-1  runs with the exact C-RUN-1 command, no extra flags\n'
SVC=$(new_container "$IMAGE" --network=none --cpus=1 --memory=512m -p 8080:8080) || SVC=""
if [ -n "$SVC" ]; then
	sleep 1
	STATE=$(docker inspect "$SVC" --format '{{.State.Status}}' 2>/dev/null)
	MOUNTS=$(docker inspect "$SVC" --format '{{len .Mounts}}' 2>/dev/null)
	CMDLINE=$(docker inspect "$IMAGE" --format '{{json .Config.Entrypoint}} {{json .Config.Cmd}}' 2>/dev/null)
	if [ "$STATE" = "running" ] && [ "$MOUNTS" = "0" ] && [ -n "$CMDLINE" ] && [ "$CMDLINE" != "null null" ]; then
		pass "C-RUN-1  container running from the exact command, no mounts, no arguments needed"
	else
		fail "C-RUN-1  container running from the exact command, no mounts, no arguments needed" \
			"state=$STATE mounts=$MOUNTS cmd=$CMDLINE"
	fi
	if [ "$(netns_get "$SVC" /health)" = '{"status": "ok"}' ]; then
		pass "C-RUN-1  GET /health inside the container is {\"status\": \"ok\"}"
	else
		fail "C-RUN-1  GET /health inside the container is {\"status\": \"ok\"}" "$(netns_get "$SVC" /health)"
	fi
	printf '' # keep the shell quiet about the port-publishing warning
	PORTS=$(docker inspect "$SVC" --format '{{json .NetworkSettings.Ports}}' 2>/dev/null)
	printf '        note: --network=none gives the container only its loopback, so the\n'
	printf '        %s of C-RUN-1 has nothing to publish; %s\n' "-p 8080:8080" "$PORTS"
else
	fail "C-RUN-1  container starts from the exact command" "docker run failed"
fi

# ---------------------------------------------------------------- C-RUN-2
printf 'C-RUN-2  listens on 8080 on all interfaces and answers /health within 10 s\n'
READY_SVC=$(new_container "$IMAGE" --network=none --cpus=1 --memory=512m -p 8080:8080) || READY_SVC=""
if [ -n "$READY_SVC" ]; then
	START=$(date +%s)
	READY=""
	i=0
	while [ $i -lt 100 ]; do
		if [ "$(netns_get "$READY_SVC" /health)" = '{"status": "ok"}' ]; then
			READY=$(($(date +%s) - START))
			break
		fi
		sleep 0.1
		i=$((i + 1))
	done
	if [ -n "$READY" ] && [ "$READY" -le 10 ]; then
		pass "C-RUN-2  answers GET /health ${READY}s after docker run (limit 10s)"
	else
		fail "C-RUN-2  answers GET /health within 10s of container start" "not ready after 10s"
	fi
	# The listening socket, seen from inside the container: 00000000:1F90 (or the
	# dual-stack all-zero form in tcp6) is a wildcard bind, 0100007F:1F90 would be
	# loopback only.
	LISTEN=$(docker exec "$READY_SVC" sh -c \
		"cat /proc/net/tcp /proc/net/tcp6 2>/dev/null | awk '\$4 == \"0A\" && \$2 ~ /:1F90\$/ {print \$2}'" 2>/dev/null)
	case "$LISTEN" in
	*00000000:1F90|*00000000000000000000000000000000:1F90)
		pass "C-RUN-2  the listening socket on 8080 is bound to all interfaces ($LISTEN)"
		;;
	"")
		fail "C-RUN-2  the listening socket on 8080 is bound to all interfaces" "no listening socket on port 8080 found"
		;;
	*)
		fail "C-RUN-2  the listening socket on 8080 is bound to all interfaces" "bound to $LISTEN, not a wildcard"
		;;
	esac
else
	fail "C-RUN-2  readiness" "docker run failed"
fi

# All interfaces, end to end: the same image on a network with a real interface,
# addressed by its container address, must answer.
docker network create practice-acceptance-net >/dev/null 2>&1
BRIDGE=$(docker run -d --network practice-acceptance-net "$IMAGE" 2>/dev/null)
CONTAINERS="$CONTAINERS $BRIDGE"
if [ -n "$BRIDGE" ]; then
	IP=$(docker inspect "$BRIDGE" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null)
	# The image is alpine-based, so its own busybox wget is the probe.
	BODY=$(docker run --rm --network practice-acceptance-net --entrypoint wget "$IMAGE" -q -O- \
		"http://$IP:8080/health" 2>/dev/null)
	if [ "$BODY" = '{"status": "ok"}' ]; then
		pass "C-RUN-2  reachable on a non-loopback address ($IP:8080), so the bind is not loopback-only"
	else
		fail "C-RUN-2  reachable on a non-loopback address" "got '$BODY' from $IP:8080"
	fi
	docker rm -f "$BRIDGE" >/dev/null 2>&1
	CONTAINERS=$(echo $CONTAINERS | sed "s/ $BRIDGE//")
else
	fail "C-RUN-2  reachable on a non-loopback address" "could not start a container on a bridge network"
fi

# ---------------------------------------------------------------- C-RUN-3
printf 'C-RUN-3  starts from an empty database on every fresh container\n'
FRESH1=$(new_container "$IMAGE" --network=none --cpus=1 --memory=512m -p 8080:8080) || FRESH1=""
FRESH2=""
if [ -n "$FRESH1" ]; then
	DBPATHS=$(docker exec "$FRESH1" sh -c 'find / -xdev -type f \( -name "*.db" -o -name "*.sqlite*" \) 2>/dev/null' 2>/dev/null)
	DBPATH=$(echo "$DBPATHS" | head -1)
	OWNER=$(docker exec "$FRESH1" id 2>/dev/null)
	# A second fresh container with the first one's leftovers already in place
	# must still start clean: C-RUN-3 asks for an empty database every time.
	STALE=$(new_container "$IMAGE" --network=none --cpus=1 --memory=512m -p 8080:8080) || STALE=""
	[ -n "$STALE" ] && docker rm -f "$STALE" >/dev/null 2>&1
	STALE=$(docker create --network=none --cpus=1 --memory=512m -p 8080:8080 "$IMAGE" 2>/dev/null)
	CONTAINERS="$CONTAINERS $STALE"
	if [ -n "$STALE" ]; then
		if [ -n "$DBPATH" ]; then
			docker cp "$FRESH1:$DBPATH" "$WORK/stale.db" >/dev/null 2>&1
			docker cp "$WORK/stale.db" "$STALE:$DBPATH" >/dev/null 2>&1
			uid=$(echo "$OWNER" | sed -n 's/.*uid=\([0-9]*\).*/\1/p')
			gid=$(echo "$OWNER" | sed -n 's/.*gid=\([0-9]*\).*/\1/p')
			# docker cp writes as root, which no previous run of this image would
			# ever have done; restore the ownership the service itself would have.
			docker exec -u 0 "$STALE" chown "${uid:-10001}:${gid:-10001}" "$DBPATH" 2>/dev/null
			pass "C-RUN-3  located the container's own database file ($DBPATH, $OWNER)"
		else
			printf '        note: no database file found inside the container yet (WI-1 has no\n'
			printf '        data routes); checking only that a fresh container starts clean\n'
		fi
		if docker start "$STALE" >/dev/null 2>&1; then
			sleep 1
			if [ "$(netns_get "$STALE" /health)" = '{"status": "ok"}' ]; then
				pass "C-RUN-3  a fresh container starts and answers even with a previous run's file in place"
			else
				fail "C-RUN-3  a fresh container starts and answers even with a previous run's file in place" \
					"$(netns_get "$STALE" /health)"
			fi
			# And the same with a corrupt leftover, which an empty database must
			# never have to read.
			FRESH2=$(new_container "$IMAGE" --network=none --cpus=1 --memory=512m -p 8080:8080) || FRESH2=""
			if [ -n "$FRESH2" ] && [ -n "$DBPATH" ]; then
				printf 'this is not a database\n' >"$WORK/garbage.db"
				docker rm -f "$FRESH2" >/dev/null 2>&1
				FRESH2=$(docker create --network=none --cpus=1 --memory=512m -p 8080:8080 "$IMAGE" 2>/dev/null)
				CONTAINERS="$CONTAINERS $FRESH2"
				docker cp "$WORK/garbage.db" "$FRESH2:$DBPATH" >/dev/null 2>&1
				docker exec -u 0 "$FRESH2" chown "${uid:-10001}:${gid:-10001}" "$DBPATH" 2>/dev/null
				if docker start "$FRESH2" >/dev/null 2>&1; then
					sleep 1
					if [ "$(netns_get "$FRESH2" /health)" = '{"status": "ok"}' ]; then
						pass "C-RUN-3  a fresh container starts even with a corrupt leftover file in place"
					else
						fail "C-RUN-3  a fresh container starts even with a corrupt leftover file in place" \
							"$(netns_get "$FRESH2" /health)"
					fi
				else
					fail "C-RUN-3  a fresh container starts even with a corrupt leftover file in place" \
						"container did not start"
				fi
			fi
		else
			fail "C-RUN-3  a fresh container starts with a previous run's file in place" "container did not start"
		fi
	else
		fail "C-RUN-3  a fresh container starts with a previous run's file in place" "docker create failed"
	fi
	# No volume is needed, so nothing survives the container that made it.
	if [ "$(docker inspect "$FRESH1" --format '{{len .Mounts}}' 2>/dev/null)" = "0" ]; then
		pass "C-RUN-3  the C-RUN-1 command mounts nothing, so no data outlives a container"
	else
		fail "C-RUN-3  the C-RUN-1 command mounts nothing" "the container has mounts"
	fi
else
	fail "C-RUN-3  fresh container" "docker run failed"
fi

printf '\n== %d passed, %d failed ==\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
