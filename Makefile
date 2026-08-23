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

test: generate
	go test ./...

run: generate
	go run ./server/cmd/laminara-server start

clean:
	rm -rf gen bin
