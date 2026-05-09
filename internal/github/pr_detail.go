package github

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
)

// PRDetail is the rich per-pull-request payload feeding the PRs
// drill-in view (v0.11.0+). Mirrors the shape of RepoDetail —
// single targeted GraphQL query for ONE PR, no per-list fan-out
// — so it sits comfortably under GitHub's gateway complexity
// ceiling (the per-item drill-in pattern, see CLAUDE.md).
//
// Sections that don't apply to the current PR (no reviewers
// requested yet, no checks reported, no timeline events) leave
// the corresponding slices nil or empty; the UI hides those
// sections rather than rendering "(none)" placeholders.
type PRDetail struct {
	// Identity
	Owner          string // "gfazioli"
	RepoName       string // "octoscope"
	NameWithOwner  string // "gfazioli/octoscope"
	Number         int
	URL            string

	// Headline
	Title       string
	Body        string
	State       string // "OPEN" | "CLOSED" | "MERGED"
	IsDraft     bool
	Mergeable   string // "MERGEABLE" | "CONFLICTING" | "UNKNOWN"

	// Branches
	BaseRefName string // target (where the PR merges into)
	HeadRefName string // source (the branch with the changes)

	// Author + lifetime
	AuthorLogin string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Diff size
	Additions    int
	Deletions    int
	ChangedFiles int

	// Reviewers — pending requests + reviews actually submitted.
	// Two separate slices because the data shapes diverge: a
	// requested reviewer has only a name, a submitted review has
	// state + when.
	RequestedReviewers []string         // login or team slug
	Reviews            []ReviewSummary  // most recent state per reviewer

	// Checks (CI / status). Empty when the PR's head commit hasn't
	// triggered any rollup yet. State is the overall rollup
	// ("SUCCESS" / "FAILURE" / "PENDING" / "ERROR" / "EXPECTED").
	ChecksState     string
	ChecksContexts  []CheckSummary

	// Timeline — most recent ~10 events relevant to a PR's life.
	// Events outside the curated set (label changes, milestones,
	// trivia) are filtered out at query time.
	Timeline []TimelineEvent

	// Labels
	Labels []LabelSummary
}

// ReviewSummary is one entry in PRDetail.Reviews — the latest
// review state per reviewer. State follows GitHub's enum:
// APPROVED / CHANGES_REQUESTED / COMMENTED / DISMISSED / PENDING.
type ReviewSummary struct {
	AuthorLogin string
	State       string
	SubmittedAt time.Time
}

// CheckSummary is one CI / status context attached to the PR's
// head commit. CheckRun and StatusContext (the two GraphQL
// types under StatusCheckRollupContext) collapse into the same
// shape here — the UI only cares about "name + outcome".
type CheckSummary struct {
	Name       string
	Conclusion string // "SUCCESS" / "FAILURE" / "NEUTRAL" / "CANCELLED" / "SKIPPED" / "TIMED_OUT" / "ACTION_REQUIRED" / "STALE" / "STARTUP_FAILURE" / ""
	Status     string // "QUEUED" / "IN_PROGRESS" / "COMPLETED" / "WAITING" / "PENDING" / "REQUESTED"
}

// TimelineEvent captures one item from the PR's timeline that we
// chose to surface. Kind is a short discriminator string the UI
// renders directly ("review", "comment", "merged", "ready",
// "closed", "reopened", "commit"); Actor is the login that
// triggered it; Detail is a free-form short summary the UI
// shows next to the event line; At is the timestamp.
type TimelineEvent struct {
	Kind   string
	Actor  string
	Detail string
	At     time.Time
}

// LabelSummary is one label attached to the PR. Color is the
// GitHub-assigned hex without the leading '#' (matches the
// Language.Color shape used elsewhere in this package).
type LabelSummary struct {
	Name  string
	Color string
}

