package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestViewPrefKeysLoadAndRoundTrip pins the #35 config keys: they load
// from TOML, default to empty (no behavioural change when unset), and
// survive the Load→Save round-trip persistConfig relies on — a settings
// panel write must not drop a hand-edited default_* key.
func TestViewPrefKeysLoadAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	src := `
default_sort = "stars"
default_work_filter = "ci-broken"
default_star_history = "cumulative"
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultSort != "stars" || cfg.DefaultWorkFilter != "ci-broken" || cfg.DefaultStarHistory != "cumulative" {
		t.Errorf("loaded = (%q, %q, %q), want (stars, ci-broken, cumulative)",
			cfg.DefaultSort, cfg.DefaultWorkFilter, cfg.DefaultStarHistory)
	}

	// Round-trip: Save must carry the keys back to disk.
	out := filepath.Join(dir, "roundtrip.toml")
	if err := Save(out, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(out)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if back.DefaultSort != "stars" || back.DefaultWorkFilter != "ci-broken" || back.DefaultStarHistory != "cumulative" {
		t.Errorf("round-trip = (%q, %q, %q), want the same three values",
			back.DefaultSort, back.DefaultWorkFilter, back.DefaultStarHistory)
	}

	// Unset keys stay empty — the "no behavioural change" guarantee.
	d := Defaults()
	if d.DefaultSort != "" || d.DefaultWorkFilter != "" || d.DefaultStarHistory != "" {
		t.Errorf("Defaults() view prefs = (%q, %q, %q), want all empty",
			d.DefaultSort, d.DefaultWorkFilter, d.DefaultStarHistory)
	}
}
