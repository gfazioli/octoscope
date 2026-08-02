package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestRESTClient points a Client's REST transport at a local server
// that dispatches on request path, so the capability probes can be
// exercised without the network. Reuses rewriteHost, the same harness
// the GraphQL fetch tests use.
func newTestRESTClient(t *testing.T, routes map[string]func(w http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for suffix, h := range routes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				w.Header().Set("Content-Type", "application/json")
				h(w)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"not found"}`)
	}))
	t.Cleanup(srv.Close)
	httpClient := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{rest: httpClient, authenticated: true}
}

func json200(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}
}

func status(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	}
}

// The mixed case a real minimal token produces: some probes answer,
// others are refused. Run under -race this also exercises the three
// concurrent goroutines, which no other test reaches.
func TestFetchCapabilityProbesMixed(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/actions/runners": json200(`{"runners":[{"name":"build-box","status":"online"}]}`),
		"/keys":            status(http.StatusForbidden),
		"/hooks": json200(`[
			{"active":true,"config":{"url":"https://hooks.example.net/x"}},
			{"active":true,"config":{"url":"https://api.github.com/internal"}},
			{"active":false,"config":{"url":"https://disabled.example.org/y"}}
		]`),
	})

	p := c.fetchCapabilityProbes(context.Background(), "o", "r")

	if len(p.SelfHostedRunners) != 1 || p.SelfHostedRunners[0] != "build-box" {
		t.Errorf("runners = %v, want [build-box]", p.SelfHostedRunners)
	}
	// Only the active, off-platform hook counts: github.com is not
	// off-platform, and an inactive hook delivers nothing.
	if len(p.OffPlatformHooks) != 1 || p.OffPlatformHooks[0] != "hooks.example.net" {
		t.Errorf("off-platform hooks = %v, want [hooks.example.net]", p.OffPlatformHooks)
	}
	if len(p.Unchecked) != 1 {
		t.Fatalf("unchecked = %+v, want exactly the refused probe", p.Unchecked)
	}
	if p.Unchecked[0].Name != "deploy keys" || !strings.Contains(p.Unchecked[0].Reason, "scope") {
		t.Errorf("unchecked entry = %+v, want deploy keys / a scope reason", p.Unchecked[0])
	}
}

// The load-bearing property: a token with no extra scopes gets every
// probe refused, and that must be reported rather than raised.
func TestFetchCapabilityProbesFailOpen(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/actions/runners": status(http.StatusForbidden),
		"/keys":            status(http.StatusForbidden),
		"/hooks":           status(http.StatusUnauthorized),
	})

	p := c.fetchCapabilityProbes(context.Background(), "o", "r")
	if len(p.Unchecked) != 3 {
		t.Errorf("unchecked = %d, want all 3 declared: %+v", len(p.Unchecked), p.Unchecked)
	}
	if len(p.SelfHostedRunners)+len(p.WriteDeployKeys)+len(p.OffPlatformHooks) != 0 {
		t.Error("refused probes must contribute no findings")
	}
}

// Malformed JSON must be reported as unreadable rather than decoded
// into a silent zero value that reads as "checked, found nothing".
func TestFetchCapabilityProbesGarbageIsDeclared(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/actions/runners": json200(`{"runners": [ this is not json`),
		"/keys":            json200(`[]`),
		"/hooks":           json200(`[]`),
	})

	p := c.fetchCapabilityProbes(context.Background(), "o", "r")
	if len(p.Unchecked) != 1 || p.Unchecked[0].Name != "self-hosted runners" {
		t.Errorf("unchecked = %+v, want the runners probe declared unreadable", p.Unchecked)
	}
}

// A cancelled context must not turn into a scan failure either.
func TestFetchCapabilityProbesCancelledContext(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/actions/runners": json200(`{"runners":[]}`),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := c.fetchCapabilityProbes(ctx, "o", "r")
	if len(p.Unchecked) != 3 {
		t.Errorf("unchecked = %d, want 3 — a dead context must be declared, not raised: %+v", len(p.Unchecked), p.Unchecked)
	}
}

func TestHookHost(t *testing.T) {
	tests := map[string]string{
		"https://hooks.slack.com/services/x": "hooks.slack.com",
		"https://API.GitHub.com/thing":       "api.github.com",
		"http://192.168.1.5:9000/hook":       "192.168.1.5",
		"":                                   "",
		"not a url at all":                   "",
		"://broken":                          "",
	}
	for in, want := range tests {
		if got := hookHost(in); got != want {
			t.Errorf("hookHost(%q) = %q, want %q", in, got, want)
		}
	}
}
