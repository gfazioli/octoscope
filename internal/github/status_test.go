package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The fixture is the shape www.githubstatus.com actually returned on
// 2026-08-29, trimmed to the rows that matter and with the component
// statuses varied. The placeholder row is real and is kept verbatim: it
// is the trap the issue warned about.
const statusFixture = `{
  "page":{"id":"kctbh9vrtdwd","name":"GitHub"},
  "status":{"indicator":"major","description":"Partial System Outage"},
  "components":[
    {"name":"Git Operations","status":"major_outage"},
    {"name":"Webhooks","status":"operational"},
    {"name":"Visit www.githubstatus.com for more information","status":"major_outage"},
    {"name":"API Requests","status":"degraded_performance"},
    {"name":"Issues","status":"operational"},
    {"name":"Pull Requests","status":"partial_outage"},
    {"name":"Actions","status":"under_maintenance"},
    {"name":"Packages","status":"major_outage"},
    {"name":"Pages","status":"operational"},
    {"name":"Copilot","status":"major_outage"},
    {"name":"Codespaces","status":"operational"},
    {"name":"Copilot AI Model Providers","status":"major_outage"}
  ],
  "incidents":[
    {"name":"Incident with Actions and Pull Requests","status":"investigating",
     "impact":"major","shortlink":"https://stspg.io/abc123"}
  ],
  "scheduled_maintenances":[]
}`

func decodeStatus(t *testing.T, body string) *ServiceStatus {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nothing about a third-party status page should ever see the
		// user's PAT. Client.rest shares the oauth2 transport, so this
		// is the guard that a later refactor cannot quietly route this
		// fetch through it.
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("the status fetch sent an Authorization header (%q) to a third party", h)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	got, err := fetchServiceStatus(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchServiceStatus: %v", err)
	}
	return got
}

// The whole design rests on octoscope's own surfaces, not on GitHub's
// global indicator: three of the components down in the fixture --
// Git Operations, Packages, Copilot -- must not produce a word.
func TestServiceStatusReportsOnlyWhatOctoscopeUses(t *testing.T) {
	got := decodeStatus(t, statusFixture)

	var names []string
	for _, c := range got.Affected {
		names = append(names, c.Name)
	}
	joined := strings.Join(names, ",")

	for _, unwanted := range []string{
		"Git Operations", "Packages", "Copilot", "Pages", "Codespaces",
		"Copilot AI Model Providers",
		// The placeholder row is not a service, and it is down in the
		// fixture precisely so that iterating instead of selecting
		// would show up here.
		"Visit www.githubstatus.com for more information",
	} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%q is not something octoscope depends on, but it was reported", unwanted)
		}
	}

	// Operational tracked components stay silent too.
	for _, quiet := range []string{"Issues", "Webhooks"} {
		if strings.Contains(joined, quiet) {
			t.Errorf("%q is operational but was reported as affected", quiet)
		}
	}

	if len(got.Affected) != 3 {
		t.Fatalf("affected = %v, want the 3 impaired tracked components", names)
	}
}

// Worst first, so the headline names the most alarming state.
func TestServiceStatusOrdersBySeverity(t *testing.T) {
	got := decodeStatus(t, statusFixture)
	want := []struct {
		name  string
		state ServiceState
	}{
		{"Pull Requests", ServicePartialOutage},
		{"API Requests", ServiceDegraded},
		{"Actions", ServiceMaintenance},
	}
	if len(got.Affected) != len(want) {
		t.Fatalf("got %d affected, want %d", len(got.Affected), len(want))
	}
	for i, w := range want {
		if got.Affected[i].Name != w.name || got.Affected[i].State != w.state {
			t.Errorf("row %d = %s/%v, want %s/%v",
				i, got.Affected[i].Name, got.Affected[i].State, w.name, w.state)
		}
	}
	if got.Worst() != ServicePartialOutage {
		t.Errorf("Worst() = %v, want ServicePartialOutage", got.Worst())
	}
	if h := got.Headline(); h != "GitHub reports partially down: Pull Requests, API Requests and Actions" {
		t.Errorf("Headline() = %q", h)
	}
}

