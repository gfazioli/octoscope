package github

// Axis 4 — capability escalation, the elevated-scope half.
//
// Self-hosted runners, deploy keys and webhooks describe what a
// compromise of this repository could reach: someone else's hardware,
// a persistent write credential, an off-platform delivery path.
//
// Every probe here needs a token scope a minimal token will not have,
// so all three **fail open**: a 403 is not an error worth interrupting
// anyone for. Failing open is not the same as staying quiet, though.
// Whatever could not be checked is recorded and surfaced, because a
// security report that hides its own blind spots is worse than one that
// admits them (#85, and the same rule the partial-branch disclosure
// follows).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// UncheckedProbe names a capability probe that did not run, and why, so
// the report can say so rather than let a clean verdict read as
// complete.
type UncheckedProbe struct {
	Name   string
	Reason string
}

// capabilityProbes is the gathered result of the elevated-scope half.
type capabilityProbes struct {
	SelfHostedRunners []string // runner labels/names
	WriteDeployKeys   []string // titles of keys that are not read-only
	OffPlatformHooks  []string // hook targets outside github.com
	Unchecked         []UncheckedProbe
}

// restList performs one authenticated GET and decodes into out. It
// reports checked=false with a human reason for the statuses that mean
// "your token cannot see this", rather than returning an error: those
// are expected on a minimal token and must not fail the scan.
func (c *Client) restList(ctx context.Context, path string, out any) (checked bool, reason string) {
	// Ask for the largest page GitHub allows. Without this the default
	// is 30, and a repository's only write-capable deploy key could sit
	// on page two — omitted from the report while the probe still
	// claimed to have checked. More than 100 is declared rather than
	// silently walked: paging is unbounded work inside a scan that is
	// deliberately bounded everywhere else.
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path+sep+"per_page=100", nil)
	if err != nil {
		return false, "request could not be built"
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return false, "the request did not complete"
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusUnauthorized:
		return false, "the token lacks the scope this needs"
	case resp.StatusCode == http.StatusNotFound:
		// GitHub returns 404 rather than 403 for some resources a token
		// may not see, so this is indistinguishable from "none exist".
		// Saying so is more honest than assuming either.
		return false, "not visible to this token"
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return false, fmt.Sprintf("GitHub answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, "the response could not be read"
	}
	if hasNextPage(resp.Header.Get("Link")) {
		return true, "more than 100 entries — only the first page was read"
	}
	return true, ""
}

// hasNextPage reports whether GitHub's Link header advertises another
// page, which is how a truncated list announces itself.
func hasNextPage(link string) bool {
	return strings.Contains(link, `rel="next"`)
}

// fetchCapabilityProbes runs the three elevated-scope probes
// concurrently. It never returns an error: everything that goes wrong
// becomes an entry in Unchecked.
func (c *Client) fetchCapabilityProbes(ctx context.Context, owner, name string) capabilityProbes {
	base := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(name))

	var (
		mu  sync.Mutex
		out capabilityProbes
		wg  sync.WaitGroup
	)
	skip := func(n, why string) {
		mu.Lock()
		out.Unchecked = append(out.Unchecked, UncheckedProbe{Name: n, Reason: why})
		mu.Unlock()
	}

	wg.Add(3)

	go func() {
		defer wg.Done()
		var payload struct {
			Runners []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"runners"`
		}
		ok, why := c.restList(ctx, base+"/actions/runners", &payload)
		if !ok {
			skip("self-hosted runners", why)
			return
		}
		if why != "" {
			// Checked, but not exhaustively — still a coverage gap.
			skip("self-hosted runners", why)
		}
		mu.Lock()
		for _, r := range payload.Runners {
			out.SelfHostedRunners = append(out.SelfHostedRunners, Sanitize(r.Name))
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		var keys []struct {
			Title    string `json:"title"`
			ReadOnly bool   `json:"read_only"`
		}
		ok, why := c.restList(ctx, base+"/keys", &keys)
		if !ok {
			skip("deploy keys", why)
			return
		}
		if why != "" {
			// Checked, but not exhaustively — still a coverage gap.
			skip("deploy keys", why)
		}
		mu.Lock()
		for _, k := range keys {
			if !k.ReadOnly {
				out.WriteDeployKeys = append(out.WriteDeployKeys, Sanitize(k.Title))
			}
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		var hooks []struct {
			Active bool `json:"active"`
			Config struct {
				URL string `json:"url"`
			} `json:"config"`
		}
		ok, why := c.restList(ctx, base+"/hooks", &hooks)
		if !ok {
			skip("webhooks", why)
			return
		}
		if why != "" {
			// Checked, but not exhaustively — still a coverage gap.
			skip("webhooks", why)
		}
		mu.Lock()
		for _, h := range hooks {
			if !h.Active || h.Config.URL == "" {
				continue
			}
			if host := hookHost(h.Config.URL); host != "" && !isGitHubHost(host) {
				out.OffPlatformHooks = append(out.OffPlatformHooks, Sanitize(host))
			}
		}
		mu.Unlock()
	}()

	wg.Wait()
	return out
}

// isGitHubHost matches github.com and its subdomains only. A plain
// HasSuffix check would read "evilgithub.com" as GitHub infrastructure
// and drop it from the report — the classic suffix-without-a-boundary
// bug, and here it hides exactly the exfiltration target that matters.
func isGitHubHost(host string) bool {
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}

// hookHost extracts a webhook target's host. A URL that will not parse
// yields "", which the caller treats as nothing to report rather than
// guessing — the same reasoning as querying URL fields as strings.
func hookHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
