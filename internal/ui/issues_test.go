package ui

import (
	"reflect"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// mkIssue builds an Issue with a deterministic UpdatedAt derived from
// the number so the "updated" sort is stable in tests.
func mkIssue(repo string, number int) github.Issue {
	return github.Issue{
		Number:    number,
		Title:     repo + " issue",
		URL:       "https://github.com/" + repo + "/issues/" + itoa(number),
		Repo:      repo,
		UpdatedAt: time.Unix(int64(number), 0),
	}
}

func itoa(n int) string {
	// tiny local helper so the test file doesn't need strconv just for
	// building fixture URLs.
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestIssueID pins the "owner/name#N" identity construction.
func TestIssueID(t *testing.T) {
	got := issueID(mkIssue("alice/alpha", 42))
	if got != "alice/alpha#42" {
		t.Errorf("issueID = %q, want %q", got, "alice/alpha#42")
	}
}

// TestIsPinnedIssue covers the case-insensitive membership check used
// for the action-menu Pin/Unpin label.
func TestIsPinnedIssue(t *testing.T) {
	it := mkIssue("alice/alpha", 42)
	if isPinnedIssue(it, nil) {
		t.Errorf("empty pins should report not pinned")
	}
	if !isPinnedIssue(it, []string{"alice/alpha#42"}) {
		t.Errorf("exact match should report pinned")
	}
	if !isPinnedIssue(it, []string{"ALICE/ALPHA#42"}) {
		t.Errorf("case-insensitive match should report pinned")
	}
	if isPinnedIssue(it, []string{"alice/alpha#7"}) {
		t.Errorf("different number should report not pinned")
	}
	if isPinnedIssue(it, []string{"alice/beta#42"}) {
		t.Errorf("different repo should report not pinned")
	}
}

// TestPartitionIssuesByPinned pins the partition contract:
//   - pinned slice ordered by position in the pins list, not by
//     position in the issues input
//   - case-insensitive "owner/name#N" match
//   - rest preserves input order
//   - a pin that matches no issue is silently absent (a stale/closed
//     pinned entry doesn't appear)
func TestPartitionIssuesByPinned(t *testing.T) {
	issues := []github.Issue{
		mkIssue("alice/alpha", 1),
		mkIssue("bob/beta", 2),
		mkIssue("carol/gamma", 3),
	}
	tests := []struct {
		name       string
		pins       []string
		wantPinned []string // expected "owner/name#N" in the pinned slice (in order)
		wantRest   []string
	}{
		{
			name:       "empty pins → everything in rest",
			pins:       nil,
			wantPinned: nil,
			wantRest:   []string{"alice/alpha#1", "bob/beta#2", "carol/gamma#3"},
		},
		{
			name:       "single pin pulled to top",
			pins:       []string{"bob/beta#2"},
			wantPinned: []string{"bob/beta#2"},
			wantRest:   []string{"alice/alpha#1", "carol/gamma#3"},
		},
		{
			name:       "pinned order matches pins order, not issue order",
			pins:       []string{"carol/gamma#3", "alice/alpha#1"},
			wantPinned: []string{"carol/gamma#3", "alice/alpha#1"},
			wantRest:   []string{"bob/beta#2"},
		},
		{
			name:       "case-insensitive match",
			pins:       []string{"ALICE/ALPHA#1"},
			wantPinned: []string{"alice/alpha#1"},
			wantRest:   []string{"bob/beta#2", "carol/gamma#3"},
		},
		{
			name:       "pin that matches no issue is silently absent",
			pins:       []string{"nobody/nothing#9", "alice/alpha#1"},
			wantPinned: []string{"alice/alpha#1"},
			wantRest:   []string{"bob/beta#2", "carol/gamma#3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinned, rest := partitionIssuesByPinned(issues, tt.pins)
			if got := issueIDs(pinned); !reflect.DeepEqual(got, tt.wantPinned) {
				t.Errorf("pinned = %v, want %v", got, tt.wantPinned)
			}
			if got := issueIDs(rest); !reflect.DeepEqual(got, tt.wantRest) {
				t.Errorf("rest = %v, want %v", got, tt.wantRest)
			}
		})
	}
}

// TestVisibleIssuesPartitioned checks the single-source-of-truth row
// pipeline: pinned float to the top in config order, the rest is
// filtered + sorted, and the section counts are correct. The filter
// and sort apply to the rest only — a pinned row stays pinned and in
// config order regardless of the active sort.
func TestVisibleIssuesPartitioned(t *testing.T) {
	open := []github.Issue{
		mkIssue("alice/alpha", 1),
		mkIssue("bob/beta", 2),
		mkIssue("carol/gamma", 3),
		mkIssue("alice/delta", 4),
	}

	t.Run("no pins, sort by number", func(t *testing.T) {
		rows, pinCount, restCount := visibleIssuesPartitioned(open, "", IssuesSortNumber, nil)
		if pinCount != 0 {
			t.Errorf("pinCount = %d, want 0", pinCount)
		}
		if restCount != 4 {
			t.Errorf("restCount = %d, want 4", restCount)
		}
		want := []string{"alice/alpha#1", "bob/beta#2", "carol/gamma#3", "alice/delta#4"}
		if got := issueIDs(rows); !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})

	t.Run("pinned float to top in config order; rest sorted", func(t *testing.T) {
		// Pin gamma then alpha (config order). Rest (beta, delta) is
		// sorted by number ascending.
		pins := []string{"carol/gamma#3", "alice/alpha#1"}
		rows, pinCount, restCount := visibleIssuesPartitioned(open, "", IssuesSortNumber, pins)
		if pinCount != 2 {
			t.Errorf("pinCount = %d, want 2", pinCount)
		}
		if restCount != 2 {
			t.Errorf("restCount = %d, want 2", restCount)
		}
		want := []string{"carol/gamma#3", "alice/alpha#1", "bob/beta#2", "alice/delta#4"}
		if got := issueIDs(rows); !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})

	t.Run("filter applies to both segments; counts reflect matches", func(t *testing.T) {
		// Filter "alice" matches alpha (#1) and delta (#4). Pin alpha:
		// it stays pinned, delta is the only rest row.
		pins := []string{"alice/alpha#1"}
		rows, pinCount, restCount := visibleIssuesPartitioned(open, "alice", IssuesSortNumber, pins)
		if pinCount != 1 {
			t.Errorf("pinCount = %d, want 1", pinCount)
		}
		if restCount != 1 {
			t.Errorf("restCount = %d, want 1", restCount)
		}
		want := []string{"alice/alpha#1", "alice/delta#4"}
		if got := issueIDs(rows); !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})

	t.Run("filter that drops a pinned issue removes it from pinned", func(t *testing.T) {
		// gamma is pinned but the "alice" filter excludes it, so the
		// pinned section collapses to nothing.
		pins := []string{"carol/gamma#3"}
		rows, pinCount, restCount := visibleIssuesPartitioned(open, "alice", IssuesSortNumber, pins)
		if pinCount != 0 {
			t.Errorf("pinCount = %d, want 0", pinCount)
		}
		if restCount != 2 {
			t.Errorf("restCount = %d, want 2", restCount)
		}
		want := []string{"alice/alpha#1", "alice/delta#4"}
		if got := issueIDs(rows); !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})
}

// issueIDs maps issues to their "owner/name#N" identity for compact
// assertions.
func issueIDs(issues []github.Issue) []string {
	if len(issues) == 0 {
		return nil
	}
	out := make([]string, len(issues))
	for i, it := range issues {
		out[i] = issueID(it)
	}
	return out
}
