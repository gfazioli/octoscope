package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// The views payload as GitHub sends it (2026-10-09), with the days
// shuffled: FetchTraffic promises oldest first and must not lean on
// the order it was handed.
const trafficViewsBody = `{"count":21,"uniques":7,"views":[
	{"timestamp":"2026-10-08T00:00:00Z","count":10,"uniques":4},
	{"timestamp":"2026-09-25T00:00:00Z","count":8,"uniques":2},
	{"timestamp":"2026-10-01T00:00:00Z","count":3,"uniques":1}
]}`

const trafficClonesBody = `{"count":1701,"uniques":323,"clones":[
	{"timestamp":"2026-09-25T00:00:00Z","count":1000,"uniques":200},
	{"timestamp":"2026-10-08T00:00:00Z","count":701,"uniques":123}
]}`

func TestFetchTraffic(t *testing.T) {
	// Exact paths, not suffixes: a URL built from the wrong owner or
	// name must not find an answer here.
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/gfazioli/octoscope/traffic/views":
			_, _ = io.WriteString(w, trafficViewsBody)
		case "/repos/gfazioli/octoscope/traffic/clones":
			_, _ = io.WriteString(w, trafficClonesBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}

	got, err := c.FetchTraffic(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchTraffic: %v (asked %v)", err, asked)
	}
	if strings.Join(asked, " ") != "/repos/gfazioli/octoscope/traffic/views /repos/gfazioli/octoscope/traffic/clones" {
		t.Errorf("asked %v; want exactly the views, then the clones", asked)
	}
	if got.Views != 21 || got.ViewsUnique != 7 || got.Clones != 1701 || got.ClonesUnique != 323 {
		t.Errorf("totals = %+v, want GitHub's own 21/7 and 1701/323", got)
	}
	if len(got.DailyViews) != 3 || got.DailyViews[0].Count != 8 || got.DailyViews[2].Count != 10 {
		t.Errorf("daily views = %+v, want three days, oldest first", got.DailyViews)
	}
	if d := got.DailyViews[0].Day; d.Format(time.RFC3339) != "2026-09-25T00:00:00Z" {
		t.Errorf("first day = %v, want 2026-09-25 at UTC midnight — the day is what the renderer places", d)
	}
	if len(got.DailyClones) != 2 || got.DailyClones[1].Uniques != 123 {
		t.Errorf("daily clones = %+v", got.DailyClones)
	}
}

func TestFetchTrafficRefusedViewsSkipTheClones(t *testing.T) {
	var clones atomic.Int32
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/traffic/views": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Must have push access to repository","status":"403"}`)
		},
		"/traffic/clones": func(w http.ResponseWriter) {
			clones.Add(1)
			w.WriteHeader(http.StatusForbidden)
		},
	})
	_, err := c.FetchTraffic(context.Background(), "charmbracelet", "bubbletea")
	var r *refusal
	if !errors.As(err, &r) || r.status != http.StatusForbidden || r.message != "Must have push access to repository" {
		t.Fatalf("err = %#v, want the 403 kept raw with GitHub's message", err)
	}
	if n := clones.Load(); n != 0 {
		t.Errorf("asked for the clones %d times after the views were refused; the same permission covers both", n)
	}
}

func TestReadRefusal(t *testing.T) {
	answer := func(code int, header map[string]string, body string) *refusal {
		rec := httptest.NewRecorder()
		for k, v := range header {
			rec.Header().Set(k, v)
		}
		rec.WriteHeader(code)
		_, _ = io.WriteString(rec, body)
		return readRefusal(rec.Result())
	}
	if r := answer(403, map[string]string{"X-RateLimit-Remaining": "0"}, `{"message":"API rate limit exceeded"}`); !r.limited {
		t.Error("a 403 with no budget left is a rate limit, not a permission")
	}
	if r := answer(403, map[string]string{"Retry-After": "60"}, `{"message":"You have exceeded a secondary rate limit"}`); !r.limited {
		t.Error("a 403 with Retry-After is a secondary rate limit")
	}
	if r := answer(429, nil, `{}`); !r.limited {
		t.Error("a 429 is always a rate limit")
	}
	if r := answer(403, map[string]string{"X-RateLimit-Remaining": "4000"}, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`); !r.limited {
		t.Error("a secondary limit can come as a 403 with no Retry-After; its message says so")
	}
	if r := answer(403, nil, `{"message":"You have triggered an abuse detection mechanism. Please wait a few minutes before you try again."}`); !r.limited {
		t.Error("the older abuse-detection wording is a rate limit too")
	}
	if r := answer(403, map[string]string{"X-RateLimit-Remaining": "4999"}, `{"message":"Must have push access to repository"}`); r.limited {
		t.Error("a 403 with budget left is not a rate limit")
	}
	if r := answer(403, nil, "{\"message\":\"evil \\u001b[31mred\"}"); strings.Contains(r.message, "\x1b") {
		t.Errorf("message = %q, want GitHub's text sanitized before it can reach the terminal", r.message)
	}
	if r := answer(502, nil, "<html>502 Bad Gateway</html>"); r.message != "" || r.status != 502 {
		t.Errorf("a non-JSON body leaves the message empty: %+v", r)
	}
}

