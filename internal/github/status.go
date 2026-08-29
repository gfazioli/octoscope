package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// octoscope does nothing but talk to GitHub, so when GitHub itself
// degrades every symptom — a timeout, an empty tab, a 502, a scan that
// will not finish — is indistinguishable from octoscope being broken.
// This file is how the tool says "this is not me".
//
// **It deliberately does not hang off Client.** Client.rest shares the
// oauth2 transport with the GraphQL client, so a status fetch made
// through it would attach the user's PAT to a request aimed at
// www.githubstatus.com — Atlassian Statuspage behind CloudFront, not
// GitHub. A package-level function with its own transport is what keeps
// that from being one careless `c.rest.Do` away.

// statusSummaryURL is Statuspage's combined endpoint, and choosing it
// over status.json is a measurement rather than a preference.
//
// status.json is 215 bytes but carries only the *global* indicator,
// which is the generic "GitHub is having problems" banner this feature
// exists not to be: a Copilot or Codespaces incident moves that
// indicator and changes nothing about a dashboard fetch. Mapping onto
// octoscope's actual surfaces needs per-component state, and that lives
// in components.json (4197 bytes). summary.json is 4310 bytes and
// carries status, all components *and* unresolved incidents together —
// 113 bytes more than components alone, and one request instead of
// three. Measured 2026-08-29.
const statusSummaryURL = "https://www.githubstatus.com/api/v2/summary.json"

// statusTimeout is a backstop, not the operative budget. Callers pass a
// context and that deadline is what normally fires (the UI allows 5s);
// this only exists so a caller that forgets cannot hang forever. It is
// deliberately the *longer* of the two: when the shorter number is the
// client's, the caller's context can never fire and reads as meaningful
// while being decorative. Measured at ~100ms from a warm CDN edge.
const statusTimeout = 15 * time.Second

// statusClient is separate from Client.rest and carries no credentials —
// see the file comment. No redirects to follow, no auth, no retries.
var statusClient = &http.Client{Timeout: statusTimeout}

// ServiceState is a component's health, ordered by how alarming it is.
// The order is load-bearing: Worst picks the maximum.
type ServiceState int

const (
	ServiceOperational ServiceState = iota
	ServiceMaintenance
	ServiceDegraded
	ServicePartialOutage
	ServiceMajorOutage
)

// Label is the user-facing wording, kept here rather than in the
// renderer so it can be asserted without a terminal profile — under
// `go test` lipgloss resolves to Ascii and colour assertions are
// vacuous, so anything worth testing has to be a value first.
func (s ServiceState) Label() string {
	switch s {
	case ServiceMaintenance:
		return "under maintenance"
	case ServiceDegraded:
		return "degraded"
	case ServicePartialOutage:
		return "partially down"
	case ServiceMajorOutage:
		return "down"
	}
	return "operational"
}

// parseServiceState maps a Statuspage component status onto that
// vocabulary.
//
// **Why an unknown value is Degraded and not Operational.** Atlassian's
// own API reference states the set verbatim: "one of operational,
// degraded_performance, partial_outage, or major_outage". That list is
// incomplete — measured 2026-08-29, 29 of Cloudflare's 478 components
// were `under_maintenance`, which the reference does not mention. Since
// the published vocabulary has already been observed missing a value,
// treating an unrecognised one as healthy would report a clean state
// octoscope never verified. That is the same failure the scan's
// Unchecked disclosures exist to avoid, so it falls the other way.
//
// under_maintenance is kept distinct rather than folded into Degraded
// because planned work is not a fault, and a warning that cannot tell
// the two apart is one people learn to dismiss.
func parseServiceState(s string) ServiceState {
	switch s {
	case "operational":
		return ServiceOperational
	case "under_maintenance":
		return ServiceMaintenance
	case "degraded_performance":
		return ServiceDegraded
	case "partial_outage":
		return ServicePartialOutage
	case "major_outage":
		return ServiceMajorOutage
	}
	return ServiceDegraded
}

