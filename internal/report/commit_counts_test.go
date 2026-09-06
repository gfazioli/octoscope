package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// TestCommitsLastYearInReport pins #70's JSON contract: the field is
// present (even at 0) when the branch ran, absent when it did not, and
// never attached to watched repos, which the branch does not cover.
func TestCommitsLastYearInReport(t *testing.T) {
	s := &github.Stats{
		Login:                  "alice",
		CommitsLastYearApplied: true,
		Repositories: []github.Repo{
			{Name: "alpha", URL: "https://github.com/alice/alpha", CommitsLastYear: 12},
			{Name: "beta", URL: "https://github.com/alice/beta", CommitsLastYear: 0},
		},
		WatchedRepos: []github.Repo{{Name: "tea", URL: "https://github.com/charm/tea", CommitsLastYear: 99}},
	}
	r := FromStats(s, "0.32.0", time.Unix(0, 0).UTC(), false)
	if r.Repositories[0].CommitsLastYear == nil || *r.Repositories[0].CommitsLastYear != 12 {
		t.Fatalf("alpha commits_last_year = %v, want 12", r.Repositories[0].CommitsLastYear)
	}
	if r.Repositories[1].CommitsLastYear == nil || *r.Repositories[1].CommitsLastYear != 0 {
		t.Errorf("beta commits_last_year = %v, want an explicit 0 when applied", r.Repositories[1].CommitsLastYear)
	}
	if r.WatchedRepos[0].CommitsLastYear != nil {
		t.Errorf("watched repo carries commits_last_year = %v; the branch never counts watched repos", *r.WatchedRepos[0].CommitsLastYear)
	}
	b, _ := json.Marshal(r.Repositories[1])
	if !strings.Contains(string(b), `"commits_last_year":0`) {
		t.Errorf("applied zero must serialise explicitly: %s", b)
	}

	s.CommitsLastYearApplied = false
	r = FromStats(s, "0.32.0", time.Unix(0, 0).UTC(), false)
	b, _ = json.Marshal(r.Repositories[0])
	if strings.Contains(string(b), "commits_last_year") {
		t.Errorf("not applied: field must be omitted entirely: %s", b)
	}
}
