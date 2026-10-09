package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DependabotAlerts is the open Dependabot alerts of one repository
// (#58): how many there are at each severity, and the alerts
// themselves, most severe first. GitHub shows them to the people who
// administer the repository, so they live on the drill-in, read on
// demand for the selected repository, never on the list fetch.
type DependabotAlerts struct {
	Critical, High, Medium, Low int
	Alerts                      []DependabotAlert
	// Truncated is set when the walk stopped at maxAlertPages before
	// GitHub said there were no more: the counts are then a floor.
	Truncated bool
}

// Total is every open alert counted.
func (a *DependabotAlerts) Total() int { return a.Critical + a.High + a.Medium + a.Low }

// DependabotAlert is one open alert, reduced to what a row shows.
type DependabotAlert struct {
	Number    int
	Severity  string // "critical", "high", "medium" or "low"
	Package   string
	Ecosystem string
	Summary   string
	URL       string // the alert on github.com
	// FixedIn is the first patched version, or "" when GitHub knows of
	// none yet — the difference between "bump it" and "wait".
	FixedIn string
}

// alertsPerPage is GitHub's maximum page size for this endpoint, and
// maxAlertPages bounds the walk: a repository with more than 500 open
// alerts is reported as "500+" rather than paid for in full on every
// drill-in.
const (
	alertsPerPage = 100
	maxAlertPages = 5
)

// severityRank orders the severities, most severe first.
var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}

// dependabotAlertJSON is the part of an alert the extractor reads.
type dependabotAlertJSON struct {
	Number     int    `json:"number"`
	HTMLURL    string `json:"html_url"`
	Dependency struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
	} `json:"dependency"`
	SecurityAdvisory struct {
		Summary  string `json:"summary"`
		Severity string `json:"severity"`
	} `json:"security_advisory"`
	SecurityVulnerability struct {
		FirstPatchedVersion *struct {
			Identifier string `json:"identifier"`
		} `json:"first_patched_version"`
	} `json:"security_vulnerability"`
}

// FetchDependabotAlerts reads the open Dependabot alerts of one
// repository, following GitHub's cursor pagination up to
// maxAlertPages. A refusal comes back as a *refusal for ownerAccess to
// judge once the viewer's role is known.
func (c *Client) FetchDependabotAlerts(ctx context.Context, owner, name string) (*DependabotAlerts, error) {
	next := fmt.Sprintf("https://api.github.com/repos/%s/%s/dependabot/alerts?state=open&per_page=%d",
		url.PathEscape(owner), url.PathEscape(name), alertsPerPage)
	out := &DependabotAlerts{}
	for page := 0; next != ""; page++ {
		if page == maxAlertPages {
			out.Truncated = true
			break
		}
		batch, link, err := c.getAlertsPage(ctx, next)
		if err != nil {
			return nil, err
		}
		for _, a := range batch {
			sev := strings.ToLower(Sanitize(a.SecurityAdvisory.Severity))
			switch sev {
			case "critical":
				out.Critical++
			case "high":
				out.High++
			case "medium":
				out.Medium++
			case "low":
				out.Low++
			default:
				// A severity GitHub adds later is still an open alert:
				// count it with the lowest rather than lose it.
				sev = "low"
				out.Low++
			}
			alert := DependabotAlert{
				Number:    a.Number,
				Severity:  sev,
				Package:   Sanitize(a.Dependency.Package.Name),
				Ecosystem: Sanitize(a.Dependency.Package.Ecosystem),
				Summary:   Sanitize(a.SecurityAdvisory.Summary),
				URL:       Sanitize(a.HTMLURL),
			}
			if p := a.SecurityVulnerability.FirstPatchedVersion; p != nil {
				alert.FixedIn = Sanitize(p.Identifier)
			}
			out.Alerts = append(out.Alerts, alert)
		}
		next = nextLink(link)
	}
	sort.SliceStable(out.Alerts, func(i, j int) bool {
		return severityRank[out.Alerts[i].Severity] < severityRank[out.Alerts[j].Severity]
	})
	return out, nil
}

// getAlertsPage reads one page and returns its Link header.
func (c *Client) getAlertsPage(ctx context.Context, pageURL string) ([]dependabotAlertJSON, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", &FetchError{Reason: ReasonUnknown, Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, "", &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", readRefusal(resp)
	}
	var batch []dependabotAlertJSON
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, "", &FetchError{Reason: ReasonServer, Err: fmt.Errorf("reading the Dependabot alerts: %w", err)}
	}
	return batch, resp.Header.Get("Link"), nil
}

// linkNext matches the rel="next" target of an RFC 8288 Link header.
var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextLink returns the next page's URL from a Link header, or "" when
// there is none. Only a URL on GitHub's API host is followed: the
// header is GitHub-sourced, and the client sends the token with every
// request it makes.
func nextLink(header string) string {
	m := linkNext.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	u, err := url.Parse(m[1])
	if err != nil || u.Scheme != "https" || u.Host != "api.github.com" {
		return ""
	}
	return u.String()
}
