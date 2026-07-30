package ui

import (
	"strings"
	"testing"
)

// TestPrintThemeList pins #63: the colour path lists every built-in
// palette with a swatch, and the NO_COLOR path degrades to names only
// (a colour preview with NO_COLOR set is meaningless).
func TestPrintThemeList(t *testing.T) {
	_ = applyTheme("octoscope", "")

	t.Run("with colour: every name plus swatches", func(t *testing.T) {
		var b strings.Builder
		PrintThemeList(&b, false)
		out := b.String()
		for _, name := range themeOrder {
			if !strings.Contains(out, name) {
				t.Errorf("theme %q missing from the list:\n%s", name, out)
			}
		}
		if !strings.Contains(out, "██") {
			t.Errorf("colour list should render swatch blocks:\n%s", out)
		}
	})

	t.Run("NO_COLOR: names only, no swatches", func(t *testing.T) {
		var b strings.Builder
		PrintThemeList(&b, true)
		out := b.String()
		for _, name := range themeOrder {
			if !strings.Contains(out, name) {
				t.Errorf("theme %q missing from the names-only list:\n%s", name, out)
			}
		}
		if strings.Contains(out, "██") {
			t.Errorf("NO_COLOR list must not render colour swatches:\n%s", out)
		}
	})
}
