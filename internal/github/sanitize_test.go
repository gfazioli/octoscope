package github

import (
	"testing"
)

// TestSanitize pins every behavior described in the Sanitize docstring.
// The function is the sole terminal-injection boundary for all GitHub-sourced
// strings, so regressions here are security-relevant.
func TestSanitize(t *testing.T) {
	const esc = "\x1b"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ascii unchanged", "hello world", "hello world"},
		{"newline preserved", "a\nb", "a\nb"},
		{"tab preserved", "a\tb", "a\tb"},
		{"carriage return dropped", "a\rb", "ab"},
		{"null byte dropped", "a\x00b", "ab"},
		{"bell dropped", "a\x07b", "ab"},
		{"DEL dropped", "a\x7fb", "ab"},
		{"CSI SGR color stripped", esc + "[31mred" + esc + "[0m", "red"},
		{"OSC 8 hyperlink stripped", esc + "]8;;https://x" + esc + "\\" + "label" + esc + "]8;;" + esc + "\\", "label"},
		{"OSC 52 clipboard stripped", esc + "]52;c;QUjD" + "\x07" + "tail", "tail"},
		{"utf8 accents preserved", "café résumé", "café résumé"},
		{"utf8 cjk preserved", "日本語", "日本語"},
		{"utf8 emoji preserved", "ship 🚀 it", "ship 🚀 it"},
		{"mixed ansi + C0 + tab", esc + "[1m" + "a\x00b" + esc + "[0m" + "\tc", "ab\tc"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize(c.in)
			if got != c.want {
				t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeIdempotent verifies that calling Sanitize twice yields the same
// result as calling it once. This holds because the byte-level filter already
// removes every byte that ansi.Strip could also remove on a second pass.
func TestSanitizeIdempotent(t *testing.T) {
	const esc = "\x1b"

	inputs := []string{
		"",
		"plain text no control chars",
		esc + "[31mcolor" + esc + "[0m",
		"a\x00b\x1bc",
		"café 🚀 日本語",
	}

	for _, in := range inputs {
		once := Sanitize(in)
		twice := Sanitize(once)
		if once != twice {
			t.Errorf("Sanitize not idempotent for %q: first=%q second=%q", in, once, twice)
		}
	}
}

// TestSanitizeC1ControlUTF8 characterizes the behavior of Sanitize when given
// a string containing U+009B (the 8-bit CSI introducer), which encodes in
// UTF-8 as the two bytes 0xC2 0x9B.
//
// The docstring claims: "C1 controls inside multi-byte UTF-8 sequences are
// already covered by the ansi pass." This test pins what the code actually
// does today, so that any future change in behavior is caught as a regression.
//
// Observed result: U+009B survives sanitization. ansi.Strip does not remove
// it (it treats 0xC2 0x9B as an ordinary two-byte UTF-8 sequence, not a
// CSI introducer), and the byte-level filter passes both bytes because they
// are >= 0x80. The docstring claim is therefore INACCURATE for this code
// path: a UTF-8-encoded C1 control is NOT stripped by the current
// implementation. This is a characterization finding only — sanitize.go is
// not modified here; the decision whether to fix the implementation is left
// to the maintainer.
func TestSanitizeC1ControlUTF8(t *testing.T) {
	// U+009B (8-bit CSI introducer) encoded as UTF-8: 0xC2 0x9B
	in := "ab"

	got := Sanitize(in)

	// Log the observed output so the result is always visible in verbose mode.
	t.Logf("Sanitize(%q) = %q", in, got)

	// Characterization: U+009B survives (observed behavior, not the
	// docstring's claim). Pin it so a future change is detected.
	want := "ab"
	if got != want {
		t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
	}

	// Safety check: ensure no raw C0 control byte (< 0x20) or DEL (0x7F)
	// slipped through, which would indicate a genuine security issue rather
	// than a characterization variance.
	for i := 0; i < len(got); i++ {
		b := got[i]
		if (b < 0x20 && b != '\n' && b != '\t') || b == 0x7F {
			t.Errorf("Sanitize(%q) left raw control byte 0x%02X at position %d in output %q", in, b, i, got)
		}
	}
}
