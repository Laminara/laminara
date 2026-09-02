.PHONY: generate lint tidy build test run clean

VERSION := $(shell cat VERSION)
LDFLAGS := -X github.com/laminara/laminara/server/internal/version.Current=$(VERSION)

generate:
	buf generate
	buf generate --template buf.gen.sdk.yaml

lint:
	buf lint
	go vet ./...
	cd sdk/go && go vet ./...
	@test -z "$$(gofmt -l server sdk)" || { echo "gofmt не выровнял:"; gofmt -l server sdk; exit 1; }

tidy:
	go mod tidy
	cd sdk/go && go mod tidy

build: generate
	go build ./...
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/laminara-server ./server/cmd/laminara-server

test: generate
	go test ./...
	cd sdk/go && go test ./...

run: generate
	go run ./server/cmd/laminara-server start

clean:
	rm -rf gen bin
