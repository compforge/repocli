.PHONY: build test lint fix

build:
	go build -o bin/repocli .

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l main.go cmd internal)" || (gofmt -l main.go cmd internal; exit 1)
	go vet ./...

fix:
	gofmt -w main.go cmd internal
