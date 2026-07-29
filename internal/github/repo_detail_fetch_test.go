package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// repoDetailHappyBody is a minimal-but-valid repoDetailQuery
// response. The nil-able sub-nodes (license, language, previews,
// commits, topics) are simply absent, which extractRepoDetail already
// tolerates — enough to prove the mandatory detail branch was applied.
const repoDetailHappyBody = `{"data":{"repository":{
	"name":"octoscope",
	"url":"https://github.com/gfazioli/octoscope",
	"stargazerCount":57,
	"forkCount":3
}}}`

// newSplitDetailServer routes the two parallel queries of
// FetchRepoDetail by request body: the star-history walk hits the
// stargazers connection, everything else is the detail query. The
// stargazers branch replies with the caller-supplied status/body so a
// test can make just the star-history walk fail while the detail query
// succeeds.
func newSplitDetailServer(t *testing.T, starStatus int, starBody string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// A read error would otherwise route to the detail branch
			// (empty body) and fail the test confusingly — surface it.
			t.Errorf("test server: reading request body: %v", err)
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "stargazers(") {
			w.WriteHeader(starStatus)
			_, _ = io.WriteString(w, starBody)
			return
		}
		_, _ = io.WriteString(w, repoDetailHappyBody)
	}))
	t.Cleanup(srv.Close)
	return &Client{
		gql: githubv4.NewClient(&http.Client{Transport: &rewriteHost{host: srv.URL}}),
		// authenticated:false skips ensureViewerID's bootstrap
		// round-trip, so only the detail + star-history queries run.
		authenticated: false,
	}
}

// TestFetchRepoDetailStarHistoryFailureIsNonFatal pins the best-effort
// contract for the star-history walk: the stargazers connection that
// GitHub has been restricting can 5xx (or be denied) without aborting
// the whole drill-in. The detail query still succeeds; only the
// sparkline is dropped.
func TestFetchRepoDetailStarHistoryFailureIsNonFatal(t *testing.T) {
	c := newSplitDetailServer(t, http.StatusBadGateway, "502 Bad Gateway")

	d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchRepoDetail err = %v; a failing star-history walk must NOT fail the detail view", err)
	}
	if d.Stars != 57 {
		t.Errorf("Stars = %d, want 57 (the mandatory detail query must still be applied)", d.Stars)
	}
	if len(d.StarHistory) != 0 {
		t.Errorf("StarHistory = %v, want empty (a failed walk yields no sparkline)", d.StarHistory)
	}
}

// TestFetchRepoDetailQueryFailureIsFatal is the other half of the
// contract: the detail query itself is mandatory, so its failure still
// aborts the fetch — unlike the best-effort star-history walk.
func TestFetchRepoDetailQueryFailureIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "502 Bad Gateway")
	}))
	t.Cleanup(srv.Close)
	c := &Client{
		gql:           githubv4.NewClient(&http.Client{Transport: &rewriteHost{host: srv.URL}}),
		authenticated: false,
	}

	_, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
	if err == nil {
		t.Fatal("expected an error when the mandatory detail query fails, got nil")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("err type = %T, want *FetchError", err)
	}
	if fe.Reason != ReasonServer {
		t.Errorf("Reason = %d, want ReasonServer (%d) for a 502", fe.Reason, ReasonServer)
	}
}

// repoDetailChecksBody exercises the status-check rollup on the default
// branch: a CheckRun that passed, one that failed, a legacy
// StatusContext, and an empty node. The empty node is deliberate — it
// stands for a rollup context of neither concrete type, which the
// extractor has to skip rather than record as a nameless check.
//
// Note the third entry points off github.com: a third-party CI provider
// reporting through the Checks API is legitimate, and the extractor's
// job is to carry that URL faithfully. Deciding whether it may become a
// clickable link belongs to the UI (isGitHubURL), not here.
const repoDetailChecksBody = `{"data":{"repository":{
	"name":"octoscope",
	"url":"https://github.com/gfazioli/octoscope",
	"stargazerCount":57,
	"defaultBranchRef":{"target":{
		"statusCheckRollup":{
			"state":"FAILURE",
			"contexts":{"totalCount":9,"nodes":[
				{"name":"build (ubuntu-latest)","conclusion":"SUCCESS","status":"COMPLETED","detailsUrl":"https://github.com/gfazioli/octoscope/actions/runs/1/job/2"},
				{"name":"govulncheck","conclusion":"FAILURE","status":"COMPLETED","detailsUrl":"https://github.com/gfazioli/octoscope/actions/runs/1/job/3"},
				{"context":"ci/legacy-status","state":"ERROR","targetUrl":"https://circleci.com/gh/o/r/9"},
				{}
			]}
		}
	}}
}}}`

