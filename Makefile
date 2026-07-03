.PHONY: build install test race tapes tape tapes-clean fmt vet help

# Dev build — what the maintainer runs after every Go edit.
# `make build` produces `./octoscope` in the repo root: the
# iterate-and-test binary. Run it as `./octoscope` from the repo to
# exercise your changes.
#
# The global `octoscope` on $PATH is the *Homebrew release*, managed
# by brew (`brew upgrade gfazioli/tap/octoscope` after a tagged
# release). `make build` deliberately does NOT touch it, so a
# `brew upgrade` always lands cleanly.
#
# The optional second target is OFF by default (BINDIR empty). Set
# BINDIR only if you want a global install of the *dev* build:
#
#   make build BINDIR=/usr/local/bin           # some dir on $PATH
#
# NEVER point BINDIR at Homebrew's own bin dir (/opt/homebrew/bin):
# it overwrites brew's symlink with a plain file, which shadows every
# future `brew upgrade` (the new version lands in the Cellar but
# never reaches $PATH). The second build is skipped silently when
# BINDIR is empty or the directory doesn't exist.
BINDIR ?=

build:
	go build -o ./octoscope .
	@if [ -n "$(BINDIR)" ] && [ -d "$(BINDIR)" ]; then \
		echo "→ also building into $(BINDIR)/octoscope"; \
		go build -o $(BINDIR)/octoscope .; \
	fi

install:
	go install .

# Race-checked unit suite. Same command we run in CI so local
# results match what the workflow will report.
test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# tapes/ — vhs-driven smoke session + asset generator.
# Renders every .tape into tapes/out/*.gif and *.png. Requires
# `vhs` on $PATH (`brew install vhs`) and a valid $GITHUB_TOKEN so
# octoscope's real fetch path actually returns data; the tapes are
# not mocked.
TAPES_DIR := tapes
TAPES_OUT := $(TAPES_DIR)/out

tapes: tapes-clean
	@command -v vhs >/dev/null 2>&1 || { \
		echo "vhs not installed — run 'brew install vhs' first."; \
		exit 1; \
	}
	@mkdir -p $(TAPES_OUT)
	@for tape in $(TAPES_DIR)/*.tape; do \
		echo "→ rendering $$tape"; \
		(cd $(TAPES_DIR) && vhs $$(basename $$tape)); \
	done
	@echo "✓ tapes rendered into $(TAPES_OUT)"

# Render a single tape: `make tape NAME=overview`.
tape:
	@if [ -z "$(NAME)" ]; then \
		echo "usage: make tape NAME=<tape-base-name>"; \
		exit 2; \
	fi
	@command -v vhs >/dev/null 2>&1 || { \
		echo "vhs not installed — run 'brew install vhs' first."; \
		exit 1; \
	}
	@mkdir -p $(TAPES_OUT)
	(cd $(TAPES_DIR) && vhs $(NAME).tape)

tapes-clean:
	@rm -rf $(TAPES_OUT)

help:
	@echo "Targets:"
	@echo "  build       — dev build → ./octoscope (also \$$BINDIR/octoscope when BINDIR set; never point it at brew's bin)"
	@echo "  install     — go install"
	@echo "  test        — go test ./..."
	@echo "  race        — go test -race ./..."
	@echo "  fmt         — gofmt -w ."
	@echo "  vet         — go vet ./..."
	@echo "  tapes       — render every tapes/*.tape via vhs (needs \$$GITHUB_TOKEN)"
	@echo "  tape NAME=x — render tapes/x.tape only"
	@echo "  tapes-clean — wipe tapes/out/"
