package github

import (
	"context"
	"time"

	"github.com/shurcooL/githubv4"
)

// IssueDetail is the rich per-issue payload feeding the Issues
// drill-in view (v0.10.2+). Same architectural posture as
// RepoDetail / PRDetail: single targeted GraphQL query for ONE
// issue, no per-list fan-out.
//
// Issues are simpler than PRs — no diff, no checks, no
// mergeable state, no head/base branches — so the structure
// here is leaner. The shared types from pr_detail.go (TimelineEvent,
// LabelSummary) are reused.
type IssueDetail struct {
	// Identity
	Owner         string
	RepoName      string
	NameWithOwner string
	Number        int
	URL           string

	// Headline
	Title string
	Body  string
	State string // "OPEN" | "CLOSED"

	// Author + lifetime
	AuthorLogin string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// People assigned to the issue (login per node).
	Assignees []string

	// Aggregate comment count (full count from GitHub) plus a
	// preview of the most recent ones. Preview is capped at 3
	// to keep the section compact; users who want the full
	// thread open the issue in the browser.
	CommentsTotal   int
	CommentsPreview []CommentSummary

	// Curated timeline events. Same shape as PRDetail.Timeline
	// (TimelineEvent type defined in pr_detail.go).
	Timeline []TimelineEvent

	Labels []LabelSummary

	// LinkedPRs are the pull requests that, when merged, would
	// close this issue. Mostly the "Closes #123" linkages. Empty
	// when no PR is linked.
	LinkedPRs []LinkedPR
}

// CommentSummary is one entry in the issue's comment preview.
// Body is rendered truncated by the UI; we hand the full string
// over and let the renderer decide how much to show.
type CommentSummary struct {
	AuthorLogin string
	Body        string
	CreatedAt   time.Time
}

// LinkedPR points to a pull request linked (via "Closes #")
// to the issue. Number + title + URL + state — enough for the
// UI to render a clickable-via-`o` row even though the row
// isn't directly clickable in the detail (the linked-PRs
// section is read-only context).
type LinkedPR struct {
	Number int
	Title  string
	URL    string
	State  string // "OPEN" | "CLOSED" | "MERGED"
}