// repoDetailChecksHostileBody puts terminal escapes in a check name and
// its URL, as JSON \u escapes so no raw control byte lives in this
// source file. Both fields are attacker-influenceable — a check name
// comes from a workflow file, the URL from whatever app posted the
// check — so both must be sanitized at this boundary, before any
// renderer or OSC 8 escape sees them.
const repoDetailChecksHostileBody = `{"data":{"repository":{
	"name":"octoscope",
	"url":"https://github.com/gfazioli/octoscope",
	"defaultBranchRef":{"target":{
		"statusCheckRollup":{
			"state":"FAILURE",
			"contexts":{"nodes":[
				{"name":"\u001b[31mfake-red\u001b[0m","conclusion":"FAILURE","status":"COMPLETED","detailsUrl":"https://github.com/o/r/actions/runs/\u001b]8;;evil\u0007"}
			]}
		}
	}}
}}}`

// newDetailServer serves body for the detail query and an empty
// stargazers page for the best-effort star-history walk, so a test can
// focus on what extractRepoDetail did with the payload.
func newDetailServer(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("test server: reading request body: %v", err)
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(reqBody), "stargazers(") {
			_, _ = io.WriteString(w, `{"data":{"repository":{"stargazers":{"pageInfo":{"hasNextPage":false},"edges":[]}}}}`)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{
		gql:           githubv4.NewClient(&http.Client{Transport: &rewriteHost{host: srv.URL}}),
		authenticated: false,
	}
}

// TestFetchRepoDetailChecks covers the CI insight payload: the rollup
// state, both concrete context types collapsing into CheckSummary, the
// reporting URLs, and the skip for a node that is neither type.
func TestFetchRepoDetailChecks(t *testing.T) {
	c := newDetailServer(t, repoDetailChecksBody)

	d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchRepoDetail err = %v", err)
	}

	if d.CIState != "FAILURE" {
		t.Errorf("CIState = %q, want FAILURE", d.CIState)
	}
	if len(d.Checks) != 3 {
		t.Fatalf("len(Checks) = %d, want 3 (the typeless node must be skipped): %+v", len(d.Checks), d.Checks)
	}
	// The rollup reports 9 contexts while the fetch returned 4 (3 usable
	// + 1 typeless). Carrying the real total is what lets the UI report
	// overflow honestly instead of counting only what it holds.
	if d.ChecksTotal != 9 {
		t.Errorf("ChecksTotal = %d, want 9 (the rollup's own count, not len(Checks))", d.ChecksTotal)
	}

	// CheckRun: name / conclusion / status / detailsUrl.
	if got := d.Checks[0]; got.Name != "build (ubuntu-latest)" || got.Conclusion != "SUCCESS" ||
		got.Status != "COMPLETED" || got.URL != "https://github.com/gfazioli/octoscope/actions/runs/1/job/2" {
		t.Errorf("Checks[0] = %+v, want the passing CheckRun with its detailsUrl", got)
	}
	if got := d.Checks[1]; got.Name != "govulncheck" || got.Conclusion != "FAILURE" {
		t.Errorf("Checks[1] = %+v, want the failing CheckRun", got)
	}

	// StatusContext: context -> Name, state -> Conclusion, targetUrl ->
	// URL. Status stays empty; a legacy status context has no notion of
	// one, which is why checkOutcome falls back to Conclusion.
	got := d.Checks[2]
	if got.Name != "ci/legacy-status" || got.Conclusion != "ERROR" || got.Status != "" {
		t.Errorf("Checks[2] = %+v, want the legacy StatusContext mapped onto CheckSummary", got)
	}
	if got.URL != "https://circleci.com/gh/o/r/9" {
		t.Errorf("Checks[2].URL = %q, want the third-party URL carried through verbatim", got.URL)
	}
}

// TestFetchRepoDetailChecksSanitized is the boundary contract: escape
// sequences in a check name or its URL are stripped here, so no
// renderer downstream has to remember to do it.
func TestFetchRepoDetailChecksSanitized(t *testing.T) {
	c := newDetailServer(t, repoDetailChecksHostileBody)

	d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchRepoDetail err = %v", err)
	}
	if len(d.Checks) != 1 {
		t.Fatalf("len(Checks) = %d, want 1", len(d.Checks))
	}

	for _, field := range []struct{ label, value string }{
		{"Name", d.Checks[0].Name},
		{"URL", d.Checks[0].URL},
	} {
		for _, b := range []byte(field.value) {
			if b < 0x20 || b == 0x7f {
				t.Errorf("%s = %q still carries control byte %#x after Sanitize", field.label, field.value, b)
			}
		}
	}
	if !strings.Contains(d.Checks[0].Name, "fake-red") {
		t.Errorf("Name = %q, want the visible text preserved", d.Checks[0].Name)
	}
}

// TestFetchRepoDetailNoChecks keeps the absent-CI case honest: a repo
// with no rollup yields no state and no checks, which is what lets the
// drill-in drop the section entirely instead of heading an empty list.
func TestFetchRepoDetailNoChecks(t *testing.T) {
	c := newDetailServer(t, repoDetailHappyBody)

	d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchRepoDetail err = %v", err)
	}
	if d.CIState != "" || len(d.Checks) != 0 {
		t.Errorf("CIState = %q, Checks = %+v; want both empty for a repo with no rollup", d.CIState, d.Checks)
	}
}
