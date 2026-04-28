package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsField identifies one row of the in-app settings panel.
// Order is the visual order, top to bottom.
type settingsField int

const (
	fieldRefresh settingsField = iota
	fieldCompact
	fieldPublicOnly
	settingsFieldCount
)

// settingsAction is what the parent Model should do once the modal
// returns from a key event. The modal itself never persists or
// applies — it just signals intent.
type settingsAction int

const (
	actionNone        settingsAction = iota // stay open, no side effects
	actionSaveAndExit                       // commit values + persist + close
	actionCancel                            // close without changes
)

// SettingsModel holds the live form state of the in-app settings
// panel: which row is focused, what the user has typed into the
// refresh field, the current toggle states, and any validation
// error to show under the form.
//
// All fields stay in this struct (rather than reaching into the
// parent Model) so the panel can be reasoned about in isolation.
// The parent Model snapshots its current values into Open() and
// reads the final values out of the model on actionSaveAndExit.
type SettingsModel struct {
	open  bool
	focus settingsField

	// refreshBuf is the raw text the user is editing in the refresh
	// field. Validated against time.ParseDuration on save; kept as
	// string so partial/invalid edits don't bounce the cursor.
	refreshBuf string
	compact    bool
	publicOnly bool

	// err is shown under the form when the user tries to save with
	// an invalid refresh value. Cleared on every keystroke.
	err string
}

// IsOpen reports whether the panel is currently visible. The root
// Update uses this to route keystrokes (settings absorbs everything
// while open, including digits 1-5 and "q").
func (sm SettingsModel) IsOpen() bool {
	return sm.open
}

// Open populates the form from the current live values and shows it.
// Call this from the root Update when the user presses the open key.
func (sm SettingsModel) Open(refresh time.Duration, compact, publicOnly bool) SettingsModel {
	sm.open = true
	sm.focus = fieldRefresh
	sm.refreshBuf = refresh.String()
	sm.compact = compact
	sm.publicOnly = publicOnly
	sm.err = ""
	return sm
}

// Close hides the panel without changing any of the staged values.
// The parent Model is expected to call this on actionCancel and to
// not read any of the staged values.
func (sm SettingsModel) Close() SettingsModel {
	sm.open = false
	sm.err = ""
	return sm
}

// Refresh returns the parsed duration from the current buffer, or an
// error if it doesn't parse. Used by the parent Model on save.
func (sm SettingsModel) Refresh() (time.Duration, error) {
	return time.ParseDuration(strings.TrimSpace(sm.refreshBuf))
}

// Compact returns the staged compact flag. Read-only accessor so the
// parent Model can apply it after a successful save.
func (sm SettingsModel) Compact() bool { return sm.compact }

// PublicOnly returns the staged public-only flag.
func (sm SettingsModel) PublicOnly() bool { return sm.publicOnly }

// Update handles a key event while the panel is open. Returns the
// updated sub-model and the action the parent should take.
//
// The action enum (rather than a tea.Cmd) keeps the side effects —
// persisting to disk, mutating the github client, triggering a
// refetch — in the parent's hands. This sub-model stays pure form.
func (sm SettingsModel) Update(msg tea.Msg) (SettingsModel, settingsAction) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return sm, actionNone
	}

	switch km.String() {
	case "esc":
		return sm, actionCancel
	case "enter":
		// Validate before signalling save. Bad refresh string keeps
		// the panel open with an inline error rather than silently
		// closing.
		if _, err := sm.Refresh(); err != nil {
			sm.err = fmt.Sprintf("invalid refresh: %v", err)
			return sm, actionNone
		}
		sm.err = ""
		return sm, actionSaveAndExit
	case "up", "k":
		if sm.focus > 0 {
			sm.focus--
		}
		return sm, actionNone
	case "down", "j", "tab":
		if sm.focus < settingsFieldCount-1 {
			sm.focus++
		}
		return sm, actionNone
	case "shift+tab":
		if sm.focus > 0 {
			sm.focus--
		}
		return sm, actionNone
	case " ":
		// Space toggles boolean fields. On the refresh field it
		// would just be appended like any other char; we reserve
		// it for toggles to match every TUI form ever.
		switch sm.focus {
		case fieldCompact:
			sm.compact = !sm.compact
		case fieldPublicOnly:
			sm.publicOnly = !sm.publicOnly
		case fieldRefresh:
			sm.refreshBuf += " "
			sm.err = ""
		}
		return sm, actionNone
	case "backspace":
		if sm.focus == fieldRefresh && len(sm.refreshBuf) > 0 {
			sm.refreshBuf = sm.refreshBuf[:len(sm.refreshBuf)-1]
			sm.err = ""
		}
		return sm, actionNone
	}

	// Single-rune key on the text field: append. Multi-rune keys
	// ("left", "ctrl+c", etc.) are dropped so they don't pollute
	// the buffer. ctrl+c reaches us only because the parent forwards
	// every key when open — but we don't want a literal "ctrl+c"
	// string in our buffer, so the rune-length guard handles it.
	if sm.focus == fieldRefresh && len(km.Runes) == 1 {
		sm.refreshBuf += string(km.Runes)
		sm.err = ""
	}
	return sm, actionNone
}

