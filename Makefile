.PHONY: build install test lint fix check generate-languages
COMPONENTS := toolkit/go toolkit/typescript toolkit/python apps/cli

# Component-owned entrypoints also work directly; root checks cover all implementations.
fix lint test:
	@set -e; for component in $(COMPONENTS); do $(MAKE) -C $$component $@; done
check: lint test
build:
	$(MAKE) -C toolkit/go build
	$(MAKE) -C toolkit/typescript build
	$(MAKE) -C toolkit/python build
	$(MAKE) -C apps/cli build
install:
	$(MAKE) -C apps/cli install
generate-languages:
	$(MAKE) -C toolkit/go generate-languages
