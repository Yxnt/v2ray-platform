GO ?= go

.PHONY: build test fmt run-control-plane run-agent

build:
	$(GO) build ./...

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

run-control-plane:
	$(GO) run ./cmd/control-plane

run-agent:
	$(GO) run ./cmd/node-agent