// prDetailQuery is the GraphQL shape for FetchPRDetail. Mirrors
// PRDetail closely; the conversion happens in extractPRDetail.
//
// Single repository → single pullRequest, with all the per-PR
// metadata, reviewers, checks (via the head commit's
// statusCheckRollup), timeline items (curated to the events that
// actually matter on a PR), and labels. The whole thing lives in
// one round-trip — same shape as the Repos drill-in established
// in v0.10.0.
type prDetailQuery struct {
	Repository struct {
		PullRequest struct {
			Number       githubv4.Int
			Title        githubv4.String
			Body         githubv4.String
			URL          githubv4.String `graphql:"url"`
			State        githubv4.PullRequestState
			IsDraft      githubv4.Boolean
			Mergeable    githubv4.MergeableState
			CreatedAt    githubv4.DateTime
			UpdatedAt    githubv4.DateTime
			Additions    githubv4.Int
			Deletions    githubv4.Int
			ChangedFiles githubv4.Int
			BaseRefName  githubv4.String
			HeadRefName  githubv4.String

			Author struct {
				Login githubv4.String
			}

			// reviewRequests: who's been asked but hasn't
			// submitted yet. The requestedReviewer is a union
			// (User | Team | Mannequin) so we narrow with inline
			// fragments and pick whichever resolves.
			ReviewRequests struct {
				Nodes []struct {
					RequestedReviewer struct {
						User struct {
							Login githubv4.String
						} `graphql:"... on User"`
						Team struct {
							Slug githubv4.String
						} `graphql:"... on Team"`
						Mannequin struct {
							Login githubv4.String
						} `graphql:"... on Mannequin"`
					}
				}
			} `graphql:"reviewRequests(first: 10)"`

			// latestReviews: most recent review per reviewer
			// (GitHub already collapses to "latest per author").
			LatestReviews struct {
				Nodes []struct {
					Author struct {
						Login githubv4.String
					}
					State       githubv4.PullRequestReviewState
					SubmittedAt githubv4.DateTime
				}
			} `graphql:"latestReviews(first: 10)"`

			// commits(last: 1).statusCheckRollup feeds the Checks
			// section — the rollup state plus per-context
			// CheckRun / StatusContext entries.
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							State    githubv4.StatusState
							Contexts struct {
								Nodes []struct {
									CheckRun struct {
										Name       githubv4.String
										Conclusion githubv4.CheckConclusionState
										Status     githubv4.CheckStatusState
									} `graphql:"... on CheckRun"`
									StatusContext struct {
										Context githubv4.String
										State   githubv4.StatusState
									} `graphql:"... on StatusContext"`
								}
							} `graphql:"contexts(first: 20)"`
						}
					}
				}
			} `graphql:"commits(last: 1)"`

			// timelineItems: curated set of event types — review,
			// comment, ready-for-review, assigned, merged, closed,
			// reopened, push (PullRequestCommit). Anything else is
			// noise on a personal dashboard. Discriminated by
			// __typename — see the matching comment on
			// issueDetailQuery.TimelineItems for the rationale.
			TimelineItems struct {
				Nodes []struct {
					Typename githubv4.String `graphql:"__typename"`
					Review struct {
						Author      struct{ Login githubv4.String }
						State       githubv4.PullRequestReviewState
						SubmittedAt githubv4.DateTime
					} `graphql:"... on PullRequestReview"`
					Comment struct {
						Author    struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on IssueComment"`
					Assigned struct {
						Actor     struct{ Login githubv4.String }
						Assignee  struct {
							User struct{ Login githubv4.String } `graphql:"... on User"`
						}
						CreatedAt githubv4.DateTime
					} `graphql:"... on AssignedEvent"`
					Merged struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on MergedEvent"`
					ReadyForReview struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on ReadyForReviewEvent"`
					Closed struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on ClosedEvent"`
					Reopened struct {
						Actor     struct{ Login githubv4.String }
						CreatedAt githubv4.DateTime
					} `graphql:"... on ReopenedEvent"`
					Commit struct {
						Commit struct {
							MessageHeadline githubv4.String
							CommittedDate   githubv4.DateTime
							Author          struct {
								User *struct{ Login githubv4.String }
								Name githubv4.String
							}
						}
					} `graphql:"... on PullRequestCommit"`
				}
			} `graphql:"timelineItems(last: 10, itemTypes: [PULL_REQUEST_REVIEW, ISSUE_COMMENT, ASSIGNED_EVENT, MERGED_EVENT, READY_FOR_REVIEW_EVENT, CLOSED_EVENT, REOPENED_EVENT, PULL_REQUEST_COMMIT])"`

			Labels struct {
				Nodes []struct {
					Name  githubv4.String
					Color githubv4.String
				}
			} `graphql:"labels(first: 20)"`
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchPRDetail runs a single targeted query for one pull
// request and returns the rich detail payload. Same shape as
// FetchRepoDetail; FetchError wraps transport / GraphQL errors so
// the UI can render the classified actionable line. Owner/name/
// number are taken from the PR's URL via SplitOwnerNameNumber by
// callers who already have a PullRequest on hand.
func (c *Client) FetchPRDetail(ctx context.Context, owner, name string, number int) (*PRDetail, error) {
	var q prDetailQuery
	variables := map[string]interface{}{
		"owner":  githubv4.String(owner),
		"name":   githubv4.String(name),
		"number": githubv4.Int(number),
	}
	if err := c.gql.Query(ctx, &q, variables); err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	return extractPRDetail(owner, name, q), nil
}

// SplitOwnerNameNumber parses a PR/issue github.com URL into its
// (owner, name, number) tuple. Defensive: returns ("", "", 0) on
// anything that doesn't match the expected
// `https://github.com/<owner>/<name>/(pull|issues)/<number>`
// shape, so callers can short-circuit cleanly.
func SplitOwnerNameNumber(itemURL string) (string, string, int) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(itemURL, prefix) {
		return "", "", 0
	}
	rest := strings.TrimPrefix(itemURL, prefix)
	parts := strings.Split(rest, "/")
	// owner / name / (pull|issues) / number
	if len(parts) < 4 {
		return "", "", 0
	}
	// parts[2] must be the segment that names the resource type.
	// Anything else (e.g. ".../tree/main", ".../blob/...") would
	// have parts[3] match \d+ by coincidence on a numeric branch
	// name and produce a bogus tuple — guard against that.
	if parts[2] != "pull" && parts[2] != "issues" {
		return "", "", 0
	}
	owner := parts[0]
	name := parts[1]
	num := 0
	for _, c := range parts[3] {
		if c < '0' || c > '9' {
			return "", "", 0
		}
		num = num*10 + int(c-'0')
	}
	if num == 0 || owner == "" || name == "" {
		return "", "", 0
	}
	return owner, name, num
}

