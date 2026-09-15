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
	// commits_last_year is a pointer that exists only when the opt-in
	// branch ran (#70): absent here because the fixture never set
	// CommitsLastYearApplied, so 0 and "not fetched" stay distinct.
	if r.Repositories[0].CommitsLastYear != nil {
		t.Errorf("repo[0].commits_last_year = %v, want absent when not applied", *r.Repositories[0].CommitsLastYear)
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

// The scriptable output has to carry the new tab, or --json silently
// describes less than the TUI shows. Adding a key is additive, so no
// SchemaVersion bump — that is the documented contract.
func TestReportCarriesGists(t *testing.T) {
	s := &github.Stats{
		Gists: []github.Gist{
			{Name: "hash1", Description: "Sample list", URL: "https://gist.github.com/u/1",
				IsPublic: true, Stars: 5,
				Files: []github.GistFile{{Name: "a.json"}, {Name: "b.json"}}},
			{Name: "hash2", URL: "https://gist.github.com/u/2", IsPublic: false,
				Files: []github.GistFile{{Name: "about.json"}}},
		},
	}
	r := FromStats(s, "0.29.0", time.Now(), false)

	if len(r.Gists) != 2 {
		t.Fatalf("report carries %d gists, want 2", len(r.Gists))
	}
	if r.Gists[0].Label != "Sample list" {
		t.Errorf("label = %q, want the description", r.Gists[0].Label)
	}
	// The untitled one must be callable by the same name the dashboard
	// shows, or a script and the TUI disagree about what a gist is called.
	if r.Gists[1].Label != "about.json" {
		t.Errorf("untitled gist label = %q, want its first filename", r.Gists[1].Label)
	}
	if r.Gists[1].Description != "" {
		t.Errorf("description was invented: %q", r.Gists[1].Description)
	}
	if r.Gists[0].FilesCapped {
		t.Error("a two-file gist reported as capped")
	}
}

// Every list is always an array, never null — the documented promise that
// lets consumers iterate unconditionally.
func TestReportGistsIsAlwaysAnArray(t *testing.T) {
	r := FromStats(&github.Stats{}, "0.29.0", time.Now(), false)
	if r.Gists == nil {
		t.Error("gists is null on an account with none; consumers cannot iterate it")
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"gists": []`)) {
		t.Errorf("empty gists did not render as an array:\n%s", buf.String())
	}
}

// A capped file list must say so, because GitHub's Gist.files has no
// totalCount — a consumer reading len(files) would take a truncated count
// for a complete one.
func TestReportFlagsACappedFileList(t *testing.T) {
	files := make([]github.GistFile, github.GistFilesLimit)
	for i := range files {
		files[i] = github.GistFile{Name: "f.go"}
	}
	r := FromStats(&github.Stats{Gists: []github.Gist{{Name: "big", Files: files}}},
		"0.29.0", time.Now(), false)
	if !r.Gists[0].FilesCapped {
		t.Error("a gist at the fetch cap is not flagged, so len(files) reads as complete")
	}
}

func sampleEvents() []github.Event {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return []github.Event{
		{
			ID: "1001", Type: "PullRequestEvent", Repo: "gfazioli/octoscope",
			CreatedAt: ts, IsPublic: true, Action: "merged", Number: 42,
			IsPullRequest: true, Title: "Fix the thing",
			URL: "https://github.com/gfazioli/octoscope/pull/42",
		},
		{
			ID: "1002", Type: "PushEvent", Repo: "gfazioli/private-thing",
			CreatedAt: ts.Add(-time.Hour), IsPublic: false, Ref: "main",
			URL: "https://github.com/gfazioli/private-thing",
		},
		{
			// The documented exception: Action holds the review state here,
			// not the payload action. It must survive the mapping verbatim.
			ID: "1003", Type: "PullRequestReviewEvent", Repo: "acme/lib",
			CreatedAt: ts.Add(-2 * time.Hour), IsPublic: true,
			Action: "changes_requested", Number: 7, IsPullRequest: true,
			URL: "https://github.com/acme/lib/pull/7",
		},
	}
}

// The distinction the pointer exists for. Three states, three different
// JSON documents — and the middle one is the whole point: a consumer must
// be able to tell "nobody asked for the feed" from "asked, and there was
// nothing".
func TestRecentActivityDistinguishesUnaskedFromEmpty(t *testing.T) {
	render := func(t *testing.T, r Report) string {
		t.Helper()
		var buf bytes.Buffer
		if err := RenderJSON(&buf, r); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		return buf.String()
	}

	t.Run("not asked for: the key is absent", func(t *testing.T) {
		out := render(t, FromStats(sampleStats(), "0.35.0", time.Now(), false))
		if strings.Contains(out, "recent_activity") {
			t.Errorf("recent_activity must be omitted when AttachEvents was never called, got:\n%s", out)
		}
	})

	t.Run("asked for, nothing there: an empty array", func(t *testing.T) {
		r := FromStats(sampleStats(), "0.35.0", time.Now(), false)
		AttachEvents(&r, nil)
		out := render(t, r)
		if !strings.Contains(out, `"recent_activity": []`) {
			t.Errorf(`expected "recent_activity": [], got:\n%s`, out)
		}
	})

	t.Run("asked for, events present", func(t *testing.T) {
		r := FromStats(sampleStats(), "0.35.0", time.Now(), false)
		AttachEvents(&r, sampleEvents())
		out := render(t, r)
		// Decoded rather than substring-matched: the shape is the contract.
		var doc struct {
			RecentActivity []map[string]any `json:"recent_activity"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(doc.RecentActivity) != 3 {
			t.Fatalf("got %d events, want 3", len(doc.RecentActivity))
		}
		first := doc.RecentActivity[0]
		for k, want := range map[string]any{
			"id": "1001", "type": "PullRequestEvent", "repo": "gfazioli/octoscope",
			"action": "merged", "number": float64(42), "is_pull_request": true,
			"public": true, "title": "Fix the thing",
		} {
			if first[k] != want {
				t.Errorf("event[0][%q] = %#v, want %#v", k, first[k], want)
			}
		}
		// IsPublic -> "public": the field is renamed across the boundary, so
		// a private event must not read as public.
		if doc.RecentActivity[1]["public"] != false {
			t.Errorf("event[1].public = %#v, want false", doc.RecentActivity[1]["public"])
		}
		// The review-state exception survives verbatim.
		if doc.RecentActivity[2]["action"] != "changes_requested" {
			t.Errorf("event[2].action = %#v, want changes_requested", doc.RecentActivity[2]["action"])
		}
	})
}

