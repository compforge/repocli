.PHONY: build install test lint fix

build:
	go build -o bin/repocli .

install:
	go install .

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l main.go cmd internal)" || (gofmt -l main.go cmd internal; exit 1)
	go vet ./...

fix:
	gofmt -w main.go cmd internal
