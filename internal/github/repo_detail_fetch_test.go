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
		body, _ := io.ReadAll(r.Body)
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
