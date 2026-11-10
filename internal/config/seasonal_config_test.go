package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeasonalThemeConfigRoundTrip pins the seasonal_theme key: on by
// default, a full opt-out round-trip, and on for a config file written
// before the key existed.
func TestSeasonalThemeConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if def := Defaults(); !def.SeasonalTheme {
		t.Fatalf("defaults: SeasonalTheme=%v, want true", def.SeasonalTheme)
	}

	if err := Save(path, Defaults()); err != nil {
		t.Fatalf("save defaults: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "seasonal_theme = true") {
		t.Errorf("pristine config missing seasonal_theme:\n%s", body)
	}
	if got, err := Load(path); err != nil || !got.SeasonalTheme {
		t.Errorf("pristine config reloads SeasonalTheme=%v (err %v), want true", got.SeasonalTheme, err)
	}

	c := Defaults()
	c.SeasonalTheme = false
	if err := Save(path, c); err != nil {
		t.Fatalf("save opted-out: %v", err)
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), "seasonal_theme = false") {
		t.Errorf("opted-out config should carry seasonal_theme = false:\n%s", body)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SeasonalTheme {
		t.Error("SeasonalTheme should round-trip false")
	}
	// The key sits beside the theme it stands in for, and the save
	// leaves the theme itself alone.
	if got.Theme != "octoscope" {
		t.Errorf("theme became %q", got.Theme)
	}

	old := filepath.Join(dir, "old.toml")
	if err := os.WriteFile(old, []byte("theme = \"octoscope\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oc, err := Load(old)
	if err != nil {
		t.Fatalf("load old: %v", err)
	}
	if !oc.SeasonalTheme {
		t.Error("absent seasonal_theme should default to true")
	}
}
