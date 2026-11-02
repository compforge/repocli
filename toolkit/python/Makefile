UV ?= uv
.PHONY: format lint lint-ci test build
format:
	$(UV) run ruff format repocli tests
lint:
	$(UV) run ruff check repocli tests
	$(UV) run ruff format --check repocli tests
	$(UV) run mypy repocli
lint-ci: lint
test:
	$(UV) run pytest $(TEST_FILES)
build:
	$(UV) build
