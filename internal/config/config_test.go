package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSanitizePinnedRepos pins the contract: drop empties, drop
// malformed entries, de-duplicate case-insensitively, preserve
// first-occurrence order, return nil for an all-empty input.
func TestSanitizePinnedRepos(t *testing.T) {
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
			got := SanitizePinnedRepos(tt.in)
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
