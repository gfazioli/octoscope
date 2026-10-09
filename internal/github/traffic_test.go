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
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/traffic/views":  json200(trafficViewsBody),
		"/traffic/clones": json200(trafficClonesBody),
	})
	got, err := c.FetchTraffic(context.Background(), "gfazioli", "octoscope")
	if err != nil {
		t.Fatalf("FetchTraffic: %v", err)
	}
	if got.Views != 21 || got.ViewsUnique != 7 || got.Clones != 1701 || got.ClonesUnique != 323 {
		t.Errorf("totals = %+v, want GitHub's own 21/7 and 1701/323", got)
	}
	if len(got.DailyViews) != 3 || got.DailyViews[0].Count != 8 || got.DailyViews[2].Count != 10 {
		t.Errorf("daily views = %+v, want three days, oldest first", got.DailyViews)
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
		{"fine-grained token short a permission, whatever the role", refused(403, "Resource not accessible by personal access token"), false, AccessTokenLacks},
		{"404 without the role", refused(404, "Not Found"), false, AccessNotPermitted},
		{"404 with the role", refused(404, "Not Found"), true, AccessTokenLacks},
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

// newDetailWithTrafficServer answers FetchRepoDetail's GraphQL queries
// and the two traffic GETs from one server, as an authenticated client
// whose viewer ID is already cached, so the drill-in runs end to end:
// the detail query with the viewer's role, the star-history walk, and
// the traffic beside them.
func newDetailWithTrafficServer(t *testing.T, permission string, views func(http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/traffic/views"):
			views(w)
		case strings.HasSuffix(r.URL.Path, "/traffic/clones"):
			_, _ = io.WriteString(w, trafficClonesBody)
		default:
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "stargazers(") {
				_, _ = io.WriteString(w, `{"data":{"repository":{"stargazers":{"pageInfo":{"hasNextPage":false},"edges":[]}}}}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"repository":{"name":"octoscope","url":"https://github.com/gfazioli/octoscope","viewerPermission":"`+permission+`","stargazerCount":57}}}`)
		}
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
		d, err := c.FetchRepoDetail(context.Background(), "charmbracelet", "bubbletea")
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
