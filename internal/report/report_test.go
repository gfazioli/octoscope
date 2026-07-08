package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

func sampleStats() *github.Stats {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &github.Stats{
		Login:               "gfazioli",
		Name:                "Giovambattista",
		Bio:                 "hi",
		CreatedAt:           ts,
		Followers:           12,
		Following:           8,
		TotalStars:          340,
		TotalStarsWithForks: 351,
		PRsTotal:            120,
		PRsMerged:           98,
		IssuesAuthored:      45,
		OpenPRsAuthored:     3,
		CommitsLastYear:     1204,
		PublicRepos:         74,
		ForksReceived:       20,
		OpenIssues:          5,
		OpenPRs:             2,
		Languages: []github.Language{
			{Name: "Go", Bytes: 750, Color: "#00ADD8"},
			{Name: "Shell", Bytes: 250, Color: "#89e051"},
		},
		Repositories: []github.Repo{
			{
				Name: "octoscope", URL: "https://github.com/gfazioli/octoscope",
				PrimaryLanguage: "Go", Stars: 300, Forks: 15, OpenIssues: 2,
				PushedAt: ts, CIState: "SUCCESS",
				LatestReleaseTag: "v0.23.0", LatestReleasePublishedAt: ts,
			},
			{Name: "private-thing", URL: "https://github.com/gfazioli/private-thing", IsPrivate: true},
		},
		OpenPullRequests: []github.PullRequest{
			{Number: 42, Title: "Fix", Repo: "gfazioli/octoscope", URL: "u", IsDraft: true},
		},
		OpenIssuesList: []github.Issue{
			{Number: 34, Title: "CI insight", Repo: "gfazioli/octoscope", URL: "u"},
		},
		ReviewRequests: []github.PullRequest{
			{Number: 7, Title: "Review me", Repo: "acme/lib", URL: "u", AuthorLogin: "octocat"},
		},
		Organizations:  []github.Organization{{Login: "acme", Name: "Acme Inc"}},
		WatchedRepos:   []github.Repo{{Name: "acme/lib", URL: "u", Stars: 10}},
		WatchedSkipped: []string{"acme/renamed"},
		Authenticated:  true,
		IsViewer:       true,
		RateLimit:      &github.RateLimit{Cost: 3, Limit: 5000, Remaining: 4997, ResetAt: ts},
	}
}

func TestFromStatsMapping(t *testing.T) {
	ts := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	r := FromStats(sampleStats(), "0.24.0", ts, false)

	if r.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", r.SchemaVersion, SchemaVersion)
	}
	if r.OctoscopeVersion != "0.24.0" {
		t.Errorf("octoscope_version = %q", r.OctoscopeVersion)
	}
	if !r.GeneratedAt.Equal(ts) {
		t.Errorf("generated_at = %v, want %v", r.GeneratedAt, ts)
	}
	if r.Profile.Login != "gfazioli" || r.Social.Followers != 12 {
		t.Errorf("profile/social mismatch: %+v %+v", r.Profile, r.Social)
	}
	if r.Activity.PRsMerged != 98 || r.Operational.PublicRepos != 74 {
		t.Errorf("activity/operational mismatch")
	}
	if got := len(r.Repositories); got != 2 {
		t.Fatalf("repositories = %d, want 2", got)
	}
	// LatestRelease pointer set only when a tag exists.
	if r.Repositories[0].LatestRelease == nil || r.Repositories[0].LatestRelease.Tag != "v0.23.0" {
		t.Errorf("repo[0].latest_release = %+v", r.Repositories[0].LatestRelease)
	}
	if r.Repositories[1].LatestRelease != nil {
		t.Errorf("repo[1] with no release must have nil latest_release")
	}
	if r.ReviewRequests[0].Author != "octocat" {
		t.Errorf("review request author = %q", r.ReviewRequests[0].Author)
	}
	if r.Organizations[0].Login != "acme" || r.Organizations[0].Name != "Acme Inc" {
		t.Errorf("org mismatch: %+v", r.Organizations[0])
	}
	if r.RateLimit == nil || r.RateLimit.Remaining != 4997 {
		t.Errorf("rate_limit mismatch: %+v", r.RateLimit)
	}
}

