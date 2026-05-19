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

You need vhs installed (`brew install vhs` on macOS / Linux) and a
valid `$GITHUB_TOKEN` exported (octoscope's real fetch path, no
mocking — the tapes exercise the same code a user would). Then:

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
