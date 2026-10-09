package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shurcooL/githubv4"
)

// botTip is a default-branch tip wearing the GitHub Actions identity
// without GitHub's signature, as both a worm's forgery and a workflow
// that pushes with git produce.
func botTip() BranchProvenance {
	p := provBranch("main", true)
	p.Signed = false
	p.Bot = true
	p.AuthorName = "github-actions[bot]"
	return p
}

func spoofFinding(s *RepoScan) (Finding, bool) {
	for _, f := range s.Findings {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "github-actions") {
			return f, true
		}
	}
	return Finding{}, false
}

// #230: the identity alone no longer scores. What the tip changed
// decides, and not knowing what it changed scores.
func TestBotIdentityIsScoredOnWhatTheTipChanged(t *testing.T) {
	anomalousBlob := map[string]blobAnalysis{"h": {Size: 40, Fetched: true, Analysed: true, IsText: true, Markers: []string{"eval() call"}}}
	hook := []ignitionMatch{{Path: ".claude/settings.json", Size: 40, BlobSHA: "h", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}}}

	cases := []struct {
		name       string
		changed    []string
		read       bool
		blobs      map[string]blobAnalysis
		wantWeight int
		wantReason string
	}{
		{"a workflow's data commit", nil, true, nil, 0, "as a workflow that pushes with git does"},
		{"it changed an auto-executing file", []string{".github/setup.js"}, true, nil, wProvSpoofIdentity, "and it changed .github/setup.js"},
		{"what it changed could not be read", nil, false, nil, wProvSpoofIdentity, "what it changed could not be read"},
		{"an anomalous blob on its branch", nil, true, anomalousBlob, wProvSpoofIdentity, "forged as"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := botTip()
			p.TipChanged, p.TipChangesRead = c.changed, c.read
			b := scanBranch{Prov: p}
			if c.blobs != nil {
				b.Matches = hook
			}
			s := evaluateScan(scanInput{Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1, Branches: []scanBranch{b}, Blobs: c.blobs})
			f, ok := spoofFinding(s)
			if !ok {
				t.Fatalf("no provenance finding for the bot tip: %+v", s.Findings)
			}
			if f.Weight != c.wantWeight || !strings.Contains(f.Reason, c.wantReason) {
				t.Errorf("finding = %+v; want weight %d and %q", f, c.wantWeight, c.wantReason)
			}
			if s.Branches[0].Forged != (c.wantWeight > 0) {
				t.Errorf("Forged = %v with weight %d; the table's label must follow the score", s.Branches[0].Forged, f.Weight)
			}
		})
	}

	t.Run("a GitHub-signed bot commit is not this rule's", func(t *testing.T) {
		p := botTip()
		p.Signed, p.SignedByGitHub, p.TipChangesRead = true, true, false
		s := evaluateScan(scanInput{Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1, Branches: []scanBranch{{Prov: p}}})
		if f, ok := spoofFinding(s); ok {
			t.Errorf("a genuine Actions commit produced %+v", f)
		}
	})
}

// The reference case still reaches compromised when the forged tip is
// known to have added the dropper, not only when its changes are
// unknown.
func TestReferenceForgeryStillCompromised(t *testing.T) {
	p := botTip()
	p.TipChanged, p.TipChangesRead = []string{".github/setup.js"}, true
	s := evaluateScan(scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1,
		Branches: []scanBranch{{Prov: p, Matches: []ignitionMatch{
			{Path: ".github/setup.js", Size: 4500000, BlobSHA: "d", Rule: ignitionRule{Class: classDropper, Weight: wIgnitionNamedIOC}},
		}}},
		Blobs: map[string]blobAnalysis{"d": {Size: 4500000}},
	})
	if s.Verdict != VerdictCompromised {
		t.Errorf("verdict = %v (score %d), want likely compromised", s.Verdict, s.Score)
	}
}

