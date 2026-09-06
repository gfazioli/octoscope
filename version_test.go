package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/gfazioli/octoscope/internal/ui"
)

// The version number is written by hand in five places and nothing used
// to check that they agree. Every way they can drift fails quietly:
// nothing errors, nothing turns red, a feature just stops being there.
// These tests are the check, and they live in package main because that
// is where the number itself lives.
//
// `go test` runs with the package directory as its working directory,
// and main is at the repo root, so the docs surfaces can be read
// straight off disk — no build tag, no network, no fixture. Coupling a
// Go test to files under docs/ is the point rather than a wart: the
// assertion is only ever violated when the surfaces genuinely disagree.

// TestVersionHasBundledHighlights pins the What's new tab to the running
// version. The tab looks up whatsNew[version] and its miss path is
// deliberately graceful — it renders a link to the release notes instead
// of the previous release's highlights — so a forgotten entry does not
// break anything, it just quietly stops being a feature for that
// release. Nothing else would ever notice.
func TestVersionHasBundledHighlights(t *testing.T) {
	if !ui.HasBundledHighlights(version) {
		t.Errorf("no What's new entry bundled for %q — add it to whatsNew in internal/ui/whatsnew.go "+
			"(a patch may alias the preceding minor's entry, as 0.30.1 and 0.31.1 do)", version)
	}
}

// versionSurface is one place outside main.go that spells the version
// out by hand.
type versionSurface struct {
	name string
	path string
	// pat captures the version in group 1. Each is anchored on an
	// element id or a shell assignment and never on the digits
	// themselves, so a surface that is renamed, restructured or deleted
	// makes this test fail rather than silently stop being checked. A
	// check that can quietly measure nothing is the failure these tests
	// exist to prevent, so it must not be one.
	pat *regexp.Regexp
	// why says what a reader sees when this surface is stale.
	why string
}

var versionSurfaces = []versionSurface{
	{
		name: "docs/index.html hero pill",
		path: "docs/index.html",
		pat:  regexp.MustCompile(`id="version-pill"[^>]*>([^<]*)<`),
		why:  "the landing's inline fallback, rendered whenever the Releases API fetch fails",
	},
	{
		name: "docs/guide/docs.js sidebar brand",
		path: "docs/guide/docs.js",
		pat:  regexp.MustCompile(`id="guide-ver"[^>]*>v([^<]*)<`),
		why:  "the guide's inline fallback, same deal as the landing's pill",
	},
	{
		// The one surface here a user copy-pastes rather than merely
		// reads, and the only one whose staleness is invisible even in
		// the failure: the asset name embeds the version, so an old
		// value does not 404 — it installs an old octoscope.
		name: "README.md bare-binary curl snippet",
		path: "README.md",
		pat:  regexp.MustCompile(`(?m)^VERSION=(\S+)`),
		why:  "a copy-pasteable curl that silently downloads whatever version it names",
	},
}

// TestVersionSurfacesAgree asserts every hand-written copy of the
// version matches the constant the binary reports.
func TestVersionSurfacesAgree(t *testing.T) {
	for _, s := range versionSurfaces {
		t.Run(s.name, func(t *testing.T) {
			b, err := os.ReadFile(s.path)
			if err != nil {
				t.Fatalf("reading %s: %v", s.path, err)
			}
			m := s.pat.FindAllStringSubmatch(string(b), -1)
			if len(m) != 1 {
				t.Fatalf("%s: found %d matches for %s, want exactly 1 — the surface moved or was duplicated, "+
					"so this assertion is no longer checking anything (%s)", s.path, len(m), s.pat, s.why)
			}
			if got := m[0][1]; got != version {
				t.Errorf("%s carries %q, main.version is %q — %s", s.path, got, version, s.why)
			}
		})
	}
}
