.PHONY: build test lint lint-ci fix generate-languages
build:
	go build ./...
test:
	go test ./...
lint:
	@test -z "$$(gofmt -l *.go internal)" || (gofmt -l *.go internal; exit 1)
	go vet ./...
lint-ci: lint
fix:
	gofmt -w *.go internal
generate-languages:
	go test ./internal/project -run TestLanguageCatalog -update-language-catalog
