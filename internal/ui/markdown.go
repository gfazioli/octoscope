package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

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
// Falls back to the raw markdown source on any error: glamour
// has been stable in our tests, but on the off chance an issue
// body trips the parser we want to show *something* rather than
// blank out the section.
func renderMarkdown(body string, width int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if width <= 8 {
		// Word-wrap below ~8 cells produces unreadable single-
		// character columns. Skip glamour, return source — at
		// these widths the user has bigger problems anyway.
		return body
	}

	r, err := getMarkdownRenderer(width)
	if err != nil {
		return body
	}
	out, err := r.Render(body)
	if err != nil {
		return body
	}
	// glamour appends a trailing newline + leading blank line
	// for breathing room around the block; trim them so the
	// caller controls the section's vertical spacing.
	return strings.TrimRight(strings.TrimLeft(out, "\n"), "\n")
}

// renderer cache — see renderMarkdown for the rationale. Mutex
// rather than sync.Map because we need the get-or-create
// race-free behaviour and the contention is minimal (one
// renderer per terminal width over the lifetime of a session).
var (
	mdRenderersMu sync.Mutex
	mdRenderers   = map[int]*glamour.TermRenderer{}
)

func getMarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	mdRenderersMu.Lock()
	defer mdRenderersMu.Unlock()
	if r, ok := mdRenderers[width]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	mdRenderers[width] = r
	return r, nil
}
