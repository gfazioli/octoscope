package github

import (
	"context"
	"sort"
	"time"

	"github.com/shurcooL/githubv4"
)

// PRStack is the stack a pull request belongs to (#99): GitHub's
// stacked pull requests split one change into ordered layers, each
// targeting the layer below, and this is the map of them — where the
// viewed PR sits, what it is built on, what waits on top of it.
type PRStack struct {
	Position    int    // the viewed PR's layer, 1 = closest to the base branch
	Size        int    // layers in the stack, as GitHub counts them
	BaseRefName string // the branch the whole stack lands on
	Entries     []PRStackEntry
}

// PRStackEntry is one layer of a stack. Number is 0 when GitHub
// returned the layer without its pull request — one the viewer cannot
// see — so the renderer can say so rather than drop the layer and
// renumber the rest.
type PRStackEntry struct {
	Position int
	Number   int
	Title    string
	State    string // "OPEN" | "CLOSED" | "MERGED"
	IsDraft  bool
	URL      string
}

// prStackTimeout bounds the stack query on its own. It runs beside the
// drill-in's mandatory requests and the drill-in waits for all of
// them, so without a budget of its own a slow answer to a decorative
// section would hold the whole view on its loading spinner. Past it,
// the section is simply absent.
var prStackTimeout = 5 * time.Second // a variable for the test that waits it out

// prStackEntriesMax caps the layers fetched. The web's stack map shows
// them all; a stack taller than this is reported as Size layers with
// the first prStackEntriesMax listed, the overflow counted.
const prStackEntriesMax = 20

// prStackQuery reads a pull request's stack on its own. Stacked pull
// requests left public preview on 2026-10-06 (GitHub's changelog,
// "Stacked pull requests generally available"), so the schema is no
// longer the worry it was; the query stays separate and best-effort
// because the section is decorative, and a decorative section must not
// be able to fail the drill-in — nor stall it, which is what
// prStackTimeout is for. Measured 2026-10-09 against github/gh-stack,
// where GitHub stacks its own CLI's pull requests: #343 answers
// position 3 of a 5-layer stack onto main; a pull request outside any
// stack answers stackEntry: null.
type prStackQuery struct {
	Repository struct {
		PullRequest struct {
			StackEntry *struct {
				Position githubv4.Int
				Stack    *struct {
					Size        githubv4.Int
					BaseRefName githubv4.String
					Entries     struct {
						// Pointers: the schema types the list's elements
						// as nullable, and a null decoded into a struct
						// would come out as a layer at position 0.
						Nodes []*struct {
							Position    githubv4.Int
							PullRequest *struct {
								Number  githubv4.Int
								Title   githubv4.String
								State   githubv4.String
								IsDraft githubv4.Boolean
								URL     githubv4.String `graphql:"url"`
							}
						}
					} `graphql:"entries(first: 20)"`
				}
			}
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// fetchPRStack returns the stack the pull request belongs to, or nil
// when it belongs to none.
func (c *Client) fetchPRStack(ctx context.Context, owner, name string, number int) (*PRStack, error) {
	ctx, cancel := context.WithTimeout(ctx, prStackTimeout)
	defer cancel()
	var q prStackQuery
	err := c.gql.Query(ctx, &q, map[string]interface{}{
		"owner":  githubv4.String(owner),
		"name":   githubv4.String(name),
		"number": githubv4.Int(number),
	})
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	return extractPRStack(q), nil
}

// extractPRStack flattens the stack, sanitizing what GitHub's users
// wrote (titles) at the boundary and ordering the layers by position,
// which the API does not promise.
func extractPRStack(q prStackQuery) *PRStack {
	e := q.Repository.PullRequest.StackEntry
	if e == nil || e.Stack == nil {
		return nil
	}
	s := &PRStack{
		Position:    int(e.Position),
		Size:        int(e.Stack.Size),
		BaseRefName: Sanitize(string(e.Stack.BaseRefName)),
	}
	for _, n := range e.Stack.Entries.Nodes {
		if n == nil {
			continue
		}
		entry := PRStackEntry{Position: int(n.Position)}
		if pr := n.PullRequest; pr != nil {
			entry.Number = int(pr.Number)
			entry.Title = Sanitize(string(pr.Title))
			entry.State = Sanitize(string(pr.State))
			entry.IsDraft = bool(pr.IsDraft)
			entry.URL = Sanitize(string(pr.URL))
		}
		s.Entries = append(s.Entries, entry)
	}
	sort.SliceStable(s.Entries, func(i, j int) bool { return s.Entries[i].Position < s.Entries[j].Position })
	return s
}
