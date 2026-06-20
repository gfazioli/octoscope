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

// TestSanitizeC1ControlUTF8 verifies that Sanitize strips UTF-8-encoded C1
// controls (U+0080–U+009F). These encode as the two-byte sequences 0xC2 0x80
// through 0xC2 0x9F and include the 8-bit CSI/OSC/DCS introducers.
// ansi.Strip only handles the 7-bit ESC-prefixed forms; the byte-pair case
// in Sanitize covers the rest.
func TestSanitizeC1ControlUTF8(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// U+0080–U+009F (UTF-8 0xC2 0x80–0xC2 0x9F) are the C1 controls.
		{"C1 CSI introducer U+009B dropped", "a\u009bb", "ab"},
		{"C1 low boundary U+0080 dropped", "a\u0080b", "ab"},
		{"C1 high boundary U+009F dropped", "a\u009fb", "ab"},
		// The introducer is removed, so an 8-bit SGR degrades to inert text.
		{"8-bit CSI sequence neutered to literal text", "x\u009b31my", "x31my"},
		// U+00A0 (just past C1) is a normal printable rune — must survive.
		{"U+00A0 (just past C1) preserved", "a\u00a0b", "a\u00a0b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize(c.in)
			if got != c.want {
				t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
			for i := 0; i < len(got); i++ {
				if b := got[i]; (b < 0x20 && b != '\n' && b != '\t') || b == 0x7F {
					t.Errorf("Sanitize(%q) left raw control byte 0x%02X", c.in, b)
				}
			}
		})
	}
}