// View renders the panel. Called from the root view; the caller is
// responsible for sizing — we draw at the natural width and let
// lipgloss handle vertical/horizontal centering at the parent level.
//
// The styling deliberately echoes the rest of the app: muted body,
// accent-pink focus border, value-style cyan numbers in the toggles.
func (sm SettingsModel) View(width int) string {
	title := boldStyle.Foreground(colAccent).Render("Settings")
	hint := mutedStyle.Render("↑↓ move · space toggle · enter save · esc cancel")

	// Pad all labels to the length of the longest so the value
	// column lands at a single x-coordinate. Looks much tidier than
	// staggered values across rows of varying label width.
	const labelWidth = 18 // long enough for "Public-only mode "
	rows := []string{
		renderSettingsRow("Refresh interval", labelWidth,
			settingTextValue(sm.refreshBuf, sm.focus == fieldRefresh),
			sm.focus == fieldRefresh,
			"how often to re-fetch · go duration: 30s, 1m, 5m, 1h"),
		renderSettingsRow("Compact layout", labelWidth,
			settingBoolValue(sm.compact),
			sm.focus == fieldCompact,
			"smaller cards + abbreviated labels in the Overview tab"),
		renderSettingsRow("Public-only mode", labelWidth,
			settingBoolValue(sm.publicOnly),
			sm.focus == fieldPublicOnly,
			"hide private repos / PRs / issues from the lists"),
	}

	body := strings.Join(rows, "\n\n")

	parts := []string{title, "", body}
	if sm.err != "" {
		parts = append(parts, "", lipgloss.NewStyle().
			Foreground(colError).Render(sm.err))
	}
	parts = append(parts, "", hint)

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Render(strings.Join(parts, "\n"))

	// Centre horizontally inside the available width. We deliberately
	// don't centre vertically — the panel stays anchored a couple of
	// lines below the tab bar so the eye finds it predictably.
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, panel)
}

// renderSettingsRow draws one form row: bold label, the value, and
// a muted help line below. Focused row gets an accent caret to the
// left of the label so the eye lands on it immediately.
//
// labelW pads the label to a fixed width so values across rows align
// in a single vertical column even when label lengths differ.
func renderSettingsRow(label string, labelW int, value string, focused bool, help string) string {
	caret := "  "
	padded := padTo(label, labelW)
	labelStyled := mutedStyle.Render(padded)
	if focused {
		caret = lipgloss.NewStyle().Foreground(colAccent).Render("▸ ")
		// Default terminal foreground (no override) for the focused
		// label, just bolded — keeps it bright against muted siblings
		// without picking a colour the theme might want to override.
		labelStyled = boldStyle.Render(padded)
	}
	line := caret + labelStyled + "   " + value
	helpLine := lipgloss.NewStyle().
		Foreground(colMuted).
		Faint(true).
		Render("    " + help)
	return line + "\n" + helpLine
}

// padTo right-pads s with spaces until it reaches width w. Width is
// counted in runes, which is fine for our ASCII labels — see padRight
// in repos.go for the cell-width-aware variant the table headers use.
func padTo(s string, w int) string {
	if n := len(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// settingTextValue renders the editable text field; when focused, a
// block cursor is appended so the user sees where the next keystroke
// will land.
func settingTextValue(buf string, focused bool) string {
	v := buf
	if focused {
		v += boldStyle.Foreground(colAccent).Render("█")
	}
	return valueStyle.Render(v)
}

// settingBoolValue renders a bool as [ON]/[OFF] in the value style so
// it's instantly distinguishable from the text-input field above.
func settingBoolValue(v bool) string {
	if v {
		return valueStyle.Render("[ON]")
	}
	return mutedStyle.Render("[OFF]")
}
