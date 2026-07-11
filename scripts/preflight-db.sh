#!/usr/bin/env bash
# scripts/preflight-db.sh — provisions an ephemeral Postgres so auth's
# TEST_DATABASE_URL-gated tests (auth/account_test.go) actually run instead
# of silently skipping. .github/workflows/ci.yml never set TEST_DATABASE_URL,
# so TestPgxAccountStore_RoundTrip has never run in CI, for any PR, since it
# was added in PR #34 — a "skip-to-green" gap (see the PR that added this
# script for the writeup).
#
# Ported from go-grad's scripts/preflight-db.sh, simplified: go-panel has
# exactly ONE DB-gated package today (auth, confirmed via
# `grep -rl TEST_DATABASE_URL --include=*_test.go .`) and ZERO
# `CREATE EXTENSION` requirements (confirmed via
# `grep -rn "CREATE EXTENSION" --include=*.go .` — no hits). go-grad's
# per-package-database machinery exists to solve a real, investigated
# cross-package data conflict (see go-grad's script header) that does not
# exist here with a single package — so this script provisions ONE throwaway
# database directly via POSTGRES_DB at container start (no manual
# CREATE/DROP DATABASE round-trip needed) and uses vanilla `postgres:17`,
# not `krolik-postgres-age:17` (that image is for pgvector/AGE, which
# go-panel needs neither). If a second DB-gated package appears later,
# revisit go-grad's pattern instead of growing this ad hoc.
#
# Usage: scripts/preflight-db.sh (invoked by `make preflight-db` and by CI)
set -euo pipefail

CONTAINER="go-panel-preflight-pg-$$"
IMAGE="postgres:17"
DB_USER="panel"
DB_PASSWORD="panel"
DB_NAME="panel_test"

# Reap any container orphaned by a SIGKILL'd previous run (e.g. a cancelled
# CI job — `trap cleanup EXIT` never fires on SIGKILL, so a `--rm` container
# can survive until stopped). Matches only this script's own naming
# convention, never touches an unrelated container.
orphans=$(docker ps -q --filter "name=go-panel-preflight-pg-")
if [ -n "$orphans" ]; then
	echo "--- Reaping orphaned preflight-pg container(s) from a previous run ---"
	echo "$orphans" | xargs -r docker rm -f >/dev/null
fi

cleanup() {
	echo "--- Stopping ephemeral Postgres (${CONTAINER}) ---"
	# --rm means stop also removes the container (and with it panel_test —
	# there is nothing else to drop separately, unlike go-grad which reuses
	# one container across many packages/databases and must clean between
	# iterations).
	docker stop "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "--- Spinning up ephemeral Postgres (${IMAGE}) on a docker-assigned port ---"
# -p 127.0.0.1::5432 lets docker pick a free host port so a concurrent CI run
# for another repo on this shared runner host never collides on a fixed port.
docker run -d --rm \
	--name "$CONTAINER" \
	-e POSTGRES_USER="$DB_USER" \
	-e POSTGRES_PASSWORD="$DB_PASSWORD" \
	-e POSTGRES_DB="$DB_NAME" \
	-p 127.0.0.1::5432 \
	"$IMAGE" >/dev/null

PORT=$(docker port "$CONTAINER" 5432/tcp | head -1 | cut -d: -f2)
echo "--- Waiting for Postgres to be ready on 127.0.0.1:${PORT} (up to 30s) ---"
# `docker exec ... pg_isready` with NO -h defaults to the unix socket, which
# the official postgres image's TEMPORARY init server (used to run init
# scripts, started with listen_addresses='') answers on — a false positive
# that reports ready before the FINAL, TCP-listening server has started.
# Forcing `-h 127.0.0.1` makes pg_isready dial TCP inside the container's own
# namespace, so it only succeeds once the real server — the one
# TEST_DATABASE_URL below actually connects to — is accepting connections.
ready=0
for _ in $(seq 1 30); do
	if docker exec "$CONTAINER" pg_isready -h 127.0.0.1 -p 5432 -U "$DB_USER" -q 2>/dev/null; then
		ready=1
		break
	fi
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	echo "FATAL: Postgres (${CONTAINER}) did not become ready within 30s — aborting." >&2
	docker logs "$CONTAINER" 2>&1 | tail -30 >&2 || true
	exit 1
fi
echo "--- Postgres ready ---"

echo "=== auth ==="
# GOMAXPROCS=2 (not the default = all cores): krolik is a shared 4-core box
# also running prod services + other repos' CI — this step must not starve a
# concurrent build. `-race` already multiplies CPU/memory overhead on its
# own, so capping cores here matters more than it looks.
GOMAXPROCS=2 \
	GOWORK=off \
	TEST_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${PORT}/${DB_NAME}?sslmode=disable" \
	go test -race -count=1 ./auth/...
