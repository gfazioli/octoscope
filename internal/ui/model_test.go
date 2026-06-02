package ui

import (
	"testing"
	"time"
)

// TestStaleTickIgnored pins the generation guard: a tick from a
// superseded chain (gen != refreshGen) must NOT trigger a fetch and
// must NOT reschedule — it self-terminates, leaving one chain alive.
func TestStaleTickIgnored(t *testing.T) {
	m := newTestModel(t, "", false, nil)
	m.refreshGen = 1 // current chain is gen 1

	updated, cmd := m.Update(tickMsg{gen: 0}) // a leftover gen-0 tick
	m2 := updated.(Model)
	if m2.loading {
		t.Error("a stale tick must not start a fetch (loading should stay false)")
	}
	if cmd != nil {
		t.Error("a stale tick must not reschedule (cmd should be nil)")
	}
}

// TestCurrentGenTickFetches confirms the live chain's tick still fetches.
func TestCurrentGenTickFetches(t *testing.T) {
	m := newTestModel(t, "", false, nil) // refreshGen == 0
	updated, cmd := m.Update(tickMsg{gen: 0})
	if !updated.(Model).loading {
		t.Error("the live tick should start a fetch (loading=true)")
	}
	if cmd == nil {
		t.Error("the live tick should return a fetch cmd")
	}
}

// TestManualFetchDoesNotReschedule pins the core fix: a manual-origin
// fetch (startup / `r` / settings) leaves no rescheduled tick, while a
// timer-origin fetch reschedules exactly one. (The returned cmd is the
// reschedule tick on the no-changes path; nil means no new chain.)
func TestManualFetchDoesNotReschedule(t *testing.T) {
	m := newTestModel(t, "", false, nil)

	_, manualCmd := m.Update(fetchMsg{manual: true, at: time.Now()})
	if manualCmd != nil {
		t.Error("a manual fetch must not reschedule a tick (cmd should be nil)")
	}

	_, autoCmd := m.Update(fetchMsg{manual: false, at: time.Now()})
	if autoCmd == nil {
		t.Error("a timer-origin fetch should reschedule exactly one tick (cmd non-nil)")
	}
}

// TestSettingsIntervalChangeSupersedesChain pins that changing the
// refresh interval in the settings panel bumps the generation (old
// chain dies) and returns a fresh chain — no doubling.
func TestSettingsIntervalChangeSupersedesChain(t *testing.T) {
	m := newTestModel(t, "", false, nil) // interval 60s, refreshGen 0
	// Stage a different interval in the settings panel.
	m.settings = m.settings.Open(30*time.Second, m.compact, m.client.PublicOnly(), m.theme)

	cmd := m.applySettingsAndClose()
	if m.refreshGen != 1 {
		t.Errorf("interval change should bump refreshGen to 1, got %d", m.refreshGen)
	}
	if cmd == nil {
		t.Error("interval change should return a fresh tick chain")
	}
	if m.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", m.interval)
	}

	// A leftover tick from the old (gen-0) chain is now ignored.
	if _, c := m.Update(tickMsg{gen: 0}); c != nil || m.loading {
		t.Error("the superseded gen-0 tick should be dropped after the interval change")
	}
}
