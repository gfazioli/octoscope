package ui

import (
	"testing"
	"time"
)

// TestValidAccentColor pins the accent save-time guard (#61): empty,
// #RGB / #RRGGBB hex, and ANSI-256 indices pass; everything else fails.
func TestValidAccentColor(t *testing.T) {
	valid := []string{"", "   ", "#fff", "#FFF", "#FF0080", "#abcdef", "0", "201", "255"}
	invalid := []string{"reed", "#ff", "#gggggg", "#12345", "256", "-1", "0x1f", "#", "12 34"}

	for _, s := range valid {
		if !validAccentColor(s) {
			t.Errorf("validAccentColor(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if validAccentColor(s) {
			t.Errorf("validAccentColor(%q) = true, want false", s)
		}
	}
}

// TestSettingsAccentAndSponsor exercises the two new panel fields (#61):
// Open seeds them, space toggles the sponsor row, the accent field takes
// typed text, and a valid accent saves.
func TestSettingsAccentAndSponsor(t *testing.T) {
	sm := SettingsModel{}.Open(30*time.Second, false, false, "octoscope", "#123456", true)
	if sm.AccentColor() != "#123456" {
		t.Errorf("seeded AccentColor() = %q, want #123456", sm.AccentColor())
	}
	if !sm.ShowSponsor() {
		t.Error("seeded ShowSponsor() = false, want true")
	}

	// Space on the sponsor row toggles it off.
	sm.focus = fieldShowSponsor
	sm, _ = sm.Update(key(" "))
	if sm.ShowSponsor() {
		t.Error("space on the sponsor row should toggle it off")
	}

	// Retype the accent field.
	sm.focus = fieldAccentColor
	sm.accentBuf = ""
	for _, r := range "201" {
		sm, _ = sm.Update(key(string(r)))
	}
	if sm.AccentColor() != "201" {
		t.Errorf("AccentColor() after typing = %q, want 201", sm.AccentColor())
	}

	// A valid accent saves.
	_, action := sm.Update(key("enter"))
	if action != actionSaveAndExit {
		t.Errorf("valid accent should save, got %v", action)
	}
}

// TestSettingsRejectsInvalidAccent pins that a malformed accent keeps
// the panel open with an inline error instead of persisting garbage.
func TestSettingsRejectsInvalidAccent(t *testing.T) {
	sm := SettingsModel{}.Open(30*time.Second, false, false, "octoscope", "", true)
	sm.focus = fieldAccentColor
	for _, r := range "nope" {
		sm, _ = sm.Update(key(string(r)))
	}

	sm2, action := sm.Update(key("enter"))
	if action != actionNone {
		t.Errorf("invalid accent must not save, got %v", action)
	}
	if sm2.err == "" {
		t.Error("invalid accent should surface an inline error")
	}
}
