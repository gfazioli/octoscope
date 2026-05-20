package github

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize strips ANSI escape sequences and other terminal
// control characters from a GitHub-sourced string before it
// reaches the rendering layer. Applied at the boundary
// (inside extractPRDetail / extractIssueDetail /
// extractRepoDetail / extractStats) so by the time UI code
// touches a Stats / *Detail field the string is already safe to
// paint into the terminal.
//
// PR / issue titles, commit messages, label names, repo
// descriptions, login strings and any other user-controlled
// field can carry ANSI escape sequences that would otherwise
// move the cursor, set the OSC clipboard, or otherwise hijack
// the terminal once the TUI paints them. We can't trust GitHub
// to scrub these for us — the API returns whatever the user
// typed.
//
// Two-stage strip:
//  1. ansi.Strip removes well-formed CSI / OSC / SGR
//     sequences (covers the common attack shapes).
//  2. The byte-level filter removes any remaining C0 control
//     characters (0x00–0x1F) plus DEL (0x7F), keeping only
//     newline and tab. C1 controls inside multi-byte UTF-8
//     sequences are already covered by the ansi pass.
//
// Idempotent — sanitising an already-sanitised string returns
// the same string. Safe to call defensively at the render
// layer as well as the boundary; the extra cost is one regex
// pass and one O(n) byte scan over a typically short field.
//
// The internal/ui package has a sanitizeBody helper with the
// same logic; both exist by design (defense in depth across
// the package boundary). Keeping the implementation duplicated
// rather than shared in a third package avoids a circular
// import path between internal/github and internal/ui.
func Sanitize(s string) string {
	if s == "" {
		return ""
	}
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c < 0x20 || c == 0x7F:
			// drop
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
