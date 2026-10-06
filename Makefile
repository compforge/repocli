.PHONY: build install test lint fix

build:
	go build -o bin/repocli ./cmd/repocli

install:
	go install ./cmd/repocli

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l *.go cmd internal)" || (gofmt -l *.go cmd internal; exit 1)
	go vet ./...

fix:
	gofmt -w *.go cmd internal
