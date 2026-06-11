package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHyperlink(t *testing.T) {
	const url = "https://github.com/sponsors/gfazioli"

	t.Run("wraps label in OSC 8", func(t *testing.T) {
		got := hyperlink(url, "click me")
		want := ansi.SetHyperlink(url) + "click me" + ansi.ResetHyperlink()
		if got != want {
			t.Errorf("hyperlink() = %q, want %q", got, want)
		}
	})

	t.Run("strips back to the label (unsupported-terminal fallback)", func(t *testing.T) {
		if got := ansi.Strip(hyperlink(url, url)); got != url {
			t.Errorf("ansi.Strip(hyperlink) = %q, want the bare label %q", got, url)
		}
	})

	t.Run("empty url returns label unwrapped", func(t *testing.T) {
		if got := hyperlink("", "label"); got != "label" {
			t.Errorf("hyperlink(\"\", label) = %q, want %q", got, "label")
		}
	})
}

// TestSponsorURLsAreHyperlinked confirms the splash and the What's new
// tab emit OSC 8 around their URLs while the visible text stays the bare
// URL (so copy / width / strip-based assertions are unaffected).
func TestSponsorURLsAreHyperlinked(t *testing.T) {
	_ = applyTheme("octoscope", "")

	var sp SponsorModel
	spOut := sp.Open(sponsorURL).View(80)
	if !strings.Contains(spOut, ansi.SetHyperlink(sponsorURL)) {
		t.Error("sponsor splash should hyperlink the Sponsors URL")
	}
	if !strings.Contains(ansi.Strip(spOut), sponsorURL) {
		t.Error("sponsor splash should still show the bare URL after strip")
	}
	if !strings.Contains(spOut, ansi.SetHyperlink(coffeeURL)) {
		t.Error("sponsor splash should hyperlink the buy-me-a-coffee URL")
	}
	if !strings.Contains(ansi.Strip(spOut), coffeeURL) {
		t.Error("sponsor splash should still show the bare coffee URL after strip")
	}

	wn := renderWhatsNewTab("0.16.0", 80)
	if !strings.Contains(wn, ansi.SetHyperlink(sponsorURL)) {
		t.Error("What's new tab should hyperlink the sponsor URL")
	}
	if !strings.Contains(wn, ansi.SetHyperlink(releasesURL)) {
		t.Error("What's new tab (bundled) should hyperlink the releases URL")
	}

	// Fallback branch (unbundled version) also hyperlinks the releases URL.
	fb := renderWhatsNewTab("0.0.0-dev", 80)
	if !strings.Contains(fb, ansi.SetHyperlink(releasesURL)) {
		t.Error("What's new tab (fallback) should hyperlink the releases URL")
	}
}
