.PHONY: build test lint fix

build:
	go build -o bin/repocli ./cmd/repocli

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...

fix:
	gofmt -w cmd internal