// trackedComponents maps the components octoscope actually depends on
// onto what a user would notice going missing. Everything else GitHub
// publishes — Git Operations, Packages, Pages, Codespaces, Copilot,
// Copilot AI Model Providers — is absent by design: octoscope never
// clones and never touches them, and firing on those is precisely how a
// warning becomes noise.
//
// Selecting these by name, rather than iterating the published list,
// also disposes of two traps by construction. GitHub's list contains a
// row literally named "Visit www.githubstatus.com for more information"
// (a placeholder, not a service), and Statuspage pages may contain
// *group* rows carrying their own aggregate status — 8 of Cloudflare's
// 478 on 2026-08-29. Neither can match a name in this table.
var trackedComponents = []struct {
	Name    string // exactly as githubstatus.com publishes it
	Affects string // what the user would notice
}{
	{"API Requests", "everything octoscope shows"},
	{"Issues", "the Issues tab"},
	{"Pull Requests", "the PRs tab"},
	{"Actions", "CI status on pull requests"},
	{"Webhooks", "the scan's webhook check"},
}

// ServiceComponent is one tracked component that is not operational.
type ServiceComponent struct {
	Name    string
	Affects string
	State   ServiceState
}

// ServiceIncident is an unresolved incident, carried as context for an
// impairment rather than as a signal of its own.
type ServiceIncident struct {
	Name   string // "Incident with Actions and Pull Requests"
	Status string // investigating | identified | monitoring
	Impact string // none | minor | major | critical
	URL    string // Statuspage shortlink
}

// ServiceStatus is GitHub's own view of GitHub, reduced to the parts
// octoscope depends on.
//
// Affected holds only components that are *not* operational, so "is
// there anything to say" is len(Affected) > 0 and the silent-while-green
// rule is structural rather than remembered at each call site. A tracked
// component missing from the payload (a rename on GitHub's side) also
// yields silence — deliberately: octoscope never renders a green "all
// systems operational", so saying nothing is not a claim that anything
// is fine, it is the absence of one.
type ServiceStatus struct {
	Indicator   string // none | minor | major | critical
	Description string // "All Systems Operational"
	Affected    []ServiceComponent
	Incidents   []ServiceIncident
	FetchedAt   time.Time
}

// Impaired reports whether something octoscope depends on is unhealthy.
func (s *ServiceStatus) Impaired() bool {
	return s != nil && len(s.Affected) > 0
}

// Worst is the most severe state among the affected components.
func (s *ServiceStatus) Worst() ServiceState {
	worst := ServiceOperational
	if s == nil {
		return worst
	}
	for _, c := range s.Affected {
		if c.State > worst {
			worst = c.State
		}
	}
	return worst
}

// Describe names each impaired component in the state it is *actually*
// in, grouping components that share one.
//
// It exists because the obvious shortcut is wrong in the direction this
// whole feature exists to avoid. Naming the worst state and then listing
// every affected component reads as a claim about all of them: with
// Pull Requests partially down, API Requests merely degraded and Actions
// under *planned maintenance*, "GitHub reports partially down: Pull
// Requests, API Requests and Actions" asserts a partial outage for two
// services that do not have one — and calls scheduled maintenance an
// outage. Overstating what was measured is the failure mode, whichever
// direction it points.
//
// Affected is already sorted worst-first, so grouping in order yields
// groups in severity order.
func (s *ServiceStatus) Describe() string {
	if !s.Impaired() {
		return ""
	}
	var groups []string
	for i := 0; i < len(s.Affected); {
		state := s.Affected[i].State
		var names []string
		for i < len(s.Affected) && s.Affected[i].State == state {
			names = append(names, s.Affected[i].Name)
			i++
		}
		groups = append(groups, humanJoin(names)+" "+state.Label())
	}
	return strings.Join(groups, ", ")
}