func TestOwnerAccess(t *testing.T) {
	refused := func(status int, msg string) error { return &refusal{status: status, message: msg} }
	cases := []struct {
		name string
		err  error
		role bool
		want Access
	}{
		{"data", nil, false, AccessOK},
		{"push access refused, no role: GitHub working as designed", refused(403, "Must have push access to repository"), false, AccessNotPermitted},
		{"same refusal with the role: it can only be the token", refused(403, "Must have push access to repository"), true, AccessTokenLacks},
		{"fine-grained token short a permission, with the role", refused(403, "Resource not accessible by personal access token"), true, AccessTokenLacks},
		// A reader whose fine-grained token was refused is still a
		// reader: nothing may tell them they can push.
		{"fine-grained refusal for a reader stays silent", refused(403, "Resource not accessible by personal access token"), false, AccessNotPermitted},
		{"404 without the role", refused(404, "Not Found"), false, AccessNotPermitted},
		{"a 502 for a reader is still nothing to show", refused(502, ""), false, AccessNotPermitted},
		{"a network failure for a reader too", &FetchError{Reason: ReasonNetwork, Err: errors.New("i/o timeout")}, false, AccessNotPermitted},
		{"a refusal that only resembles the wording is not the token", refused(403, "Not authorized by organization policy"), true, AccessFailed},
		// With the role, a refusal that does not name access is not
		// blamed on the token: GitHub's message is the better guide.
		{"404 with the role fails with its reason", refused(404, "Not Found"), true, AccessFailed},
		{"SAML enforcement is not a missing permission", refused(403, "Resource protected by organization SAML enforcement. You must grant your Personal Access token access to this organization."), true, AccessFailed},
		{"an IP allow list is not a missing permission", refused(403, "Although you appear to have the correct authorization credentials, the `acme` organization has an IP allow list enabled"), true, AccessFailed},
		{"rate limit, even as a 403", &refusal{status: 403, limited: true}, true, AccessFailed},
		{"server error", refused(502, ""), true, AccessFailed},
		{"rejected token", refused(401, "Bad credentials"), true, AccessFailed},
		{"network", &FetchError{Reason: ReasonNetwork, Err: errors.New("i/o timeout")}, true, AccessFailed},
	}
	for _, tc := range cases {
		if got := ownerAccess(tc.err, tc.role); got != tc.want {
			t.Errorf("%s: ownerAccess = %d, want %d", tc.name, got, tc.want)
		}
	}
	for perm, want := range map[string]bool{"ADMIN": true, "MAINTAIN": true, "WRITE": true, "TRIAGE": false, "READ": false, "": false} {
		if canPush(perm) != want {
			t.Errorf("canPush(%q) = %v, want %v", perm, !want, want)
		}
	}
}

// ownerReadPaths are the drill-in's owner-only reads for the served
// repository, with the answer each gets when a test does not set one:
// an empty but valid payload, the shape of a repository with nothing to
// report.
var ownerReadPaths = map[string]string{
	"/repos/gfazioli/octoscope/traffic/views":     `{"count":0,"uniques":0,"views":[]}`,
	"/repos/gfazioli/octoscope/traffic/clones":    trafficClonesBody,
	"/repos/gfazioli/octoscope/dependabot/alerts": `[]`,
}

