package ui

import (
	"reflect"
	"testing"

	"github.com/gfazioli/octoscope/internal/github"
)

// TestViewPrefKeyValidation pins the accepted config values for
// default_sort / default_work_filter / default_star_history — the
// contract main.go validates against at startup.
func TestViewPrefKeyValidation(t *testing.T) {
	for _, k := range []string{"pushed", "stars", "forks", "name", "ci", "release", "updated", "repo", "number"} {
		if !IsValidSortKey(k) {
			t.Errorf("IsValidSortKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "bogus", "★ stars", "PUSHED"} {
		if IsValidSortKey(k) {
			t.Errorf("IsValidSortKey(%q) = true, want false", k)
		}
	}
	for _, k := range []string{"prs-open", "ci-broken", "stale"} {
		if !IsValidWorkFilterKey(k) {
			t.Errorf("IsValidWorkFilterKey(%q) = false, want true", k)
		}
	}
	if IsValidWorkFilterKey("") || IsValidWorkFilterKey("none") {
		t.Error("empty / unknown work-filter keys must not validate")
	}
	if !IsValidStarHistoryKey("density") || !IsValidStarHistoryKey("cumulative") {
		t.Error("star-history keys density/cumulative must validate")
	}
	if IsValidStarHistoryKey("") {
		t.Error("empty star-history key must not validate")
	}

	// Key lists feed the startup error messages — sorted and complete.
	wantSort := []string{"ci", "forks", "name", "number", "pushed", "release", "repo", "stars", "updated"}
	if got := SortKeys(); !reflect.DeepEqual(got, wantSort) {
		t.Errorf("SortKeys() = %v, want %v", got, wantSort)
	}
	if got, want := WorkFilterKeys(), []string{"ci-broken", "prs-open", "stale"}; !reflect.DeepEqual(got, want) {
		t.Errorf("WorkFilterKeys() = %v, want %v", got, want)
	}
	if got, want := StarHistoryKeys(), []string{"cumulative", "density"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StarHistoryKeys() = %v, want %v", got, want)
	}
}

// TestNewModelSeedsViewPrefs pins #35: the Default* options seed the
// sub-models' initial state, empty options preserve the pre-#35
// defaults, and a sort key seeds only the tabs whose cycle has it.
func TestNewModelSeedsViewPrefs(t *testing.T) {
	t.Run("empty options keep built-in defaults", func(t *testing.T) {
		m := NewModel(nil, "test", Options{})
		if m.repos.sort != ReposSortPushed || m.repos.work != WorkFilterNone {
			t.Errorf("repos = (sort %d, work %d), want (pushed, none)", m.repos.sort, m.repos.work)
		}
		if m.prs.sort != PRsSortUpdated || m.issues.sort != IssuesSortUpdated {
			t.Errorf("prs/issues sort = (%d, %d), want (updated, updated)", m.prs.sort, m.issues.sort)
		}
		if m.starModeDefault != StarModeDensity {
			t.Errorf("starModeDefault = %d, want density", m.starModeDefault)
		}
	})

	t.Run("repos-only sort key leaves PRs and Issues on their default", func(t *testing.T) {
		m := NewModel(nil, "test", Options{DefaultSort: "stars", DefaultWorkFilter: "ci-broken", DefaultStarHistory: "cumulative"})
		if m.repos.sort != ReposSortStars || m.repos.work != WorkFilterCIBroken {
			t.Errorf("repos = (sort %d, work %d), want (stars, ci-broken)", m.repos.sort, m.repos.work)
		}
		if m.prs.sort != PRsSortUpdated || m.issues.sort != IssuesSortUpdated {
			t.Errorf("prs/issues sort = (%d, %d), want (updated, updated) for a Repos-only key", m.prs.sort, m.issues.sort)
		}
		if m.starModeDefault != StarModeCumulative {
			t.Errorf("starModeDefault = %d, want cumulative", m.starModeDefault)
		}
	})

	t.Run("list sort key leaves Repos on its default", func(t *testing.T) {
		m := NewModel(nil, "test", Options{DefaultSort: "repo"})
		if m.repos.sort != ReposSortPushed {
			t.Errorf("repos.sort = %d, want pushed for a list-only key", m.repos.sort)
		}
		if m.prs.sort != PRsSortRepo || m.issues.sort != IssuesSortRepo {
			t.Errorf("prs/issues sort = (%d, %d), want (repo, repo)", m.prs.sort, m.issues.sort)
		}
	})

	t.Run("drill-in Open starts from the configured star default", func(t *testing.T) {
		m := NewModel(nil, "test", Options{DefaultStarHistory: "cumulative"})
		nm, _ := m.Update(viewRepoDetailMsg{repo: github.Repo{Name: "x", URL: "https://github.com/o/x"}})
		got := nm.(Model).repoDetail
		if !got.IsOpen() || got.starMode != StarModeCumulative {
			t.Errorf("after Open: open=%v starMode=%d, want open + cumulative", got.IsOpen(), got.starMode)
		}
	})
}
