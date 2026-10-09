package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// Traffic is a repository's views and clones over the window GitHub
// keeps, the last 14 days (#73). Totals are GitHub's own; the daily
// series are what it returned, oldest first. GitHub serves this only
// to people with push access, so it lives on the drill-in, one
// request pair per selected repository, never on the list fetch.
type Traffic struct {
	Views        int
	ViewsUnique  int
	Clones       int
	ClonesUnique int
	DailyViews   []TrafficDay
	DailyClones  []TrafficDay
}

// TrafficDay is one day of a traffic series, at UTC midnight.
type TrafficDay struct {
	Day     time.Time
	Count   int
	Uniques int
}

// trafficJSON is the shared shape of the views and clones payloads:
// the same totals, and a list under a key that differs between them.
type trafficJSON struct {
	Count   int `json:"count"`
	Uniques int `json:"uniques"`
	Views   []struct {
		Timestamp time.Time `json:"timestamp"`
		Count     int       `json:"count"`
		Uniques   int       `json:"uniques"`
	} `json:"views"`
	Clones []struct {
		Timestamp time.Time `json:"timestamp"`
		Count     int       `json:"count"`
		Uniques   int       `json:"uniques"`
	} `json:"clones"`
}

// FetchTraffic reads the views and then the clones of one repository.
// A refusal comes back as a *refusal for ownerAccess to judge once the
// viewer's role is known; the clones are not asked for when the views
// were refused, since the two need the same permission.
func (c *Client) FetchTraffic(ctx context.Context, owner, name string) (*Traffic, error) {
	views, err := c.getTraffic(ctx, owner, name, "views")
	if err != nil {
		return nil, err
	}
	clones, err := c.getTraffic(ctx, owner, name, "clones")
	if err != nil {
		return nil, err
	}
	t := &Traffic{
		Views:        views.Count,
		ViewsUnique:  views.Uniques,
		Clones:       clones.Count,
		ClonesUnique: clones.Uniques,
	}
	for _, v := range views.Views {
		t.DailyViews = append(t.DailyViews, TrafficDay{Day: v.Timestamp.UTC(), Count: v.Count, Uniques: v.Uniques})
	}
	for _, v := range clones.Clones {
		t.DailyClones = append(t.DailyClones, TrafficDay{Day: v.Timestamp.UTC(), Count: v.Count, Uniques: v.Uniques})
	}
	// GitHub returns them oldest first today; the renderer counts on
	// it, so it is made true here rather than assumed.
	sort.SliceStable(t.DailyViews, func(i, j int) bool { return t.DailyViews[i].Day.Before(t.DailyViews[j].Day) })
	sort.SliceStable(t.DailyClones, func(i, j int) bool { return t.DailyClones[i].Day.Before(t.DailyClones[j].Day) })
	return t, nil
}

// getTraffic is one of the two GETs behind FetchTraffic.
func (c *Client) getTraffic(ctx context.Context, owner, name, kind string) (*trafficJSON, error) {
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/traffic/%s",
		url.PathEscape(owner), url.PathEscape(name), kind)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &FetchError{Reason: ReasonUnknown, Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readRefusal(resp)
	}
	var out trafficJSON
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &FetchError{Reason: ReasonServer, Err: fmt.Errorf("reading the %s traffic: %w", kind, err)}
	}
	return &out, nil
}
