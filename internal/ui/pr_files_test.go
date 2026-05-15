package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestTruncatePathLeft covers the left-trim helper used by
// fileRow. Two contracts to defend:
//   - the returned string fits within `w` terminal cells
//   - no rune is ever split (no UTF-8 invalid byte sequences in
//     the output)
//
// We strip ANSI before measuring because callers paint the
// active row in bold accent, but truncatePathLeft itself works
// on the raw string.
func TestTruncatePathLeft(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		width     int
		wantWidth int    // max expected display width of the result
		wantFinal string // last few chars we expect preserved (filename)
	}{
		{
			name:      "fits without trim",
			in:        "a/b/c.go",
			width:     20,
			wantWidth: 8,
			wantFinal: "c.go",
		},
		{
			name:      "trim leaves the filename intact",
			in:        "internal/very/long/path/to/file.go",
			width:     15,
			wantWidth: 15,
			wantFinal: "file.go",
		},
		{
			name:      "trim on a path with multi-byte runes does not split a rune",
			in:        "intérnal/ÜtilitÿDir/файл.go",
			width:     14,
			wantWidth: 14,
			wantFinal: "файл.go",
		},
		{
			name:      "width 1 collapses to ellipsis",
			in:        "anything",
			width:     1,
			wantWidth: 1,
			wantFinal: "…",
		},
		{
			name:      "width 0 collapses to ellipsis",
			in:        "anything",
			width:     0,
			wantWidth: 1,
			wantFinal: "…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncatePathLeft(tt.in, tt.width)
			if w := cellWidth(got); w > tt.wantWidth {
				t.Errorf("cellWidth(%q) = %d, want <= %d", got, w, tt.wantWidth)
			}
			if !strings.HasSuffix(got, tt.wantFinal) {
				t.Errorf("result %q does not end with %q", got, tt.wantFinal)
			}
			// UTF-8 validity: round-tripping via ansi.Strip + []byte
			// should produce a valid utf-8 string (no half runes).
			plain := ansi.Strip(got)
			for _, r := range plain {
				if r == '�' {
					t.Errorf("output %q contains replacement rune (truncation split a UTF-8 sequence)", got)
				}
			}
		})
	}
}

// TestFileStatusGlyph covers the small mapping table that turns
// GitHub's REST status enum into a one-rune visual indicator.
// Status values come from
// https://docs.github.com/en/rest/pulls/pulls#list-pull-requests-files
func TestFileStatusGlyph(t *testing.T) {
	cases := []struct {
		status string
		want   string // ansi-stripped expected glyph
	}{
		{"added", "A"},
		{"removed", "D"},
		{"renamed", "R"},
		{"copied", "C"},
		{"modified", "M"},
		{"changed", "M"},
		{"unchanged", "·"},
		{"", "·"},
		{"something-new-from-github", "·"},
	}
	for _, c := range cases {
		got := ansi.Strip(fileStatusGlyph(c.status))
		if got != c.want {
			t.Errorf("fileStatusGlyph(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestRenderDiff covers the three branches of the diff renderer:
//   - Truncated patches return the explicit banner
//   - Empty patches fall through to a status-specific message
//   - Normal patches are passed to chroma highlight, which we
//     don't assert on character-by-character (the formatter
//     output is large) but we do assert that the returned string
//     contains the original patch's recognisable tokens
func TestRenderDiff(t *testing.T) {
	t.Run("truncated banner mentions github", func(t *testing.T) {
		f := github.FileChange{Path: "huge.go", Truncated: true}
		got := ansi.Strip(renderDiff(f))
		if !strings.Contains(got, "github") {
			t.Errorf("truncated banner = %q, expected to mention github", got)
		}
	})

	t.Run("empty patch on renamed", func(t *testing.T) {
		f := github.FileChange{Path: "moved.go", Status: "renamed"}
		got := ansi.Strip(renderDiff(f))
		if !strings.Contains(strings.ToLower(got), "rename") {
			t.Errorf("renamed empty-patch message = %q", got)
		}
	})

	t.Run("empty patch on removed", func(t *testing.T) {
		f := github.FileChange{Path: "old.go", Status: "removed"}
		got := ansi.Strip(renderDiff(f))
		if !strings.Contains(strings.ToLower(got), "removed") {
			t.Errorf("removed empty-patch message = %q", got)
		}
	})

	t.Run("empty patch on binary / unknown falls back", func(t *testing.T) {
		f := github.FileChange{Path: "logo.png", Status: "modified"}
		got := ansi.Strip(renderDiff(f))
		if !strings.Contains(strings.ToLower(got), "binary") {
			t.Errorf("binary empty-patch message = %q", got)
		}
	})

	t.Run("normal patch survives chroma round trip", func(t *testing.T) {
		patch := "@@ -1,2 +1,2 @@\n-old\n+new"
		f := github.FileChange{Path: "a.go", Status: "modified", Patch: patch}
		got := ansi.Strip(renderDiff(f))
		// chroma may reflow whitespace but the hunk header and
		// the +/- markers should still be in the output.
		if !strings.Contains(got, "@@") {
			t.Errorf("rendered diff missing hunk header: %q", got)
		}
		if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
			t.Errorf("rendered diff missing diff lines: %q", got)
		}
	})
}

// TestKeyHints exercises the key-hint helper used across every
// footer / title-bar. The exact ANSI codes vary by theme, so we
// only assert on plain-text shape and the canonical separator.
func TestKeyHints(t *testing.T) {
	plain := func(s string) string { return ansi.Strip(s) }

	t.Run("zero pairs returns empty", func(t *testing.T) {
		if keyHints() != "" {
			t.Errorf("keyHints() with no args should be empty")
		}
	})

	t.Run("single pair joins key + label", func(t *testing.T) {
		got := plain(keyHints("q", "quit"))
		if got != "q quit" {
			t.Errorf("got %q, want %q", got, "q quit")
		}
	})

	t.Run("multiple pairs are joined by the canonical separator", func(t *testing.T) {
		got := plain(keyHints("r", "refresh", "q", "quit"))
		want := "r refresh" + keyHintsSep + "q quit"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("trailing odd key renders as bare key", func(t *testing.T) {
		got := plain(keyHints("esc", "back", "?"))
		want := "esc back" + keyHintsSep + "?"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty key falls back to plain muted label", func(t *testing.T) {
		got := plain(keyHint("", "loading…"))
		if got != "loading…" {
			t.Errorf("got %q, want %q", got, "loading…")
		}
	})
}
