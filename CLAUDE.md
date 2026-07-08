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
- **PR workflow is the standard since v0.11.0**. Feature branches go
  through PR → Copilot review loop → rebase + merge. The "push to main
  directly" rule from the MVP days survives only for trivial doc-only
  fixes or release-prep follow-ups when no review is needed.
- **Atomic PR pattern (since v0.13.0)**: release-prep changes
  (`main.go` version bump, README updates, `docs/index.html` version
  pill + "At a glance" card) go in the **last commit of the feature
  PR**, not a separate post-merge commit. Merging the PR leaves `main`
  immediately taggable — no intermediate "release prep" commits on
  `main` between feature merges and tags.
  - **The invariant is "release-prep is *in* the PR", not "literally the
    last commit".** Review happens *after* the PR is opened, so a
    `fix:`/`polish:` commit landing **after** the `chore(release): prep`
    commit is normal and fine — `main` is still taggable at the merge tip
    (version + whatsnew + landing are all present). **Don't force-reorder
    history** to keep release-prep physically last. Reference: PRs #44 and
    #46 both have the review-polish commit sitting after `prep`.
  - **Standalone release of an already-merged item**: if you decide
    *after* merge to ship a single item that went in **without** the
    release-prep changes, open a dedicated **release-prep PR** (version
    bump + whatsnew + README + `docs/index.html`) before tagging — `main`
    isn't taggable until it lands. Reference: v0.22.0 shipped #39 (the
    NO_COLOR feature) then #40 (release-prep), since #39 was merged as
    the first item of a cycle, not as a release.
- **Code review = Claude + Copilot.** When the user invokes `/review`
  on an octoscope PR, the deliverable always includes inspecting
  Copilot's review threads on the PR alongside Claude's own analysis;
  valid Copilot suggestions get applied in the same polish commit and
  threads resolved with a reply pointing at the fix commit.
  - **Requesting the Copilot reviewer**: `gh pr edit --add-reviewer
    copilot` fails with "Could not resolve user" — the bot isn't a
    resolvable login. Request it via REST instead:
    ```
    gh api -X POST repos/gfazioli/octoscope/pulls/<NN>/requested_reviewers \
      -f 'reviewers[]=copilot-pull-request-reviewer[bot]'
    ```
- Never add `Co-Authored-By: Claude` trailers.
- Assign new issues to `gfazioli`.
- **Hybrid backlog model (since v0.21.0 / 2026-06-25).** The backlog
  lives in two places by design, split by *thinking* vs *concrete work*:
  - **`ROADMAP.md` (local, gitignored)** is the source of truth for
    the thinking: long-form designs / RFCs (e.g. the supply-chain
    integrity scan), the version-history archive, theme/cycle planning,
    strategic notes ("leading candidate", feasibility gates), and
    unripe / parking-lot ideas. None of this goes public — exposing it
    would either make implicit promises or leak strategy.
  - **Public GitHub issues** are for matured/decided work + community
    signal: items ripe enough to surface for 👍 prioritization,
    confirmed bugs, and a channel to collect external feature requests
    (PH / Discord / etc.). This supersedes the old "only open issues
    when explicitly asked" rule — but since issues are public-facing,
    confirm the shortlist with the maintainer before creating them.
  - **Keep the two in sync.** When a ROADMAP item is promoted to a
    public issue, note the issue number on the item; the long-form
    design stays in the file (the issue links back to it, not the
    reverse). Reordering / dropping a *file-only* item still carries no
    public cost — that freedom is exactly why strategy stays in the file.

### Go

- Minimum Go version: **1.25.11** (the `go` directive in `go.mod`; CI
  pins to it via `go-version-file: go.mod`).
- Standard layout: `main.go` at repo root, `internal/` for private packages,
  `cmd/` only if we grow to multiple binaries.
- Prefer small packages with a clear single responsibility (`auth`,
  `github`, `ui`, …) over one big package.
- Use `context.Context` for cancellation and timeouts on every network
  call — never a bare `http.Get`.
- Exported types and functions carry a doc comment starting with the
  symbol name.
