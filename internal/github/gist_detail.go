package github

// Gists — the drill-in half. The list tab answers "which gists do I have";
// this answers the question you actually opened one for: *what is in it*.
//
// The content is deliberately NOT part of the list query. `Gist.files`
// exposes a `text` field, so it could be, and the first cut of the tab was
// built on the assumption that it should not — which quietly turned the
// tab into a browser launcher. Both halves of that are right for different
// reasons: 100 gists × 20 files of source on every dashboard refresh is a
// payload and complexity-ceiling problem, and this repository has the 502
// scars to prove it. So the content is fetched **one gist at a time, on
// demand** — the drill-in pattern the rest of the app already follows.

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// GistFileContent is one file with its body.
//
// IsTruncated and IsImage are carried rather than inferred because the UI
// has to *decline* in both cases rather than render nonsense: GitHub cuts
// very large files, and a gist can hold a binary. Neither is common — a
// survey of 47 real files found zero of each — which is exactly why they
// need to arrive as facts instead of being guessed from a size threshold.
type GistFileContent struct {
	Name        string
	Language    string
	Size        int
	Text        string
	IsTruncated bool
	IsImage     bool
}

// GistDetail is one gist with its file bodies.
type GistDetail struct {
	Name        string
	Description string
	URL         string
	IsPublic    bool
	Files       []GistFileContent
}

type gistDetailFields struct {
	Name        githubv4.String
	Description githubv4.String
	URL         githubv4.String `graphql:"url"`
	IsPublic    githubv4.Boolean
	Files       []struct {
		Name        githubv4.String
		Size        githubv4.Int
		Text        githubv4.String
		IsTruncated githubv4.Boolean
		IsImage     githubv4.Boolean
		Language    struct {
			Name githubv4.String
		}
	} `graphql:"files(limit: 20)"`
}

// FetchGistDetail returns one gist with the contents of its files.
//
// `name` is the gist's hash, which is what GitHub calls its name and what
// the list rows carry. Resolved against the same account the dashboard is
// showing, so viewing someone else's profile drills into their gists.
func (c *Client) FetchGistDetail(ctx context.Context, name string) (*GistDetail, error) {
	vars := map[string]interface{}{"name": githubv4.String(name)}

	var f gistDetailFields
	if c.login == "" {
		var q struct {
			Viewer struct {
				Gist gistDetailFields `graphql:"gist(name: $name)"`
			}
		}
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		f = q.Viewer.Gist
	} else {
		var q struct {
			User struct {
				Gist gistDetailFields `graphql:"gist(name: $name)"`
			} `graphql:"user(login: $login)"`
		}
		vars["login"] = githubv4.String(c.login)
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		f = q.User.Gist
	}

	return extractGistDetail(f), nil
}

// extractGistDetail is the pure half.
//
// The file body crosses Sanitize like every other GitHub-sourced string,
// and this is the case that function was built for: anyone who can share a
// gist link controls its content, and the content is about to be written
// to a terminal. Sanitize keeps `\n` and `\t` — so code stays readable and
// indented — while dropping ANSI escapes and the 8-bit C1 introducers that
// would otherwise move the cursor, repaint the screen, or drive the OSC
// clipboard.
func extractGistDetail(f gistDetailFields) *GistDetail {
	d := &GistDetail{
		Name:        Sanitize(string(f.Name)),
		Description: Sanitize(string(f.Description)),
		URL:         Sanitize(string(f.URL)),
		IsPublic:    bool(f.IsPublic),
	}
	for _, file := range f.Files {
		d.Files = append(d.Files, GistFileContent{
			Name:        Sanitize(string(file.Name)),
			Language:    Sanitize(string(file.Language.Name)),
			Size:        int(file.Size),
			Text:        Sanitize(string(file.Text)),
			IsTruncated: bool(file.IsTruncated),
			IsImage:     bool(file.IsImage),
		})
	}
	return d
}
