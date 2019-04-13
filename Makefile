.PHONY: build

GOENV  := GO15VENDOREXPERIMENT="1" GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64
GO := $(GOENV) go build

default: build

build: plugin agent

plugin:
	$(GO) -o kubectl-debug cmd/agent/main.go

agent:
	$(GO) -o debug-agent cmd/agent/main.go
