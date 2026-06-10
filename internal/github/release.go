package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// latestReleaseURL is the REST endpoint for octoscope's own latest
// published release. It is hard-coded to the canonical repo on purpose:
// the in-app update check asks "is there a newer octoscope?", which is
// always about gfazioli/octoscope regardless of whose profile the user
// is viewing. Pre-releases are excluded by /releases/latest.
const latestReleaseURL = "https://api.github.com/repos/gfazioli/octoscope/releases/latest"

// FetchLatestRelease returns the tag name of octoscope's latest
// published release (e.g. "v0.19.0"), for the in-app update check.
//
// The endpoint is public, so this works unauthenticated too. A repo
// with no releases yet (404) returns ("", nil) rather than an error —
// "no release to compare against" is a normal state, not a failure.
// Mirrors the REST + error-classification shape of FetchRateLimits.
func (c *Client) FetchLatestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return "", &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", &FetchError{
			Reason: ReasonServer,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, Sanitize(strings.TrimSpace(string(body)))),
		}
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", &FetchError{Reason: ReasonServer, Err: err}
	}
	// Sanitize at the extractor boundary like every other GitHub string.
	return Sanitize(strings.TrimSpace(payload.TagName)), nil
}