// Silent while green is the whole contract: a caller renders the
// headline unconditionally and must still show nothing.
func TestServiceStatusSaysNothingWhenHealthy(t *testing.T) {
	const healthy = `{"status":{"indicator":"none","description":"All Systems Operational"},
	  "components":[
	    {"name":"API Requests","status":"operational"},
	    {"name":"Issues","status":"operational"},
	    {"name":"Pull Requests","status":"operational"},
	    {"name":"Actions","status":"operational"},
	    {"name":"Webhooks","status":"operational"},
	    {"name":"Git Operations","status":"major_outage"}],
	  "incidents":[{"name":"Disruption with GitHub Billing","status":"investigating",
	                "impact":"minor","shortlink":"https://stspg.io/x"}]}`
	got := decodeStatus(t, healthy)
	if got.Impaired() {
		t.Errorf("Impaired() with nothing tracked down: %+v", got.Affected)
	}
	if h := got.Headline(); h != "" {
		t.Errorf("Headline() = %q, want empty -- an incident that misses octoscope is not news", h)
	}
	// The incident is still carried; it is context, not a trigger.
	if len(got.Incidents) != 1 {
		t.Errorf("incidents = %d, want 1 kept as context", len(got.Incidents))
	}
}

// A tracked component GitHub renames or withdraws is unknown, and
// unknown stays quiet -- silence is the absence of a claim, where a
// green banner would be a false one.
func TestServiceStatusStaysQuietOnAMissingComponent(t *testing.T) {
	const renamed = `{"status":{"indicator":"none","description":"ok"},
	  "components":[{"name":"REST API Requests","status":"major_outage"}],"incidents":[]}`
	got := decodeStatus(t, renamed)
	if got.Impaired() {
		t.Errorf("a component octoscope cannot find was reported: %+v", got.Affected)
	}
}

// The published vocabulary is already known to be incomplete --
// under_maintenance is live on other Statuspage pages and absent from
// Atlassian's own reference -- so an unrecognised value must not be
// read as healthy.
func TestParseServiceStateFallsTowardsTheWarning(t *testing.T) {
	for in, want := range map[string]ServiceState{
		"operational":          ServiceOperational,
		"under_maintenance":    ServiceMaintenance,
		"degraded_performance": ServiceDegraded,
		"partial_outage":       ServicePartialOutage,
		"major_outage":         ServiceMajorOutage,
		// Not in any published list today:
		"some_future_state": ServiceDegraded,
		"":                  ServiceDegraded,
		"OPERATIONAL":       ServiceDegraded,
	} {
		if got := parseServiceState(in); got != want {
			t.Errorf("parseServiceState(%q) = %v, want %v", in, got, want)
		}
	}
}

// An incident name is third-party text on its way to a terminal.
//
// The payload is built rather than typed: the escape is a real control
// byte here and encoding/json emits it in the only form a JSON string
// may legally carry, so this also proves the round trip through the
// real encoder rather than through a hand-written approximation.
func TestServiceStatusSanitizes(t *testing.T) {
	esc, bel := string(rune(0x1b)), string(rune(0x07))
	hostile := esc + "]0;pwned" + bel

	payload, err := json.Marshal(map[string]any{
		"status": map[string]string{
			"indicator":   "major",
			"description": "real" + hostile + "desc",
		},
		"components": []map[string]string{
			{"name": "API Requests", "status": "major_outage"},
		},
		"incidents": []map[string]string{{
			"name": "real" + hostile + "name", "status": "investigating",
			"impact": "major", "shortlink": "https://stspg.io/x",
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := decodeStatus(t, string(payload))
	for _, s := range []string{got.Description, got.Incidents[0].Name, got.Headline()} {
		if strings.ContainsAny(s, esc+bel) || strings.Contains(s, "pwned") {
			t.Errorf("a terminal escape survived: %q", s)
		}
	}
}

// Best-effort by contract: an unreachable or broken status page is an
// error the caller ignores, never a green all-clear.
func TestServiceStatusFailsRatherThanClaimingHealth(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"5xx", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }},
		{"404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }},
		{"garbage", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>nope")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.h)
			defer srv.Close()
			got, err := fetchServiceStatus(context.Background(), srv.Client(), srv.URL)
			if err == nil {
				t.Fatalf("want an error, got %+v", got)
			}
			if got != nil {
				t.Errorf("a failed fetch returned a status: %+v", got)
			}
		})
	}
}

// The status page is somebody else's infrastructure and the dashboard
// must never wait on it.
func TestServiceStatusHonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := fetchServiceStatus(ctx, srv.Client(), srv.URL); err == nil {
		t.Fatal("want a cancellation error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v -- the fetch did not honour the context", d)
	}
}

func TestHumanJoin(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"A"}, "A"},
		{[]string{"A", "B"}, "A and B"},
		{[]string{"A", "B", "C"}, "A, B and C"},
	} {
		if got := humanJoin(tc.in); got != tc.want {
			t.Errorf("humanJoin(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A nil status is what every caller holds before the first fetch and
// after a failed one, so the accessors have to be safe on it.
func TestNilServiceStatusIsSafe(t *testing.T) {
	var s *ServiceStatus
	if s.Impaired() || s.Worst() != ServiceOperational || s.Headline() != "" {
		t.Error("a nil ServiceStatus is not inert")
	}
}
