package ui

import (
	"testing"
)

// TestSanitizeBody pins the core behaviors of sanitizeBody: ANSI stripping,
// C0 control removal, and C1 UTF-8 byte-pair removal.
func TestSanitizeBody(t *testing.T) {
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
		{"utf8 accents preserved", "caf\u00e9 r\u00e9sum\u00e9", "caf\u00e9 r\u00e9sum\u00e9"},
		{"utf8 cjk preserved", "\u65e5\u672c\u8a9e", "\u65e5\u672c\u8a9e"},
		{"utf8 emoji preserved", "ship \U0001f680 it", "ship \U0001f680 it"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeBody(c.in)
			if got != c.want {
				t.Errorf("sanitizeBody(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeBodyC1Controls verifies that sanitizeBody strips UTF-8-encoded
// C1 controls (U+0080-U+009F). These encode as the two-byte sequences
// 0xC2 0x80 through 0xC2 0x9F and include the 8-bit CSI/OSC/DCS
// introducers. ansi.Strip only handles the 7-bit ESC-prefixed forms; the
// byte-pair case in sanitizeBody covers the rest.
func TestSanitizeBodyC1Controls(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// U+0080-U+009F (UTF-8 0xC2 0x80 through 0xC2 0x9F) are the C1 controls.
		{"C1 CSI introducer U+009B dropped", "a\u009bb", "ab"},
		{"C1 low boundary U+0080 dropped", "a\u0080b", "ab"},
		{"C1 high boundary U+009F dropped", "a\u009fb", "ab"},
		// The introducer is removed, so an 8-bit SGR degrades to inert text.
		{"8-bit CSI sequence neutered to literal text", "x\u009b31my", "x31my"},
		// U+00A0 (just past C1) is a normal printable rune -- must survive.
		{"U+00A0 (just past C1) preserved", "a\u00a0b", "a\u00a0b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeBody(c.in)
			if got != c.want {
				t.Errorf("sanitizeBody(%q) = %q, want %q", c.in, got, c.want)
			}
			for i := 0; i < len(got); i++ {
				if b := got[i]; (b < 0x20 && b != '\n' && b != '\t') || b == 0x7F {
					t.Errorf("sanitizeBody(%q) left raw control byte 0x%02X", c.in, b)
				}
			}
		})
	}
}
