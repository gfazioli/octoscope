# tapes/ — vhs renders and the stills they feed

Loaded when working under `tapes/`. These sections were in the root
`CLAUDE.md` until 2026-10-10. The root keeps the one rule that leaks
rather than breaks: `--public-only` is not the same as publishable.

#### Rendering the tapes

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
  - **vhs 0.12.0 renders nothing, and exits 0 while doing it** (measured
    2026-09-13). It prints the whole tape trace and `Creating out/x.gif…`,
    writes no file, and returns success — `ttyd` never starts, so the
    failure is before the browser, and there is no error anywhere in the
    output. brew upgraded to it on 2026-09-10; the last good render was
    2026-09-08, which is the correlation that found it. **0.11.0 works**:
    ```shell
    GOBIN=/tmp/vhsbin go install github.com/charmbracelet/vhs@v0.11.0
    (cd tapes && /tmp/vhsbin/vhs overview.tape)
    ```
    brew offers only 0.12.0, so a downgrade has to come from source. The
    shape is the one the root `CLAUDE.md` keeps relearning — a tool that reports
    success having produced nothing — so **check the output file's
    timestamp**, never the exit code: `ls -l tapes/out/overview.png`.
  - **Output lands in `tapes/out/`, not in `docs/`**. The Makefile
    renders `*.gif` / `*.png` into `tapes/out/`; promoting a still to
    the landing is a **manual copy** into `docs/screenshots/` (e.g.
    `docs/screenshots/drill-in/screenshot-repo-detail.png`). The
    `Output`/`Screenshot` paths inside a `.tape` are relative to
    `tapes/`, so the "Regenerates docs/…" header comment names the
    *destination*, not what vhs writes — don't expect the file to
    appear under `docs/` on its own.
  - **A still is quantised before it is committed.** Run every image
    promoted into `docs/` through ImageOptim with lossy compression on
    (or `pngquant`): a screenshot becomes a 256-colour palette, which the
    terminal's flat colours never show. Measured 2026-09-30 over the 28
    images in the repo: 8.86 MB → 3.24 MB, every screenshot at SSIM
    ≥ 0.998, and Chrome decodes the quantised carousel six times faster.
    `images_test.go` decodes every PNG and JPEG under `docs/` and refuses a
    truecolour PNG (the two favicons excepted) or a JPEG whose
    quantisation table reads above quality 85: what a still copied from
    `tapes/out/`, or a JPEG straight from an export, would be.
  - **Refreshing the hero at a not-yet-released version** (release
    step 6): the tapes type `octoscope …`, resolving it from `$PATH`
    — which is the **Homebrew build, still on the old version**. To
    capture the banner reading the *new* number before the tag exists,
    build the dev binary (`make build`) and prepend the repo to `$PATH`
    for the render:
    `PATH="$PWD:$PATH" GITHUB_TOKEN=$(gh auth token) make tape NAME=overview`
    (still needs `dangerouslyDisableSandbox: true`). Read back
    `tapes/out/overview.png` to confirm the banner, then copy it to
    `docs/screenshots/screenshot.png`. **Never** overwrite the brew
    symlink to get the new binary on `$PATH` (see the `make build`
    BINDIR trap in the root `CLAUDE.md`) — the `PATH` prepend is
    non-destructive.
  - **A version bump refreshes ONLY the hero** (`screenshot.png`), not
    the drill-in / tab-row stills. The carousel geometry contract's
    "touch geometry → regenerate the whole set together" fires when the
    **UI or geometry** changes (v0.19/v0.20-class), not for a routine
    version number — the drill-in banners lagging one version is the
    accepted trade-off (v0.22.0's release commit touched only
    `screenshot.png`). It's normally a **post-merge, pre-tag**
    `chore(release): refresh landing hero screenshot` commit, since the
    banner only reads the bumped number once the version is built —
    despite step 6 living under the "atomic in PR" heading.

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
- **Determinism**: use `--public-only` (keeps private repositories
  out + suppresses the sponsor splash), `Sleep 14s` after launch for
  the first dashboard fetch (five parallel branches + possible
  transient retry), and filter list tabs to a stable public row before
  drilling in (the PR tapes filter `gantt` →
  `OctopBP/mantine-gantt-chart`).
- **`--public-only` is not the same as publishable.** It drops
  *private* repositories and nothing else, so a tab that lists other
  people's repositories — Inbox, PRs, Issues, the Activity feed —
  shows every **public** one the account watches or works in, an
  employer's included, by name and with its titles. What a still shows
  depends on what the live account holds that day, which no tape can
  filter: read every row before promoting a still.

#### The release hero (checklist step 6)

6. `docs/screenshots/screenshot.png` — retake if the TUI's own
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
