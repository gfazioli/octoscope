package ui

import "testing"

func TestPackLines(t *testing.T) {
	cases := []struct {
		name  string
		items []string
		sep   string
		width int
		want  string
	}{
		{
			name:  "empty input returns empty",
			items: nil,
			sep:   " · ",
			width: 40,
			want:  "",
		},
		{
			name:  "single item fits",
			items: []string{"abc"},
			sep:   " · ",
			width: 40,
			want:  "abc",
		},
		{
			name:  "all items fit on one line",
			items: []string{"aa", "bb", "cc"},
			sep:   " · ",
			width: 40,
			want:  "aa · bb · cc",
		},
		{
			name:  "wraps when next item would overflow",
			items: []string{"aaa", "bbb", "ccc"},
			sep:   " · ",
			width: 10,
			want:  "aaa · bbb\nccc",
		},
		{
			name:  "zero width falls back to single line",
			items: []string{"a", "b"},
			sep:   " · ",
			width: 0,
			want:  "a · b",
		},
		{
			name:  "item longer than width still placed alone",
			items: []string{"aaaaaaaaaa", "bb"},
			sep:   " · ",
			width: 4,
			want:  "aaaaaaaaaa\nbb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := packLines(tc.items, tc.sep, tc.width)
			if got != tc.want {
				t.Errorf("packLines(%v, %q, %d)\n  got:  %q\n  want: %q",
					tc.items, tc.sep, tc.width, got, tc.want)
			}
		})
	}
}
