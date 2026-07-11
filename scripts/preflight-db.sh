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

TEST_DSN="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${PORT}/${DB_NAME}?sslmode=disable"

# Scoped to a derived set of tests, not the whole ./auth/... package.
# Measured live on this runner (2026-07-10): setting TEST_DATABASE_URL to any
# new value invalidates Go's test cache for the WHOLE package (confirmed
# empirically — it is not tracked per-test), so a plain `go test ./auth/...`
# here forces every test in the package to execute fresh, including ~20
# bcrypt.GenerateFromPassword/CompareHashAndPassword calls at
# auth.DefaultBcryptCost=12 (each individually measured 10-30s under -race on
# this ARM box). That is genuinely slow enough on its own (3m41s in an
# unconstrained interactive shell) to blow Go's default 10m test timeout once
# this step also runs under actions-runner-go-panel.service's CPUQuota=150%
# cgroup on a contended shared box — it did, on the first real CI run of this
# script (job 29133254017: "panic: test timed out after 10m0s", mid
# TestBcrypt_RevocationFailClosed_DeniesOnTransientError, not even a
# DB-touching test).
#
# The run-set is DERIVED from the actual TEST_DATABASE_URL gating token in
# source, never a hardcoded name prefix — same philosophy as go-grad's own
# preflight-db.sh, which derives its package list via
# `grep -rlE '(FEEDSTORE|VECSTORE|...)_TEST_DSN' --include=*_test.go .`
# specifically so a differently-named future DB-gated test is picked up
# automatically instead of silently skipped (an earlier version of this
# script hardcoded `-run '^TestPgxAccountStore_'` with a guard that only
# checked "does a test matching that literal prefix still exist" — a
# tautology that could never catch a NEW test gated on TEST_DATABASE_URL
# under a DIFFERENT name; caught in review, fixed here).
#
# File-granularity, not per-function: several of the TOTP tests share a
# setup helper in account_test.go that itself checks TEST_DATABASE_URL,
# rather than every test checking it inline — a per-function text match on
# "does THIS func's own body mention TEST_DATABASE_URL" would miss those
# (the reference is in the helper's body, not the caller's). Collecting
# every top-level `func Test*` declared in any *_test.go file under ./auth
# that mentions TEST_DATABASE_URL anywhere cannot miss that indirection —
# the failure mode left is strictly over-inclusion (a non-DB test sharing a
# file with a DB-gated one also gets run), never a silently-skipped DB test.
db_files=$(grep -rl 'TEST_DATABASE_URL' --include='*_test.go' ./auth || true)
db_funcs=$(printf '%s\n' "$db_files" | xargs -r grep -hoE '^func (Test[A-Za-z0-9_]+)' | awk '{print $2}' | sort -u)
if [ -z "$db_funcs" ]; then
	echo "FATAL: no ./auth/*_test.go file references TEST_DATABASE_URL, so" >&2
	echo "there is nothing to derive a run-set from. Either the gating" >&2
	echo "convention was renamed (update this script to match the new name)" >&2
	echo "or DB-gated coverage genuinely dropped to zero. Refusing to" >&2
	echo "silently report success while running zero DB-gated tests." >&2
	exit 1
fi
run_pattern="^($(printf '%s' "$db_funcs" | paste -sd'|'))\$"

# go test -list is the staleness oracle, not another hardcoded assumption:
# it only sees what actually BUILDS and uses the same regex dialect as
# -run, so a zero match here means either the pattern built above is wrong
# or ./auth has a compile error (this script redirects -list's stderr to
# /dev/null, so a build failure would otherwise fail silently rather than
# with a clear signal) — not just "someone renamed a test".
listed=$(GOWORK=off go test -list "$run_pattern" ./auth/... 2>/dev/null | grep -c '^Test' || true)
if [ "$listed" -eq 0 ]; then
	echo "FATAL: derived pattern '${run_pattern}' (from: $(printf '%s' "$db_funcs" | tr '\n' ' ')) matched zero tests via 'go test -list ./auth/...', even though grep found the TEST_DATABASE_URL reference(s) in source." >&2
	echo "Most likely a compile error in ./auth (stderr is suppressed on the -list call above) — run 'go build ./auth/...' directly to see it. Could also mean the derived names above don't actually exist as runnable tests." >&2
	echo "Refusing to silently report success while running zero DB-gated tests." >&2
	exit 1
fi

echo "=== auth (${listed} test(s) referencing TEST_DATABASE_URL, live Postgres) ==="
GOWORK=off \
	TEST_DATABASE_URL="$TEST_DSN" \
	go test -race -count=1 -timeout=15m -run "$run_pattern" ./auth/...
