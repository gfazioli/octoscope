package ui

import "github.com/charmbracelet/x/ansi"

// hyperlink wraps label in an OSC 8 terminal hyperlink pointing at url.
// Terminals that support OSC 8 render label as a clickable link;
// terminals that don't ignore the escapes and print label verbatim, so
// the visible text — and the ansi.Strip output — is always just label, a
// safe fallback. An empty url returns label unwrapped. Width math is
// unaffected: lipgloss.Width / ansi.Strip both ignore OSC 8.
//
// NOTE: only use this on TRUSTED urls (hardcoded consts). A
// GitHub-sourced URL must pass github.Sanitize first — an unsanitised
// URI inside an OSC 8 escape is a terminal-injection vector. That's why
// repo / PR / issue URLs are deliberately NOT wrapped here (yet).
func hyperlink(url, label string) string {
	if url == "" {
		return label
	}
	return ansi.SetHyperlink(url) + label + ansi.ResetHyperlink()
}
