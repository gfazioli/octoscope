package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Action is one entry in an ActionMenu — a label, a discoverable
// shortcut letter, and the tea.Cmd to fire when chosen. The shortcut
// renders highlighted next to the label so the user can see "press
// 'd' next time to skip the menu". Cmd is allowed to be nil for the
// degenerate "no-op" action; in practice every shipping action has a
// concrete command (open browser, copy URL, request detail view).
type Action struct {
	Label    string
	Shortcut rune
	Cmd      tea.Cmd
}

// ActionMenuModel is the modal action picker shown over a list-tab
// row when the user presses Ctrl+Enter (or a row-level shortcut).
// Centered horizontally inside the body's available width, anchored
// below the tab bar like the settings panel — same visual idiom on
// purpose so users recognise "this is a modal, esc closes it".
//
// The menu is stateless beyond cursor position: it doesn't know what
// tab opened it or which item the actions target. Each Action carries
// its own pre-built Cmd, captured at Open() time over the relevant
// row data. That keeps the menu agnostic and makes it reusable for
// any list-tab without dispatch tables.
type ActionMenuModel struct {
	open    bool
	title   string   // e.g. "Actions for octoscope"
	actions []Action // ordered: render + cycle order
	cursor  int      // index into actions; 0..len(actions)-1
}

// IsOpen reports whether the menu is currently visible. Used by the
// root model's Update to route keystrokes to the menu while open
// (the same dispatch idiom as the settings panel).
func (m ActionMenuModel) IsOpen() bool {
	return m.open
}

// Open returns a fresh open menu seeded with the given title and
// actions, cursor on the first action. Callers replace the stored
// model:
//
//	m.actionMenu = m.actionMenu.Open("Actions for "+name, []Action{...})
//
// `actions` order is preserved — callers control rendering order.
func (m ActionMenuModel) Open(title string, actions []Action) ActionMenuModel {
	return ActionMenuModel{
		open:    true,
		title:   title,
		actions: actions,
		cursor:  0,
	}
}

// Close returns a closed menu (zero value). Used internally on esc /
// after firing an action; exposed in case the root needs to dismiss
// the menu in response to an external event (window resize, fetch
// error, etc.) — not used today but cheap to keep.
func (m ActionMenuModel) Close() ActionMenuModel {
	return ActionMenuModel{}
}

// Update handles a single key event while the menu is open. Returns:
//
//   - the updated model (cursor moved, or closed after a confirm)
//   - a tea.Cmd to run when the user picked something (nil otherwise)
//
// Direct-shortcut letters (e.g. 'o', 'd', 'c') match against any
// action's Shortcut field — bypassing the cursor entirely. This is
// the "fast path" pattern from lazygit / gh dash: the menu is the
// discoverable surface, single letters are the muscle memory.
func (m ActionMenuModel) Update(msg tea.KeyMsg) (ActionMenuModel, tea.Cmd) {
	if !m.open {
		return m, nil
	}
	switch msg.String() {
	case "q":
		// Mirror the root-level shortcut: q quits the app at any
		// depth, same as ctrl+c. Without this the menu silently
		// swallowed q and forced the user to esc + q (or ctrl+c)
		// to leave — surprising for a "quit" key.
		return m.Close(), tea.Quit
	case "esc":
		return m.Close(), nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.actions)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.actions) {
			cmd := m.actions[m.cursor].Cmd
			return m.Close(), cmd
		}
	default:
		// Single-rune key press — try to match a shortcut letter on
		// any action. Multi-rune strings ("left", "ctrl+c", etc.) are
		// ignored so they don't accidentally fire something.
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			for _, a := range m.actions {
				if a.Shortcut == r {
					return m.Close(), a.Cmd
				}
			}
		}
	}
	return m, nil
}

// View renders the centered modal box. `width` is the available
// horizontal space (caller passes the tab body's `available`); the
// menu picks a natural width clamped to a sensible range and centres
// itself with PlaceHorizontal — same idiom as the settings panel.
//
// Rendering layout, top to bottom:
//
//	▸ accent title           (sectionTitleStyle)
//	(blank)
//	▸ X  Label  ← cursor row   (active style + accent shortcut)
//	  X  Label                 (muted label, accent shortcut)
//	  …
//	(blank)
//	muted hint                 (↑/↓ select · enter confirm · esc back)
func (m ActionMenuModel) View(width int) string {
	if !m.open {
		return ""
	}

	const (
		minBoxW = 28
		maxBoxW = 50
	)

	// Measure the longest content line so the box hugs its content
	// rather than always rendering at maxBoxW.
	contentW := lipgloss.Width(m.title)
	for _, a := range m.actions {
		// "▸ X  Label" — 2 (cursor) + 1 (shortcut) + 2 (gap) + len(label)
		lineW := 5 + lipgloss.Width(a.Label)
		if lineW > contentW {
			contentW = lineW
		}
	}
	boxW := contentW
	if boxW < minBoxW {
		boxW = minBoxW
	}
	if boxW > maxBoxW {
		boxW = maxBoxW
	}
	if width > 0 && boxW > width-4 {
		boxW = width - 4
	}

	var lines []string
	lines = append(lines, sectionTitleStyle.Render(m.title))
	lines = append(lines, "")
	for i, a := range m.actions {
		marker := "  "
		labelStyled := mutedStyle.Render(a.Label)
		if i == m.cursor {
			marker = activeTabStyle.Render("▸ ")
			labelStyled = activeTabStyle.Render(a.Label)
		}
		shortcut := boldStyle.Foreground(colAccent).Render(string(a.Shortcut))
		lines = append(lines, fmt.Sprintf("%s%s  %s", marker, shortcut, labelStyled))
	}
	lines = append(lines, "")
	lines = append(lines, keyHints(
		"↑/↓", "select",
		"enter", "confirm",
		"esc", "back",
	))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	if width <= 0 {
		return panel
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, panel)
}
