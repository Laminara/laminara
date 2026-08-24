.PHONY: generate lint tidy build test run clean

generate:
	buf generate

lint:
	buf lint
	go vet ./...

tidy:
	go mod tidy

build: generate
	go build ./...
	go build -trimpath -o bin/laminara-server ./server/cmd/laminara-server

test: generate
	go test ./...

run: generate
	go run ./server/cmd/laminara-server start

clean:
	rm -rf gen bin
