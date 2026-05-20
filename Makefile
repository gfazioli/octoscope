.PHONY: build install test race tapes tape tapes-clean fmt vet help

# Dual-target build — what the maintainer runs after every Go edit.
# `./octoscope` is the iterate-and-test binary; the binary under
# $(BINDIR) is what `octoscope` from anywhere picks up (a brew
# install on macOS lands it at /opt/homebrew/bin/, hence the
# default). Override BINDIR on the command line — or unset it — on
# systems with a different install path:
#
#   make build BINDIR=/usr/local/bin           # Intel macOS / Linux
#   make build BINDIR=                          # skip second target
#
# The second build is skipped silently when BINDIR is empty or
# when the directory doesn't exist, so the target is portable
# instead of macOS-brew-only.
BINDIR ?= /opt/homebrew/bin

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
	@echo "  build       — dual-target build (./octoscope + \$$BINDIR/octoscope when set)"
	@echo "  install     — go install"
	@echo "  test        — go test ./..."
	@echo "  race        — go test -race ./..."
	@echo "  fmt         — gofmt -w ."
	@echo "  vet         — go vet ./..."
	@echo "  tapes       — render every tapes/*.tape via vhs (needs \$$GITHUB_TOKEN)"
	@echo "  tape NAME=x — render tapes/x.tape only"
	@echo "  tapes-clean — wipe tapes/out/"
