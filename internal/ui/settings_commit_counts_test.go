package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/config"
)

// The commit-count column (#70) got a settings-panel toggle in 0.32.0.
// Three things have to hold: the panel stages it like the sponsor knob,
// saving applies it to BOTH halves that read it (the ReposModel for the
// sort cycle, the client for the fetch) and refetches at once, and the
// key round-trips to disk through persistConfig.

func TestSettingsCommitCountsToggle(t *testing.T) {
	sm := SettingsModel{}.Open(30*time.Second, false, false, "octoscope", "", true, true)
	if !sm.CommitCounts() {
		t.Fatal("seeded CommitCounts() = false, want true")
	}
	sm.focus = fieldCommitCounts
	sm, _ = sm.Update(key(" "))
	if sm.CommitCounts() {
		t.Error("space on the commit-counts row should toggle it off")
	}
	// It is the last row: down from the sponsor row lands on it, and
	// down again stays.
	sm.focus = fieldShowSponsor
	sm, _ = sm.Update(key("down"))
	if sm.focus != fieldCommitCounts {
		t.Errorf("down from sponsor: focus = %v, want fieldCommitCounts", sm.focus)
	}
	sm, _ = sm.Update(key("down"))
	if sm.focus != fieldCommitCounts {
		t.Errorf("down past the last row moved focus to %v", sm.focus)
	}
	if !strings.Contains(sm.View(100), "Commit counts") {
		t.Error("the panel does not render the Commit counts row")
	}
}

func TestApplySettingsCommitCountsRefetchesAndSetsBothHalves(t *testing.T) {
	m := newTestModel(t, "", false, nil)
	if m.repos.commitCounts || m.client.CommitCounts() {
		t.Fatal("harness should start with commit counts off")
	}
	m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor, false)
	m.settings.focus = fieldCommitCounts
	m.settings, _ = m.settings.Update(key(" "))

	cmd := m.applySettingsAndClose()
	if !m.repos.commitCounts {
		t.Error("ReposModel.commitCounts not set — the s cycle would keep skipping commits")
	}
	if !m.client.CommitCounts() {
		t.Error("client.commitCounts not set — the next fetch would not run the branch")
	}
	if cmd == nil || !m.loading {
		t.Errorf("a changed toggle must refetch at once: cmd=%v loading=%v", cmd != nil, m.loading)
	}
	if m.refreshGen != 0 {
		t.Errorf("toggle alone must not bump the tick generation, got %d", m.refreshGen)
	}

	// Unchanged value: no refetch, no spinner.
	m2 := newTestModel(t, "", false, nil)
	m2.settings = m2.settings.Open(m2.interval, m2.compact, m2.client.PublicOnly(), m2.theme, m2.accentColor, m2.showSponsor, false)
	if cmd := m2.applySettingsAndClose(); cmd != nil || m2.loading {
		t.Errorf("unchanged toggle must not refetch: cmd=%v loading=%v", cmd != nil, m2.loading)
	}
}

func TestPersistConfigWritesCommitCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.Defaults()); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, path, false, nil)
	m.repos.commitCounts = true
	if err := m.persistConfig(); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !got.CommitCounts {
		body, _ := os.ReadFile(path)
		t.Errorf("commit_counts not persisted:\n%s", body)
	}
}
