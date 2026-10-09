package github

import (
	"context"
	"encoding/json"
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

// #230: the identity alone no longer scores. Who last changed the
// auto-executing files decides, and not knowing scores.
func TestBotIdentityIsScoredOnWhoChangedTheFiles(t *testing.T) {
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
		{"a bot last changed an auto-executing file", []string{".github/setup.js"}, true, nil, wProvSpoofIdentity, "unsigned bot commits changed .github/setup.js"},
		{"who changed them could not be read", nil, false, nil, wProvSpoofIdentity, "could not be read"},
		{"an anomalous blob on its branch", nil, true, anomalousBlob, wProvSpoofIdentity, "forged as"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := botTip()
			p.BotChanged, p.BotChangesRead = c.changed, c.read
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
		p.Signed, p.SignedByGitHub, p.BotChangesRead = true, true, false
		s := evaluateScan(scanInput{Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1, Branches: []scanBranch{{Prov: p}}})
		if f, ok := spoofFinding(s); ok {
			t.Errorf("a genuine Actions commit produced %+v", f)
		}
	})
}

// The reference case still reaches compromised when a forged commit is
// known to have delivered the dropper, not only when that is unknown.
func TestReferenceForgeryStillCompromised(t *testing.T) {
	p := botTip()
	p.BotChanged, p.BotChangesRead = []string{".github/setup.js"}, true
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

// lastChange is how the fixture answers "who last changed this path":
// a bot or a person, signed by GitHub or not, or an error.
type lastChange struct {
	bot, byGitHub, fail, empty bool
	// laundered: the latest change is a person's, and the one before it
	// the bot's.
	laundered bool
}

// newBotTipServer answers FetchRepoScan for a repository whose default
// tip is an unsigned commit (the bot's when bot is set) over a tree of
// the given paths, and answers each per-path history query from
// history. It counts the history queries.
func newBotTipServer(t *testing.T, bot bool, tree []string, history map[string]lastChange) (*Client, *atomic.Int32) {
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
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if !strings.Contains(body.Query, "history(") {
				fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":1,"nodes":[
					{"name":"main","target":{"oid":"c1abcdef","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t1"},"author":{"name":%q},"committer":{"name":%q},"signature":null}}]}}}}`, author, author)
				return
			}
			calls.Add(1)
			if body.Variables["oid"] != "c1abcdef" {
				t.Errorf("history asked from %q, not the tip", body.Variables["oid"])
			}
			h := history[fmt.Sprint(body.Variables["path"])]
			switch {
			case h.fail:
				w.WriteHeader(http.StatusBadGateway)
				return
			case h.empty:
				_, _ = io.WriteString(w, `{"data":{"repository":{"object":{"history":{"nodes":[]}}}}}`)
				return
			}
			depth, _ := body.Variables["depth"].(float64)
			who, sig := "maintainer", "null"
			if h.bot {
				who = "github-actions[bot]"
			}
			if h.byGitHub {
				sig = `{"wasSignedByGitHub":true}`
			}
			node := fmt.Sprintf(`{"author":{"name":%q,"user":null},"committer":{"name":%q},"signature":%s}`, who, who, sig)
			if h.laundered && depth >= 2 {
				node += `,{"author":{"name":"github-actions[bot]","user":null},"committer":{"name":"github-actions[bot]"},"signature":null}`
			}
			fmt.Fprintf(w, `{"data":{"repository":{"object":{"history":{"nodes":[%s]}}}}}`, node)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t1"):
			var entries []string
			for i, p := range tree {
				entries = append(entries, fmt.Sprintf(`{"path":%q,"type":"blob","size":40,"sha":"b%d"}`, p, i))
			}
			fmt.Fprintf(w, `{"tree":[%s],"truncated":false}`, strings.Join(entries, ","))
		case strings.Contains(r.URL.Path, "/git/blobs/"):
			_, _ = io.WriteString(w, `{"name":"x","version":"1.0.0"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}, &calls
}

