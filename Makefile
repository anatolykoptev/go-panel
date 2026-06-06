GOWORK=off
GO=go
TEMPL=templ
GOLANGCI_LINT=golangci-lint

.PHONY: generate build test lint check clean

generate:
	$(TEMPL) generate ./...

build: generate
	GOWORK=off $(GO) build ./...

test: generate
	GOWORK=off $(GO) test -race ./...

lint: generate
	GOWORK=off $(GOLANGCI_LINT) run ./...

check: lint test

clean:
	find . -name '*_templ.go' -delete

fmt:
	$(TEMPL) fmt ./...
	GOWORK=off $(GO) fmt ./...
