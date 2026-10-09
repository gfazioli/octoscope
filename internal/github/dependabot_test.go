package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// alertJSON builds one alert the way GitHub sends it (2026-10-09),
// reduced to the fields the extractor reads.
func alertJSON(n int, severity, pkg, summary, fixed string) string {
	js := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	fix := "null"
	if fixed != "" {
		fix = `{"identifier":` + js(fixed) + `}`
	}
	return fmt.Sprintf(`{"number":%d,"html_url":"https://github.com/o/r/security/dependabot/%d",
		"dependency":{"package":{"ecosystem":"npm","name":%s}},
		"security_advisory":{"summary":%s,"severity":%s},
		"security_vulnerability":{"first_patched_version":%s}}`, n, n, js(pkg), js(summary), js(severity), fix)
}

func TestFetchDependabotAlerts(t *testing.T) {
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			pages.Add(1)
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state = %q, want open: dismissed and fixed alerts are not the question", got)
			}
			// The next page on GitHub's own host, as GitHub writes it;
			// the test transport sends it back here.
			w.Header().Set("Link", `<https://api.github.com/repositories/1/dependabot/alerts?state=open&per_page=100&after=CURSOR>; rel="next"`)
			_, _ = io.WriteString(w, "["+
				alertJSON(3, "low", "minimist", "Prototype pollution", "1.2.6")+","+
				alertJSON(2, "high", "braces", "Uncontrolled resource consumption \u001b[31min braces", "")+"]")
			return
		}
		pages.Add(1)
		_, _ = io.WriteString(w, "["+
			alertJSON(1, "critical", "lodash", "Command injection", "4.17.21")+","+
			alertJSON(4, "moderate-ish", "left-pad", "A severity GitHub adds later", "")+","+
			alertJSON(5, "medium", "semver", "ReDoS", "7.5.2")+"]")
	}))
	t.Cleanup(srv.Close)
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}

	got, err := c.FetchDependabotAlerts(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("FetchDependabotAlerts: %v", err)
	}
	if pages.Load() != 2 {
		t.Errorf("pages read = %d, want 2 — the Link header's next page must be followed", pages.Load())
	}
	if got.Critical != 1 || got.High != 1 || got.Medium != 1 || got.Low != 1 || got.Other != 1 || got.Total() != 5 {
		t.Errorf("counts = %+v; want 1/1/1/1 and an unknown severity counted as other, not lost and not called low", got)
	}
	order := []string{}
	for _, a := range got.Alerts {
		order = append(order, a.Severity)
	}
	if strings.Join(order, ",") != "critical,high,medium,low,other" {
		t.Errorf("order = %v, want most severe first", order)
	}
	if a := got.Alerts[0]; a.Package != "lodash" || a.FixedIn != "4.17.21" || a.URL != "https://github.com/o/r/security/dependabot/1" || a.Ecosystem != "npm" {
		t.Errorf("first alert = %+v", a)
	}
	if a := got.Alerts[1]; a.FixedIn != "" || strings.Contains(a.Summary, "\x1b") {
		t.Errorf("second alert = %+v; want no fix and a sanitized summary", a)
	}
	if got.Truncated {
		t.Error("a walk that reached the last page is not truncated")
	}
}

func TestFetchDependabotAlertsStopsAtTheCap(t *testing.T) {
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := pages.Add(1)
		w.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/repositories/1/dependabot/alerts?after=P%d>; rel="next"`, n))
		_, _ = io.WriteString(w, "["+alertJSON(int(n), "high", "x", "y", "")+"]")
	}))
	t.Cleanup(srv.Close)
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}

	got, err := c.FetchDependabotAlerts(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("FetchDependabotAlerts: %v", err)
	}
	if int(pages.Load()) != maxAlertPages || !got.Truncated || got.High != maxAlertPages {
		t.Errorf("pages = %d, truncated = %v, high = %d; want %d pages, then a floor flagged as such", pages.Load(), got.Truncated, got.High, maxAlertPages)
	}
}

func TestFetchDependabotAlertsRefused(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){
		"/dependabot/alerts": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Dependabot alerts are disabled for this repository.","status":"403"}`)
		},
	})
	_, err := c.FetchDependabotAlerts(context.Background(), "gfazioli", "octoscope")
	var r *refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want the refusal kept raw", err)
	}
	if got := ownerAccess(err, true); got != AccessDisabled {
		t.Errorf("ownerAccess = %d, want AccessDisabled — alerts switched off are a fact about the repository, not the token", got)
	}
}