// newBotTipServer answers FetchRepoScan for a repository whose default
// tip wears the bot identity unsigned, and answers get-a-commit with
// commit(w). It counts get-a-commit calls.
func newBotTipServer(t *testing.T, bot bool, commit func(w http.ResponseWriter)) (*Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	author := "me"
	if bot {
		author = "github-actions[bot]"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/graphql":
			fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":1,"nodes":[
				{"name":"main","target":{"oid":"c1abcdef","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t1"},"author":{"name":%q},"committer":{"name":%q},"signature":null}}]}}}}`, author, author)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t1"):
			_, _ = io.WriteString(w, `{"tree":[{"path":"Casks/octoscope.rb","type":"blob","size":900,"sha":"b1"}],"truncated":false}`)
		case strings.HasSuffix(r.URL.Path, "/commits/c1abcdef"):
			calls.Add(1)
			commit(w)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}, &calls
}

func TestFetchRepoScanReadsWhatABotTipChanged(t *testing.T) {
	files := func(names ...string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			var parts []string
			for _, n := range names {
				parts = append(parts, fmt.Sprintf(`{"filename":%q,"status":"modified","patch":"@@ -1 +1 @@"}`, n))
			}
			fmt.Fprintf(w, `{"sha":"c1abcdef","files":[%s]}`, strings.Join(parts, ","))
		}
	}
	cases := []struct {
		name       string
		commit     func(http.ResponseWriter)
		wantWeight int
	}{
		{"a cask bump, as homebrew-tap's", files("Casks/octoscope.rb"), 0},
		{"an added dropper", files("Casks/octoscope.rb", ".github/setup.js"), wProvSpoofIdentity},
		{"a hook moved into place", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"files":[{"filename":"notes.txt","previous_filename":".vscode/tasks.json","status":"renamed"}]}`)
		}, wProvSpoofIdentity},
		{"GitHub refused the read", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusInternalServerError)
		}, wProvSpoofIdentity},
		{"a file list GitHub paginates", func(w http.ResponseWriter) {
			w.Header().Set("Link", `<https://api.github.com/repositories/1/commits/c1abcdef?page=2>; rel="next"`)
			files("Casks/octoscope.rb")(w)
		}, wProvSpoofIdentity},
		{"an answer that is not JSON", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `<html>`)
		}, wProvSpoofIdentity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, calls := newBotTipServer(t, true, c.commit)
			s, err := client.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
			if err != nil {
				t.Fatalf("FetchRepoScan: %v (a failed commit read must never fail the scan)", err)
			}
			f, ok := spoofFinding(s)
			if !ok || f.Weight != c.wantWeight {
				t.Errorf("finding = %+v (found %v), want weight %d", f, ok, c.wantWeight)
			}
			if calls.Load() != 1 {
				t.Errorf("get-a-commit called %d times, want once", calls.Load())
			}
		})
	}

	t.Run("a tip that is not the bot's costs no call", func(t *testing.T) {
		client, calls := newBotTipServer(t, false, files())
		if _, err := client.FetchRepoScan(context.Background(), "o", "r", ScanOptions{}); err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if calls.Load() != 0 {
			t.Errorf("get-a-commit called %d times for an ordinary tip", calls.Load())
		}
	})
}

// #232: the content is fetched once per SHA, under the first path that
// reaches it. A workflow sharing its bytes with an earlier match must
// still be parsed, not declared "content not retrieved".
func TestAWorkflowSharingBytesIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/graphql":
			_, _ = io.WriteString(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":1,"nodes":[
				{"name":"main","target":{"oid":"c1","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t1"},"author":{"name":"me"},"committer":{"name":"me"},"signature":null}}]}}}}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t1"):
			// The agent hook comes first in the tree, so it is the path
			// that reaches the shared blob.
			_, _ = io.WriteString(w, `{"tree":[
				{"path":".claude/settings.json","type":"blob","size":40,"sha":"same"},
				{"path":".github/workflows/ci.yml","type":"blob","size":40,"sha":"same"}],"truncated":false}`)
		case strings.HasSuffix(r.URL.Path, "/git/blobs/same"):
			_, _ = io.WriteString(w, "on: push\npermissions: {}\njobs: {}\n")
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	c := &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}
	s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{})
	if err != nil {
		t.Fatalf("FetchRepoScan: %v", err)
	}
	for _, u := range s.Unchecked {
		if u.Name == ".github/workflows/ci.yml" {
			t.Errorf("the workflow was fetched under another path and then declared %q", u.Reason)
		}
	}
}

// The unscored note has to reach the reader: the report renders
// ContextFindings, and a weight-0 provenance entry outside it would be
// recorded and never shown.
func TestTheUnsignedBotNoteIsContext(t *testing.T) {
	p := botTip()
	p.TipChangesRead = true
	s := evaluateScan(scanInput{Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1, Branches: []scanBranch{{Prov: p}}})
	for _, f := range s.ContextFindings() {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "not scored") {
			return
		}
	}
	t.Errorf("context = %+v; want the unsigned-bot note among it", s.ContextFindings())
}
