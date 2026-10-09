package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// prStackBody is github/gh-stack#343's stack as GitHub answered it on
// 2026-10-09 — position 3 of 5 onto main — with the layers shuffled,
// one layer whose pull request the viewer cannot see, and an escape
// sequence in a title.
const prStackBody = `{"data":{"repository":{"pullRequest":{"stackEntry":{"position":3,"stack":{
	"size":6,"baseRefName":"main","entries":{"nodes":[
		{"position":4,"pullRequest":{"number":307,"title":"Merge Stacked PRs with merge command","state":"OPEN","isDraft":true,"url":"https://github.com/github/gh-stack/pull/307"}},
		{"position":1,"pullRequest":{"number":330,"title":"Rebase stacks onto the latest remote trunk","state":"MERGED","isDraft":false,"url":"https://github.com/github/gh-stack/pull/330"}},
		{"position":3,"pullRequest":{"number":343,"title":"Minor \u001b[31mdocs updates","state":"OPEN","isDraft":false,"url":"https://github.com/github/gh-stack/pull/343"}},
		{"position":2,"pullRequest":null},
		{"position":5,"pullRequest":{"number":321,"title":"Async merge API docs","state":"CLOSED","isDraft":false,"url":"https://github.com/github/gh-stack/pull/321"}}
	]}}}}}}}`

func TestFetchPRStack(t *testing.T) {
	c := newTestGQLClient(t, http.StatusOK, prStackBody)
	s, err := c.fetchPRStack(context.Background(), "github", "gh-stack", 343)
	if err != nil {
		t.Fatalf("fetchPRStack: %v", err)
	}
	if s == nil || s.Position != 3 || s.Size != 6 || s.BaseRefName != "main" {
		t.Fatalf("stack = %+v", s)
	}
	got := []string{}
	for _, e := range s.Entries {
		got = append(got, strconv.Itoa(e.Position)+":"+strconv.Itoa(e.Number))
	}
	if strings.Join(got, " ") != "1:330 2:0 3:343 4:307 5:321" {
		t.Errorf("entries = %v; want position order, the unseen layer kept as 0", got)
	}
	if strings.Contains(s.Entries[2].Title, "\x1b") {
		t.Errorf("title %q reached the UI unsanitized", s.Entries[2].Title)
	}
	if !s.Entries[3].IsDraft || s.Entries[4].State != "CLOSED" {
		t.Errorf("draft / state lost: %+v %+v", s.Entries[3], s.Entries[4])
	}
}

func TestFetchPRStackOutsideAnyStack(t *testing.T) {
	c := newTestGQLClient(t, http.StatusOK, `{"data":{"repository":{"pullRequest":{"stackEntry":null}}}}`)
	s, err := c.fetchPRStack(context.Background(), "gfazioli", "octoscope", 218)
	if err != nil || s != nil {
		t.Errorf("got %+v, %v; want nil, nil — the shape GitHub gives a PR outside any stack", s, err)
	}
}

// The struct tag cannot interpolate prStackEntriesMax, which the
// renderer's overflow count relies on; this fails if the two drift.
func TestPRStackQueryAsksForTheCap(t *testing.T) {
	var sent string
	c := newTestGQLClientCapturing(t, `{"data":{"repository":{"pullRequest":{"stackEntry":null}}}}`, &sent)
	_, _ = c.fetchPRStack(context.Background(), "o", "r", 1)
	if want := "entries(first: " + strconv.Itoa(prStackEntriesMax) + ")"; !strings.Contains(sent, want) {
		t.Errorf("query lacks %q:\n%s", want, sent)
	}
}

