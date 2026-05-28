package ui

import (
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// TestVisiblePRsPartitioned pins the contract that drives the PRs
// tab partitioning: review-requests on top (API order, no sort
// applied), authored below (filtered + sorted), counts reported
// so the renderer knows where to insert the rule.
func TestVisiblePRsPartitioned(t *testing.T) {
	mk := func(num int, title, repo string, updated time.Time) github.PullRequest {
		return github.PullRequest{Number: num, Title: title, Repo: repo, UpdatedAt: updated}
	}
	now := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)

	reviews := []github.PullRequest{
		// API returns DESC by UpdatedAt — assume that here too.
		mk(234, "fix: race", "alice/foo", now.Add(-2*24*time.Hour)),
		mk(189, "refactor", "bob/bar", now.Add(-5*24*time.Hour)),
	}
	authored := []github.PullRequest{
		// Mixed-order on purpose to verify sort kicks in.
		mk(50, "my old PR", "me/x", now.Add(-30*24*time.Hour)),
		mk(100, "my new PR", "me/y", now.Add(-1*24*time.Hour)),
	}

	t.Run("both sections present, ordering", func(t *testing.T) {
		rows, rc, ac := visiblePRsPartitioned(reviews, authored, "", PRsSortUpdated)
		if rc != 2 || ac != 2 {
			t.Fatalf("counts = (%d, %d), want (2, 2)", rc, ac)
		}
		if len(rows) != 4 {
			t.Fatalf("len(rows) = %d, want 4", len(rows))
		}
		// First two: reviews in API order.
		if rows[0].Number != 234 || rows[1].Number != 189 {
			t.Errorf("review-requests order broken: got #%d, #%d", rows[0].Number, rows[1].Number)
		}
		// Next two: authored sorted DESC by UpdatedAt → #100 first.
		if rows[2].Number != 100 || rows[3].Number != 50 {
			t.Errorf("authored sort broken: got #%d, #%d (want #100, #50)", rows[2].Number, rows[3].Number)
		}
	})

	t.Run("empty review-requests → only authored", func(t *testing.T) {
		rows, rc, ac := visiblePRsPartitioned(nil, authored, "", PRsSortUpdated)
		if rc != 0 || ac != 2 {
			t.Errorf("counts = (%d, %d), want (0, 2)", rc, ac)
		}
		if len(rows) != 2 || rows[0].Number != 100 {
			t.Errorf("authored-only path broken: got len=%d first=#%d", len(rows), rows[0].Number)
		}
	})

	t.Run("empty authored → only review-requests", func(t *testing.T) {
		rows, rc, ac := visiblePRsPartitioned(reviews, nil, "", PRsSortUpdated)
		if rc != 2 || ac != 0 {
			t.Errorf("counts = (%d, %d), want (2, 0)", rc, ac)
		}
		if len(rows) != 2 || rows[0].Number != 234 {
			t.Errorf("review-only path broken: got len=%d first=#%d", len(rows), rows[0].Number)
		}
	})

	t.Run("filter applies to both segments", func(t *testing.T) {
		rows, rc, ac := visiblePRsPartitioned(reviews, authored, "race", PRsSortUpdated)
		// Only #234 ("fix: race") matches in reviews; nothing in authored.
		if rc != 1 || ac != 0 {
			t.Errorf("filter counts = (%d, %d), want (1, 0)", rc, ac)
		}
		if len(rows) != 1 || rows[0].Number != 234 {
			t.Errorf("filter broken: got len=%d first=#%v", len(rows), rows[0].Number)
		}
	})
}