// extractPRDetail flattens the GraphQL response into the
// UI-facing PRDetail. Pure function; timeline event
// discrimination switches on the per-node `__typename` —
// shurcooL/githubv4 replicates shared field names like
// `CreatedAt` / `Actor` across inline fragments so a
// "is this field non-zero?" heuristic gives false positives.
// See the matching note on prDetailQuery.TimelineItems.
func extractPRDetail(owner, name string, q prDetailQuery) *PRDetail {
	// All GitHub-sourced strings flow through Sanitize at this
	// boundary so the UI layer never sees ANSI escapes or
	// terminal control characters embedded by a malicious user.
	// Enum-typed fields (State, Mergeable, Conclusion, ...) are
	// safe by schema and pass through verbatim.
	pr := q.Repository.PullRequest
	d := &PRDetail{
		Owner:         owner,
		RepoName:      name,
		NameWithOwner: owner + "/" + name,
		Number:        int(pr.Number),
		URL:           Sanitize(string(pr.URL)),
		Title:         Sanitize(string(pr.Title)),
		Body:          Sanitize(string(pr.Body)),
		State:         string(pr.State),
		IsDraft:       bool(pr.IsDraft),
		Mergeable:     string(pr.Mergeable),
		BaseRefName:   Sanitize(string(pr.BaseRefName)),
		HeadRefName:   Sanitize(string(pr.HeadRefName)),
		AuthorLogin:   Sanitize(string(pr.Author.Login)),
		CreatedAt:     pr.CreatedAt.Time,
		UpdatedAt:     pr.UpdatedAt.Time,
		Additions:     int(pr.Additions),
		Deletions:     int(pr.Deletions),
		ChangedFiles:  int(pr.ChangedFiles),
	}

	for _, n := range pr.ReviewRequests.Nodes {
		switch {
		case string(n.RequestedReviewer.User.Login) != "":
			d.RequestedReviewers = append(d.RequestedReviewers, Sanitize(string(n.RequestedReviewer.User.Login)))
		case string(n.RequestedReviewer.Team.Slug) != "":
			d.RequestedReviewers = append(d.RequestedReviewers, "@team:"+Sanitize(string(n.RequestedReviewer.Team.Slug)))
		case string(n.RequestedReviewer.Mannequin.Login) != "":
			d.RequestedReviewers = append(d.RequestedReviewers, Sanitize(string(n.RequestedReviewer.Mannequin.Login)))
		}
	}

	for _, n := range pr.LatestReviews.Nodes {
		d.Reviews = append(d.Reviews, ReviewSummary{
			AuthorLogin: Sanitize(string(n.Author.Login)),
			State:       string(n.State),
			SubmittedAt: n.SubmittedAt.Time,
		})
	}

	if len(pr.Commits.Nodes) > 0 && pr.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
		rollup := pr.Commits.Nodes[0].Commit.StatusCheckRollup
		d.ChecksState = string(rollup.State)
		for _, ctx := range rollup.Contexts.Nodes {
			cs := CheckSummary{}
			switch {
			case string(ctx.CheckRun.Name) != "":
				cs.Name = Sanitize(string(ctx.CheckRun.Name))
				cs.Conclusion = string(ctx.CheckRun.Conclusion)
				cs.Status = string(ctx.CheckRun.Status)
			case string(ctx.StatusContext.Context) != "":
				cs.Name = Sanitize(string(ctx.StatusContext.Context))
				cs.Conclusion = string(ctx.StatusContext.State)
			default:
				continue
			}
			d.ChecksContexts = append(d.ChecksContexts, cs)
		}
	}

	for _, n := range pr.TimelineItems.Nodes {
		switch string(n.Typename) {
		case "PullRequestReview":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "review",
				Actor:  Sanitize(string(n.Review.Author.Login)),
				Detail: string(n.Review.State),
				At:     n.Review.SubmittedAt.Time,
			})
		case "IssueComment":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "comment",
				Actor:  Sanitize(string(n.Comment.Author.Login)),
				Detail: "commented",
				At:     n.Comment.CreatedAt.Time,
			})
		case "AssignedEvent":
			actor := Sanitize(string(n.Assigned.Actor.Login))
			assignee := Sanitize(string(n.Assigned.Assignee.User.Login))
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
		case "MergedEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "merged",
				Actor:  Sanitize(string(n.Merged.Actor.Login)),
				Detail: "merged",
				At:     n.Merged.CreatedAt.Time,
			})
		case "ReadyForReviewEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "ready",
				Actor:  Sanitize(string(n.ReadyForReview.Actor.Login)),
				Detail: "marked ready for review",
				At:     n.ReadyForReview.CreatedAt.Time,
			})
		case "ClosedEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "closed",
				Actor:  Sanitize(string(n.Closed.Actor.Login)),
				Detail: "closed",
				At:     n.Closed.CreatedAt.Time,
			})
		case "ReopenedEvent":
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "reopened",
				Actor:  Sanitize(string(n.Reopened.Actor.Login)),
				Detail: "reopened",
				At:     n.Reopened.CreatedAt.Time,
			})
		case "PullRequestCommit":
			actor := Sanitize(string(n.Commit.Commit.Author.Name))
			if n.Commit.Commit.Author.User != nil && string(n.Commit.Commit.Author.User.Login) != "" {
				actor = Sanitize(string(n.Commit.Commit.Author.User.Login))
			}
			d.Timeline = append(d.Timeline, TimelineEvent{
				Kind:   "commit",
				Actor:  actor,
				Detail: Sanitize(string(n.Commit.Commit.MessageHeadline)),
				At:     n.Commit.Commit.CommittedDate.Time,
			})
		}
	}

	for _, n := range pr.Labels.Nodes {
		d.Labels = append(d.Labels, LabelSummary{
			Name:  Sanitize(string(n.Name)),
			Color: Sanitize(string(n.Color)),
		})
	}

	return d
}
