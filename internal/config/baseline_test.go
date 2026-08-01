package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "scan-baselines.json")

	// Missing file is the ordinary first-run state, not an error.
	if got := LoadBaselines(path); len(got.Repos) != 0 {
		t.Fatalf("missing file yielded %d repos, want 0", len(got.Repos))
	}

	when := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	in := Baselines{Repos: map[string]BaselineFingerprint{
		"gfazioli/octoscope": {
			CapturedAt: when,
			Verdict:    "clean",
			Ignition:   map[string]string{"main\x00.claude/settings.json": "abc123"},
			Signed:     map[string]bool{"main": true},
		},
	}}
	if err := SaveBaselines(path, in); err != nil {
		t.Fatalf("SaveBaselines: %v", err)
	}

	got := LoadBaselines(path)
	fp, ok := got.Repos["gfazioli/octoscope"]
	if !ok {
		t.Fatal("saved repo missing after reload")
	}
	if !fp.CapturedAt.Equal(when) {
		t.Errorf("CapturedAt = %v, want %v", fp.CapturedAt, when)
	}
	if fp.Verdict != "clean" {
		t.Errorf("Verdict = %q, want clean", fp.Verdict)
	}
	if fp.Ignition["main\x00.claude/settings.json"] != "abc123" {
		t.Errorf("ignition OID not preserved: %+v", fp.Ignition)
	}
	if !fp.Signed["main"] {
		t.Error("signed state not preserved")
	}
}

// A corrupted store must degrade to "no baseline", never block a scan:
// it is machine-written state, not a user config worth stopping for.
func TestLoadBaselinesToleratesGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan-baselines.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := LoadBaselines(path)
	if got.Repos == nil {
		t.Fatal("Repos must be non-nil so callers can index it safely")
	}
	if len(got.Repos) != 0 {
		t.Errorf("garbage yielded %d repos, want 0", len(got.Repos))
	}
}

// An empty path means "no persistence available" (no HOME, no XDG) and
// must be a silent no-op at both ends rather than an error.
func TestBaselinesEmptyPathIsANoOp(t *testing.T) {
	if got := LoadBaselines(""); len(got.Repos) != 0 {
		t.Errorf("empty path yielded %d repos, want 0", len(got.Repos))
	}
	if err := SaveBaselines("", Baselines{}); err != nil {
		t.Errorf("SaveBaselines(\"\") = %v, want nil", err)
	}
}

// The save is atomic, so a reader never sees a partial file and no
// stray temp files are left behind.
func TestSaveBaselinesLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan-baselines.json")
	if err := SaveBaselines(path, Baselines{Repos: map[string]BaselineFingerprint{
		"o/r": {CapturedAt: time.Now(), Verdict: "watch"},
	}}); err != nil {
		t.Fatalf("SaveBaselines: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "scan-baselines.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only scan-baselines.json", names)
	}
}

func TestBaselinePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-example")
	want := filepath.Join("/tmp/xdg-example", "octoscope", "scan-baselines.json")
	if got := BaselinePath(); got != want {
		t.Errorf("BaselinePath() = %q, want %q", got, want)
	}
}
