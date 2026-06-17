# go-panel — developer quality gate. `make check` is the pre-merge contract.
.RECIPEPREFIX = >
.PHONY: check build vet test race lint vuln

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

vuln:
> go run golang.org/x/vuln/cmd/govulncheck@latest ./...
