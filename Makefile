GO ?= go

.PHONY: build test fmt run-control-plane run-agent deploy-preflight deploy deploy-cloudrun deploy-server-preflight deploy-server

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

deploy-preflight:
	bash deploy/preflight-auto.sh

deploy:
	bash deploy/deploy-auto.sh

deploy-cloudrun:
	bash deploy/deploy-cloudrun.sh

deploy-server-preflight:
	bash deploy/preflight-server.sh

deploy-server:
	bash deploy/deploy-server.sh
