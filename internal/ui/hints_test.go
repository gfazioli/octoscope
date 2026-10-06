package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestKeyHintsWithinBreaksBetweenEntries holds keyHintsWithin to its two
// promises: no line wider than the pane while every entry fits on its
// own, and no entry split across lines — every entry appears whole, in
// order. At a wide pane it is keyHints, unchanged.
func TestKeyHintsWithinBreaksBetweenEntries(t *testing.T) {
	_ = applyTheme("octoscope", "")
	pairs := []string{"o", "sponsor", "b", "coffee", "c", "copy"}
	entries := []string{"o sponsor", "b coffee", "c copy"}

	if got, want := ansi.Strip(keyHintsWithin(200, pairs...)), ansi.Strip(keyHints(pairs...)); got != want {
		t.Errorf("at a wide pane keyHintsWithin = %q, want keyHints' %q", got, want)
	}
	for w := 9; w <= 40; w++ {
		lines := strings.Split(ansi.Strip(keyHintsWithin(w, pairs...)), "\n")
		var seen []string
		for _, line := range lines {
			if n := cellWidth(line); n > w {
				t.Errorf("width %d: a line is %d cells: %q", w, n, line)
			}
			seen = append(seen, strings.Split(line, keyHintsSep)...)
		}
		if strings.Join(seen, "|") != strings.Join(entries, "|") {
			t.Errorf("width %d: entries came out as %q, want %q whole and in order", w, seen, entries)
		}
	}
}
