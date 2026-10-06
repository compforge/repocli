UV ?= uv
.PHONY: fix lint lint-ci test build
fix:
	$(UV) run ruff format repocli tests
	$(UV) run ruff check --fix repocli tests
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
