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
- **Backlog stays in local `ROADMAP.md` (gitignored), not public GitHub
  issues.** Feature requests from PH / Discord / etc. get logged
  privately. Only open issues when explicitly asked.

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
  make build
  ```
  The Makefile (v0.13.0+) wraps the dual-target call with a
  configurable `BINDIR` (defaults to `/opt/homebrew/bin` for the
  maintainer's brew layout). On macOS/brew the default Just Works;
  Linux / non-brew contributors override with
  `make build BINDIR=/usr/local/bin` or `make build BINDIR=` to skip
  the second target. Forgetting either half on the maintainer's
  setup causes "I don't see my change" loops.
- **Pre-push hygiene**: `gofmt -w .` (or `make fmt`) before every
  push. The CI workflow lints with `gofmt -l .` and a single
  unformatted file fails the build (caught the hard way on the
  first run of `ci.yml` in v0.13.0).
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
ANSI escape sequences and C0 control characters that could
otherwise hijack the terminal cursor, OSC clipboard, or
mouse-tracking protocol.

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
   on the landing right under the hero). All landing assets live in
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

If any of these stays stale post-tag, ship a patch release — don't
force-move the tag. See v0.5.0 → v0.5.1 history for an example.

**Maintainer shortcut** (local, not shared with this repo):
`/octoscope-release` is a Claude Code slash command kept under
the gitignored `.claude/commands/` that automates steps 6-12
once the user says "tagghiamo". Companion commands
`/octoscope-smoke` (build-tag-gated integration tests) and
`/octoscope-ph-thread` (release-time PH + tweet generation)
live in the same place. None of these land in the public repo —
they wrap the maintainer's personal workflow, not octoscope's
user-facing surface.

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
