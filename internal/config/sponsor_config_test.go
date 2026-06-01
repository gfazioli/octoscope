package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSponsorConfigRoundTrip pins the v0.16.0 show_sponsor key:
// default-on, a full opt-out round-trip, and back-compat with pre-0.16
// config files that lack the key entirely.
func TestSponsorConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Default: splash on.
	if def := Defaults(); !def.ShowSponsor {
		t.Fatalf("defaults: ShowSponsor=%v, want true", def.ShowSponsor)
	}

	// A pristine save emits show_sponsor = true.
	if err := Save(path, Defaults()); err != nil {
		t.Fatalf("save defaults: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "show_sponsor = true") {
		t.Errorf("pristine config missing show_sponsor:\n%s", body)
	}

	// Opt out → round-trips false.
	c := Defaults()
	c.ShowSponsor = false
	if err := Save(path, c); err != nil {
		t.Fatalf("save opted-out: %v", err)
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), "show_sponsor = false") {
		t.Errorf("opted-out config should carry show_sponsor = false:\n%s", body)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.ShowSponsor {
		t.Error("ShowSponsor should round-trip false")
	}

	// Back-compat: a pre-0.16 file without the key defaults to
	// ShowSponsor=true, so existing users get the splash.
	old := filepath.Join(dir, "old.toml")
	if err := os.WriteFile(old, []byte("theme = \"amber\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oc, err := Load(old)
	if err != nil {
		t.Fatalf("load old: %v", err)
	}
	if !oc.ShowSponsor {
		t.Error("absent show_sponsor should default to true (back-compat)")
	}
}
