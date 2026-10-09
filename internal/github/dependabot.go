package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DependabotAlerts is the open Dependabot alerts of one repository
// (#58): how many there are at each severity, and the alerts
// themselves, most severe first. GitHub shows them to the people with
// write access or above, so they live on the drill-in, read on demand
// for the selected repository, never on the list fetch.
type DependabotAlerts struct {
	Critical, High, Medium, Low int
	// Other counts alerts whose severity is none of GitHub's four,
	// rather than filing them under one they were not given.
	Other  int
	Alerts []DependabotAlert
	// Truncated is set when the walk stopped before GitHub said there
	// were no more — the page cap, or a next page it would not follow —
	// so every count is then a floor.
	Truncated bool
}

// Total is every open alert counted.
func (a *DependabotAlerts) Total() int { return a.Critical + a.High + a.Medium + a.Low + a.Other }

// DependabotAlert is one open alert, reduced to what a row shows.
type DependabotAlert struct {
	Number    int
	Severity  string // "critical", "high", "medium", "low", or "other"
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

// severityRank orders the severities, most severe first; "other" last.
var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "other": 4}

// maxAlertsPageBytes bounds one page's body: a hundred alerts measured
// at a few kilobytes each, with room to spare.
const maxAlertsPageBytes = 8 << 20

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
	// SecurityVulnerability is the advisory as it applies to this
	// package, and its severity is the alert's: GitHub keeps the two
	// apart, the advisory's being for the advisory as a whole.
	SecurityVulnerability struct {
		Severity            string `json:"severity"`
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
	seen := map[string]bool{}
	for page := 0; next != ""; page++ {
		if page == maxAlertPages || seen[next] {
			// The cap, or a next page already read: either way there is
			// more than was counted, or a loop, and the counts are floors.
			out.Truncated = true
			break
		}
		seen[next] = true
		batch, link, err := c.getAlertsPage(ctx, next)
		if err != nil {
			return nil, err
		}
		for _, a := range batch {
			sev := strings.ToLower(Sanitize(a.SecurityVulnerability.Severity))
			if sev == "" {
				sev = strings.ToLower(Sanitize(a.SecurityAdvisory.Severity))
			}
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
				// counted, under a name that does not pretend to know.
				sev = "other"
				out.Other++
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
		if next == "" && hasNextRel(link) {
			// GitHub said there is a next page, and it is not one this
			// client will follow: what was read is not the whole list.
			out.Truncated = true
		}
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAlertsPageBytes)).Decode(&batch); err != nil {
		return nil, "", &FetchError{Reason: ReasonServer, Err: fmt.Errorf("reading the Dependabot alerts: %w", err)}
	}
	return batch, resp.Header.Get("Link"), nil
}

// linkNext matches the rel="next" target of an RFC 8288 Link header.
var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextLink returns the next page's URL from a Link header, or "" when
// there is none this client will follow. Only the alerts endpoint on
// GitHub's API host is followed: the header is GitHub-sourced, and the
// client sends the token with every request it makes. GitHub writes the
// next page as /repositories/{id}/dependabot/alerts (measured), so the
// path is checked by its end rather than against the owner/name form
// the first request used.
func nextLink(header string) string {
	m := linkNext.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	u, err := url.Parse(m[1])
	if err != nil || u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil ||
		!strings.HasSuffix(u.Path, "/dependabot/alerts") {
		return ""
	}
	return u.String()
}

// hasNextRel reports whether a Link header names a next page at all,
// whether or not nextLink would follow it.
func hasNextRel(header string) bool {
	return linkNext.MatchString(header)
}
