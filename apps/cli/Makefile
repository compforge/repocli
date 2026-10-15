.PHONY: build install test lint lint-ci fix
VERSION := $(shell cat ../../VERSION)
LDFLAGS := -X github.com/compforge/repocli/apps/cli/internal/cli.Version=$(VERSION)
build:
	go build -ldflags "$(LDFLAGS)" -o ../../bin/repocli ./cmd/repocli
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/repocli
test:
	node --test internal/viewer/*_test.cjs
	go test ./...
lint:
	node --check internal/viewer/static/app.js
	node --check internal/viewer/static/reading-map.js
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...
lint-ci: lint
fix:
	gofmt -w cmd internal