- **Local dev build**: after every Go edit, rebuild the dev binary:
  ```
  make build
  ```
  This produces `./octoscope` in the repo root — the iterate-and-test
  binary. Run it as `./octoscope` from the repo to exercise a change.
  The global `octoscope` on `$PATH` is the **Homebrew release**,
  managed by brew (`brew upgrade gfazioli/tap/octoscope` after a
  tagged release); `make build` deliberately does **not** touch it, so
  a `brew upgrade` always lands cleanly. `BINDIR` now defaults to
  **empty** (the second, install-into-a-dir target is opt-in:
  `make build BINDIR=/usr/local/bin`). **Never** set
  `BINDIR=/opt/homebrew/bin`: it overwrites brew's symlink with a
  plain file, which *shadows* every future `brew upgrade` — the new
  version lands in the Cellar but never reaches `$PATH` (the exact
  0.20.0-vs-0.22.0 trap hit in 2026-07). Fix if it recurs — first
  `rm /opt/homebrew/bin/octoscope` (drop the shadow file), then pick by
  Cellar state:
  - **still installed** (`brew list octoscope` non-empty):
    `brew link --overwrite octoscope`.
  - **Cellar empty / tap gone** (`brew list octoscope` empty, as in the
    v0.24.0 release where the tap wasn't even tapped):
    `brew tap gfazioli/tap && brew install gfazioli/tap/octoscope`.

  Verify: `ls -la /opt/homebrew/bin/octoscope` shows a **symlink** into
  `../Cellar/octoscope/<version>/bin/octoscope`, not a plain file.
- **Pre-push hygiene**: `gofmt -w .` (or `make fmt`) before every
  push. The CI workflow lints with `gofmt -l .` and a single
  unformatted file fails the build (caught the hard way on the
  first run of `ci.yml` in v0.13.0).
- **CI supply-chain gate (since v0.20.2)**: `ci.yml` runs `govulncheck`
  on every push/PR (pinned `@v1.4.0`) and scans the **stdlib too**. A
  fresh Go advisory turns CI red and can hit **either** the stdlib **or**
  a module dependency — the fix differs:
  - **stdlib** → bump the `go` directive in `go.mod` to the patched
    release.
  - **dependency** → `go get <module>@<patched> && go mod tidy` (e.g.
    v0.24.0 bumped `github.com/yuin/goldmark` to v1.7.17 for GO-2026-5320,
    reachable via glamour's markdown renderer). This is common: the
    advisory usually lands on a PR that never touched the flagged code —
    it's a *pre-existing* red, not something that PR introduced.

  Either way the bump *is* the fix, not a suppression. Reproduce and
  verify locally before pushing with the same pin as CI:
  `go run golang.org/x/vuln/cmd/govulncheck@v1.4.0 ./...` (expect
  `No vulnerabilities found`). Workflow actions are pinned to commit SHAs (with a
  `# vX.Y.Z` comment) and kept current by `.github/dependabot.yml`
  (weekly, grouped) — bump via Dependabot's PR, never refloat to a tag.
- **vhs smoke tapes** (`tapes/`, v0.13.0+) drive octoscope through
  canonical user flows and produce deterministic GIFs/PNGs for the
  landing. `make tapes` renders the whole set, `make tape NAME=x`
  one at a time. Tapes need `vhs` installed (`brew install vhs`),
  `$GITHUB_TOKEN`, and `octoscope` on `$PATH`. They are NOT invoked
  by `ci.yml` — asset generation stays human-in-the-loop.
  - **Sandbox**: vhs opens local `ttyd` + headless-Chrome sockets, so
    running it under Claude's sandbox fails with
    `ERR_CONNECTION_REFUSED`. Invoke `make tapes` / `make tape` (or
    `vhs` directly) with `dangerouslyDisableSandbox: true`.
  - **Output lands in `tapes/out/`, not in `docs/`**. The Makefile
    renders `*.gif` / `*.png` into `tapes/out/`; promoting a still to
    the landing is a **manual copy** into `docs/screenshots/` (e.g.
    `docs/screenshots/drill-in/screenshot-repo-detail.png`). The
    `Output`/`Screenshot` paths inside a `.tape` are relative to
    `tapes/`, so the "Regenerates docs/…" header comment names the
    *destination*, not what vhs writes — don't expect the file to
    appear under `docs/` on its own.
  - **Refreshing the hero at a not-yet-released version** (release
    step 5): the tapes type `octoscope …`, resolving it from `$PATH`
    — which is the **Homebrew build, still on the old version**. To
    capture the banner reading the *new* number before the tag exists,
    build the dev binary (`make build`) and prepend the repo to `$PATH`
    for the render:
    `PATH="$PWD:$PATH" GITHUB_TOKEN=$(gh auth token) make tape NAME=overview`
    (still needs `dangerouslyDisableSandbox: true`). Read back
    `tapes/out/overview.png` to confirm the banner, then copy it to
    `docs/screenshots/screenshot.png`. **Never** overwrite the brew
    symlink to get the new binary on `$PATH` (see the `make build`
    BINDIR trap) — the `PATH` prepend is non-destructive.
  - **A version bump refreshes ONLY the hero** (`screenshot.png`), not
    the drill-in / tab-row stills. The carousel geometry contract's
    "touch geometry → regenerate the whole set together" fires when the
    **UI or geometry** changes (v0.19/v0.20-class), not for a routine
    version number — the drill-in banners lagging one version is the
    accepted trade-off (v0.22.0's release commit touched only
    `screenshot.png`). It's normally a **post-merge, pre-tag**
    `chore(release): refresh landing hero screenshot` commit, since the
    banner only reads the bumped number once the version is built —
    despite step 5 living under the "atomic in PR" heading.
- **Landing visual checks** (`docs/index.html`) go through **headless
  Chrome**, not vhs (vhs is for the TUI). The Chrome MCP extension is
  often not connected, so fall back to the CLI:
  `"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  --headless=new --hide-scrollbars --window-size=W,H
  --screenshot=out.png "file://…/docs/index.html"` with
  `dangerouslyDisableSandbox: true`. Two tricks: a **tall
  `--window-size` height** captures the whole page in one shot (for
  below-the-fold / pre-footer sections, since `--screenshot` only grabs
  the viewport); to photograph an **interactive state** (e.g. the
  scroll-triggered newsletter modal) copy the file, force its `.open`
  class on in the copy, then screenshot that. Read the PNG back to
  inspect it. Used to verify the v0.22.0 landing newsletter (modal +
  pre-footer banner).
- **Smoke integration tests gated behind a build tag**
  (`//go:build smoke`) are the maintainer-side check for new fetch
  paths: write one, run via
  `GITHUB_TOKEN=$(gh auth token) go test -tags smoke -v -run TestVxxx ./internal/...`,
  delete it before committing. Used twice (v0.13.0 CI dot fetch,
  v0.14.0 star-history + watched-repos). Never lands in git — the
  unit suite stays hermetic.

#### Carousel slide geometry (landing drill-in slideshow, since v0.18.0)

The landing's drill-in slideshow **cross-fades** between stills
(`action-menu`, `repo-detail`, `pr-drill-in`, `pr-diff-viewer` ×2).
The fade only looks clean if every slide is a *pixel-identical
capture* — banner, profile card, tab bar and footer must land on the
same coordinates in all of them, or the transition visibly jumps.
That makes geometry a **shared contract across all the drill-in
tapes**, not a per-tape choice:

- **One geometry, copied verbatim** into every drill-in tape:
  `FontSize 36`, `Width 3400`, `Height 2340`, `Padding 20`, and the
  inline `octoscope-black` pure-black `Set Theme {…}` block. The hero
  (`overview.tape`) shares everything but is taller (`Height 3000`).
  3400 wide (~148 cols) clears the single-line-footer threshold
  (~147 cols); FontSize 36 keeps glyphs crisp at @2x retina and
  avoids the washed-out / low-detail header that smaller fonts
  produced.
- **Capture a real terminal of those dimensions** — header pinned at
  the top, single-line footer pinned at the bottom, octoscope's own
  spacing in between. Not a centred / letterboxed window.
- **Touch the geometry → regenerate *all* the slides together.**
  Re-rendering a single slide on a tweaked geometry reintroduces the
  jump. If one needs a new capture (e.g. a version bump in the
  banner), re-run the whole drill-in set so they stay aligned.
- **Determinism**: use `--public-only` (screenshot-safe + suppresses
  the sponsor splash), `Sleep 14s` after launch for the first
  dashboard fetch (five parallel branches + possible transient
  retry), and filter list tabs to a stable public row before drilling
  in (the PR tapes filter `gantt` → `OctopBP/mantine-gantt-chart`).

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

#### Nested sub-views inside a drill-in (since v0.12.0)

A drill-in can nest further sub-views as **fields of the parent
model**, not peers of the root. The PR diff viewer is the
reference: `PRDetailModel.files PRFilesModel` and
`PRFilesModel.diff PRDiffModel`. Rules:

- **Title bar is contextual.** The parent's `renderTitle` reads the
  open state of its nested sub-views and extends the breadcrumb
  one segment per level (`▸ PRs / owner/repo#NN`, then `… / Files`,
  then `… / Files / path/to/file`). Hints are narrowed to what
  actually works at that depth — never advertise `f inspect` while
  the user is already inside the inspect surface.
- **`esc` backs out one level, `q` quits the whole app.** The
  parent's Update dispatcher routes keys to the deepest open
  sub-view first; only when no sub-view is open do the top-level
  keys fire.
- **One sub-view open at a time per parent.** `applyFetched`-equivalent
  resets the nested field to its zero value so a refresh doesn't
  strand a stale sub-view pointing at the previous payload.

#### Sticky section partition pattern (since v0.13.0)

List tabs (Repos, PRs) can split their visible rows into multiple
ordered sections under a unified cursor. Used by: Repos pinned
(v0.13), Repos watched (v0.14), PRs review-requests (v0.15).

- A `visibleXPartitioned(...)` helper is the single source of
  truth for the row pipeline: it returns the flat slice + the
  count of each section. The `selectedX` accessor, the `Update`
  cursor-bounds check, and `renderXTab` all consume the same
  output so the cursor can never disagree with the paint
  (lesson from the v0.11.0 filtered-stats bug).
- Sticky sections (pinned, watched, awaiting-review) **preserve
  their natural order** — config order for pinned/watched, API
  order for review-requests. The active sort cycle re-orders
  only the "main" segment.
- The filter (`/`) applies to **all segments uniformly**.
- Section dividers are `tabRuleStyle`-rendered rules whose width
  matches the table header (re-use `lipgloss.Width(header)`).
  Empty sections render no header and no rule; they simply
  collapse.
- Section absence is the default UX: if the user has no pinned
  repos / no watched repos / no review-requests, nothing in the
  tab hints that the feature exists. Discovery happens via the
  config example in README and the action menu.

#### Boundary sanitization (since v0.11.0)

Every GitHub-sourced string (title, body, label name, branch
name, login, check context name, commit headline, repo
description, language name, etc.) passes through
`github.Sanitize` at the **extractor boundary** — inside
`extract*` / `Fetch*` functions in `internal/github/`. By the
time strings reach the rendering layer they're already free of
ANSI escape sequences, C0 control characters, and UTF-8-encoded C1
controls (U+0080–U+009F, the 8-bit CSI/OSC/DCS introducers — added
v0.20.2) that could otherwise hijack the terminal cursor, OSC
clipboard, or mouse-tracking protocol.

The render-layer `sanitizeBody` (`internal/ui/markdown.go`)
stays as defense in depth on the markdown path — duplication is
deliberate, see the comment in `sanitize.go`.

Since v0.17.0 the same discipline applies at the **user-input
boundary**: BubbleTea's bracketed paste delivers clipboard bytes
verbatim into `Key.Runes`, so everything typed/pasted into the
list filters (`/`) passes through `sanitizeFilterInput`
(`internal/ui/repos.go`, shared by Repos / PRs / Issues). Any new
text-input surface must route its `KeyRunes` input through the
same helper — never append `Key.Runes` to rendered state raw.

#### Theme fidelity in monochromatic themes (since v0.14.0)

`Theme.Monochromatic bool` declares "this theme promises a single
tonal palette" (true for `monochrome`, `phosphor`, `amber`). The
renderer reads it via `IsMonochromatic()` and substitutes anything
that would otherwise leak external semantic colour:

- Language bars / chips: GitHub-hex palette → either plain
  foreground (chips) or a six-step rank-scale through the
  theme's own slots (the Overview bar; see
  `monoRankColor` in `internal/ui/monochrome.go`).
- CI rollup dot: green/red/yellow chroma → distinct one-rune
  glyphs (`✓` / `✕` / `⋯` / `·`) styled through the theme.
- Activity heatmap: pink gradient → `monoHeatColor` walks
  `Muted → Accent` through the theme's own slots.
- PR-detail labels: drop the per-label hex.

New renderers that introduce external semantic colour are
expected to honour `IsMonochromatic()`. The flag is the contract;
`monochrome.go` centralises the helpers so future surfaces have
one place to look.

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

1. **The dashboard fetch is N parallel branches.** Started as two
   parallel queries in v0.10.1 (`profileFields` + `repoFields`),
   currently up to **five** as of v0.15.0:
   1. `profileFields` — profile, counters, open PR/Issue nodes,
      contribution calendar
   2. `repoFields` — `repositories(first: 100)` with full nested
      fields
   3. `repoCIFields` — CI rollup state + latest release per repo
      (split from repoFields after v0.13.0 inline attempt 502'd)
   4. `watch_repos` fan-out (v0.14.0, gated on `len(watchRefs) > 0`)
      — one `singleRepoQuery` per entry, **bounded** by a
      semaphore (`watchedRepoConcurrency = 10`) so a 200-entry
      config can't burst-flood GitHub
   5. `reviewRequests` search (v0.15.0, gated on
      `authenticated && viewer-mode`) — single search query
   All run via goroutines + `sync.WaitGroup`. Wall-clock latency
   stays close to the slowest branch rather than their sum. See
   `internal/github/client.go` `FetchStats` for the canonical
   layout.
2. **Per-item fan-out across many items is forbidden when
   unbounded.** Asking GitHub to walk N repos × M sub-queries in
   a single GraphQL doc (history fan-out, statusCheckRollup inline
   on `repoFields`, etc.) consistently 502s on busy accounts. Two
   safe alternatives:
   - **Drill-in pattern**: one query per *selected* item, on demand.
   - **Bounded fan-out**: one targeted query per *config-listed*
     item (≤ tens), capped by a semaphore. Used for `watch_repos`.
3. **Sibling-cancellation on error.** When a fetch combines
   multiple goroutines whose results are all needed
   (`FetchPRDetail` GraphQL + REST, `FetchRepoDetail` GraphQL +
   stargazers), wrap the caller's ctx in a `context.WithCancel`
   child and use `sync.Once` to capture the first error. The
   sibling-cancellation echo (`ReasonNetwork` from a cancelled
   query) would otherwise clobber the real failure (Auth /
   RateLimit / 5xx). Reference: `FetchPRDetail` v0.12.0 polish.
4. **Adding new fields to a query**: estimate complexity first.
   `languages(first: 10)` × 100 repos was already a meaningful
   chunk of the budget; `defaultBranchRef.target.statusCheckRollup`
   inline on 100 repos blew it. New nested aggregates ride on top
   of what's already there.
5. **If a feature needs per-repo data on the list**, surface it
   on-demand in the detail view first, then evaluate whether a
   list-level column is even necessary. The drill-in already
   answers most of those questions.
6. **Transient 5xx are noise, not always complexity** (v0.17.0).
   A 502 can also hit an *unchanged*, previously-fine query —
   pure gateway flakiness on GitHub's side — and HTTP/2 transport
   failures (`stream error`, `received from peer`, GOAWAY)
   surface the same way. Both classify as `ReasonServer` via
   `classifyErr` (`internal/github/client.go`); the dashboard
   fetch wraps in `retryTransient` (`internal/ui/model.go` — 3
   attempts, short backoff, retries **only** `ReasonServer`).
   New fetch paths reuse the same retry helper, and any new
   transport-level error string gets taught to `classifyErr`
   rather than leaking raw text into the error screen.

The principle "one GraphQL query per refresh" from v0.x.x docs is
**superseded** — current invariant is "as many parallel branches
as the feature shape demands, each one estimated against the
complexity ceiling before adding fields".

### Testing

- Unit tests colocated with the code they test (`foo.go` → `foo_test.go`).
- Pure functions (formatters, parsers, config loaders) get table-driven
  tests. Network-touching code gets a fake transport rather than real
  HTTP.
- GraphQL fetch paths can reuse the `newTestGQLClient` harness
  (`internal/github/watched_repo_fetch_test.go`, since v0.20.2): it points
  a `githubv4.Client` at an `httptest` server through the `rewriteHost`
  round-tripper, so a fetch is exercised hermetically against a canned
  JSON response.

### Distribution

- v0.1.0: `go install` + manual binary via `gh release create`.
- v0.2.0+: `goreleaser` for multi-arch archives + Homebrew tap at
  `gfazioli/homebrew-tap`. CI via GitHub Actions on tag push.

### Release checklist (IMPORTANT — cut each new version cleanly)

Every release bump touches several places. The goreleaser workflow
handles the binaries / GitHub Release / Homebrew formula
automatically on tag push, but **documentation and landing assets
are manual**. Since v0.13.0 the release-prep changes (steps 1-5
below) go in the **last commit of the feature PR** so merging the
PR leaves `main` immediately taggable — no separate post-merge
commit on `main`.

**Inside the feature PR (atomic):**

1. `main.go` — bump `const version` to the target (e.g. `0.15.0`)
2. `internal/ui/whatsnew.go` — add the `whatsNew["X.Y.Z"]` entry
   for the "What's new" tab (bundled into the binary since
   v0.16.0). Skipping it means the tab shows the *previous*
   release's highlights after the upgrade.
3. `README.md` — update any version references (shields badges
   auto-update via shields.io, but prose mentions don't) and surface
   new features under *What it does* / *Live feedback* / etc.
4. `docs/index.html` — the hero version pill (`#version-pill`) now
   auto-updates via a fetch to GitHub Releases API on page load,
   but the inlined fallback value should still be current in case
   the API is unreachable (rate limit, offline preview). **Any
   headline feature added in this release should also get a card in
   the "At a glance" grid** — the README and the landing tell the
   same story, don't let them drift.
5. `docs/screenshots/screenshot.png` — retake if the TUI's own
   version banner needs to read the new number (cosmetic but visible
   on the landing right under the hero). In practice this is a
   **post-merge, pre-tag** `chore(release): refresh landing hero
   screenshot` commit rather than atomic-in-PR (the banner only reads
   the bumped number once the version is built) — regenerate the hero
   with the not-yet-released binary via the `PATH`-prepend trick in the
   vhs-tapes notes above, and refresh **only the hero** for a version
   bump (not the whole drill-in set). All landing assets live in
   `docs/<category>/` since v0.12.0: `icons/`, `logo/`, `screenshots/`
   (with `screenshots/drill-in/` for the cycling drill-in
   slideshow), `themes/`. Ideally regenerated via `make tapes`.

**Wait for explicit go-ahead.** The user types "tagghiamo" (or
equivalent) **after** smoke-testing the merged code on `main`.
Until that signal, tag work doesn't start. When the signal arrives
in chat, run the `/octoscope-release` command (see *Maintainer
shortcut* below) rather than improvising the post-merge steps by
hand — it encodes the polling pattern and the safety checks.

**After the merge + go-ahead:**

6. `git checkout main && git pull` — align with the merged result
7. Tag `vX.Y.Z` annotated with **detailed narrative notes** (not
   the one-liner default — past tags `v0.11.0` onwards are the
   reference style: headline + sections per major change +
   "Notable polish" + tests)
8. Push the tag (`git push origin vX.Y.Z`)
9. Wait ~1-2 min for the goreleaser workflow to finish
10. **Apply narrative release notes** via `gh release edit vX.Y.Z
    --notes-file ...`. The goreleaser default body is too thin;
    write a proper user-facing narrative with headline, sections,
    bullets, upgrade command. Past tags `v0.12.0` onwards are the
    reference style.
11. Verify: GitHub Release exists, Homebrew formula bumped,
    `brew upgrade gfazioli/tap/octoscope` reports the new version
12. Verify: landing shows the new version in the hero pill (Pages
    rebuilds in 30-60s after the commit that touches `docs/`)
13. **Hand the user the Product Hunt thread + Twitter/X short
    version** generated from the release notes — this is now part
    of every release (since v0.11.0). The user posts; Claude
    generates. Social copy is **plain text with one paragraph per
    line** (no hard-wrapping, no markdown headers) — it gets
    pasted into boxes that treat newlines literally.
14. **Maintainer (local):** file the release **newsletter** (published
    on **Substack — <https://octoscope.substack.com>**) and the
    **Product Hunt** entry in the maintainer's private Notion hub — and
    draft any **pillole** (tips / dev articles) on demand — via the
    `/octoscope-content` command (local, not shared; see *Maintainer
    shortcut*). The user reviews drafts and publishes by hand. (Newsletter
    drafts always carry a `Subtitle`, and shell/brew commands go in a
    fenced code block — see the command for the full content rules.)

If any of these stays stale post-tag, ship a patch release — don't
force-move the tag. See v0.5.0 → v0.5.1 history for an example.

**Maintainer shortcut** (local, not shared with this repo). Two Claude
Code slash commands currently live under the gitignored
`.claude/commands/`:
- `/octoscope-release` — automates steps 6-13 once the user says
  "tagghiamo" (pre-flight checks, annotated tag, goreleaser poll,
  narrative release notes, brew/landing verification).
- `/octoscope-content` — files the newsletter / Product Hunt / pillole
  drafts in the maintainer's private Notion hub.

`/octoscope-smoke` (build-tag-gated integration tests) and
`/octoscope-ph-thread` (release-time PH + tweet) are **referenced but
not yet created** — until the files exist, do those steps manually from
the checklist (generate the PH thread + tweet inline). None of these
land in the public repo — they wrap the maintainer's personal workflow,
not octoscope's user-facing surface.

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
