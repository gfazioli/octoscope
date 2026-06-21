# Contributing to octoscope

Thanks for your interest in octoscope! This is a small, focused project
— a cross-platform terminal dashboard for GitHub written in Go with
[BubbleTea](https://github.com/charmbracelet/bubbletea). Contributions
are welcome; this page covers how to work with the codebase.

## Before you start

- **Bugs**: open an [issue](https://github.com/gfazioli/octoscope/issues/new/choose)
  with the bug-report template — a reproduction and your octoscope
  version (`octoscope --version`) help a lot.
- **Features / ideas**: the roadmap is curated by the maintainer, so
  feel free to suggest an idea in an issue, but please **open a PR only
  after a quick discussion** so effort isn't wasted on something out of
  scope. octoscope is intentionally **read-only** (it never mutates
  GitHub state) and **public-GitHub only** — proposals that change those
  invariants need a strong rationale.
- **Security**: do not open a public issue — see [SECURITY.md](SECURITY.md).

## Development setup

Requirements: **Go 1.25.11+** (matches the `go` directive in `go.mod`;
CI pins to it) and [`vhs`](https://github.com/charmbracelet/vhs) only if
you regenerate landing assets.

```bash
git clone https://github.com/gfazioli/octoscope
cd octoscope
go build -o octoscope .
GITHUB_TOKEN=$(gh auth token) ./octoscope   # or just ./octoscope if $GITHUB_TOKEN is set
```

A `Makefile` wraps the common tasks:

```bash
make build   # build the binary
make test    # go test ./...
make race    # go test -race ./...
make fmt     # gofmt -w .
make vet     # go vet ./...
```

## Conventions

- **Language**: all code, comments, commit messages, documentation and
  user-facing strings are in **English**.
- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/)
  — `feat:`, `fix:`, `chore:`, `refactor:`, `docs:`, `perf:`, `test:`.
- **Formatting**: run `gofmt -w .` before pushing. CI fails on a single
  unformatted file (`gofmt -l .`) and runs `go vet ./...`.
- **Tests**: unit tests live next to the code they test
  (`foo.go` → `foo_test.go`). Pure functions get table-driven tests;
  network-touching code uses a fake transport rather than real HTTP, so
  the suite stays hermetic. Run `make test` (or `make race`) before
  opening a PR.
- **Packages**: prefer small packages with a single responsibility
  (`auth`, `github`, `ui`, `config`, …). Network calls always take a
  `context.Context` for cancellation/timeout — never a bare `http.Get`.
- **Styling**: colour and border styles live in
  `internal/ui/styles.go`; views never construct lipgloss styles inline
  (keeps theming a one-file change).

## Pull requests

1. Branch off `main`.
2. Keep the PR focused; build, `gofmt`, `go vet` and `go test ./...`
   must pass.
3. Open the PR — CI (build / test / lint) runs automatically, and
   automated reviewers may comment. Address feedback, then it's
   rebased and merged.

By contributing you agree your work is licensed under the project's
[MIT License](LICENSE) and to abide by the
[Code of Conduct](CODE_OF_CONDUCT.md).
