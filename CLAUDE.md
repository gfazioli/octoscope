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
- **Local build dual-target**: after every Go edit, rebuild **both**
  binaries:
  ```
  go build -o ./octoscope . && go build -o /opt/homebrew/bin/octoscope .
  ```
  The user runs the brew binary by default (`octoscope` from anywhere);
  `./octoscope` from the project root is for inline test-and-iterate.
  Forgetting one half causes "I don't see my change" loops.

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

#### Drill-in detail views (canonical pattern, since v0.10.0)

Per-item rich detail follows a fixed shape — codify it once, reuse for
every list tab. See `internal/ui/repo_detail.go` as the reference
implementation; PRs/Issues drill-ins (v0.10.2+) follow the same template.

- **Sub-model with three states**: `loading` (kicked off by an `Open`
  call + a fetch `tea.Cmd`), `error` (with `r retry · esc back`), and
  `loaded`. Each state renders inline; only the loaded state needs the
  scroll machinery.
- **Sticky title row + viewport-wrapped body**: the title with the
  breadcrumb + key hints (`esc back · o open in github · r refresh`)
  stays anchored. The body lives inside a `bubbles/viewport` so a long
  detail (many languages, long topics, big issues/PRs preview) scrolls
  internally instead of pushing the pinned footer off-screen on short
  terminals. Same pattern as v0.9.1's Overview/Activity scrolling.
- **Tab body replace ("option B")**: when the detail is open, the
  tab-content area renders the detail instead of the list. Banner,
  profile card, tab bar and footer all stay pinned.
- **Stale-fetch protection**: the fetch result message carries a
  correlation key (URL works fine). The model only applies the
  payload if the still-open detail matches the key — otherwise the
  user has navigated away and the late response is dropped.
- **Action menu as the entry surface**: detail is reached via the
  `space`-opened modal action menu (or `Enter` direct, post-v0.10.2),
  not from a dedicated keybind invented for one tab. Single keymap
  across Repos / PRs / Issues.
- **Read-only**: detail views never expose mutating actions
  (close issue, merge PR, delete, edit). The principle in *Out of
  scope* below applies inside the drill-in too.

When extending: copy `repo_detail.go` as the skeleton, swap the
section list, define a parallel `<Item>DetailModel` with the same
`Open`/`Update`/`View`/`applyFetched` shape, wire `viewXDetailMsg`
into root model, register the new action in the action menu's
per-tab seed.

### GitHub API

- GraphQL (`shurcooL/githubv4`) is the default. Drop to REST only when
  GraphQL doesn't expose what we need (rare).
- Auth token resolution is one place (`internal/auth`): env var first,
  then `gh auth token`, then unauthenticated. Never hard-code a token.
- Every query returns a plain struct, not raw GraphQL types, so the TUI
  layer doesn't import GraphQL tags.

#### Complexity ceiling — what we can and can't query (since v0.10.1)

GitHub's GraphQL gateway has a **per-request complexity budget that
isn't documented as a hard number**. Empirically, on a real ~74-repo
authenticated account in early 2026, these patterns hit it and got
HTTP 502 *from the proxy* (before the request reached the GraphQL
backend):

- A single combined query covering profile + counters + open PR/Issue
  nodes + 52-week contribution calendar + `repositories(first: 100)`
  with full nested fields. **Always 502.** This is what forced the
  v0.10.1 split.
- `defaultBranchRef.target.history.totalCount` requested once per
  repo across `repositories(first: 100)` (i.e. per-item fan-out on
  100 items). **Always 502.** This killed the original issue #4
  plan (configurable columns + commit-count metrics).

**Rules of thumb derived from those scars**:

1. **The dashboard fetch is two parallel queries**: `profileFields`
   (everything except the repo list) and `repoFields` (`repositories`
   with full nested fields). See `internal/github/client.go` for the
   canonical layout. Both run via goroutines + `sync.WaitGroup`; the
   first error fails the whole fetch.
2. **Per-item fan-out across many items is forbidden** — anything
   that asks GitHub to walk N repos × M sub-queries (history,
   defaultBranchRef.target details, etc.) needs a different shape.
   The drill-in pattern (one query per *selected* item, on demand)
   is the established alternative.
3. **Adding new fields to either query**: estimate complexity first.
   `languages(first: 10)` × 100 repos was already a meaningful chunk
   of the budget. New nested aggregates ride on top of that.
4. **If a feature needs per-repo data on the list, surface it
   on-demand in the detail view first**, evaluate whether a
   list-level column is even necessary. The drill-in already
   answers most of those questions.

The principle "one GraphQL query per refresh" from v0.x.x docs is
**superseded** — current invariant is "two queries per refresh,
splittable further only if clearly justified".

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
4. `docs/screenshots/screenshot.png` — retake if the TUI's own
   version banner needs to read the new number (cosmetic but visible
   on the landing right under the hero). All landing assets live in
   `docs/<category>/` since v0.12.0: `icons/`, `logo/`, `screenshots/`,
   `themes/`.
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

### Security & secrets

A few rules to handle credentials sanely. They sound obvious, but
specifying them explicitly prevents the "well-meaning but wrong"
default of "user gave me their token, let me use it":

- **Never accept, log, or use a credential pasted into chat**, even
  if the user offers one explicitly to "help". Tokens, passwords,
  cookies, API keys — all out of bounds.
- **If a credential lands in the conversation, immediately**:
  1. Treat the transcript as compromised — chat history persists
     and may be cached, indexed, or shared.
  2. Tell the user to revoke it now, with the canonical revoke URL:
     - GitHub PATs / fine-grained tokens: <https://github.com/settings/tokens>
     - GitHub OAuth apps: <https://github.com/settings/applications>
  3. Continue the underlying task **without** the leaked credential —
     fall back to whatever auth path the user normally uses
     (`$GITHUB_TOKEN`, `gh auth token`, etc.).
- **Don't echo the token value back** in your responses, not even
  partially. Reference it with a non-revealing label
  (`gho_Ab8x…` truncated, or just "the token you pasted").
- The same rules apply to anything that looks token-shaped in
  config files, `.env`, command output. If a snippet contains a
  secret, ask whether the user wants it redacted before continuing.
