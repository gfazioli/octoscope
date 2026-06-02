package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func longPatch(n int) string {
	var b strings.Builder
	b.WriteString("@@ -1,1 +1," + fmt.Sprint(n) + " @@\n")
	for i := 0; i < n-1; i++ {
		fmt.Fprintf(&b, "+added line %d\n", i)
	}
	b.WriteString("+LASTLINEMARKER")
	return b.String()
}

// TestPRDiffViewportHeightFits pins that the diff viewer never overflows
// the height it's given AND that the last patch line is reachable at the
// bottom — i.e. the height handoff (parent reserves title+blank, diff
// reserves its own counts+hints) is balanced, not double-counted.
func TestPRDiffViewportHeightFits(t *testing.T) {
	_ = applyTheme("octoscope", "")
	file := github.FileChange{Path: "main.go", Additions: 199, Deletions: 0, Status: "modified", Patch: longPatch(200)}

	for _, h := range []int{8, 16, 24, 50} {
		dm := PRDiffModel{}.Open(file, "o", "r", 1, 80, h)
		out := dm.View(80, h)
		if got := lipgloss.Height(out); got > h {
			t.Errorf("h=%d: View rendered %d lines, overflows budget", h, got)
		}
		// Scroll to the bottom and confirm the final line is on screen.
		dm = dm.syncViewport(80, h)
		dm.viewport.GotoBottom()
		if !strings.Contains(ansi.Strip(dm.viewport.View()), "LASTLINEMARKER") {
			t.Errorf("h=%d: last patch line not reachable at bottom of viewport", h)
		}
	}
}

// TestRenderDiffMonochromatic pins the theme-fidelity fix: under a
// monochromatic theme the diff body must NOT carry chroma/monokai colour
// (the add/delete lines route through the theme's own okStyle/errorStyle
// instead); under a chromatic theme chroma is used.
func TestRenderDiffMonochromatic(t *testing.T) {
	t.Cleanup(func() { _ = applyTheme("octoscope", "") })

	patch := "@@ -1,2 +1,2 @@\n-old line\n+new line\n context\n"
	file := github.FileChange{Path: "x.go", Patch: patch}

	// monokai's signature add-green is ANSI 256 index 148 ("38;5;148").
	const monokaiGreen = "38;5;148"

	t.Run("monochrome: no chroma leak", func(t *testing.T) {
		_ = applyTheme("monochrome", "")
		out := renderDiff(file)
		if strings.Contains(out, monokaiGreen) {
			t.Errorf("monochrome diff leaked monokai colour (%s):\n%q", monokaiGreen, out)
		}
		// The +/- lines still carry SOME styling (the theme's slots),
		// and the text survives.
		stripped := ansi.Strip(out)
		for _, want := range []string{"new line", "old line", "context"} {
			if !strings.Contains(stripped, want) {
				t.Errorf("mono diff dropped %q:\n%s", want, stripped)
			}
		}
	})

	t.Run("octoscope: chroma used", func(t *testing.T) {
		_ = applyTheme("octoscope", "")
		out := renderDiff(file)
		if !strings.Contains(out, monokaiGreen) {
			t.Errorf("chromatic theme should use chroma monokai (expected %s):\n%q", monokaiGreen, out)
		}
	})
}
