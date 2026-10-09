package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
