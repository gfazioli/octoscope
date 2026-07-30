package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// TestFetchIssueDetail covers the issue drill-in payload through the
// single-response harness: a happy decode, and an auth failure that
// classifies rather than leaking a raw transport error.
func TestFetchIssueDetail(t *testing.T) {
	t.Run("happy path decodes the issue", func(t *testing.T) {
		const body = `{"data":{"repository":{"issue":{
			"number":42,
			"title":"Bug: it breaks",
			"url":"https://github.com/octocat/proj/issues/42",
			"state":"OPEN"
		}}}}`
		c := newTestGQLClient(t, http.StatusOK, body)

		d, err := c.FetchIssueDetail(context.Background(), "octocat", "proj", 42)
		if err != nil {
			t.Fatalf("FetchIssueDetail err = %v", err)
		}
		if d.Number != 42 || d.Title != "Bug: it breaks" || d.State != "OPEN" {
			t.Errorf("decoded = %+v, want #42 OPEN with the title", d)
		}
	})

	t.Run("auth failure is classified", func(t *testing.T) {
		c := newTestGQLClient(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)

		_, err := c.FetchIssueDetail(context.Background(), "octocat", "proj", 42)
		var fe *FetchError
		if !errors.As(err, &fe) {
			t.Fatalf("want a *FetchError, got %v", err)
		}
		if fe.Reason != ReasonAuth {
			t.Errorf("Reason = %v, want ReasonAuth", fe.Reason)
		}
	})
}

// TestFetchPRDetailCancelledSiblingKeepsRealError pins the first-error
// contract: when the GraphQL half fails with a real reason (auth), the
// REST half's cancellation echo (context canceled → ReasonNetwork) must
// not clobber it. The REST handler blocks until the sibling's failure
// cancels the shared context, so the GraphQL error is deterministically
// first.
func TestFetchPRDetailCancelledSiblingKeepsRealError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "graphql") {
			// GraphQL: fail fast with an auth error.
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
			return
		}
		// REST /pulls/{n}/files: never respond on its own — only unblock
		// when the GraphQL failure cancels the shared context. That makes
		// this branch's only possible error a cancellation.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	httpClient := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	c := &Client{
		gql:           githubv4.NewClient(httpClient),
		rest:          httpClient,
		authenticated: true,
	}

	_, err := c.FetchPRDetail(context.Background(), "octocat", "proj", 7)
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("want a *FetchError, got %v", err)
	}
	if fe.Reason == ReasonNetwork {
		t.Errorf("the cancelled REST sibling clobbered the real error (got ReasonNetwork)")
	}
	if fe.Reason != ReasonAuth {
		t.Errorf("Reason = %v, want ReasonAuth (the GraphQL failure)", fe.Reason)
	}
}
