package ui

import "strings"

// keyHint renders one entry of a key-hints line: the key itself in
// accent+bold so the eye locks onto it, and the human label in the
// usual muted footer style. Convention shared with lazygit /
// gh-dash / ranger / k9s: emphasising the key (the actionable
// part) over the description makes long footer strings scannable
// instead of forming a uniform grey block.
//
// Used everywhere a footer or title-bar lists available actions —
// the drill-in titles, the sub-view footers (files / diff), the
// list-tab footers, the action menu, the settings panel hint,
// the global footer.
//
// Special cases handled implicitly:
//   - Empty key (e.g. for "search:" prompts that aren't bound to
//     a single keystroke): caller can pass key=="" and we fall
//     back to plain muted label.
//   - Multi-key combos ("↑↓", "1-5/tab", "pgup/pgdn"): treated as
//     one logical "key" — passed verbatim to the accent style.
func keyHint(key, label string) string {
	key = strings.TrimSpace(key)
	label = strings.TrimSpace(label)
	if key == "" {
		return mutedStyle.Render(label)
	}
	if label == "" {
		return boldStyle.Foreground(colAccent).Render(key)
	}
	return boldStyle.Foreground(colAccent).Render(key) +
		mutedStyle.Render(" "+label)
}

// keyHintsSep is the canonical separator between hint entries.
// Centralised so a single change (e.g. swapping " · " for " | ")
// updates every hint line in the app.
const keyHintsSep = " · "

// keyHints assembles a complete hint line from (key, label) pairs.
// Each pair becomes one keyHint entry; entries are joined by
// keyHintsSep. Pairs are flat: keyHints("esc", "back", "o", "open",
// "r", "refresh") — odd-indexed args are keys, even-indexed are
// labels.
//
// Wraps the common case so call sites read declaratively:
//
//	mutedStyle.Render("  esc back · o open · r refresh")
//
// becomes
//
//	keyHints("esc", "back", "o", "open", "r", "refresh")
//
// Odd-length input (a trailing key without label) is rendered as
// a bare accent key — useful for hints like "?" or a lone
// modifier reminder.
func keyHints(pairs ...string) string {
	if len(pairs) == 0 {
		return ""
	}
	var entries []string
	for i := 0; i < len(pairs); i += 2 {
		key := pairs[i]
		label := ""
		if i+1 < len(pairs) {
			label = pairs[i+1]
		}
		entries = append(entries, keyHint(key, label))
	}
	return strings.Join(entries, mutedStyle.Render(keyHintsSep))
}