// issueDetailQuery is the GraphQL shape for FetchIssueDetail.
// Same idiom as prDetailQuery; sections we don't need on issues
// (commits, checks, head/base branches) just aren't here.
type issueDetailQuery struct {
	Repository struct {
		Issue struct {
			Number    githubv4.Int
			Title     githubv4.String
			Body      githubv4.String
			URL       githubv4.String `graphql:"url"`
			State     githubv4.IssueState
			CreatedAt githubv4.DateTime
			UpdatedAt githubv4.DateTime

			Author struct {
				Login githubv4.String
			}

			Assignees struct {
				Nodes []struct {
					Login githubv4.String
				}
			} `graphql:"assignees(first: 10)"`

			Comments struct {
				TotalCount githubv4.Int
				Nodes      []struct {
					Author    struct{ Login githubv4.String }
					Body      githubv4.String
					CreatedAt githubv4.DateTime
				}
			} `graphql:"comments(last: 3)"`

			TimelineItems struct {
				Nodes []struct {
					// __typename is the canonical discriminator
					// for the node's actual type. The "is field
					// X populated?" check we used through v0.10.2
					// development was unreliable: shurcooL/githubv4
					// resolves shared field names (CreatedAt, Actor)
					// across inline fragments such that a node of
					// one type can leave non-zero values in another
					// fragment's struct. Switching on Typename is
					// the only correct discriminator.
					Typename githubv4.String `graphql:"__typename"`
					Comment struct {
						Author    struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on IssueComment"`
					Assigned struct {
						Actor    struct{ Login githubv4.String }
						Assignee struct {
							User struct{ Login githubv4.String } `graphql:"... on User"`
						}
						CreatedAt githubv4.DateTime
					} `graphql:"... on AssignedEvent"`
					Labeled struct {
						Actor     struct{ Login githubv4.String }
						Label     struct{ Name githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on LabeledEvent"`
					Closed struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on ClosedEvent"`
					Reopened struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on ReopenedEvent"`
					CrossReferenced struct {
						Actor     struct{ Login githubv4.String }
						Source    struct {
							PullRequest struct {
								Number githubv4.Int
								Title  githubv4.String
							} `graphql:"... on PullRequest"`
							Issue struct {
								Number githubv4.Int
								Title  githubv4.String
							} `graphql:"... on Issue"`
						}
						CreatedAt githubv4.DateTime
					} `graphql:"... on CrossReferencedEvent"`
				}
			} `graphql:"timelineItems(last: 10, itemTypes: [ISSUE_COMMENT, ASSIGNED_EVENT, LABELED_EVENT, CLOSED_EVENT, REOPENED_EVENT, CROSS_REFERENCED_EVENT])"`

			Labels struct {
				Nodes []struct {
					Name  githubv4.String
					Color githubv4.String
				}
			} `graphql:"labels(first: 20)"`

			ClosedByPullRequestsReferences struct {
				Nodes []struct {
					Number githubv4.Int
					Title  githubv4.String
					URL    githubv4.String `graphql:"url"`
					State  githubv4.PullRequestState
				}
			} `graphql:"closedByPullRequestsReferences(first: 5, includeClosedPrs: true)"`
		} `graphql:"issue(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchIssueDetail runs a single targeted query for one issue
// and returns the rich detail payload. Mirrors FetchPRDetail.
func (c *Client) FetchIssueDetail(ctx context.Context, owner, name string, number int) (*IssueDetail, error) {
	var q issueDetailQuery
	variables := map[string]interface{}{
		"owner":  githubv4.String(owner),
		"name":   githubv4.String(name),
		"number": githubv4.Int(number),
	}
	if err := c.gql.Query(ctx, &q, variables); err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	return extractIssueDetail(owner, name, q), nil
}

// extractIssueDetail flattens the GraphQL response into the
// UI-facing IssueDetail. Pure function.
func extractIssueDetail(owner, name string, q issueDetailQuery) *IssueDetail {
	is := q.Repository.Issue
	d := &IssueDetail{
		Owner:         owner,
		RepoName:      name,
		NameWithOwner: owner + "/" + name,
		Number:        int(is.Number),
		URL:           string(is.URL),
		Title:         string(is.Title),
		Body:          string(is.Body),
		State:         string(is.State),
		AuthorLogin:   string(is.Author.Login),
		CreatedAt:     is.CreatedAt.Time,
		UpdatedAt:     is.UpdatedAt.Time,
		CommentsTotal: int(is.Comments.TotalCount),
	}

	for _, n := range is.Assignees.Nodes {
		d.Assignees = append(d.Assignees, string(n.Login))
	}

	for _, n := range is.Comments.Nodes {
		d.CommentsPreview = append(d.CommentsPreview, CommentSummary{
			AuthorLogin: string(n.Author.Login),
			Body:        string(n.Body),
			CreatedAt:   n.CreatedAt.Time,
		})
	}

	for _, n := range is.TimelineItems.Nodes {
		switch string(n.Typename) {
		case "IssueComment":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "comment",
				Actor:  string(n.Comment.Author.Login),
				Detail: "commented",
				At:     n.Comment.CreatedAt.Time,
			})
		case "AssignedEvent":
			actor := string(n.Assigned.Actor.Login)
			assignee := string(n.Assigned.Assignee.User.Login)
			detail := "assigned"
			if assignee != "" {
				detail = "assigned " + assignee
			}
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "assigned",
				Actor:  actor,
				Detail: detail,
				At:     n.Assigned.CreatedAt.Time,
			})
		case "LabeledEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "labeled",
				Actor:  string(n.Labeled.Actor.Login),
				Detail: "added label " + string(n.Labeled.Label.Name),
				At:     n.Labeled.CreatedAt.Time,
			})
		case "ClosedEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "closed",
				Actor:  string(n.Closed.Actor.Login),
				Detail: "closed",
				At:     n.Closed.CreatedAt.Time,
			})
		case "ReopenedEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "reopened",
				Actor:  string(n.Reopened.Actor.Login),
				Detail: "reopened",
				At:     n.Reopened.CreatedAt.Time,
			})
		case "CrossReferencedEvent":
			detail := "referenced"
			switch {
			case int(n.CrossReferenced.Source.PullRequest.Number) > 0:
				detail = "referenced in PR"
			case int(n.CrossReferenced.Source.Issue.Number) > 0:
				detail = "referenced in issue"
			}
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "ref",
				Actor:  string(n.CrossReferenced.Actor.Login),
				Detail: detail,
				At:     n.CrossReferenced.CreatedAt.Time,
			})
		}
	}

	for _, n := range is.Labels.Nodes {
		d.Labels = append(d.Labels, LabelSummary{
			Name:  string(n.Name),
			Color: string(n.Color),
		})
	}

	for _, n := range is.ClosedByPullRequestsReferences.Nodes {
		d.LinkedPRs = append(d.LinkedPRs, LinkedPR{
			Number: int(n.Number),
			Title:  string(n.Title),
			URL:    string(n.URL),
			State:  string(n.State),
		})
	}

	return d
}
