.PHONY: build plugin agent

GOENV  := GO15VENDOREXPERIMENT="1" GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64
GO := $(GOENV) go build

default: build

build: plugin agent-docker

plugin:
	GO111MODULE=on CGO_ENABLED=0 go build -o kubectl-debug cmd/plugin/main.go

agent-docker: agent
	docker build . -t aylei/debug-agent:latest

agent:
	$(GO) -o debug-agent cmd/agent/main.go
