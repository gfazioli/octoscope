package github

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// reviewRequestsPageSize bounds the inbox returned by
// FetchReviewRequests. 20 is plenty for "PRs waiting on you" —
// past that the user has bigger problems than a sparkline can
// help with, and the PRs tab caps its visible window anyway.
const reviewRequestsPageSize = 20

// reviewRequestsQuery uses GitHub's search interface (the only
// path that exposes the `review-requested:@me` filter — there's
// no equivalent on `viewer.pullRequests`). One search query,
// roughly 1-2 complexity points, well under the gateway budget.
type reviewRequestsQuery struct {
	Search struct {
		IssueCount githubv4.Int
		Nodes      []struct {
			Typename    githubv4.String `graphql:"__typename"`
			PullRequest struct {
				Number     githubv4.Int
				Title      githubv4.String
				URL        githubv4.String `graphql:"url"`
				IsDraft    githubv4.Boolean
				UpdatedAt  githubv4.DateTime
				Repository struct {
					NameWithOwner githubv4.String
					IsPrivate     githubv4.Boolean
				}
				Author struct {
					Login githubv4.String
				}
				Mergeable githubv4.MergeableState
			} `graphql:"... on PullRequest"`
		}
	} `graphql:"search(query: $q, type: ISSUE, first: $first)"`
}

// FetchReviewRequests returns the open pull requests where the
// authenticated viewer has been requested as a reviewer. The
// `review-requested:@me` qualifier is GitHub's idiomatic way to
// express this; combined with `is:open` it surfaces exactly the
// "PRs waiting on you" set.
//
// Returns nil + nil error when the client isn't a candidate for
// this query (unauthenticated, or running with an explicit user
// arg). Callers can treat nil as "feature not applicable here"
// without distinguishing it from "no results today".
func (c *Client) FetchReviewRequests(ctx context.Context) ([]PullRequest, error) {
	if !c.authenticated || c.login != "" {
		return nil, nil
	}
	var q reviewRequestsQuery
	vars := map[string]interface{}{
		"q":     githubv4.String("is:open is:pr review-requested:@me archived:false"),
		"first": githubv4.Int(reviewRequestsPageSize),
	}
	if err := c.gql.Query(ctx, &q, vars); err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	out := make([]PullRequest, 0, len(q.Search.Nodes))
	for _, n := range q.Search.Nodes {
		if string(n.Typename) != "PullRequest" {
			// Search returns SearchResultItem (a union); we asked
			// for type: ISSUE which yields both Issue and
			// PullRequest. Defensive skip just in case GitHub
			// ever broadens the result shape — the inline
			// fragment already filters us to PRs in practice.
			continue
		}
		pr := n.PullRequest
		out = append(out, PullRequest{
			Number:      int(pr.Number),
			Title:       Sanitize(string(pr.Title)),
			URL:         Sanitize(string(pr.URL)),
			Repo:        Sanitize(string(pr.Repository.NameWithOwner)),
			IsDraft:     bool(pr.IsDraft),
			Mergeable:   string(pr.Mergeable),
			UpdatedAt:   pr.UpdatedAt.Time,
			IsPrivate:   bool(pr.Repository.IsPrivate),
			AuthorLogin: Sanitize(string(pr.Author.Login)),
		})
	}
	return out, nil
}
