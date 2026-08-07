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

// TestIsGitHubURL pins the gate that decides whether a GitHub-sourced
// URL may enter an OSC 8 escape. The rejections matter more than the
// acceptances: each is a way an unexpected URL could either break out
// of the escape sequence or turn a GitHub-looking row into a one-click
// trip somewhere else.
func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Accepted — what GitHub returns for a check run.
		{"actions run", "https://github.com/gfazioli/octoscope/actions/runs/30474260211", true},
		{"job anchor", "https://github.com/o/r/actions/runs/1/job/2", true},
		{"query string", "https://github.com/o/r/actions/runs/1?check_suite_focus=true", true},
		{"fragment", "https://github.com/o/r/actions/runs/1#step:3:12", true},
		{"subdomain", "https://api.github.com/repos/o/r", true},
		{"host case is irrelevant", "https://GitHub.com/o/r", true},

		// Rejected — scheme.
		{"http downgrade", "http://github.com/o/r", false},
		{"javascript", "javascript:alert(1)", false},
		{"file", "file:///etc/passwd", false},
		{"data", "data:text/html,<script>", false},
		{"scheme-relative", "//github.com/o/r", false},
		{"no scheme", "github.com/o/r", false},

		// Rejected — host. A third-party CI provider reporting through
		// the Checks API is legitimate, but must not become clickable.
		{"third-party CI", "https://circleci.com/gh/o/r/123", false},
		{"lookalike suffix", "https://github.com.evil.test/o/r", false},
		{"lookalike prefix", "https://notgithub.com/o/r", false},
		{"github in path only", "https://evil.test/github.com/o/r", false},

		// Rejected — escape integrity. ';' terminates the OSC 8 URI
		// early; ESC and BEL can open or close a sequence outright.
		{"semicolon", "https://github.com/o/r;evil", false},
		{"escape byte", "https://github.com/o/r\x1b]8;;evil\x07", false},
		{"bell byte", "https://github.com/o/r\x07", false},
		{"newline", "https://github.com/o/r\nevil", false},
		{"tab", "https://github.com/o/\tr", false},
		{"del", "https://github.com/o/r\x7f", false},

		// Rejected — degenerate.
		{"empty", "", false},
		{"overlong", "https://github.com/" + strings.Repeat("a", 2100), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGitHubURL(tt.url); got != tt.want {
				t.Errorf("isGitHubURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// TestGitHubHyperlink asserts the two outcomes callers rely on: a
// vouched URL becomes a real OSC 8 link, anything else degrades to
// inert text. Either way the visible string is only ever the label,
// which is what keeps column widths and ansi.Strip honest.
func TestGitHubHyperlink(t *testing.T) {
	const label = "build (ubuntu-latest)"

	linked := githubHyperlink("https://github.com/o/r/actions/runs/1", label)
	if linked == label {
		t.Error("a github.com https URL should have been linked, got plain text")
	}
	if !strings.Contains(linked, "\x1b]8;") {
		t.Errorf("linked output carries no OSC 8 sequence: %q", linked)
	}
	if got := ansi.Strip(linked); got != label {
		t.Errorf("visible text = %q, want %q — width math depends on this", got, label)
	}

	for _, bad := range []string{"", "http://github.com/o/r", "https://circleci.com/x", "https://github.com/o/r;x"} {
		if got := githubHyperlink(bad, label); got != label {
			t.Errorf("githubHyperlink(%q) = %q, want the bare label", bad, got)
		}
	}
}

// isSafeOpenURL is the browser-open gate: the scheme and control-character
// half of isGitHubURL, deliberately **without** the host check.
//
// The two halves exist separately because openURLCmd hands its argument to
// the OS opener, which will act on `file:` or a custom scheme — that is the
// hazard, and it is identical on every tab. But the destinations are not
// all GitHub: the support link points at a payment page on purpose, so a
// host check at that choke point would break a working feature rather than
// protect anything. Split on #123.
func TestIsSafeOpenURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"a GitHub URL", "https://github.com/gfazioli/octoscope", true},
		{"a gist URL", "https://gist.github.com/gfazioli/abc123", true},
		// The case that must keep working: off-GitHub and intended.
		{"the payment link", "https://donate.stripe.com/xyz", true},
		{"plain http is not enough", "http://github.com/x", false},
		{"file scheme", "file:///etc/passwd", false},
		{"data scheme", "data:text/html,<script>alert(1)</script>", false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"custom scheme the OS opener would act on", "x-devonthink-item://abc", false},
		{"escape-sequence breakout", "https://github.com/\x1b]8;;x\x07", false},
		{"a bare control character", "https://github.com/a\x01b", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeOpenURL(tt.in); got != tt.want {
				t.Errorf("isSafeOpenURL(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The gate has to be *at* openURLCmd, not at each call site, or the next
// tab added forgets it. And it must not be the host check: the support
// link is the regression this pins.
func TestOpenURLCmdGatesTheScheme(t *testing.T) {
	if cmd := openURLCmd("file:///etc/passwd"); cmd != nil {
		t.Error("openURLCmd accepted a file: URL; the OS opener would act on it")
	}
	if cmd := openURLCmd("javascript:alert(1)"); cmd != nil {
		t.Error("openURLCmd accepted a javascript: URL")
	}
	if cmd := openURLCmd(""); cmd != nil {
		t.Error("openURLCmd accepted an empty URL")
	}
	if cmd := openURLCmd("https://github.com/gfazioli/octoscope"); cmd == nil {
		t.Error("openURLCmd rejected an ordinary GitHub URL")
	}
	if cmd := openURLCmd(coffeeURL); cmd == nil {
		t.Errorf("openURLCmd rejected the support link (%s) — a host check here "+
			"would break it, which is why the scheme half was split out", coffeeURL)
	}
	if cmd := openURLCmd(sponsorURL); cmd == nil {
		t.Error("openURLCmd rejected the sponsor link")
	}
}