// newPRDetailServer answers FetchPRDetail's three requests: the detail
// query, the stack query (told apart by its stackEntry field) and the
// REST files list.
func newPRDetailServer(t *testing.T, stackStatus int, stackBody string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/graphql" {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "stackEntry") {
			w.WriteHeader(stackStatus)
			_, _ = io.WriteString(w, stackBody)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"repository":{"pullRequest":{"number":343,"title":"Minor docs updates","state":"OPEN","baseRefName":"stack/2","headRefName":"stack/3"}}}}`)
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}
}

func TestFetchPRDetailCarriesTheStack(t *testing.T) {
	t.Run("the stack rides along", func(t *testing.T) {
		c := newPRDetailServer(t, http.StatusOK, prStackBody)
		d, err := c.FetchPRDetail(context.Background(), "github", "gh-stack", 343)
		if err != nil {
			t.Fatalf("FetchPRDetail: %v", err)
		}
		if d.Stack == nil || d.Stack.Position != 3 || d.Title != "Minor docs updates" {
			t.Errorf("stack = %+v, title %q", d.Stack, d.Title)
		}
	})
	// The point of asking separately: stacked pull requests are in
	// preview, and a field GitHub renames must cost the section, not
	// the drill-in.
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"a schema that no longer has the field": {http.StatusOK, `{"errors":[{"message":"Field 'stackEntry' doesn't exist on type 'PullRequest'"}]}`},
		"a 502 on the stack query alone":        {http.StatusBadGateway, `502 Bad Gateway`},
	} {
		t.Run(name, func(t *testing.T) {
			c := newPRDetailServer(t, tc.status, tc.body)
			d, err := c.FetchPRDetail(context.Background(), "github", "gh-stack", 343)
			if err != nil {
				t.Fatalf("a failed stack query must not fail the drill-in: %v", err)
			}
			if d.Stack != nil || d.Title != "Minor docs updates" {
				t.Errorf("stack = %+v, title %q; want no stack and the detail intact", d.Stack, d.Title)
			}
		})
	}
}

// A null in the entries list — the schema types its elements as
// nullable — is skipped, not decoded as a layer at position 0.
func TestFetchPRStackSkipsNullEntries(t *testing.T) {
	c := newTestGQLClient(t, http.StatusOK, `{"data":{"repository":{"pullRequest":{"stackEntry":{"position":1,"stack":{
		"size":2,"baseRefName":"main","entries":{"nodes":[null,
			{"position":1,"pullRequest":{"number":7,"title":"t","state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/7"}}]}}}}}}}`)
	s, err := c.fetchPRStack(context.Background(), "o", "r", 7)
	if err != nil || s == nil {
		t.Fatalf("got %+v, %v", s, err)
	}
	if len(s.Entries) != 1 || s.Entries[0].Number != 7 {
		t.Errorf("entries = %+v, want the one real layer", s.Entries)
	}
}

// The stack query asks for the PR the drill-in is about.
func TestFetchPRStackAsksForThisPR(t *testing.T) {
	var sent string
	c := newTestGQLClientCapturing(t, `{"data":{"repository":{"pullRequest":{"stackEntry":null}}}}`, &sent)
	_, _ = c.fetchPRStack(context.Background(), "github", "gh-stack", 343)
	for _, want := range []string{`"owner":"github"`, `"name":"gh-stack"`, `"number":343`} {
		if !strings.Contains(sent, want) {
			t.Errorf("request lacks %s:\n%s", want, sent)
		}
	}
}

// A stack query that never answers must not hold the drill-in past
// its own budget.
func TestFetchPRDetailDoesNotWaitOnASlowStack(t *testing.T) {
	prev := prStackTimeout
	prStackTimeout = 200 * time.Millisecond
	t.Cleanup(func() { prStackTimeout = prev })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/graphql" {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "stackEntry") {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		_, _ = io.WriteString(w, `{"data":{"repository":{"pullRequest":{"number":1,"title":"x","state":"OPEN"}}}}`)
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	c := &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}

	start := time.Now()
	d, err := c.FetchPRDetail(context.Background(), "o", "r", 1)
	if err != nil {
		t.Fatalf("FetchPRDetail: %v", err)
	}
	if elapsed := time.Since(start); elapsed > prStackTimeout+2*time.Second {
		t.Errorf("the drill-in waited %v on the stack, past its %v budget", elapsed, prStackTimeout)
	}
	if d.Stack != nil || d.Title != "x" {
		t.Errorf("stack = %+v, title %q; want no stack and the detail intact", d.Stack, d.Title)
	}
}

func TestStackPlacement(t *testing.T) {
	mk := func(pos, size int, hasStack bool) prStackFields {
		var f prStackFields
		f.StackEntry = &struct {
			Position githubv4.Int
			Stack    *struct{ Size githubv4.Int }
		}{Position: githubv4.Int(pos)}
		if hasStack {
			f.StackEntry.Stack = &struct{ Size githubv4.Int }{Size: githubv4.Int(size)}
		}
		return f
	}
	cases := []struct {
		name      string
		in        prStackFields
		pos, size int
	}{
		{"outside any stack", prStackFields{}, 0, 0},
		{"an entry without its stack", mk(2, 0, false), 0, 0},
		{"a layer", mk(2, 4, true), 2, 4},
		{"position 0 is no place", mk(0, 4, true), 0, 0},
		{"a position the stack cannot hold", mk(5, 2, true), 0, 0},
	}
	for _, tc := range cases {
		if p, s := stackPlacement(tc.in); p != tc.pos || s != tc.size {
			t.Errorf("%s: got %d/%d, want %d/%d", tc.name, p, s, tc.pos, tc.size)
		}
	}
}

// The review-requests list carries the placement the row shows.
func TestFetchReviewRequestsCarriesTheStack(t *testing.T) {
	c := newTestGQLClient(t, http.StatusOK, `{"data":{"search":{"issueCount":2,"nodes":[
		{"__typename":"PullRequest","number":307,"title":"layer","url":"https://github.com/github/gh-stack/pull/307","repository":{"nameWithOwner":"github/gh-stack"},"author":{"login":"a"},"stackEntry":{"position":4,"stack":{"size":5}}},
		{"__typename":"PullRequest","number":9,"title":"alone","url":"https://github.com/o/r/pull/9","repository":{"nameWithOwner":"o/r"},"author":{"login":"b"},"stackEntry":null}]}}}`)
	got, err := c.FetchReviewRequests(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].StackPosition != 4 || got[0].StackSize != 5 || got[1].StackPosition != 0 || got[1].StackSize != 0 {
		t.Errorf("placements = %d/%d and %d/%d, want 4/5 and none", got[0].StackPosition, got[0].StackSize, got[1].StackPosition, got[1].StackSize)
	}
}