// Headline is the one-line summary the UI renders, as plain text. Style
// belongs to the renderer; the wording is here so it is testable.
//
// Empty when nothing tracked is impaired — a caller that renders the
// result unconditionally still shows nothing.
func (s *ServiceStatus) Headline() string {
	if !s.Impaired() {
		return ""
	}
	return "GitHub reports " + s.Describe()
}

// humanJoin renders a list the way a sentence would.
func humanJoin(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// The wire shapes, narrowed to the fields used.
type statusSummaryJSON struct {
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Components []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"components"`
	Incidents []struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Impact    string `json:"impact"`
		Shortlink string `json:"shortlink"`
	} `json:"incidents"`
}

// FetchServiceStatus asks GitHub's status page what GitHub thinks of
// GitHub. Best-effort by contract: every caller treats an error as "say
// nothing", never as a failure worth surfacing.
//
// The request carries no credentials and goes to a different host from
// api.github.com, so it costs nothing against the token's rate limit.
func FetchServiceStatus(ctx context.Context) (*ServiceStatus, error) {
	return fetchServiceStatus(ctx, statusClient, statusSummaryURL)
}

func fetchServiceStatus(ctx context.Context, hc *http.Client, url string) (*ServiceStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("githubstatus.com answered %d", resp.StatusCode)
	}

	var raw statusSummaryJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return extractServiceStatus(raw, time.Now()), nil
}

// extractServiceStatus is the pure half, so the mapping is testable
// against recorded payloads without a transport.
//
// Every string crosses Sanitize: an incident name and a status page are
// third-party text on their way to a terminal, and this package cleans
// GitHub-sourced text at exactly this boundary.
func extractServiceStatus(raw statusSummaryJSON, now time.Time) *ServiceStatus {
	byName := make(map[string]string, len(raw.Components))
	for _, c := range raw.Components {
		byName[c.Name] = c.Status
	}

	var affected []ServiceComponent
	for _, t := range trackedComponents {
		s, ok := byName[t.Name]
		if !ok {
			// Renamed or withdrawn: unknown, which stays silent.
			continue
		}
		if st := parseServiceState(s); st != ServiceOperational {
			affected = append(affected, ServiceComponent{
				Name:    t.Name,
				Affects: t.Affects,
				State:   st,
			})
		}
	}
	// Worst first, and stable within a severity so the order follows
	// trackedComponents rather than shuffling between two fetches.
	sort.SliceStable(affected, func(i, j int) bool {
		return affected[i].State > affected[j].State
	})

	// Incidents are context, never a signal. They are deliberately not
	// filtered by their component join: measured across 50 real GitHub
	// incidents on 2026-08-29, 15 carried no component join at all, and
	// every component embedded in a resolved one read "operational"
	// because that field reflects the component's state now rather than
	// during the incident. Filtering on it would drop 30% of incidents
	// and mis-read the rest.
	var incidents []ServiceIncident
	for _, i := range raw.Incidents {
		incidents = append(incidents, ServiceIncident{
			Name:   Sanitize(i.Name),
			Status: Sanitize(i.Status),
			Impact: Sanitize(i.Impact),
			URL:    safeIncidentURL(i.Shortlink),
		})
	}

	return &ServiceStatus{
		Indicator:   Sanitize(raw.Status.Indicator),
		Description: Sanitize(raw.Status.Description),
		Affected:    affected,
		Incidents:   incidents,
		FetchedAt:   now,
	}
}

// safeIncidentURL keeps a link only when it is one, dropping it
// otherwise rather than printing whatever arrived.
//
// The shortlink is third-party text on its way to a terminal that will
// happily auto-link it, so a scheme is not a detail: this package
// already prefix-guards every URL it derives from an API payload rather
// than trusting the shape (see notificationURL). Same contract here.
func safeIncidentURL(raw string) string {
	u := Sanitize(strings.TrimSpace(raw))
	if !strings.HasPrefix(u, "https://") || strings.ContainsAny(u, " \t") {
		return ""
	}
	return u
}
