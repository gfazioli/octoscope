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

// newTestGQLClientCapturing is newTestGQLClient plus a copy of the request
// body, for the cases where what we send matters as much as what comes back.
// --public-only is exactly that: it has to change the *query*, and no
// assertion on the decoded result can tell a filtered response from a
// filtered request.
func newTestGQLClientCapturing(t *testing.T, body string, sent *string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*sent = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	httpClient := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{
		gql:           githubv4.NewClient(httpClient),
		rest:          httpClient,
		authenticated: true,
	}
}

// A response carrying an ANSI escape in the description and a C1
// introducer in a filename — the two fields an outsider controls, since
// anyone who can share a gist link controls both.
//
// Both are JSON escapes, and that is not a stylistic choice: JSON forbids
// unescaped control characters inside a string, so encoding/json rejects a
// raw one outright ("invalid character in string literal"). Which means
// the escaped form is what GitHub actually puts on the wire, and the raw
// byte only ever appears after the decoder has expanded it — exactly where
// Sanitize is waiting for it.
const gistsBody = `{"data":{"viewer":{"gists":{
	"totalCount": 42,
	"nodes":[
		{"name":"da51e93f0bb208a6f71f",
		 "description":"WordPress: \u001b[31mActions\u001b[0m & Filters",
		 "url":"https://gist.github.com/gfazioli/da51e93f0bb208a6f71f",
		 "isPublic":true,"isFork":false,"stargazerCount":5,
		 "updatedAt":"2026-01-02T03:04:05Z",
		 "files":[{"name":"old_version_event.js","size":812,"language":{"name":"JavaScript"}},
		          {"name":"sam\u009fple.js","size":40,"language":{"name":""}}]},
		{"name":"7ac536c358f737269b6b",
		 "description":"",
		 "url":"https://gist.github.com/gfazioli/7ac536c358f737269b6b",
		 "isPublic":false,"isFork":true,"stargazerCount":0,
		 "updatedAt":"2026-01-01T00:00:00Z",
		 "files":[{"name":"about.json","size":12,"language":{"name":"JSON"}}]}
	]}}}}`

func TestFetchGists(t *testing.T) {
	c := newTestGQLClient(t, 200, gistsBody)

	gs, total, err := c.FetchGists(context.Background())
	if err != nil {
		t.Fatalf("FetchGists: %v", err)
	}
	if total != 42 {
		t.Errorf("total = %d, want 42 — the count is what lets the tab say it is showing a window", total)
	}
	if len(gs) != 2 {
		t.Fatalf("gists = %d, want 2", len(gs))
	}

	first := gs[0]
	if first.Name != "da51e93f0bb208a6f71f" || !first.IsPublic || first.IsFork || first.Stars != 5 {
		t.Errorf("first gist decoded wrong: %+v", first)
	}
	if len(first.Files) != 2 || first.Files[0].Language != "JavaScript" || first.Files[0].Size != 812 {
		t.Errorf("files decoded wrong: %+v", first.Files)
	}
	if first.UpdatedAt.IsZero() {
		t.Error("updatedAt did not decode")
	}

	// Sanitize runs at this boundary, not in the renderer: an escape
	// sequence in a description would otherwise reach the terminal.
	if strings.ContainsRune(first.Description, 0x1b) {
		t.Errorf("ANSI escape survived into the description: %q", first.Description)
	}
	if !strings.Contains(first.Description, "Actions") {
		t.Errorf("sanitising ate the text as well as the escape: %q", first.Description)
	}
	// U+009F is the 8-bit APC introducer — a C1 control, and the class
	// added in v0.20.2 precisely because it is not an ESC.
	if strings.ContainsRune(first.Files[1].Name, 0x9f) {
		t.Errorf("C1 introducer survived into a filename: %q", first.Files[1].Name)
	}
	if !strings.Contains(first.Files[1].Name, "ple.js") {
		t.Errorf("sanitising ate the filename: %q", first.Files[1].Name)
	}

	// The empty description is preserved rather than filled in. The
	// display fallback is the UI's job — inventing one here would put a
	// value into --json output that GitHub never returned.
	if gs[1].Description != "" {
		t.Errorf("empty description was not preserved: %q", gs[1].Description)
	}
	if gs[1].IsPublic || !gs[1].IsFork {
		t.Errorf("second gist flags decoded wrong: %+v", gs[1])
	}
}

// --public-only must change the *query*, not filter the result. Fetching
// everything and hiding the secrets would leak their existence through the
// count: measured live, ALL returns 16 on a real account and PUBLIC returns
// 10 — so a "10 of 16" line would itself disclose that six secret gists
// exist. Only the request body can prove which was asked for.
func TestFetchGistsPublicOnlyAsksForPublic(t *testing.T) {
	var got string
	c := newTestGQLClientCapturing(t, `{"data":{"viewer":{"gists":{"totalCount":0,"nodes":[]}}}}`, &got)
	c.publicOnly = true

	if _, _, err := c.FetchGists(context.Background()); err != nil {
		t.Fatalf("FetchGists: %v", err)
	}
	if !strings.Contains(got, `"privacy":"PUBLIC"`) {
		t.Errorf("public-only did not ask GitHub for PUBLIC; sent: %s", got)
	}
	if strings.Contains(got, `"privacy":"ALL"`) {
		t.Errorf("public-only still asked for ALL: %s", got)
	}
}

func TestFetchGistsDefaultAsksForAll(t *testing.T) {
	var got string
	c := newTestGQLClientCapturing(t, `{"data":{"viewer":{"gists":{"totalCount":0,"nodes":[]}}}}`, &got)

	if _, _, err := c.FetchGists(context.Background()); err != nil {
		t.Fatalf("FetchGists: %v", err)
	}
	if !strings.Contains(got, `"privacy":"ALL"`) {
		t.Errorf("default mode did not ask for ALL, so secret gists would be missing: %s", got)
	}
}

// An account with none decodes to nothing and reports zero.
func TestFetchGistsEmpty(t *testing.T) {
	c := newTestGQLClient(t, 200, `{"data":{"viewer":{"gists":{"totalCount":0,"nodes":[]}}}}`)
	gs, total, err := c.FetchGists(context.Background())
	if err != nil {
		t.Fatalf("FetchGists: %v", err)
	}
	if len(gs) != 0 || total != 0 {
		t.Errorf("empty account decoded to %d gists / total %d", len(gs), total)
	}
}

// The shape that makes this branch best-effort: GitHub answers a permission
// edge with data *and* an error attached. The decode fails, so the caller
// has to be one that can absorb it — FetchStats drops this error on purpose,
// and this test is what says the drop is load-bearing rather than sloppy.
func TestFetchGistsSurfacesPartialResponseAsError(t *testing.T) {
	body := `{"data":{"user":{"gists":{"totalCount":0,"nodes":[]}}},
	          "errors":[{"message":"You don't have permission to see gists."}]}`
	c := newTestGQLClient(t, 200, body)
	c.login = "someone-else"

	if _, _, err := c.FetchGists(context.Background()); err == nil {
		t.Fatal("a response carrying a GraphQL error decoded as success — " +
			"FetchStats relies on this failing so it can swallow it")
	}
}
