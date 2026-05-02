package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the small palette every TUI element draws from. Six slots
// is the minimum to keep the UI legible without a one-off colour for
// every component:
//
//   - Accent  — identity colour (banner, section titles, icons,
//               authenticated dot, focus ring, change-pulse border)
//   - Value   — the prominent numbers that read as "data"
//   - OK      — semantic success (authenticated badge, "ready" PR state)
//   - Warn    — unauthenticated, low rate-limit, stale data
//   - Error   — fetch failed, conflicts, anything alarming
//   - Muted   — labels, borders, helper text, the visual quiet
//
// Themes are immutable maps; switching theme repoints currentTheme
// and rebuilds the lipgloss styles that depend on the colours (see
// rebuildStyles).
type Theme struct {
	Name   string
	Accent lipgloss.Color
	Value  lipgloss.Color
	OK     lipgloss.Color
	Warn   lipgloss.Color
	Error  lipgloss.Color
	Muted  lipgloss.Color
}

// themes holds the built-in palettes. Keep keys lowercase, hyphenated;
// they're what the user types in the config file and on the command line.
var themes = map[string]*Theme{
	"octoscope": {
		Name:   "octoscope",
		Accent: "#E91E63", // magenta-pink — the "o" in octoscope
		Value:  "#00D9FF", // cyan — the number that pops
		OK:     "#2ECC71", // green — authenticated / success
		Warn:   "#F1C40F", // yellow — unauthenticated / stale
		Error:  "#FF5555", // red — fetch failed
		Muted:  "241",     // grey — labels, footers
	},
	"high-contrast": {
		Name:   "high-contrast",
		Accent: "#FFFFFF",
		Value:  "#00FFFF",
		OK:     "#00FF00",
		Warn:   "#FFFF00",
		Error:  "#FF0000",
		Muted:  "250",
	},
	"terminal": {
		// ANSI 8-15 (bright variants) so the dashboard borrows the
		// terminal emulator's own palette — iTerm/Ghostty/Alacritty
		// users see octoscope shift with their theme automatically.
		Name:   "terminal",
		Accent: "13", // bright magenta
		Value:  "14", // bright cyan
		OK:     "10", // bright green
		Warn:   "11", // bright yellow
		Error:  "9",  // bright red
		Muted:  "8",  // bright black (grey)
	},
	"monochrome": {
		// All greys, no chroma. For B&W terminals or zero-distraction
		// dashboards. Hierarchy is preserved purely through value.
		Name:   "monochrome",
		Accent: "252",
		Value:  "255",
		OK:     "250",
		Warn:   "248",
		Error:  "245",
		Muted:  "240",
	},
	"stranger-things": {
		// 80s logo crimson on near-black, with the "Christmas lights"
		// yellow doing duty for both numeric values and the OK
		// semantic — green would dilute the identity. Muted is a
		// dim crimson so borders fade into an "Upside Down" vibe.
		Name:   "stranger-things",
		Accent: "#E50914",
		Value:  "#FFD700",
		OK:     "#FFD700",
		Warn:   "#FFA500",
		Error:  "#D62F1F",
		Muted:  "#5C0404",
	},
	"phosphor": {
		// Classic 80s P1/P31-phosphor CRT — vt100, ADM-3A. Pure
		// monochrome green: errors don't get a red shift because real
		// CRT terminals had nothing else to give. The user reads
		// alarm from context (the "errored" word, the position) the
		// way they did in 1983.
		Name:   "phosphor",
		Accent: "#33FF66",
		Value:  "#7FFF7F",
		OK:     "#33FF66",
		Warn:   "#A8FF8B",
		Error:  "#9FFF9F",
		Muted:  "#006633",
	},
	"amber": {
		// The other half of the 80s CRT story — IBM 5151, WordStar,
		// the amber phosphor that was easier on the eyes during long
		// terminal sessions. Same pure-monochrome philosophy as
		// phosphor: every slot is a shade of amber, errors included.
		Name:   "amber",
		Accent: "#FFB000",
		Value:  "#FFD27F",
		OK:     "#FFB000",
		Warn:   "#FFC844",
		Error:  "#FFD27F",
		Muted:  "#5C3F00",
	},
}

// themeOrder is the cycle order shown in the in-app settings panel
// and printed by --help. Default first, alternates after.
var themeOrder = []string{
	"octoscope",
	"high-contrast",
	"terminal",
	"monochrome",
	"stranger-things",
	"phosphor",
	"amber",
}

// currentTheme is what every style derives from. Mutated by
// applyTheme; never written from outside this package.
var currentTheme *Theme

// ThemeNames returns the list of built-in theme names in display
// order. Used by the settings cycler and the --help message.
func ThemeNames() []string {
	out := make([]string, len(themeOrder))
	copy(out, themeOrder)
	return out
}

// IsValidTheme reports whether name matches a built-in theme. Used by
// the config loader to reject typos at startup rather than silently
// fall back to a default.
func IsValidTheme(name string) bool {
	_, ok := themes[name]
	return ok
}

// applyTheme switches the active theme and rebuilds every dependent
// lipgloss style. accentOverride, when non-empty, replaces the theme's
// Accent slot only — the rest of the palette stays on the named theme.
// Returns an error for unknown names; accent override syntax is not
// validated here (lipgloss accepts hex, ANSI numbers, and named
// colours, all as opaque strings).
func applyTheme(name, accentOverride string) error {
	t, ok := themes[name]
	if !ok {
		return fmt.Errorf("unknown theme %q (valid: %s)", name, strings.Join(themeOrder, ", "))
	}
	currentTheme = t
	colAccent = t.Accent
	colValue = t.Value
	colOK = t.OK
	colWarn = t.Warn
	colError = t.Error
	colMuted = t.Muted
	if accentOverride != "" {
		colAccent = lipgloss.Color(accentOverride)
	}
	rebuildStyles()
	return nil
}

// init applies the default theme so the package-level style vars are
// populated before any view function runs. Callers that want a
// non-default theme call applyTheme later (typically from main.go
// after parsing config + CLI flags); the default is a safety net for
// tests and any code path that imports ui without booting Model.
func init() {
	_ = applyTheme("octoscope", "")
}
