# docs/ — the website

`.github/workflows/pages.yml` publishes `docs/` verbatim: no Jekyll, no
front matter, so markdown here (this file, `design/`) is served raw. A
failed publish is a red run of that workflow on the causing commit.
Incidents and measurements behind these rules:
`contributor-notes/docs-area-history.md`. The root `CLAUDE.md` holds the
README/guide, privacy-page, `SYSTEMS` and version-pill triggers.

## Landing and guide

- `index.html` is the marketing landing (hero, "At a glance", CTA, footer);
  it must not re-explain what `guide/` documents. Duplicated how-to drifts.
- Hand-authored static HTML. Do not add a generator or toolchain for tidiness.
- Shared chrome: `guide/style.css` and the sidebar/topbar that
  `guide/docs.js` builds from `NAV`. A new page goes in `NAV`, the
  hand-maintained pager chain at both ends (an insert strands its
  neighbour), `sitemap.xml` (no dates), and gets the `<head>` block the
  other pages carry: its own canonical, icons, share tags. `site_test.go`
  enforces sitemap, canonical, one `<main>`, one `<h1>`, a 50–160-character
  description.
- Every page links `fonts/fonts.css` itself; `style.css` only names the
  self-hosted faces, so a missing link silently renders the system font.
  Never load Google Fonts (`TestSitePagesServeTheirOwnFonts`).
- The publisher line (copyright, publisher, VAT, Legal/Privacy) is in the
  landing footer markup (the VAT number must be on the home page) and under
  guide pages via `docs.js`. `guide/legal.html` and `guide/privacy.html`
  stay out of `NAV` and the pager.
- Dark is the default with no `prefers-color-scheme` fallback; light is
  opt-in via the toggle's `data-theme` on the root. Read the theme only as
  `data-theme !== "light"`, never from the OS preference.
- Landing affordances must be keyboard-reachable. Add `tabindex`/`role`/
  keydown in JS, not markup, and skip the marquee's `aria-hidden` clones.
- The Download button links only files the Releases API lists, never one
  built from the pill's inline version, and never guesses the architecture
  (Chromium reports it and the bitness, Firefox only on Linux/Windows; every
  other Mac browser says "Intel"). Without a 64-bit answer keep the all-builds link plus this
  system's archives by name.

## Motion (`landing.js`, CSS in the landing's `<style>`)

- Reveals: `data-reveal` (`rise`, `morph`, `squash`, `pop`) items grouped by
  `data-scope`. Served HTML is at rest; a scope gets `data-armed` only when
  measured fully off screen. Every hiding rule must require `data-armed`;
  no reveal state in served markup; every variant has a pose
  (`landing_test.go`). Poses use `translate`/`scale`, never `transform`.
  Focus reveals every armed scope it lands in. The observer lag is 64px,
  not a share of the window.
- Never hand-edit the `springs:begin`…`springs:end` block: paste what
  `springsCSS()` in `landing_test.go` prints.
- The octopus is `mascotLaunch`: `#octopus-art` JSON and
  `guide/octopus.svg` are held to the Go drawing by
  `internal/ui/mascot_site_test.go`. On the landing it is one character in
  four places (version link, carousel dots, window corner, support card);
  in the guide a static `.oc-note` on five pages, never all. Rules: never
  two on screen; placement is measured from boxes every scrolled frame,
  never an IntersectionObserver; it waits while `data-newsletter-prompt` is
  on `<html>`, arrives standing under Reduce Motion, and one dismissal
  hides all four. Bubbles say only the page's own claims; the thresholds
  are constants in `landing.js`.
- One visible caption line under the dots; each slide keeps its own caption
  visually hidden, so screen readers hear it twice by design (WCAG 2.5.3).
  Only corner content the reader asks for is announced (live region).
- The support card follows the footer link row so the octopus on it clears
  the fixed nav on phones.
- Measure a scroll, not only the load. Nothing main-thread-animated on
  anything unseen (`--glint` only on cards in the band, `data-glint`).
  Carousel shots after the first stay `loading="lazy"` and load a dwell
  ahead, after the load event, only while on screen. Every `<img>` carries
  its real `width`/`height` (`TestLandingCarouselShotsAreLazy`,
  `TestLandingImagesAreSized`).

## Seeing and shipping it

- Stills: headless Chrome, not vhs, with the sandbox disabled:
  `"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  --headless=new --hide-scrollbars --window-size=W,H --screenshot=out.png
  "file://…/docs/index.html"`. A tall height captures the whole page; for an
  interactive state force its class (e.g. `.open`) on a copy. Read the PNG.
- Motion: film by driving Chrome, scroll with `behavior: 'instant'`, set
  `octoscope-newsletter-prompt-dismissed` in `localStorage` first unless
  filming the prompt, load fresh per section (reveals are one-shot), and
  sample springs numerically.
- Release prep (steps 4–5 of `contributor-notes/release.md`): bump the
  inline fallbacks `#version-pill` (`index.html`) and `#guide-ver`
  (`guide/docs.js`); add an "At a glance" card per headline feature; a
  README-worthy change also edits the guide page that owns it (flag →
  `flags.html`, key → `keybinds.html` and the surface's page, config key →
  `settings.html`).
- Verify a deploy by Pages' build and the served bytes of a string new in
  this release, never the pill. Fix a failed deploy with a commit to `main`
  (the site builds from `main`), never a patch release.
