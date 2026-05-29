package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
)

// newTestModel builds a minimal Model wired to a real (offline) client
// pointed at configPath. The GITHUB_TOKEN env forces github.New down
// its fast, network-free path (no `gh auth token` shell-out), so the
// test stays hermetic.
func newTestModel(t *testing.T, configPath string, publicOnly bool, pinned []string) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, err := github.New("octocat", github.Options{PublicOnly: publicOnly})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	return Model{
		client:      client,
		configPath:  configPath,
		interval:    60 * time.Second,
		compact:     false,
		theme:       "octoscope",
		accentColor: "",
		pinned:      append([]string(nil), pinned...),
	}
}

// TestPersistConfigPreservesHandEditedLists is the regression test for
// the v0.x data-loss bug: persisting UI settings (public-only toggle,
// settings panel) used to rewrite the file from a struct literal that
// omitted pinned_repos / watch_repos, silently erasing them. The
// read-modify-write persistConfig helper must keep both — pinned_repos
// from the Model, watch_repos straight off disk (no runtime surface).
func TestPersistConfigPreservesHandEditedLists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// A hand-written config carrying BOTH lists plus an accent the
	// Model also tracks.
	seed := config.Defaults()
	seed.PinnedRepos = []string{"gfazioli/octoscope"}
	seed.WatchRepos = []string{"charmbracelet/bubbletea", "cli/cli"}
	if err := config.Save(path, seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// Boot a Model as if launched against that file, with the pinned
	// list loaded into memory (NewModel does this from opts.PinnedRepos).
	m := newTestModel(t, path, false, seed.PinnedRepos)

	// Simulate the user flipping public-only and changing the interval
	// via the settings panel, then persisting — the exact path that
	// used to clobber the file.
	m.client.SetPublicOnly(true)
	m.interval = 30 * time.Second
	m.compact = true
	if err := m.persistConfig(); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	// Reload from disk and assert nothing was lost.
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got.PinnedRepos) != 1 || got.PinnedRepos[0] != "gfazioli/octoscope" {
		t.Errorf("pinned_repos lost: got %v, want [gfazioli/octoscope]", got.PinnedRepos)
	}
	if len(got.WatchRepos) != 2 ||
		got.WatchRepos[0] != "charmbracelet/bubbletea" || got.WatchRepos[1] != "cli/cli" {
		t.Errorf("watch_repos lost: got %v, want [charmbracelet/bubbletea cli/cli]", got.WatchRepos)
	}
	// And the UI changes did land.
	if !got.PublicOnly {
		t.Error("public_only not persisted")
	}
	if got.RefreshInterval != 30*time.Second {
		t.Errorf("refresh_interval = %v, want 30s", got.RefreshInterval)
	}
	if !got.Compact {
		t.Error("compact not persisted")
	}
}

// TestPersistConfigLeavesFileUntouchedOnReadError pins the safety
// invariant: if the on-disk file can't be re-read, persistConfig must
// NOT overwrite it (which would nuke hand-edits with Defaults()), and
// must return the error so the caller can keep state in memory only.
func TestPersistConfigLeavesFileUntouchedOnReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write a deliberately malformed TOML so config.Load errors.
	if err := os.WriteFile(path, []byte("this is = = not valid toml ]["), 0o600); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	m := newTestModel(t, path, false, []string{"gfazioli/octoscope"})
	if err := m.persistConfig(); err == nil {
		t.Fatal("persistConfig returned nil on an unreadable config; expected an error")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("persistConfig overwrote an unreadable config file:\nbefore=%q\nafter=%q", before, after)
	}
}

// TestPersistConfigNoPathIsNoOp confirms a config-less launch keeps
// settings in memory only without error.
func TestPersistConfigNoPathIsNoOp(t *testing.T) {
	m := newTestModel(t, "", false, nil)
	if err := m.persistConfig(); err != nil {
		t.Errorf("persistConfig with empty path = %v, want nil", err)
	}
}