func TestFetchRepoScanAsksWhoChangedTheAutoExecutingFiles(t *testing.T) {
	many := []string{}
	for i := 0; i <= maxBotChangePaths; i++ {
		many = append(many, fmt.Sprintf(".husky/hook%02d", i))
	}
	cases := []struct {
		name       string
		tree       []string
		history    map[string]lastChange
		wantWeight int
		wantCalls  int32
	}{
		{"a cask bump over no auto-executing file, as homebrew-tap's", []string{"Casks/octoscope.rb"}, nil, 0, 0},
		{"a hook a person last changed", []string{"Casks/x.rb", ".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {}}, 0, 1},
		{"a hook GitHub-signed Actions last changed", []string{".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {bot: true, byGitHub: true}}, 0, 1},
		{"an implant one commit beneath a data tip", []string{"Casks/x.rb", ".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {bot: true}}, wProvSpoofIdentity, 1},
		{"a bot's delivery touched since under another name", []string{".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {laundered: true}}, wProvSpoofIdentity, 1},
		{"a package manifest a bot last changed", []string{"package.json"}, map[string]lastChange{"package.json": {bot: true}}, wProvSpoofIdentity, 1},
		{"prompt files and lockfiles are not asked about", []string{"AGENTS.md", "package-lock.json"}, nil, 0, 0},
		{"a history GitHub refused", []string{".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {fail: true}}, wProvSpoofIdentity, 1},
		{"an empty history", []string{".vscode/tasks.json"}, map[string]lastChange{".vscode/tasks.json": {empty: true}}, wProvSpoofIdentity, 1},
		{"more files than the cap", many, nil, wProvSpoofIdentity, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, calls := newBotTipServer(t, true, c.tree, c.history)
			s, err := client.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
			if err != nil {
				t.Fatalf("FetchRepoScan: %v (a failed history read must never fail the scan)", err)
			}
			f, ok := spoofFinding(s)
			if !ok || f.Weight != c.wantWeight {
				t.Errorf("finding = %+v (found %v), want weight %d", f, ok, c.wantWeight)
			}
			if calls.Load() != c.wantCalls {
				t.Errorf("history queries = %d, want %d", calls.Load(), c.wantCalls)
			}
		})
	}

	t.Run("a tip that is not the bot's costs no query", func(t *testing.T) {
		client, calls := newBotTipServer(t, false, []string{".vscode/tasks.json"}, nil)
		if _, err := client.FetchRepoScan(context.Background(), "o", "r", ScanOptions{}); err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if calls.Load() != 0 {
			t.Errorf("history queries = %d for an ordinary tip", calls.Load())
		}
	})
}

// A case variant is the same surface on the file systems macOS and
// Windows default to, so the inventory matches it.
func TestMatchIgnitionIgnoresCase(t *testing.T) {
	for _, p := range []string{".vscode/Tasks.json", ".Claude/Settings.json", ".GITHUB/setup.js", "Package.json"} {
		if _, ok := matchIgnition(p); !ok {
			t.Errorf("%s is not matched", p)
		}
	}
	if _, ok := matchIgnition("src/tasks.json"); ok {
		t.Error("a path outside the catalog matched")
	}
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
	p.BotChangesRead = true
	s := evaluateScan(scanInput{Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1, Branches: []scanBranch{{Prov: p}}})
	for _, f := range s.ContextFindings() {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "not scored") {
			return
		}
	}
	t.Errorf("context = %+v; want the unsigned-bot note among it", s.ContextFindings())
}

// Wearing the bot's name must never make a tip less suspect than
// wearing nobody's: an unsigned bot tip the identity rule leaves as a
// note still meets the unsigned-tip rule, as an unsigned human tip
// would.
func TestAnUnscoredBotTipStillMeetsTheUnsignedTipRule(t *testing.T) {
	p := botTip()
	p.BotChangesRead = true
	side := provBranch("feature", false) // genuinely signed: the repo signs
	hook := []ignitionMatch{{Path: ".claude/settings.json", Size: 40, BlobSHA: "h", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}}}
	s := evaluateScan(scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 2,
		Branches: []scanBranch{{Prov: p, Matches: hook}, {Prov: side}},
		Blobs:    map[string]blobAnalysis{"h": {Size: 40, Fetched: true, Analysed: true, IsText: true}},
	})
	var note, delta bool
	for _, f := range s.Findings {
		if f.Axis != AxisProvenance || f.Branch != "main" {
			continue
		}
		note = note || (f.Weight == 0 && strings.Contains(f.Reason, "not scored"))
		delta = delta || (f.Weight == wProvUnsignedDelta && strings.Contains(f.Reason, "otherwise signs"))
	}
	if !note || !delta {
		t.Errorf("note %v, unsigned-tip finding %v; want both: %+v", note, delta, s.Findings)
	}
}

// A tree GitHub cut short may hide the file that matters, so the
// answer for that branch is incomplete, and incomplete scores.
func TestATruncatedTreeLeavesTheBotTipUnread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/graphql":
			_, _ = io.WriteString(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":1,"nodes":[
				{"name":"main","target":{"oid":"c1abcdef","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t1"},"author":{"name":"github-actions[bot]"},"committer":{"name":"github-actions[bot]"},"signature":null}}]}}}}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t1"):
			_, _ = io.WriteString(w, `{"tree":[{"path":"data.json","type":"blob","size":10,"sha":"b1"}],"truncated":true}`)
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
	if f, ok := spoofFinding(s); !ok || f.Weight != wProvSpoofIdentity {
		t.Errorf("finding = %+v (found %v); want the identity scored over a truncated tree", f, ok)
	}
}

func TestLockfileRankIgnoresCase(t *testing.T) {
	if lockfileRank("Npm-shrinkwrap.json") >= lockfileRank("package-lock.json") {
		t.Error("a case-variant shrinkwrap lost npm's precedence over package-lock.json")
	}
}
