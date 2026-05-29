package ui

import (
	"os"
	"path/filepath"
	"strings"
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

	// Change UI settings, then persist through the single helper every
	// save path routes through. (The end-to-end tests below drive the
	// actual Update / applySettingsAndClose callers; this one pins the
	// helper itself.)
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

// TestApplySettingsAndClosePreservesLists drives the real settings-panel
// caller — one of the two paths the P0 data-loss bug actually lived in —
// end-to-end: stage new values via SettingsModel.Open, call
// applySettingsAndClose, and assert pinned_repos / watch_repos survive on
// disk. This guards the wiring, not just the helper: a future
// struct-literal config.Save reintroduced in this caller (the exact
// shape of the original bug) fails here even though the helper-only tests
// would stay green.
func TestApplySettingsAndClosePreservesLists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	seed := config.Defaults()
	seed.PinnedRepos = []string{"gfazioli/octoscope"}
	seed.WatchRepos = []string{"charmbracelet/bubbletea", "cli/cli"}
	if err := config.Save(path, seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	m := newTestModel(t, path, false, seed.PinnedRepos)
	m.stats = nil // viewport syncs become safe no-ops

	// Stage the new values exactly as the settings modal would, keeping
	// the theme unchanged so applySettingsAndClose doesn't touch the
	// spinner / re-apply the theme.
	m.settings = m.settings.Open(30*time.Second, true, true, m.theme)
	_ = m.applySettingsAndClose()

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got.PinnedRepos) != 1 || got.PinnedRepos[0] != "gfazioli/octoscope" {
		t.Errorf("pinned_repos lost via settings save: %v", got.PinnedRepos)
	}
	if len(got.WatchRepos) != 2 {
		t.Errorf("watch_repos lost via settings save: %v", got.WatchRepos)
	}
	if !got.PublicOnly || got.RefreshInterval != 30*time.Second || !got.Compact {
		t.Errorf("settings not applied: public=%v interval=%v compact=%v",
			got.PublicOnly, got.RefreshInterval, got.Compact)
	}
}

// TestPinToggleThroughUpdate drives the pin-toggle caller end-to-end via
// Update(pinToggledMsg), covering the success and error branches that
// the helper-only tests don't reach.
func TestPinToggleThroughUpdate(t *testing.T) {
	const repoURL = "https://github.com/gfazioli/octoscope"

	t.Run("success persists pin and preserves watch_repos", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		seed := config.Defaults()
		seed.WatchRepos = []string{"charmbracelet/bubbletea"}
		if err := config.Save(path, seed); err != nil {
			t.Fatalf("seed Save: %v", err)
		}

		m := newTestModel(t, path, false, nil)
		updated, _ := m.Update(pinToggledMsg{url: repoURL, pin: true})
		m2 := updated.(Model)

		if len(m2.pinned) != 1 || m2.pinned[0] != "gfazioli/octoscope" {
			t.Errorf("pinned in memory = %v, want [gfazioli/octoscope]", m2.pinned)
		}
		if !strings.Contains(m2.toastMsg, "pinned") {
			t.Errorf("toast = %q, want a pinned confirmation", m2.toastMsg)
		}
		got, err := config.Load(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if len(got.PinnedRepos) != 1 || got.PinnedRepos[0] != "gfazioli/octoscope" {
			t.Errorf("pinned_repos on disk = %v", got.PinnedRepos)
		}
		if len(got.WatchRepos) != 1 || got.WatchRepos[0] != "charmbracelet/bubbletea" {
			t.Errorf("watch_repos lost on pin save: %v", got.WatchRepos)
		}
	})

	t.Run("unreadable config keeps pin in memory and reports the error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("= = not valid toml ]["), 0o600); err != nil {
			t.Fatalf("write malformed: %v", err)
		}
		before, _ := os.ReadFile(path)

		m := newTestModel(t, path, false, nil)
		updated, _ := m.Update(pinToggledMsg{url: repoURL, pin: true})
		m2 := updated.(Model)

		if len(m2.pinned) != 1 {
			t.Errorf("pin should be kept in memory on save failure, got %v", m2.pinned)
		}
		if !strings.Contains(m2.toastMsg, "kept in memory only") {
			t.Errorf("toast = %q, want an in-memory-only failure message", m2.toastMsg)
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Errorf("unreadable config was overwritten:\nbefore=%q\nafter=%q", before, after)
		}
	})
}
