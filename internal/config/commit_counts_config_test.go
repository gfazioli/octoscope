package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitCountsConfigRoundTrip pins the #70 commit_counts key: off by
// default, emitted by Save either way (a settings-panel save must not
// drop a hand-edited key), and read back by Load.
func TestCommitCountsConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if def := Defaults(); def.CommitCounts {
		t.Fatalf("defaults: CommitCounts=%v, want false — the column costs a query per refresh", def.CommitCounts)
	}

	if err := Save(path, Defaults()); err != nil {
		t.Fatalf("save defaults: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "commit_counts = false") {
		t.Errorf("pristine config missing commit_counts:\n%s", body)
	}

	c := Defaults()
	c.CommitCounts = true
	if err := Save(path, c); err != nil {
		t.Fatalf("save opted-in: %v", err)
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), "commit_counts = true") {
		t.Errorf("opted-in config should carry commit_counts = true:\n%s", body)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !got.CommitCounts {
		t.Error("CommitCounts should round-trip true")
	}

	// A file that never mentions the key keeps the default rather than
	// reading the absent key as false-by-accident-of-zero-value.
	if err := os.WriteFile(path, []byte("refresh_interval = \"1m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("load minimal: %v", err)
	}
	if got.CommitCounts {
		t.Error("absent commit_counts must stay at the default (false)")
	}
}
