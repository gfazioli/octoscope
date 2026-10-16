# Release preparation and verification

Read for release work; the root CLAUDE.md carries the cross-area triggers.

### Distribution

- v0.1.0: `go install` + manual binary via `gh release create`.
- v0.2.0+: `goreleaser` for multi-arch archives + Homebrew tap at
  `gfazioli/homebrew-tap`. CI via GitHub Actions on tag push.

### Release checklist (IMPORTANT — cut each new version cleanly)

Every release bump touches several places. The goreleaser workflow
handles the binaries / GitHub Release / Homebrew cask
automatically on tag push, but **documentation and landing assets
are manual**. Since v0.13.0 the release-prep changes (steps 1-5
below) go in the **last commit of the feature PR** so merging the
PR leaves `main` immediately taggable — no separate post-merge
commit on `main`.

**Inside the feature PR (atomic):**

1. `main.go` — bump `var version` to the target (e.g. `0.15.0`)
2. `internal/ui/whatsnew.go` — add the `whatsNew["X.Y.Z"]` entry
   for the "What's new" tab (bundled into the binary since
   v0.16.0). Skipping it means the tab shows the *previous*
   release's highlights after the upgrade.
3. `README.md` — update any version references (shields badges
   auto-update via shields.io, but prose mentions don't) and surface
   new features under *What it does* / *Live feedback* / etc.
4. `docs/index.html` — the inline fallback of `#version-pill`, and an
   "At a glance" card for each headline feature.
5. `docs/guide/` — the page that owns the behaviour, plus the
   `#guide-ver` fallback in `docs/guide/docs.js`; a new page also goes in
   `NAV`, the pager chain at both ends and `docs/sitemap.xml`.
6. `docs/screenshots/screenshot.png` — the hero only, as a post-merge,
   pre-tag `chore(release): refresh landing hero screenshot` commit.

The detail behind steps 4–5 is in `docs/CLAUDE.md`, behind step 6 in
`tapes/CLAUDE.md`.

**Wait for explicit go-ahead.** The user types "tagghiamo" (or
equivalent) **after** smoke-testing the merged code on `main`.
Until that signal, tag work doesn't start. When the signal arrives
in chat, run the `/octoscope-release` command (see *Maintainer
shortcut* below) rather than improvising the post-merge steps by
hand — it encodes the polling pattern and the safety checks.

**After the merge + go-ahead:** everything from the tag onwards —
pre-flight checks, the annotated tag, the goreleaser poll, the narrative
release notes, verifying the Release / Homebrew / landing, and the
announcement copy — is the maintainer's own procedure and lives in their
local `/octoscope-release`, whose steps are numbered to continue from 6.
It is not repeated here: two copies of a release procedure drift, and the
one that is actually run is the one that stays right.

Three traps belong in public because they are traps rather than steps.
The full account of each lives beside the thing it is about:

- **Never verify the landing by its rendered version pill**: it reads the
  Releases API, so it shows the new number over a deploy that never
  happened. Ask Pages whether it built and read the bytes it serves
  (`docs/CLAUDE.md`). A failed deploy is fixed by a commit to `main`,
  never by a patch release.
- **Never verify a Homebrew release by whether it installs**: uninstall
  the previous version, install from the real tap, run the binary, and
  assert the version string and exit 0.
- **Signed is not notarized, and a green release job is not a notarized
  release**: the `verify-macos` job in `release.yml` is what gates
  promotion, so never delete it as redundant (`.github/CLAUDE.md`).

If any of these stays stale post-tag, ship a patch release — don't
force-move the tag. See v0.5.0 → v0.5.1 history for an example.

**Maintainer shortcut** (local, not shared with this repo). The steps
after the merge run through slash commands that live in the maintainer's
own workspace, outside this repository:

- `/octoscope-release` — everything from "tagghiamo" onwards: pre-flight
  checks, annotated tag, goreleaser poll, narrative release notes,
  brew/landing verification, merged-branch cleanup.
- `/octoscope-smoke` — writes, runs and deletes a build-tag-gated
  integration test against the live API for a new or changed fetch path.
- `/octoscope-review <PR>` — the executable form of the review-loop rules
  above: establish who *actually* reviewed rather than reading the check
  line, verify each finding before applying it, reply and resolve every
  thread, and report the coverage honestly when a reviewer was absent.
- Three more handle announcement drafting, filing and comment replies.
  **Don't restate them in this file** — it is public, and the channels,
  their conventions and the accounts involved are the maintainer's, not
  the project's.

A local **pre-commit guard** lives beside them, checking staged additions
for credential shapes, local absolute paths and the maintainer's own
channels — in text, and since 2026-09-29 in the text of staged images too,
read by OCR, so a still can be refused at commit time. It is wired through
`core.hooksPath` — which is *local* config, so **a fresh clone has neither
the hook nor the setting** and both need restoring by hand. Its pattern
list is deliberately not repeated here: a tracked copy of that list would
point straight at what it exists to keep out.

None of these commands land in the public repo: they wrap the
maintainer's personal workflow, not octoscope's user-facing surface.

