package ui

import (
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func mkRepo(owner, name string, stars int) github.Repo {
	return github.Repo{
		Name:     name,
		URL:      "https://github.com/" + owner + "/" + name,
		Stars:    stars,
		PushedAt: time.Unix(int64(stars), 0), // deterministic stable secondary sort
	}
}

// TestPartitionByPinned pins the partition contract:
//   - pinned slice ordered by position in the pins list, not by
//     position in the repos input
//   - case-insensitive owner/name match
//   - rest preserves input order
//   - URLs that don't parse into owner/name skip the partition
func TestPartitionByPinned(t *testing.T) {
	repos := []github.Repo{
		mkRepo("alice", "alpha", 1),
		mkRepo("bob", "beta", 2),
		mkRepo("carol", "gamma", 3),
	}
	tests := []struct {
		name       string
		pins       []string
		wantPinned []string // expected names in the pinned slice (in order)
		wantRest   []string
	}{
		{
			name:       "empty pins → everything in rest",
			pins:       nil,
			wantPinned: nil,
			wantRest:   []string{"alpha", "beta", "gamma"},
		},
		{
			name:       "single pin pulled to top",
			pins:       []string{"bob/beta"},
			wantPinned: []string{"beta"},
			wantRest:   []string{"alpha", "gamma"},
		},
		{
			name:       "pinned order matches pins order, not repo order",
			pins:       []string{"carol/gamma", "alice/alpha"},
			wantPinned: []string{"gamma", "alpha"},
			wantRest:   []string{"beta"},
		},
		{
			name:       "case-insensitive match",
			pins:       []string{"ALICE/ALPHA"},
			wantPinned: []string{"alpha"},
			wantRest:   []string{"beta", "gamma"},
		},
		{
			name:       "pin that matches no repo is silently absent",
			pins:       []string{"nobody/nothing", "alice/alpha"},
			wantPinned: []string{"alpha"},
			wantRest:   []string{"beta", "gamma"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinned, rest := partitionByPinned(repos, tt.pins)
			got := names(pinned)
			if !reflect.DeepEqual(got, tt.wantPinned) {
				t.Errorf("pinned = %v, want %v", got, tt.wantPinned)
			}
			got = names(rest)
			if !reflect.DeepEqual(got, tt.wantRest) {
				t.Errorf("rest = %v, want %v", got, tt.wantRest)
			}
		})
	}
}

// TestTogglePinList covers the add / remove / no-op-when-absent
// behaviour the root model relies on.
func TestTogglePinList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		key  string
		add  bool
		want []string
	}{
		{"add to empty", nil, "a/b", true, []string{"a/b"}},
		{"add to non-empty appends", []string{"a/b"}, "c/d", true, []string{"a/b", "c/d"}},
		{"add already-present is no-op", []string{"a/b"}, "A/B", true, []string{"a/b"}},
		{"remove present", []string{"a/b", "c/d"}, "a/b", false, []string{"c/d"}},
		{"remove case-insensitive", []string{"a/b"}, "A/B", false, nil},
		{"remove absent is no-op", []string{"a/b"}, "z/y", false, []string{"a/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := togglePinList(tt.in, tt.key, tt.add)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestPinnedEqual covers the equality helper used to decide
// whether a pin-toggle actually changed state.
func TestPinnedEqual(t *testing.T) {
	if !pinnedEqual(nil, nil) {
		t.Errorf("nil/nil should be equal")
	}
	if !pinnedEqual([]string{"a/b"}, []string{"A/B"}) {
		t.Errorf("case-insensitive equal failed")
	}
	if pinnedEqual([]string{"a/b"}, []string{"a/c"}) {
		t.Errorf("different entries reported equal")
	}
	if pinnedEqual([]string{"a/b", "c/d"}, []string{"c/d", "a/b"}) {
		t.Errorf("order mismatch reported equal")
	}
}

// TestCiSortRank ensures failures sort before pending, before
// success, before "no rollup". The Repos tab relies on this
// monotonic order for the "failures first" sort cycle entry.
func TestCiSortRank(t *testing.T) {
	cases := []struct {
		state string
		want  int
	}{
		{"FAILURE", 0},
		{"ERROR", 0},
		{"PENDING", 1},
		{"EXPECTED", 1},
		{"SUCCESS", 2},
		{"", 3},
		{"SOMETHING_NEW_FROM_GITHUB", 3},
	}
	for _, c := range cases {
		if got := ciSortRank(c.state); got != c.want {
			t.Errorf("ciSortRank(%q) = %d, want %d", c.state, got, c.want)
		}
	}
}

// TestCiDot smoke-tests the glyph mapping: every known state
// produces a 1-cell glyph after ANSI strip, and unknown states
// fall back to a placeholder rather than panicking.
func TestCiDot(t *testing.T) {
	for _, state := range []string{"SUCCESS", "FAILURE", "ERROR", "PENDING", "EXPECTED", "", "FUTURE_ENUM"} {
		got := ansi.Strip(ciDot(state))
		// Exactly one rune-wide glyph for every state, including
		// the unknown-state fallback. The column width budget in
		// the Repos table assumes 1 cell here — any deviation
		// would shift every right-side column by N cells.
		if len([]rune(got)) != 1 {
			t.Errorf("ciDot(%q) = %q (%d runes), want exactly 1 rune", state, got, len([]rune(got)))
		}
	}
}

func names(repos []github.Repo) []string {
	if len(repos) == 0 {
		return nil
	}
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Name
	}
	return out
}
