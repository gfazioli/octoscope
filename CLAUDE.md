# octoscope

A cross-platform terminal dashboard for GitHub, written in Go with BubbleTea
(Charm).

## Conventions

### Language

- All code, comments, commit messages, documentation, and CLI/UI strings
  must be in **English**.
- Chat/conversation with the developer is in **Italian**.
- GitHub-visible content (PR descriptions, issue titles/bodies/comments,
  release notes) is always in English.

### Git

- Conventional commits: `feat:`, `fix:`, `chore:`, `refactor:`, `docs:`,
  `perf:`, `test:`.
- Push to `main` directly for MVP; introduce PR workflow once contributors
  show up.
- Never add `Co-Authored-By: Claude` trailers.
- Assign new issues to `gfazioli`.

### Go

- Minimum Go version: **1.23**.
- Standard layout: `main.go` at repo root, `internal/` for private packages,
  `cmd/` only if we grow to multiple binaries.
- Prefer small packages with a clear single responsibility (`auth`,
  `github`, `ui`, …) over one big package.
- Use `context.Context` for cancellation and timeouts on every network
  call — never a bare `http.Get`.
- Exported types and functions carry a doc comment starting with the
  symbol name.

### BubbleTea / Lipgloss

- One top-level `Model` per Program. Sub-models for tabs/panels live as
  fields on the root model rather than swapped wholesale.
- `Update` must return quickly; anything that can block (network, disk,
  subprocess) belongs in a `tea.Cmd`.
- Colour and border styling go in `internal/ui/styles.go`. Views never
  create styles inline — keeps the visual identity consistent and makes
  theming (v0.4+) a one-file change.
- Keyboard shortcuts are single characters where possible (`r`, `q`, `?`)
  and documented in both the in-app footer and the README.

### GitHub API

- GraphQL (`shurcooL/githubv4`) is the default. Drop to REST only when
  GraphQL doesn't expose what we need (rare).
- Auth token resolution is one place (`internal/auth`): env var first,
  then `gh auth token`, then unauthenticated. Never hard-code a token.
- Every query returns a plain struct, not raw GraphQL types, so the TUI
  layer doesn't import GraphQL tags.

### Testing

- Unit tests colocated with the code they test (`foo.go` → `foo_test.go`).
- Pure functions (formatters, parsers, config loaders) get table-driven
  tests. Network-touching code gets a fake transport rather than real
  HTTP.

### Distribution

- v0.1.0: `go install` + manual binary via `gh release create`.
- v0.2.0+: `goreleaser` for multi-arch archives + Homebrew tap at
  `gfazioli/homebrew-tap`. CI via GitHub Actions on tag push.
- Binary must also work as a `gh` CLI extension — the single binary
  doubles as `gh-octoscope` when installed via `gh extension install`.

### Out of scope (for now)

- Mutating GitHub state (creating issues/PRs from within octoscope).
  octoscope is read-only until we have a good reason to change that.
- Enterprise GitHub / custom hostnames. Public GitHub only until asked.
