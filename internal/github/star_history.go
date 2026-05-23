package github

import (
	"context"
	"time"

	"github.com/shurcooL/githubv4"
)

// starHistoryWindow is how far back FetchStarHistory walks before
// it gives up. 12 months is the "last year of momentum" sparkline
// most users want at a glance; anything older becomes background
// info that the in-browser starchart fallback link handles.
const starHistoryWindow = 365 * 24 * time.Hour

// starHistoryPageSize is the per-page count for the stargazers
// connection. GitHub caps at 100; using the cap minimises round-
// trips for high-velocity repos.
const starHistoryPageSize = 100

// starHistoryMax is the absolute ceiling on entries returned —
// even within the 12-month window. Mostly a safety net for repos
// that pick up several thousand stars per year: past 1000 the
// sparkline is already fully saturated and additional points buy
// nothing visually. Truncation is surfaced via
// RepoDetail.StarHistoryTruncated so the UI can append a "(showing
// most recent N)" note.
const starHistoryMax = 1000

// starHistoryQuery walks stargazers DESC by starredAt — paginated
// — and stops as soon as a page returns a timestamp older than
// the 12-month window. Ordering descending means we never need to
// know the total count upfront: we walk only the prefix we need,
// then bail.
type starHistoryQuery struct {
	Repository struct {
		Stargazers struct {
			PageInfo struct {
				HasNextPage githubv4.Boolean
				EndCursor   githubv4.String
			}
			Edges []struct {
				StarredAt githubv4.DateTime
			}
		} `graphql:"stargazers(first: $first, after: $after, orderBy: {field: STARRED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchStarHistory returns the starredAt timestamps for one repo
// over the last starHistoryWindow (newest first). Truncated is
// true when the function bailed on starHistoryMax before reaching
// either the end of the window or the end of the connection.
//
// Pagination strategy: DESC by starredAt + bail-on-old-page.
// Worst case for a quiet repo: 1 round-trip. Worst case for a
// trending repo: starHistoryMax / starHistoryPageSize = 10
// round-trips, capped.
//
// Errors classify through the shared FetchError so the UI's
// "rate-limited / token rejected / network" banners stay
// consistent with the rest of the package.
func (c *Client) FetchStarHistory(ctx context.Context, owner, name string) ([]time.Time, bool, error) {
	cutoff := time.Now().Add(-starHistoryWindow)
	var (
		stars     []time.Time
		truncated bool
		after     githubv4.String
		first     = githubv4.Int(starHistoryPageSize)
	)
	for {
		var q starHistoryQuery
		vars := map[string]interface{}{
			"owner": githubv4.String(owner),
			"name":  githubv4.String(name),
			"first": first,
		}
		if after != "" {
			vars["after"] = after
		} else {
			// Initial page: GitHub requires a String! (or null)
			// for the $after var. The shurcooL client serialises
			// an empty string fine; explicit nil here would force
			// us to change variable types between iterations.
			vars["after"] = (*githubv4.String)(nil)
		}
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, false, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		page := q.Repository.Stargazers
		if len(page.Edges) == 0 {
			break
		}
		for _, e := range page.Edges {
			t := e.StarredAt.Time
			if t.Before(cutoff) {
				// Page is ordered DESC, so once we cross the
				// cutoff every subsequent star is also out of
				// window — we're done.
				return stars, truncated, nil
			}
			stars = append(stars, t)
			if len(stars) >= starHistoryMax {
				truncated = true
				return stars, truncated, nil
			}
		}
		if !bool(page.PageInfo.HasNextPage) {
			break
		}
		after = page.PageInfo.EndCursor
	}
	return stars, truncated, nil
}
