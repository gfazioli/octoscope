.PHONY: build install test race tapes tape tapes-clean fmt vet help

# Dual-target build — what the maintainer runs after every Go edit.
# `./octoscope` is the iterate-and-test binary; the brew-installed
# binary at /opt/homebrew/bin/octoscope is what `octoscope` from
# anywhere picks up. Forgetting one half causes "I don't see my
# change" loops, so the canonical build target keeps both in sync.
build:
	go build -o ./octoscope .
	go build -o /opt/homebrew/bin/octoscope .

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
	@echo "  build       — dual-target build (./octoscope + /opt/homebrew/bin/octoscope)"
	@echo "  install     — go install"
	@echo "  test        — go test ./..."
	@echo "  race        — go test -race ./..."
	@echo "  fmt         — gofmt -w ."
	@echo "  vet         — go vet ./..."
	@echo "  tapes       — render every tapes/*.tape via vhs (needs \$$GITHUB_TOKEN)"
	@echo "  tape NAME=x — render tapes/x.tape only"
	@echo "  tapes-clean — wipe tapes/out/"
