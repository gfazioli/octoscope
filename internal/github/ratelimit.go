package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// RateResource is one row of the REST /rate_limit breakdown — a
// named API budget (core, graphql, search, …) with its window
// counters. Reset is the wall-clock time the window refills.
type RateResource struct {
	Name      string
	Limit     int
	Used      int
	Remaining int
	Reset     time.Time
}

// RateLimits is the full per-resource budget snapshot backing the
// rate-limit detail panel (v0.18.0). Resources are pre-ordered for
// display: the budgets octoscope actually spends (graphql first,
// then core / search) lead, everything else follows alphabetically.
type RateLimits struct {
	Resources []RateResource
	FetchedAt time.Time
}

// restRateEntry mirrors one resource object in the /rate_limit JSON.
type restRateEntry struct {
	Limit     int   `json:"limit"`
	Used      int   `json:"used"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"` // unix seconds
}

// rateLeaders is the display order for the budgets users care about
// in octoscope: graphql is what the dashboard spends, core covers
// the REST drill-ins (PR files) and this very panel's siblings,
// search backs the review-requests query. Everything not listed
// here sorts alphabetically after them.
var rateLeaders = []string{"graphql", "core", "search"}

// FetchRateLimits pulls the per-resource budget breakdown from REST
// `GET /rate_limit`. The endpoint itself is free — calling it does
// not consume any quota — so the panel can refresh liberally.
//
// Works unauthenticated too (it reports the 60/h anonymous core
// budget), which keeps the panel honest in token-less sessions.
func (c *Client) FetchRateLimits(ctx context.Context) (*RateLimits, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/rate_limit", nil)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()

	// 401 and 403 both classify as auth: /rate_limit is exempt from
	// rate limiting, so unlike the PR-files path a 403 here can't be
	// budget exhaustion — it's a rejected / under-scoped credential
	// (or SAML enforcement), and "check your token" is the right
	// user-facing guidance. Body text is Sanitized at this extractor
	// boundary like every other GitHub-sourced string.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		return nil, &FetchError{
			Reason: ReasonAuth,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, Sanitize(strings.TrimSpace(string(body)))),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &FetchError{
			Reason: ReasonServer,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, Sanitize(strings.TrimSpace(string(body)))),
		}
	}

	var payload struct {
		Resources map[string]restRateEntry `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, &FetchError{Reason: ReasonServer, Err: err}
	}
	return &RateLimits{
		Resources: orderRateResources(payload.Resources),
		FetchedAt: time.Now(),
	}, nil
}

// orderRateResources flattens the JSON map into the canonical
// display order: rateLeaders first (in that order, when present),
// then the remaining resources alphabetically. Resource names are
// fixed JSON keys from GitHub, but they pass through Sanitize
// anyway — boundary discipline doesn't make exceptions for fields
// that are "surely" safe today.
func orderRateResources(raw map[string]restRateEntry) []RateResource {
	out := make([]RateResource, 0, len(raw))
	seen := make(map[string]bool, len(rateLeaders))

	add := func(name string, e restRateEntry) {
		out = append(out, RateResource{
			Name:      Sanitize(name),
			Limit:     e.Limit,
			Used:      e.Used,
			Remaining: e.Remaining,
			Reset:     time.Unix(e.Reset, 0),
		})
	}

	for _, name := range rateLeaders {
		if e, ok := raw[name]; ok {
			add(name, e)
			seen[name] = true
		}
	}
	rest := make([]string, 0, len(raw))
	for name := range raw {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name, raw[name])
	}
	return out
}
