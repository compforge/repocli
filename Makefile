.PHONY: build install test lint fix check generate-languages
COMPONENTS := toolkit/go toolkit/typescript toolkit/python apps/cli

# +rule=Automatic validation calls Component targets independently; root targets must not repeat leaf work.
fix lint test:
	@echo "Root $@: no local code checks; use a leaf Component target or make check for all Components."
# Explicit whole-repository validation, also used by release CI.
check:
	@set -e; for component in $(COMPONENTS); do $(MAKE) -C $$component lint test; done
build:
	$(MAKE) -C toolkit/go build
	$(MAKE) -C toolkit/typescript build
	$(MAKE) -C toolkit/python build
	$(MAKE) -C apps/cli build
install:
	$(MAKE) -C apps/cli install
generate-languages:
	$(MAKE) -C toolkit/go generate-languages
