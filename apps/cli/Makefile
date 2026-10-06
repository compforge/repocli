.PHONY: build install test lint lint-ci fix
VERSION := $(shell cat ../../VERSION)
LDFLAGS := -X github.com/compforge/repocli/apps/cli/internal/cli.Version=$(VERSION)
build:
	go build -ldflags "$(LDFLAGS)" -o ../../bin/repocli ./cmd/repocli
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/repocli
test:
	go test ./...
lint:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...
lint-ci: lint
fix:
	gofmt -w cmd internal
