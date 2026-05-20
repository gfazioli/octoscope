package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderDetailDescription is the convenience helper used by the
// PR / Issue drill-in views to render the `body` field as styled
// markdown. Passes the full available width through to
// renderMarkdown — glamour's "dark" style already supplies its
// own internal padding around the block, so no extra column
// budget is reserved here.
//
// No artificial cap on lines: the surrounding viewport already
// scrolls, so a long description just lives further down the
// body and the user pages through it. Capping inside the section
// was a v0.10.0 belt-and-braces that turned out to be redundant.
//
// Lives in markdown.go (rather than per-detail file) because PR
// detail and Issue detail share the exact same rendering rule —
// a `pr*` name on the call site read as PR-specific even though
// the function was width-agnostic.
func renderDetailDescription(body string, width int) string {
	return renderMarkdown(body, width)
}

// sanitizeBody strips ANSI escape sequences and other terminal
// control characters from a string before we render it into the
// TUI. PR / issue / comment bodies come from GitHub and are
// therefore untrusted: a malicious commenter could embed an
// escape sequence that moves the cursor, sets the OSC clipboard,
// or otherwise hijacks the terminal once we paint it.
//
// Two-stage strip:
//  1. ansi.Strip removes well-formed CSI / OSC / SGR sequences.
//  2. The byte-level filter removes any remaining C0 control
//     characters (0x00–0x1F) except tab and newline, plus DEL
//     (0x7F). C1 controls are already covered by the ansi pass.
//
// Result is safe to feed to glamour or to lipgloss directly.
// Idempotent — sanitizing an already-sanitized string returns
// the same string.
func sanitizeBody(s string) string {
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

// renderMarkdown converts a CommonMark string into a styled
// terminal block — headings, bold, italic, code blocks, lists,
// links — using glamour's "dark" preset.
//
// Used by the drill-in detail views (PR / Issue) to render the
// `body` field instead of dumping it as raw text. Without this,
// `## Heading`, `**bold**`, fenced code blocks all show as
// literal markup, which looks half-broken in a TUI.
//
// The renderer is cached per word-wrap width via a small map
// behind a mutex: rebuilding is cheap (~1ms) but the call
// frequency on a long detail view (every paint of the Open PR/Issue
// drill-in) makes the cache pay for itself. Width-keyed because
// the WordWrap is set at construction time — if a different
// width comes in we lazily build a new renderer.
//
// Body is sanitised (ANSI escapes + control chars stripped)
// before being handed to glamour, since GitHub-sourced text is
// untrusted. See sanitizeBody for the policy.
//
// Falls back to the sanitised source wrapped to `width` on any
// error: glamour has been stable in our tests, but on the off
// chance an issue body trips the parser we want to show
// *something* rather than blank out the section. The wrap on
// fallback prevents long lines from breaking the surrounding
// detail layout.
func renderMarkdown(body string, width int) string {
	body = sanitizeBody(strings.TrimSpace(body))
	if body == "" {
		return ""
	}

	fallback := func() string {
		// Use lipgloss to wrap so cell-width math handles wide
		// runes (CJK, emoji) correctly. Width<=0 (unknown)
		// passes through untouched.
		if width <= 0 {
			return body
		}
		return lipgloss.NewStyle().Width(width).Render(body)
	}

	if width <= 8 {
		// Word-wrap below ~8 cells produces unreadable single-
		// character columns. Skip glamour, return wrapped
		// source — at these widths the user has bigger problems
		// anyway.
		return fallback()
	}

	r, err := getMarkdownRenderer(width)
	if err != nil {
		return fallback()
	}
	out, err := r.Render(body)
	if err != nil {
		return fallback()
	}
	// glamour appends a trailing newline + leading blank line
	// for breathing room around the block; trim them so the
	// caller controls the section's vertical spacing.
	return strings.TrimRight(strings.TrimLeft(out, "\n"), "\n")
}

// Renderer cache — single-entry "most-recently-used" rather than
// an unbounded width→renderer map. The realistic access pattern
// is "render at the current terminal width, repeatedly", with
// occasional jumps when the user resizes; caching just the last
// width gives ~100% hit rate during steady-state painting and
// bounds the cost of a session full of resizes to one renderer
// build per resize. An LRU map sized 1 is overkill — the same
// effect is two scalars guarded by a mutex.
//
// Mutex rather than sync.Map because we need get-or-create
// race-free behaviour, and contention is minimal (renderers are
// only built when width changes).
var (
	mdRendererMu    sync.Mutex
	mdRendererWidth int
	mdRenderer      *glamour.TermRenderer
)

func getMarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()
	if mdRenderer != nil && mdRendererWidth == width {
		return mdRenderer, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	mdRenderer = r
	mdRendererWidth = width
	return r, nil
}
