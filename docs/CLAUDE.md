# docs/ — the website

Loaded when working under `docs/`. These sections were in the root
`CLAUDE.md` until 2026-10-10. Pages publishes this directory verbatim, so
this file is served too, as a raw file, like `design/`. The root keeps the
three rules whose trigger lies outside `docs/`: the README stays
canonical, the privacy page follows the binary, and the archive names
follow `.goreleaser.yaml`.

#### The website is two things: landing and guide (since 0.26.0)

`docs/` serves **two surfaces with different jobs**, and keeping them
separate is the point:

- **`docs/index.html` — the marketing landing.** Hero, "At a glance",
  a CTA into the guide, footer. It exists to make someone *want*
  octoscope, and it must **not re-explain what the guide documents**.
  0.26.0 removed the five-tabs walkthrough, the drill-in explainer,
  the themes gallery and the install steps for exactly this reason:
  duplicated how-to drifts, and the landing was already a version
  behind the guide it duplicated.
- **`docs/guide/` — the documentation.** Ten pages: eight under
  *Guide*, plus a *Reference* pair (CLI flags, keyboard shortcuts).

**Hand-authored static HTML — no generator of ours, and no build step we
wrote.** Don't introduce a toolchain without a reason bigger than "it
would be tidier".

*"Pages serves `docs/` verbatim"* is true again, as of
[#122](https://github.com/gfazioli/octoscope/issues/122), and the history
of it being false is worth keeping because the claim reads as obviously
correct either way.

From the repository's creation until 2026-09-15, Pages was configured as
`build_type: legacy` and ran **Jekyll** over `docs/` on every push to
`main`, whatever this file said. That is not what Jekyll's own
documentation describes — a markdown file without front matter is a static
file, and nothing here has front matter — because GitHub Pages is not
vanilla Jekyll: it loads **`jekyll-optional-front-matter`** by default
(0.3.2 against Jekyll 3.10.0, per
<https://pages.github.com/versions.json>, read 2026-09-12), and that plugin
turns a front-matter-less markdown file into a page and runs Liquid over
it. Which is how a sample containing `{{` in `docs/design/` failed the
publish seven times in a row in August 2026, for twenty-two hours, while
the site quietly kept serving the previous release.

**The site now publishes through `.github/workflows/pages.yml`** —
`upload-pages-artifact` + `deploy-pages`, source set to *GitHub Actions* —
so there is no Jekyll, no Liquid and no front matter between the files in
the repository and the files served. Jekyll's config file is gone with
the build it was patching.

Two consequences worth knowing. A markdown file under `docs/` is now served
as a raw file rather than rendered, `design/` included, which is what the
old `exclude:` was avoiding by a different route. And the CI `pages` job
that alarmed on a failed build is gone: `/pages/builds` reports the legacy
builds this setup no longer produces, so keeping it would have left a check
that answers about nothing. **The deploy workflow is the alarm** — a
failed publish is a red run on the commit that caused it.

**The README stays canonical.** The guide is the narrative version;
the README is the reference an outside reader hits first on GitHub.
A new feature lands in both, or it drifts — same discipline the
landing and README already had.

**Shared chrome comes from two files.** `docs/guide/style.css` is the
design system and `docs/guide/docs.js` injects the sidebar and topbar
from a single `NAV` array. Adding a page is: create the file, add it
to `NAV`, and wire it into the **pager chain** at both ends — the
chain is linear and hand-maintained, so a new page inserted in the
middle silently strands whichever page used to point past it (caught
once already, themes → keybinds skipping Configuration and Scripting).
Then list it in `docs/sitemap.xml` and give its `<head>` the block the
other guide pages carry: a canonical naming its own URL, the icons,
the share tags. `site_test.go` fails on a page the sitemap does not
list, on a canonical that names another URL, and on a page without
exactly one `<main>` and one `<h1>` or with a description outside
50–160 characters. The sitemap listed one page of twelve until
2026-09-30, under a lastmod eleven weeks old, which is why it now
carries no dates at all.

**Every page must load the fonts itself, from the site.** Oxanium +
JetBrains Mono are served from `docs/fonts/` (latin and latin-ext woff2,
both variable, OFL beside them) through `fonts/fonts.css`, linked in each
page's `<head>`. `style.css` only *names* them, so a page that forgets the
link still renders — just silently in the system font, which is why it
survived a full review round unnoticed. Until 2026-10-05 they came from
Google Fonts, which hands every visitor's address to Google;
`TestSitePagesServeTheirOwnFonts` now fails on a page that skips the link,
one that reaches Google again, and a face whose file is missing.

**The publisher line and the two pages behind it.** The line with the
copyright, the publisher, the VAT number and the Legal and Privacy links is
the one the maintainer's other product sites carry. It sits in the landing's
footer markup (the VAT number belongs on the home page, art. 35 DPR
633/72) and under every guide page through `docs.js`.
`guide/legal.html` and `guide/privacy.html` are guide pages left out of
`NAV` and the pager on purpose. **The privacy page describes what the binary
does**, read from the code on 2026-10-05: two hosts (`api.github.com`,
`www.githubstatus.com`), the token only to the first, the three files on
disk. A PR that adds a host, a file the binary writes, or flips a default
of `check_for_updates` / `check_service_status` updates that page in the
same PR, or the policy starts saying something false.

**Dark is the default, deliberately, with no `prefers-color-scheme`
fallback** — the landing commits to pure black and the docs match it.
Light is opt-in through the header toggle, which stamps `data-theme`
on the root element. Any code that needs to know the current theme
reads that one rule (`data-theme !== "light"`); consulting the OS
preference instead puts the toggle icon out of sync with the page.

**Interactive affordances on the landing must be keyboard-reachable.**
The "At a glance" cards deep-link into the guide, and shipped
mouse-only on the first pass — no `tabindex`, no `role`, no keydown.
Promote such elements **in JS, not in the markup**, since the
behaviour is JS-only and markup semantics would lie without it; and
skip the marquee's `aria-hidden` clones, because a focusable
aria-hidden element is its own violation.

**The Download button links only what a release says it carries**
(since 2026-10-06). It names the archive for the reader's machine the way
`.goreleaser.yaml`'s `archives.name_template` does, so **renaming the
archives means changing `SYSTEMS` in the landing's inline script too**, or
every machine falls back to the link to all builds. Two rules came out of
review: no link built from the pill's inline version, which release prep
bumps before the tag exists, so only the Releases API's own file list
counts; and no guessed architecture, since an Apple silicon archive does
not run on an Intel Mac and no build runs on a 32-bit system. Chromium says
the architecture and the bitness; Firefox writes the architecture into its
user agent on Linux and Windows; every Mac browser but Chromium freezes it
as "Intel". Without a 64-bit answer the button stays the link to every
build and the line under it offers this system's archives by name.

#### The landing moves, and nothing is hidden before a script runs (since 0.36.0)

The motion is the sibling sites' (findergit.app, lancetta.app), rebuilt
without their React: CSS in the landing's `<style>`, one classic script
(`docs/landing.js`), no build step.

- **Reveals.** An element with `data-reveal` (`rise`, `morph`, `squash`,
  `pop`) is an item; `data-scope` groups items under one trigger, and an
  item with no scope above it is its own. A scope is at REST in the served
  HTML, ARMED (`data-armed`) only once the script has measured it entirely
  off screen, REVEALED on its way into view — so a failed script costs the
  motion and never the content, and what is on screen at load never moves.
  Every hiding rule must require `data-armed`; `landing_test.go` fails on
  one that does not, on a reveal state in the served markup, and on a
  variant with no pose. Poses use the individual `translate` / `scale`
  properties, never `transform`, so they compose with the transforms the
  page already uses for hover and centring.
- **Springs are generated, never typed.** The `:root` block between
  `springs:begin` and `springs:end` is what `springsCSS()` in
  `landing_test.go` samples from the films' closed-form spring; the test
  prints the block to paste when they differ. The sampler reproduces
  findergit.app's generated block byte for byte.
- **The octopus** is the TUI's launch mascot: `#octopus-art` is a JSON
  copy of `mascotLaunch` that `internal/ui/mascot_site_test.go` holds to
  the Go drawing, and `landing.js` composes it the way `mascotGrid` does.
  Since 2026-10-06 it is **one character in four places**, the shape the
  sibling sites' mascots have (findergit.app, netfox.app), asked for by the
  maintainer: visible from the first second, in the corner while scrolling,
  suggesting a sponsorship at the end.
  - **Beside the version link**, on load: it walks in from the right edge
    to *what's new* and says what the release is about — the release notes'
    bold opening sentence, which the inline script that fills the pill
    publishes as `window.octoscopeRelease`; without it, just the version.
    It comes only with at least 200px of room right of it for the bubble
    (a window about 805px wide or more). The bubble hangs off the octopus,
    out of the flow, so a long headline wrapped at a narrow room ran over
    the h1 (36px at 810, 120 characters): the headline is said only while
    the drawn bubble ends 12px above the heading, else the version alone.
  - **Beside the dots**, as before: it says the current slide's caption and
    turns the carousel on a click, not at 64em or below — measured: just
    above 48em its bubble ran 32px into the next section's heading.
  - **In the window's corner** whenever neither of those is on screen,
    walking while the page scrolls; a click gives a tip, one of the "At a
    glance" cards whose text fits a bubble (160 characters), so it makes no
    claim the page does not. Where the dots leave no room, the corner says
    their caption once, by itself, and folds after 8 s. Where the version
    link leaves none, its words wait for the first click on the corner:
    opened by itself, on a page just loaded, the bubble sat on the hero's
    buttons (measured at 390).
  - **On the support card** in the footer once 30% of it is on screen,
    with the footer's own claim: free and MIT-licensed — and only while the
    card's top edge is 112px below the nav, room for the octopus and its
    bubble. On a short phone the footer is taller than the window and the
    end of the page leaves the card's edge about 80px down, so the corner
    keeps it there (measured at 320x640 and 360x740; 390x844 keeps the
    card). On a phone the octopus is smaller and its bubble grows upward
    from the card's edge, because beside it the sentence wrapped down over
    the card's heading (Codex, at 320).
  - **In the guide**, a note on five pages, never all of them (welcome on
    Getting started; tips on Authentication, Keyboard shortcuts and
    Release notes; itself on Themes): `.oc-note` in `style.css`, no script.
    Its words are the page's own claims, and the drawing is a static
    `docs/guide/octopus.svg` that `internal/ui/mascot_site_test.go` reads
    back to the Go drawing — rects or the paths ImageOptim turns them
    into, and it fails on any shape it cannot read rather than skip it.

  Never two on screen: a place in the page keeps its octopus standing out
  of sight, and one that would be seen while another place has it goes.
  An octopus leaving while another already stands on screen goes without
  its fade, which `leave` works out from the places themselves: scrolling
  back to the hero's, the corner's fade showed both for 260ms.
  Where it belongs is measured from the boxes on every scrolled frame,
  never observed, because an IntersectionObserver misses a jump straight
  past a place. It waits while the newsletter prompt is open
  (`data-newsletter-prompt` on `<html>`), arrives standing under Reduce
  Motion, and one dismissal sends it from all four for the life of the
  page. The caption a sighted reader sees is ONE line under the dots — an
  octopus's bubble while one says it, plain text otherwise — and each
  slide keeps its own caption visually hidden for screen readers and
  crawlers. A screen reader therefore meets the current caption twice
  while the octopus is out, in its slide and in the name of the bubble's
  button; that is deliberate, since a button's name has to contain its
  visible text (WCAG 2.5.3). Only what the reader asks the corner for is
  announced, from a live region; what it opens by itself is not.
- **The support card** is the sibling sites' too: copy, sponsors and the
  two buttons in a card at the foot of the page, after the footer's link
  row, which replaced a whole section of pitch mid-page. Its room above is
  the octopus's: at the bottom of a phone's scroll the card has to sit low
  enough for the octopus on it to clear the fixed nav, which is why it
  follows the link row rather than opening the footer.
- **Focus reveals.** A focused element is scrolled only as far as the
  viewport's edge, which can leave it inside the band the observer's
  margin excludes, so `landing.js` reveals every armed scope a focus
  lands in, at once.
- **Seeing it.** A screenshot cannot show motion: film it by driving
  Chrome (Playwright or raw CDP), scrolling with `behavior: 'instant'` —
  `html` scrolls smoothly, which shifts every timing — and setting
  `octoscope-newsletter-prompt-dismissed` in `localStorage` first unless
  the prompt is what is being filmed. A reveal is one-shot, so load fresh
  for each section. Sample a spring numerically rather than trusting a
  frame: on `.not-shown`, scaleY goes 0.70 → 1.048 at ~500 ms → 1 by
  ~1.4 s.
- **What it costs, measured before it ships.** At rest the motion costs
  nothing measurable; a scroll past it is where it pays, so measure a
  scroll, not only the load (Lighthouse stops at the load and never sees
  a reveal). Three rules came out of the 0.36.0 audit (2026-09-29):
  - **Nothing the compositor cannot run, on anything nobody sees.** The
    ring a landing card catches animates `--glint`, a registered custom
    property: a style recalculation on the main thread every frame, for
    every ring. It runs only on the cards inside a clipping band as the
    band arrives (`data-glint`, set by `landing.js`). Lit on all 72 of
    the marquee's cards, it doubled the main thread's share of a scroll
    at phone speed (12% → 24%) for about three that could be seen.
  - **The carousel's shots after the first are `loading="lazy"`**, and
    `landing.js` loads each a dwell ahead of its turn, never before the
    page's load event, and not at all while the carousel is off screen.
    All ten loaded with the page before: 5.2 MB. Chrome's own lazy
    loading still fetches the one or two nearest the first, because it
    measures distance without the carousel's clip.
    `TestLandingCarouselShotsAreLazy` holds the markup to it.
  - **Every `<img>` carries its real `width` and `height`**
    (`TestLandingImagesAreSized` reads each file's own). The logo did
    not, and on a slow phone the hero jumped when its first bytes
    arrived after the first paint: CLS 0.178 in five of five runs.

  The reveal lag is 64px rather than 8% of the window for the same
  reason a crawler matters: 8% of a window stretched to the whole page
  is a band a section fits in, and "Get release updates" stayed hidden
  in it for good.

#### Seeing the landing

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

#### Release prep on the site (checklist steps 4–5)

4. `docs/index.html` — the hero version pill (`#version-pill`) now
   auto-updates via a fetch to GitHub Releases API on page load,
   but the inlined fallback value should still be current in case
   the API is unreachable (rate limit, offline preview). **Any
   headline feature added in this release should also get a card in
   the "At a glance" grid** — the README and the landing tell the
   same story, don't let them drift.
5. **`docs/guide/` — the feature has to be documented here too, or
   the guide silently becomes the stalest surface octoscope has.**
   The README is canonical and the guide is the narrative version of
   it, so a change that earns a README line earns a guide edit: the
   page that owns the behaviour (a new flag → `flags.html`, a new key
   → `keybinds.html` *and* the guide page that explains the surface,
   a new config key → `settings.html`). The version in the sidebar
   brand auto-updates from the Releases API since 0.26.0 — only its
   inline fallback in `docs/guide/docs.js` (`#guide-ver`) needs
   bumping, same deal as the landing's pill. Adding a *page* is the
   one heavier case: create the file, add it to `NAV`, wire the
   pager chain at **both** ends, and list it in `docs/sitemap.xml`
   (see *Shared chrome* above).

#### Verifying a deploy

One rule from the maintainer's `/octoscope-release` belongs in public
because it is a trap rather than a step: **never verify the landing by its rendered version pill.** The pill
fetches the version from the Releases API on load, so it shows the new
number even when the deploy never happened and the page being served is
the previous release's — a check that cannot fail. Ask Pages whether it
built and read the bytes it serves, including a string from this
release's new copy. Measured 2026-08-05: Pages had failed **seven
consecutive times over twenty-two hours** while the pill check would have
passed throughout ([#122](https://github.com/gfazioli/octoscope/issues/122)).
A failed deploy is fixed by a commit to `main` — the site builds from
`main`, not from the tag — never by a patch release.
