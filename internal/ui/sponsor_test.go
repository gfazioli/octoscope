package ui

import (
	"testing"

	"github.com/gfazioli/octoscope/internal/github"
)

// TestSponsorSplashGating pins when the splash opens at launch: on when
// show_sponsor is true and we're not in --public-only mode, off
// otherwise. Setting GITHUB_TOKEN makes auth.Token() return immediately
// instead of shelling out to `gh auth token`, keeping the test hermetic
// and subprocess-free.
func TestSponsorSplashGating(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")

	mk := func(show, public bool) Model {
		client, err := github.New("octocat", github.Options{PublicOnly: public})
		if err != nil {
			t.Fatalf("github.New: %v", err)
		}
		return NewModel(client, "test", Options{ShowSponsor: show})
	}

	cases := []struct {
		name        string
		show, pub   bool
		wantOpen    bool
		description string
	}{
		{"on, not public", true, false, true, "default: splash opens every launch"},
		{"opted out", false, false, false, "show_sponsor=false suppresses it"},
		{"public-only", true, true, false, "--public-only suppresses it even when on"},
		{"off and public", false, true, false, "both off"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mk(c.show, c.pub).sponsor.IsOpen(); got != c.wantOpen {
				t.Errorf("sponsor.IsOpen() = %v, want %v (%s)", got, c.wantOpen, c.description)
			}
		})
	}
}