func TestNextLink(t *testing.T) {
	const alerts = "https://api.github.com/repositories/1/dependabot/alerts"
	cases := map[string]string{
		`<` + alerts + `?after=X>; rel="next"`:                                        alerts + "?after=X",
		`<` + alerts + `?before=Y>; rel="prev", <` + alerts + `?after=X>; rel="next"`: alerts + "?after=X",
		`<` + alerts + `?before=Y>; rel="prev"`:                                       "",
		``:                                                                            "",
		// The token rides on every request this client makes, so a next
		// page anywhere but GitHub's API host is not followed...
		`<https://evil.example/dependabot/alerts>; rel="next"`:                      "",
		`<http://api.github.com/repositories/1/dependabot/alerts>; rel="next"`:      "",
		`<https://api.github.com.evil/dependabot/alerts>; rel="next"`:               "",
		`<https://x:y@api.github.com/repositories/1/dependabot/alerts>; rel="next"`: "",
		// ...nor another endpoint on it, whose JSON array would be
		// decoded as alerts.
		`<https://api.github.com/user/repos?page=2>; rel="next"`: "",
	}
	for header, want := range cases {
		if got := nextLink(header); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestFetchRepoDetailCarriesAlerts(t *testing.T) {
	twoAlerts := json200("[" + alertJSON(1, "high", "braces", "x", "") + "," + alertJSON(2, "low", "minimist", "y", "1.2.6") + "]")
	notAuthorized := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"You are not authorized to perform this operation."}`)
	}

	t.Run("an administrator gets the alerts", func(t *testing.T) {
		c := newOwnerReadServer(t, "ADMIN", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": twoAlerts})
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("FetchRepoDetail: %v", err)
		}
		if d.AlertsAccess != AccessOK || d.Alerts == nil || d.Alerts.Total() != 2 {
			t.Errorf("access=%d alerts=%+v, want OK and two", d.AlertsAccess, d.Alerts)
		}
	})
	t.Run("a reader is refused quietly", func(t *testing.T) {
		c := newOwnerReadServer(t, "READ", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": notAuthorized})
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("a refused alerts read must not fail the drill-in: %v", err)
		}
		if d.AlertsAccess != AccessNotPermitted || d.Alerts != nil {
			t.Errorf("access=%d, want NotPermitted", d.AlertsAccess)
		}
	})
	// GitHub shows alerts to write access and above, not only to
	// administrators: a writer refused is a token short a permission.
	t.Run("a writer refused is a token short a permission", func(t *testing.T) {
		c := newOwnerReadServer(t, "WRITE", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": notAuthorized})
		d, _ := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if d.AlertsAccess != AccessTokenLacks {
			t.Errorf("access=%d, want TokenLacks", d.AlertsAccess)
		}
	})
	t.Run("an administrator refused is a token short a permission", func(t *testing.T) {
		c := newOwnerReadServer(t, "ADMIN", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": notAuthorized})
		d, _ := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if d.AlertsAccess != AccessTokenLacks {
			t.Errorf("access=%d, want TokenLacks", d.AlertsAccess)
		}
	})
	t.Run("a failure keeps its reason", func(t *testing.T) {
		c := newOwnerReadServer(t, "ADMIN", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": status(http.StatusServiceUnavailable)})
		d, err := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if err != nil {
			t.Fatalf("FetchRepoDetail: %v", err)
		}
		if d.AlertsAccess != AccessFailed || d.AlertsErr == nil || !strings.Contains(d.AlertsErr.Error(), "503") {
			t.Errorf("access=%d err=%v, want Failed with the 503", d.AlertsAccess, d.AlertsErr)
		}
	})
	t.Run("without a token nothing is asked", func(t *testing.T) {
		var asked atomic.Int32
		c := newOwnerReadServer(t, "", map[string]func(http.ResponseWriter){"/repos/gfazioli/octoscope/dependabot/alerts": func(w http.ResponseWriter) { asked.Add(1) }})
		c.authenticated = false
		d, _ := c.FetchRepoDetail(context.Background(), "gfazioli", "octoscope")
		if asked.Load() != 0 || d.AlertsAccess != AccessNotAsked {
			t.Errorf("asked %d times, access=%d", asked.Load(), d.AlertsAccess)
		}
	})
}

// The alert's severity is the package's (security_vulnerability), which
// GitHub keeps apart from the advisory's.
func TestDependabotSeverityIsThePackages(t *testing.T) {
	body := `[{"number":1,"html_url":"https://github.com/o/r/security/dependabot/1",
		"dependency":{"package":{"ecosystem":"npm","name":"p"}},
		"security_advisory":{"summary":"s","severity":"critical"},
		"security_vulnerability":{"severity":"medium","first_patched_version":null}}]`
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){"/dependabot/alerts": json200(body)})
	got, err := c.FetchDependabotAlerts(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("FetchDependabotAlerts: %v", err)
	}
	if got.Medium != 1 || got.Critical != 0 || got.Alerts[0].Severity != "medium" {
		t.Errorf("got %+v; want the package's medium, not the advisory's critical", got)
	}
}

// A next page already read stops the walk, flagged: a cursor GitHub
// repeats must not count the same alerts again.
func TestFetchDependabotAlertsStopsOnARepeatedPage(t *testing.T) {
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Add(1)
		w.Header().Set("Link", `<https://api.github.com/repositories/1/dependabot/alerts?after=SAME>; rel="next"`)
		_, _ = io.WriteString(w, "["+alertJSON(1, "high", "x", "y", "")+"]")
	}))
	t.Cleanup(srv.Close)
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}
	got, err := c.FetchDependabotAlerts(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("FetchDependabotAlerts: %v", err)
	}
	if pages.Load() != 2 || got.High != 2 || !got.Truncated {
		t.Errorf("pages = %d, high = %d, truncated = %v; want the first page and the cursor once, then a stop flagged as incomplete", pages.Load(), got.High, got.Truncated)
	}
}

// A next page this client will not follow still means there is more.
func TestFetchDependabotAlertsAnUnfollowedNextIsIncomplete(t *testing.T) {
	c := newTestRESTClient(t, map[string]func(http.ResponseWriter){"/dependabot/alerts": func(w http.ResponseWriter) {
		w.Header().Set("Link", `<https://elsewhere.example/dependabot/alerts?after=X>; rel="next"`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `[]`)
	}})
	got, err := c.FetchDependabotAlerts(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("FetchDependabotAlerts: %v", err)
	}
	if !got.Truncated || got.Total() != 0 {
		t.Errorf("got %+v; want nothing counted and the list flagged incomplete", got)
	}
}
