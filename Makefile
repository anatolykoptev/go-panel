# go-panel — developer quality gate. `make check` is the pre-merge contract.
.RECIPEPREFIX = >
.PHONY: check build vet test race lint vuln gen

# GOWORK=off — this repo must build standalone (krolik has a stray ~/go.work).
export GOWORK = off

check: build vet race lint vuln

build:
> go build ./...

vet:
> go vet ./...

test:
> go test ./...

race:
> go test -race ./...

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
