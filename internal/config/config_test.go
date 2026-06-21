package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSanitizeRepoList pins the contract: drop empties, drop
// malformed entries, de-duplicate case-insensitively, preserve
// first-occurrence order, return nil for an all-empty input.
func TestSanitizeRepoList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "nil in, nil out",
			in:   nil,
			want: nil,
		},
		{
			name: "all valid passes through",
			in:   []string{"gfazioli/octoscope", "charmbracelet/glamour"},
			want: []string{"gfazioli/octoscope", "charmbracelet/glamour"},
		},
		{
			name: "empty and whitespace entries dropped",
			in:   []string{"", "  ", "gfazioli/octoscope"},
			want: []string{"gfazioli/octoscope"},
		},
		{
			name: "malformed dropped — missing owner",
			in:   []string{"/octoscope", "gfazioli/octoscope"},
			want: []string{"gfazioli/octoscope"},
		},
		{
			name: "malformed dropped — missing name",
			in:   []string{"gfazioli/", "gfazioli/octoscope"},
			want: []string{"gfazioli/octoscope"},
		},
		{
			name: "malformed dropped — too many segments",
			in:   []string{"gfazioli/octoscope/x", "gfazioli/octoscope"},
			want: []string{"gfazioli/octoscope"},
		},
		{
			name: "duplicates dropped, case-insensitive, first wins",
			in:   []string{"gfazioli/Octoscope", "GFAZIOLI/octoscope"},
			want: []string{"gfazioli/Octoscope"},
		},
		{
			name: "all entries invalid → nil",
			in:   []string{"", "/", "no-slash", "a/b/c"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeRepoList(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestLoadPinnedRepos covers the end-to-end TOML parse + sanitize
// path: a config file with mixed valid / malformed pinned_repos
// produces a clean slice on the Config.
func TestLoadPinnedRepos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := []byte(`
refresh_interval = "30s"
pinned_repos = [
  "gfazioli/octoscope",
  "",
  "no-slash",
  "GFAZIOLI/OCTOSCOPE",
  "charmbracelet/glamour",
]
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"gfazioli/octoscope", "charmbracelet/glamour"}
	if !reflect.DeepEqual(cfg.PinnedRepos, want) {
		t.Errorf("pinned = %#v, want %#v", cfg.PinnedRepos, want)
	}
}

// TestSanitizeIssueList pins the "owner/name#N" contract: accept a
// well-formed identifier, reject the malformed shapes (no "#", empty
// or non-numeric number, signed number, bad owner/name), trim
// surrounding whitespace, and de-duplicate case-insensitively keeping
// the first occurrence.
func TestSanitizeIssueList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "nil in, nil out",
			in:   nil,
			want: nil,
		},
		{
			name: "valid passes through",
			in:   []string{"gfazioli/octoscope#42", "charmbracelet/glamour#7"},
			want: []string{"gfazioli/octoscope#42", "charmbracelet/glamour#7"},
		},
		{
			name: "leading/trailing whitespace trimmed",
			in:   []string{"  gfazioli/octoscope#42  "},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "missing # dropped",
			in:   []string{"gfazioli/octoscope", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "empty number dropped",
			in:   []string{"gfazioli/octoscope#", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "non-numeric number dropped",
			in:   []string{"gfazioli/octoscope#abc", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "signed number dropped",
			in:   []string{"gfazioli/octoscope#-1", "gfazioli/octoscope#+3", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "bad owner/name dropped — missing owner",
			in:   []string{"/octoscope#42", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "bad owner/name dropped — missing name",
			in:   []string{"gfazioli/#42", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "bad owner/name dropped — too many segments",
			in:   []string{"a/b/c#42", "gfazioli/octoscope#42"},
			want: []string{"gfazioli/octoscope#42"},
		},
		{
			name: "bare owner/name (no #N) rejected",
			in:   []string{"gfazioli/octoscope"},
			want: nil,
		},
		{
			name: "duplicates dropped, case-insensitive, first wins",
			in:   []string{"gfazioli/Octoscope#42", "GFAZIOLI/OCTOSCOPE#42"},
			want: []string{"gfazioli/Octoscope#42"},
		},
		{
			name: "same repo different numbers kept",
			in:   []string{"gfazioli/octoscope#42", "gfazioli/octoscope#7"},
			want: []string{"gfazioli/octoscope#42", "gfazioli/octoscope#7"},
		},
		{
			name: "all entries invalid → nil",
			in:   []string{"", "no-hash", "a/b#", "a/b#x", "/x#1"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeIssueList(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestLoadPinnedIssues covers the end-to-end TOML parse + sanitize
// path for pinned_issues: a file with mixed valid / malformed entries
// produces a clean slice on the Config.
func TestLoadPinnedIssues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := []byte(`
refresh_interval = "30s"
pinned_issues = [
  "gfazioli/octoscope#42",
  "",
  "no-hash",
  "gfazioli/octoscope#abc",
  "GFAZIOLI/OCTOSCOPE#42",
  "charmbracelet/glamour#7",
]
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"gfazioli/octoscope#42", "charmbracelet/glamour#7"}
	if !reflect.DeepEqual(cfg.PinnedIssues, want) {
		t.Errorf("pinnedIssues = %#v, want %#v", cfg.PinnedIssues, want)
	}
}

// TestSaveLoadPinnedIssuesRoundTrip asserts a pinned_issues list
// written by Save survives a subsequent Load unchanged — the property
// the runtime P toggle relies on.
func TestSaveLoadPinnedIssuesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Defaults()
	cfg.PinnedIssues = []string{"gfazioli/octoscope#42", "charmbracelet/glamour#7"}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.PinnedIssues, cfg.PinnedIssues) {
		t.Errorf("round-trip pinnedIssues = %#v, want %#v", got.PinnedIssues, cfg.PinnedIssues)
	}
}
