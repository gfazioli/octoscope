# tapes/

[charmbracelet/vhs](https://github.com/charmbracelet/vhs) scripts that
drive octoscope through canonical user flows and produce
deterministic GIFs / PNGs. Two payoffs:

1. **Reproducible smoke session.** The maintainer used to open
   octoscope manually before each release, walk through the tabs,
   verify the drill-ins repaint correctly. The same walkthrough is
   now scripted — `make tapes` runs every scenario headlessly and
   spits out the rendered output, no human at the keyboard.
2. **Landing-page asset generation.** The cycling drill-in slideshow
   on `docs/index.html` is built from `docs/screenshots/drill-in/`.
   When the TUI's appearance changes (theme tweak, layout shift,
   version banner bump) those screenshots used to be recaptured by
   hand. The tapes regenerate them in one command.

## Running

Three prerequisites:

1. **vhs** — `brew install vhs` on macOS / Linux.
2. **`$GITHUB_TOKEN`** exported — the tapes drive octoscope's
   real fetch path, no mocking. Without a token the tapes
   capture an empty / rate-limited dashboard, which defeats the
   point.
3. **An `octoscope` binary on `$PATH`** — every tape boots the
   app with `Type "octoscope"`, so the shell vhs spawns has to
   resolve that name. One of:
   - `go install .` from the repo root (lands in `$GOPATH/bin`,
     which should be on `$PATH`)
   - `make build BINDIR=/usr/local/bin` (or any other directory
     already on `$PATH`)
   - `brew install gfazioli/tap/octoscope` if you want the
     released binary instead of your local checkout

Then:

```sh
make tapes        # render all .tape files in this directory
make tape NAME=overview   # render a single tape
```

The output GIFs land in `tapes/out/` (gitignored). The ones we
actually publish to the landing page get copied into
`docs/screenshots/` manually after a visual review — that step
is intentionally human-in-the-loop, since landing assets are
public and should look intentional, not auto-generated.

## Adding a new tape

One `.tape` file per scenario, named after the feature it
exercises (`pr-diff-viewer.tape`, `settings-panel.tape`, …). The
vhs DSL is shell-like: `Type`, `Sleep`, `Enter`, `Hide`, `Show`,
`Screenshot`. Read the vhs README for the full command set; the
existing tapes here are the easiest reference.

Keep tapes idempotent (no destructive operations — octoscope is
read-only, so this comes for free) and short (under 30s render
time keeps the smoke loop fast).
