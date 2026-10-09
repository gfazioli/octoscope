package github

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// prStackPlacementNode is one pull request as the placements query
// reads it: its URL, to match it to the row the mandatory queries
// built, and its place in a stack.
type prStackPlacementNode struct {
	URL githubv4.String `graphql:"url"`
	prStackFields
}

// prStackPlacementsQuery reads the stack placement of the pull requests
// the viewer's PRs tab lists: their own open ones (the same connection,
// order and size as the profile query's openPRs) and the review
// requests (the same search as FetchReviewRequests). It is the
// dashboard's eighth branch and best-effort; see FetchStats.
type prStackPlacementsQuery struct {
	Viewer struct {
		PullRequests struct {
			Nodes []prStackPlacementNode
		} `graphql:"pullRequests(states: OPEN, first: 50, orderBy: {field: UPDATED_AT, direction: DESC})"`
	}
	Search struct {
		Nodes []struct {
			PullRequest prStackPlacementNode `graphql:"... on PullRequest"`
		}
	} `graphql:"search(query: $q, type: ISSUE, first: $first)"`
}

// prStackPlacementsUserQuery is the same read for another user's
// dashboard (octoscope <username>): their open pull requests only,
// since a review-requests list is the viewer's own.
type prStackPlacementsUserQuery struct {
	User struct {
		PullRequests struct {
			Nodes []prStackPlacementNode
		} `graphql:"pullRequests(states: OPEN, first: 50, orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"user(login: $login)"`
}

// fetchStackPlacements returns the placement of every listed pull
// request that is a layer of a stack, keyed by its URL: position and
// size, as stackPlacement reads them. A pull request outside any stack
// is absent from the map.
func (c *Client) fetchStackPlacements(ctx context.Context) (map[string][2]int, error) {
	var nodes []prStackPlacementNode
	if c.login == "" {
		var q prStackPlacementsQuery
		vars := map[string]interface{}{
			"q":     githubv4.String(reviewRequestsSearch),
			"first": githubv4.Int(reviewRequestsPageSize),
		}
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		nodes = append(nodes, q.Viewer.PullRequests.Nodes...)
		for _, n := range q.Search.Nodes {
			nodes = append(nodes, n.PullRequest)
		}
	} else {
		var q prStackPlacementsUserQuery
		if err := c.gql.Query(ctx, &q, map[string]interface{}{"login": githubv4.String(c.login)}); err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		nodes = q.User.PullRequests.Nodes
	}
	out := map[string][2]int{}
	for _, n := range nodes {
		url := Sanitize(string(n.URL))
		if pos, size := stackPlacement(n.prStackFields); url != "" && size > 0 {
			out[url] = [2]int{pos, size}
		}
	}
	return out, nil
}

// applyStackPlacements marks each listed pull request with its place in
// its stack, matched by URL. A nil map — the branch failed or never ran
// — leaves every row unmarked, which is what a PR outside any stack
// looks like too: the marker is decoration, never a claim that a PR is
// not stacked.
func applyStackPlacements(prs []PullRequest, placements map[string][2]int) {
	for i := range prs {
		if p, ok := placements[prs[i].URL]; ok {
			prs[i].StackPosition, prs[i].StackSize = p[0], p[1]
		}
	}
}
