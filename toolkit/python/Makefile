UV ?= uv
.PHONY: format lint lint-ci test build
format:
	$(UV) run ruff format repokit tests
lint:
	$(UV) run ruff check repokit tests
	$(UV) run ruff format --check repokit tests
	$(UV) run mypy repokit
lint-ci: lint
test:
	$(UV) run pytest $(TEST_FILES)
build:
	$(UV) build
