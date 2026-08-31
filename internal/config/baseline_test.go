package config

import (
	"os"
	"path/filepath"
	"strings"
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

// The history is nested maps under NUL-separated keys, so the round trip
// through the store is worth pinning rather than assuming: a key that did
// not survive JSON would silently restart every history on save.
func TestSeenHistorySurvivesTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan-baselines.json")

	first := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	// Built, not typed: the separator is a real NUL byte.
	key := "main" + string(rune(0)) + ".github/workflows/ci.yml"
	in := Baselines{Repos: map[string]BaselineFingerprint{
		"o/r": {
			CapturedAt: first,
			Verdict:    "clean",
			Ignition:   map[string]string{key: "bbb"},
			Signed:     map[string]bool{"main": true},
			Seen: map[string]map[string]time.Time{
				key: {"aaa": first, "bbb": first.AddDate(0, 0, 10)},
			},
		},
	}}
	if err := SaveBaselines(path, in); err != nil {
		t.Fatalf("SaveBaselines: %v", err)
	}

	hist := LoadBaselines(path).Repos["o/r"].Seen[key]
	if len(hist) != 2 {
		t.Fatalf("history came back with %d entries: %v", len(hist), hist)
	}
	if !hist["aaa"].Equal(first) {
		t.Errorf("aaa first-observed = %v, want %v", hist["aaa"], first)
	}

	// A store from before the field existed loads with a nil history and
	// must not panic on lookup — the shape every upgrade sees once.
	legacy := filepath.Join(dir, "legacy.json")
	body := `{"repos":{"o/r":{"captured_at":"2026-05-01T09:00:00Z","verdict":"clean",` +
		`"ignition":{"k":"aaa"},"signed":{"main":true}}}}`
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := LoadBaselines(legacy).Repos["o/r"]
	if old.Seen != nil {
		t.Errorf("a legacy store invented a history: %v", old.Seen)
	}
	if _, ok := old.Seen["k"]["aaa"]; ok {
		t.Error("lookup into a nil history returned a hit")
	}

	// An empty history is omitted rather than written as null, so a first
	// scan's file is no bigger than it was before the field existed.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), `"seen"`) {
		t.Error("a populated history was not written")
	}
	bare := filepath.Join(dir, "bare.json")
	if err := SaveBaselines(bare, Baselines{Repos: map[string]BaselineFingerprint{
		"o/r": {Ignition: map[string]string{}},
	}}); err != nil {
		t.Fatalf("SaveBaselines: %v", err)
	}
	b, err := os.ReadFile(bare)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(b), `"seen"`) {
		t.Errorf("an empty history was written out:\n%s", b)
	}
}
