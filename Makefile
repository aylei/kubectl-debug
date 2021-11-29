.PHONY: build kubectl-debug debug-agent debug-agent-docker-image check

LDFLAGS = $(shell ./version.sh)
GOENV  := GO15VENDOREXPERIMENT="1" GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64
GO := $(GOENV) go

default: build

build: kubectl-debug debug-agent-docker-image

kubectl-debug:
	GO111MODULE=on CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o kubectl-debug cmd/plugin/main.go

debug-agent-docker-image: debug-agent
	docker build . -t jamesgrantmediakind/debug-agent:latest

debug-agent:
	$(GO) build -ldflags '$(LDFLAGS)' -o debug-agent cmd/agent/main.go

check:
	find . -iname '*.go' -type f | grep -v /vendor/ | xargs gofmt -l
	GO111MODULE=on go test -v -race ./...
	$(GO) vet ./...
