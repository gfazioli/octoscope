package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FileChange is one entry from GitHub's
// /repos/{owner}/{name}/pulls/{number}/files endpoint, reshaped for
// the UI. One per file changed in the PR; Patch carries the unified
// diff body chroma will syntax-highlight inside the drill-in viewer.
//
// GitHub's REST schema returns more fields (sha, blob_url,
// raw_url, contents_url, previous_filename for renames, …) — we
// keep only what the renderer actually needs. Anything we end up
// surfacing later (e.g. previous_filename so the renderer can show
// "moved from X" on renames) can be threaded through one field at
// a time without re-shaping the call site.
type FileChange struct {
	Path      string // e.g. "internal/ui/pr_detail.go"
	Status    string // "added" | "modified" | "removed" | "renamed" | "copied" | "changed" | "unchanged"
	Additions int
	Deletions int
	// Patch is the unified-diff body (`@@ ... @@` hunks). May be
	// empty on binary files, on entries GitHub considers too large
	// to inline, or when the file was simply renamed without
	// content changes — Truncated and the Status field together
	// disambiguate which case applies.
	Patch string
	// Truncated is true when we dropped the patch ourselves
	// because it exceeded patchLineCap. The UI surfaces a banner
	// pointing at the GitHub URL instead of rendering the hunks.
	// Different from GitHub's own "patch omitted because file is
	// too large" response (Patch == "" and Status != "removed"),
	// which we treat identically at the render layer but track
	// distinctly for telemetry / future config knobs.
	Truncated bool
}

// patchLineCap is the maximum number of \n in Patch we will keep
// before dropping the body and flagging Truncated. Chosen so that
// scrolling through a worst-case file stays smooth even on
// modest terminals — chroma's diff lexer is fast but the viewport
// has to wrap and paint every line on each scroll tick, and a
// 5000-line patch turns that into a visible stutter. 500 covers
// the long tail of real-world PR files comfortably; anything
// larger usually warrants reading on GitHub anyway.
const patchLineCap = 500

// filesPerPage is the per-request page size for the files
// endpoint. GitHub allows up to 100; using the cap minimises
// pagination round-trips on PRs with many files.
const filesPerPage = 100

// maxFiles is the absolute ceiling on how many files we fetch
// from a single PR, across pagination. Mostly a safety net for
// "this PR touches 800 files because a generated tree got
// committed" — beyond a few hundred the viewer stops being
// useful even with caps. The UI surfaces a "showing first N of M"
// banner when this fires.
const maxFiles = 300

// restFile is the on-the-wire shape of one entry in
// /pulls/{n}/files. Kept private; FileChange is what the rest of
// the codebase sees.
type restFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// FetchPRFiles returns the per-file changeset for one pull
// request, fetched from GitHub's REST API because the GraphQL
// gateway doesn't expose the raw `patch` body. Paginates up to
// maxFiles entries; truncates per-file patches over patchLineCap.
// Every string crossing the boundary passes through Sanitize so
// a hostile path or patch body can't inject terminal control
// sequences once the renderer paints it.
//
// Returns an empty slice (not nil) when the PR has no file
// entries — keeps the loaded-but-empty state distinguishable
// from "not fetched yet" in callers that compare against nil.
func (c *Client) FetchPRFiles(ctx context.Context, owner, name string, number int) ([]FileChange, error) {
	files := make([]FileChange, 0)
	for page := 1; ; page++ {
		url := fmt.Sprintf(
			"https://api.github.com/repos/%s/%s/pulls/%d/files?per_page=%d&page=%d",
			owner, name, number, filesPerPage, page,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.rest.Do(req)
		if err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		page, decodeErr := decodePRFilesPage(resp)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if len(page) == 0 {
			break
		}
		for _, f := range page {
			files = append(files, sanitizeFileChange(f))
			if len(files) >= maxFiles {
				return files, nil
			}
		}
		if len(page) < filesPerPage {
			break
		}
	}
	return files, nil
}

// decodePRFilesPage consumes one page response, classifying HTTP
// errors via the shared FetchError so the UI's classifyErr-driven
// banner messages stay consistent with the GraphQL path. The
// response body is always closed before return.
func decodePRFilesPage(resp *http.Response) ([]restFile, error) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return nil, &FetchError{
			Reason: ReasonRateLimitSecondary,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, strings.TrimSpace(string(body))),
		}
	}
	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		// 403 on GitHub REST is overloaded: primary rate limit
		// exhaustion (X-RateLimit-Remaining: 0) and abuse throttle
		// both surface as 403. Inspecting the rate-limit headers
		// distinguishes them. The header check is intentionally
		// lenient — when in doubt, fall back to the primary
		// reason because that's the one the UI already paints
		// with a reset-time clock.
		reason := ReasonRateLimitPrimary
		if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" && remaining != "0" {
			reason = ReasonRateLimitSecondary
		}
		return nil, &FetchError{
			Reason: reason,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, strings.TrimSpace(string(body))),
		}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		return nil, &FetchError{
			Reason: ReasonAuth,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, strings.TrimSpace(string(body))),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &FetchError{
			Reason: ReasonServer,
			Err:    fmt.Errorf("github rest %s: %s", resp.Status, strings.TrimSpace(string(body))),
		}
	}
	var page []restFile
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, &FetchError{Reason: ReasonServer, Err: err}
	}
	return page, nil
}

// sanitizeFileChange runs every string field through Sanitize and
// enforces patchLineCap. Lives here so the call site in
// FetchPRFiles stays a one-liner and the truncation contract is
// in one place.
func sanitizeFileChange(f restFile) FileChange {
	patch := Sanitize(f.Patch)
	truncated := false
	if strings.Count(patch, "\n") > patchLineCap {
		patch = ""
		truncated = true
	}
	return FileChange{
		Path:      Sanitize(f.Filename),
		Status:    Sanitize(f.Status),
		Additions: f.Additions,
		Deletions: f.Deletions,
		Patch:     patch,
		Truncated: truncated,
	}
}
