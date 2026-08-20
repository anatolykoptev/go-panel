# go-panel — developer quality gate. `make check` is the pre-merge contract.
.RECIPEPREFIX = >
.PHONY: check build vet test race lint vuln gen preflight-db

# GOWORK=off — this repo must build standalone; a workspace file in an outer
# directory would otherwise pull unrelated modules into every go command.
export GOWORK = off

check: build vet race lint vuln

build:
> go build ./...

vet:
> go vet ./...

test:
> go test -timeout=20m ./...

race:
> go test -race -timeout=20m ./...

lint:
> golangci-lint run

# Pinned (not @latest) so a new govulncheck release can't silently change what
# gates the merge — bump the pin deliberately.
vuln:
> go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

# gen: regenerate all _templ.go files from their .templ sources.
# Requires: go tool templ (tool directive in go.mod, resolved via go mod tidy).
gen:
> go tool templ generate ./...

# preflight-db: provisions an ephemeral Postgres (docker) and runs auth's
# TEST_DATABASE_URL-gated tests against it — see scripts/preflight-db.sh.
# Not part of `check`: the build/vet/race/lint/vuln gate above runs anywhere
# with no external service; this one needs docker and is invoked as its own
# CI step for the same reason (a red DB step should not masquerade as a
# generic build/test failure).
preflight-db:
> ./scripts/preflight-db.sh