func TestFromStatsLanguagePercent(t *testing.T) {
	r := FromStats(sampleStats(), "0.24.0", time.Now(), false)
	// 750 / 1000 = 75.0, 250 / 1000 = 25.0
	if r.Languages[0].Percent != 75.0 {
		t.Errorf("Go percent = %v, want 75.0", r.Languages[0].Percent)
	}
	if r.Languages[1].Percent != 25.0 {
		t.Errorf("Shell percent = %v, want 25.0", r.Languages[1].Percent)
	}
	if r.Languages[0].Bytes != 750 {
		t.Errorf("Go bytes = %d, want 750", r.Languages[0].Bytes)
	}
}

func TestFromStatsPublicOnlyMetadata(t *testing.T) {
	// FromStats does not filter; it only records the flag. The caller is
	// responsible for having called Stats.Public() beforehand.
	r := FromStats(sampleStats(), "0.24.0", time.Now(), true)
	if !r.PublicOnly {
		t.Error("public_only should be true when passed true")
	}
}

func TestRenderJSONWellFormedAndStableKeys(t *testing.T) {
	r := FromStats(sampleStats(), "0.24.0", time.Now(), false)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	// Must be valid JSON.
	var generic map[string]any
	if err := json.Unmarshal(buf.Bytes(), &generic); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	// Contract keys present at the top level.
	for _, k := range []string{
		"schema_version", "octoscope_version", "generated_at",
		"authenticated", "is_viewer", "public_only",
		"profile", "social", "activity", "operational",
		"languages", "repositories", "open_pull_requests",
		"open_issues_list", "review_requests", "organizations",
		"watched_repos", "watched_skipped",
	} {
		if _, ok := generic[k]; !ok {
			t.Errorf("missing top-level key %q", k)
		}
	}

	// Round-trips back into the typed Report unchanged in the essentials.
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.SchemaVersion != SchemaVersion || back.Profile.Login != "gfazioli" {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

func TestRenderJSONEmptyListsAreArraysNotNull(t *testing.T) {
	// A minimal Stats with every list nil must still emit [] so consumers
	// can iterate unconditionally.
	r := FromStats(&github.Stats{Login: "empty"}, "0.24.0", time.Now(), false)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	out := buf.String()
	for _, frag := range []string{
		`"languages": []`,
		`"repositories": []`,
		`"open_pull_requests": []`,
		`"open_issues_list": []`,
		`"review_requests": []`,
		`"organizations": []`,
		`"watched_repos": []`,
		`"watched_skipped": []`,
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("expected %s in output, got:\n%s", frag, out)
		}
	}
	// A nil rate limit is omitted entirely.
	if strings.Contains(out, "rate_limit") {
		t.Errorf("nil rate_limit should be omitted, got:\n%s", out)
	}
}

func TestRenderPlainContainsCountersAndLists(t *testing.T) {
	r := FromStats(sampleStats(), "0.24.0", time.Now(), true)
	var buf bytes.Buffer
	if err := RenderPlain(&buf, r); err != nil {
		t.Fatalf("RenderPlain: %v", err)
	}
	out := buf.String()
	for _, frag := range []string{
		"octoscope 0.24.0 — gfazioli (Giovambattista)",
		"public-only",
		"followers 12",
		"with forks 351",
		"Languages    Go 75.0% · Shell 25.0%",
		"Repositories (2)",
		"Open pull requests (1)",
		"Open issues (1)",
		"Review requests (1)",
		"by octocat",
		"1 watched entry skipped: acme/renamed",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("expected %q in plain output, got:\n%s", frag, out)
		}
	}
}

func TestRenderPlainCapsLongLists(t *testing.T) {
	s := &github.Stats{Login: "busy"}
	for i := 0; i < plainListCap+5; i++ {
		s.Repositories = append(s.Repositories, github.Repo{Name: "r", Stars: i})
	}
	r := FromStats(s, "0.24.0", time.Now(), false)
	var buf bytes.Buffer
	if err := RenderPlain(&buf, r); err != nil {
		t.Fatalf("RenderPlain: %v", err)
	}
	if !strings.Contains(buf.String(), "…and 5 more") {
		t.Errorf("expected cap notice, got:\n%s", buf.String())
	}
}