func TestRenderPlainActivitySection(t *testing.T) {
	t.Run("not asked for: no section at all", func(t *testing.T) {
		var buf bytes.Buffer
		if err := RenderPlain(&buf, FromStats(sampleStats(), "0.35.0", time.Now(), false)); err != nil {
			t.Fatalf("RenderPlain: %v", err)
		}
		if strings.Contains(buf.String(), "Recent activity") {
			t.Errorf("unasked feed must print nothing, got:\n%s", buf.String())
		}
	})

	t.Run("asked for, nothing there: says so", func(t *testing.T) {
		r := FromStats(sampleStats(), "0.35.0", time.Now(), false)
		AttachEvents(&r, nil)
		var buf bytes.Buffer
		if err := RenderPlain(&buf, r); err != nil {
			t.Fatalf("RenderPlain: %v", err)
		}
		if !strings.Contains(buf.String(), "Recent activity (0)") ||
			!strings.Contains(buf.String(), "no recent events") {
			t.Errorf("an empty feed must say so rather than vanish, got:\n%s", buf.String())
		}
	})

	t.Run("asked for, events present", func(t *testing.T) {
		r := FromStats(sampleStats(), "0.35.0", time.Now(), false)
		AttachEvents(&r, sampleEvents())
		var buf bytes.Buffer
		if err := RenderPlain(&buf, r); err != nil {
			t.Fatalf("RenderPlain: %v", err)
		}
		out := buf.String()
		for _, frag := range []string{
			"Recent activity (3)",
			"2026-03-04 05:06",
			"PullRequestEvent/merged",
			"PR #42 Fix the thing",
			// A push has no number and no title: the ref is the subject.
			"PushEvent",
			"main",
			"PullRequestReviewEvent/changes_requested",
		} {
			if !strings.Contains(out, frag) {
				t.Errorf("expected %q in plain output, got:\n%s", frag, out)
			}
		}
	})

	t.Run("long feeds are capped like every other list", func(t *testing.T) {
		many := make([]github.Event, 0, plainListCap+5)
		for i := 0; i < plainListCap+5; i++ {
			many = append(many, github.Event{
				ID: "e", Type: "WatchEvent", Repo: "acme/lib",
				CreatedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
			})
		}
		r := FromStats(sampleStats(), "0.35.0", time.Now(), false)
		AttachEvents(&r, many)
		var buf bytes.Buffer
		if err := RenderPlain(&buf, r); err != nil {
			t.Fatalf("RenderPlain: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Recent activity (20)") {
			t.Errorf("header must report the full count, got:\n%s", out)
		}
		if got := strings.Count(out, "WatchEvent"); got != plainListCap {
			t.Errorf("printed %d rows, want the %d-row cap", got, plainListCap)
		}
	})
}
