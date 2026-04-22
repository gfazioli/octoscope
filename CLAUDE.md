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

### Release checklist (IMPORTANT — cut each new version cleanly)

Every release bump touches several places. The goreleaser workflow
handles the binaries / GitHub Release / Homebrew formula
automatically on tag push, but **documentation and landing assets
are manual**. Before tagging:

1. `main.go` — bump `const version` to the target (e.g. `0.5.1`)
2. `README.md` — update any version references (shields badges
   auto-update via shields.io, but prose mentions don't) and surface
   new features under *What it does* / *Live feedback* / etc.
3. `docs/index.html` — the hero version pill (`#version-pill`) now
   auto-updates via a fetch to GitHub Releases API on page load,
   but the inlined fallback value should still be current in case
   the API is unreachable (rate limit, offline preview). **Any
   headline feature added in this release should also get a card in
   the "At a glance" grid** — the README and the landing tell the
   same story, don't let them drift.
4. `docs/screenshot.png` — retake if the TUI's own version banner
   needs to read the new number (cosmetic but visible on the landing
   right under the hero)
5. Build + smoke-run the binary locally
6. Commit the bump with message `chore(release): X.Y.Z — <summary>`
7. Tag `vX.Y.Z` with detailed annotated notes (`git tag -a`)
8. Push branch *and* tag (`git push && git push origin vX.Y.Z`)
9. Wait ~1-2 min for the goreleaser workflow to finish
10. Verify: GitHub Release exists, Homebrew formula bumped,
    `brew upgrade gfazioli/tap/octoscope` reports the new version
11. Verify: landing shows the new version in the hero pill (Pages
    rebuilds in 30-60s after the commit that touches `docs/`)

If any of these stays stale post-tag, ship a patch release — don't
force-move the tag. See v0.5.0 → v0.5.1 history for an example.

### Out of scope (for now)

- Mutating GitHub state (creating issues/PRs from within octoscope).
  octoscope is read-only until we have a good reason to change that.
- Enterprise GitHub / custom hostnames. Public GitHub only until asked.
