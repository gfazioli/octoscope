# internal/ui — rendering patterns

Loaded when working under `internal/ui/`. These four patterns were in the
root `CLAUDE.md` until 2026-09-13 and moved here because they are
conventions for this package alone — the root file is read in every
session, and this one only when it is relevant.

The root file keeps everything that is not package-scoped: the BubbleTea
and Lipgloss conventions, boundary sanitization (which spans
`internal/github` too), the carousel geometry contract (which is about
`tapes/` and `docs/`), and every safety-critical rule.

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