// newOwnerReadServer answers FetchRepoDetail's GraphQL queries and its
// owner-only REST reads from one server, as an authenticated client
// whose viewer ID is already cached, so the drill-in runs end to end:
// the detail query with the viewer's role, the star-history walk, and
// the owner reads beside them. routes overrides an owner read by its
// exact path; any other request fails the test, since it is one the
// drill-in should not make, or one built for the wrong repository.
func newOwnerReadServer(t *testing.T, permission string, routes map[string]func(http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if h, ok := routes[r.URL.Path]; ok {
			h(w)
			return
		}
		if body, ok := ownerReadPaths[r.URL.Path]; ok {
			_, _ = io.WriteString(w, body)
			return
		}
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "stargazers(") {
			_, _ = io.WriteString(w, `{"data":{"repository":{"stargazers":{"pageInfo":{"hasNextPage":false},"edges":[]}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"repository":{"name":"octoscope","url":"https://github.com/gfazioli/octoscope","viewerPermission":"`+permission+`","stargazerCount":57}}}`)
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{
		gql:             githubv4.NewClient(hc),
		rest:            hc,
		authenticated:   true,
		viewerID:        "U_test",
		viewerIDFetched: true,
	}
}

// newDetailWithTrafficServer is newOwnerReadServer with the views
// answered as given.
func newDetailWithTrafficServer(t *testing.T, permission string, views func(http.ResponseWriter)) *Client {
	t.Helper()
	return newOwnerReadServer(t, permission, map[string]func(http.ResponseWriter){
		"/repos/gfazioli/octoscope/traffic/views": views,
	})
}

func TestFetchRepoDetailCarriesTraffic(t *testing.T) {
	t.Run("an owner gets the numbers", func(t *testing.T) {
		c := newDetailWithTrafficServer(t, "ADMIN", json200(trafficViewsBody))
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("FetchRepoDetail: %v", err)
		}
		if d.ViewerPermission != "ADMIN" || d.TrafficAccess != AccessOK || d.Traffic == nil || d.Traffic.Clones != 1701 {
			t.Errorf("perm=%q access=%d traffic=%+v, want ADMIN, OK and the payload", d.ViewerPermission, d.TrafficAccess, d.Traffic)
		}
	})

	refusedPush := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Must have push access to repository"}`)
	}
	t.Run("a reader is refused, quietly", func(t *testing.T) {
		c := newDetailWithTrafficServer(t, "READ", refusedPush)
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("a refused traffic read must not fail the drill-in: %v", err)
		}
		if d.TrafficAccess != AccessNotPermitted || d.Traffic != nil || d.Stars != 57 {
			t.Errorf("access=%d traffic=%+v stars=%d, want NotPermitted, nothing, and the detail intact", d.TrafficAccess, d.Traffic, d.Stars)
		}
	})
	t.Run("a writer refused is a token short a permission", func(t *testing.T) {
		c := newDetailWithTrafficServer(t, "WRITE", refusedPush)
		d, _ := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if d.TrafficAccess != AccessTokenLacks {
			t.Errorf("access=%d, want TokenLacks", d.TrafficAccess)
		}
	})
	t.Run("a failure keeps its reason", func(t *testing.T) {
		c := newDetailWithTrafficServer(t, "ADMIN", status(http.StatusBadGateway))
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("a failed traffic read must not fail the drill-in: %v", err)
		}
		if d.TrafficAccess != AccessFailed || d.TrafficErr == nil || !strings.Contains(d.TrafficErr.Error(), "502") {
			t.Errorf("access=%d err=%v, want Failed with the 502", d.TrafficAccess, d.TrafficErr)
		}
	})
	t.Run("without a token nothing is asked", func(t *testing.T) {
		var asked atomic.Int32
		c := newDetailWithTrafficServer(t, "", func(w http.ResponseWriter) { asked.Add(1) })
		c.authenticated = false
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("FetchRepoDetail: %v", err)
		}
		if asked.Load() != 0 || d.TrafficAccess != AccessNotAsked {
			t.Errorf("asked %d times, access=%d; GitHub answers 401 without a token, so the request is not worth making", asked.Load(), d.TrafficAccess)
		}
	})
}
