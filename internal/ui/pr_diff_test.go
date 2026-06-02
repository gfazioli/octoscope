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

	// Assert by comparing against the EXPECTED renderer per theme rather
	// than sniffing a specific chroma colour index (brittle across chroma
	// / palette updates) — under a monochromatic theme renderDiff must
	// take the mono path, under a chromatic theme the chroma path.
	t.Run("monochrome uses the mono path", func(t *testing.T) {
		_ = applyTheme("monochrome", "")
		if got, want := renderDiff(file), renderDiffMono(patch); got != want {
			t.Errorf("monochrome: renderDiff should equal renderDiffMono\n got=%q\nwant=%q", got, want)
		}
		// And the text survives the styling.
		stripped := ansi.Strip(renderDiff(file))
		for _, w := range []string{"new line", "old line", "context"} {
			if !strings.Contains(stripped, w) {
				t.Errorf("mono diff dropped %q:\n%s", w, stripped)
			}
		}
	})

	t.Run("content line starting with +++ is an addition, not a header", func(t *testing.T) {
		_ = applyTheme("monochrome", "")
		// "++count;" added -> patch line "+++count;": must be okStyle
		// (addition), not mutedStyle (file header).
		mono := renderDiffMono("@@ -0,0 +1 @@\n+++count;\n")
		if !strings.Contains(mono, okStyle.Render("+++count;")) {
			t.Errorf("a content line '+++count;' should render as an addition (okStyle), not a header:\n%q", mono)
		}
	})

	t.Run("chromatic uses the chroma path", func(t *testing.T) {
		_ = applyTheme("octoscope", "")
		chroma, err := highlightDiff(patch)
		if err != nil {
			t.Fatalf("highlightDiff: %v", err)
		}
		if got := renderDiff(file); got != chroma {
			t.Errorf("octoscope: renderDiff should equal the chroma output\n got=%q\nwant=%q", got, chroma)
		}
		if renderDiff(file) == renderDiffMono(patch) {
			t.Error("octoscope: renderDiff must NOT take the mono path")
		}
	})
}
