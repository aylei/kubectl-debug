.PHONY: build plugin agent check

LDFLAGS = $(shell ./version.sh)
GOENV  := GO15VENDOREXPERIMENT="1" GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64
GO := $(GOENV) go

default: build

build: plugin agent-docker

plugin:
	GO111MODULE=on CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o kubectl-debug cmd/plugin/main.go

agent-docker: agent
	docker build . -t aylei/debug-agent:0.0.2

agent:
	$(GO) build -ldflags '$(LDFLAGS)' -o debug-agent cmd/agent/main.go

check:
	find . -iname '*.go' -type f | grep -v /vendor/ | xargs gofmt -l
	GO111MODULE=on go test -v -race ./...
	$(GO) vet ./...
