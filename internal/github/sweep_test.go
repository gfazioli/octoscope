package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// sweepRepo is one repository of a sweep fixture: how its refs query is
// answered, by name.
type sweepRepo struct {
	defaultBranch string // "" for a repository with no commits
	status        int    // the refs query's HTTP status, 200 when 0
	failFirst     int32  // answer the first n refs queries with a 502
	private       bool
}

// newSweepServer answers FetchRepoScan for several repositories, told
// apart by the refs query's variables, each with one branch whose tree
// holds a README. It counts how many scans run at once.
func newSweepServer(t *testing.T, repos map[string]*sweepRepo) (*Client, *atomic.Int32) {
	t.Helper()
	var (
		inFlight, peak atomic.Int32
		mu             sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/graphql" {
			if strings.Contains(r.URL.Path, "/git/trees/") {
				_, _ = io.WriteString(w, `{"tree":[{"path":"README.md","type":"blob","size":10,"sha":"b1"}],"truncated":false}`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		mu.Lock()
		if n > peak.Load() {
			peak.Store(n)
		}
		mu.Unlock()
		// Long enough for scans to overlap, so the bound is what holds
		// the count down rather than the speed of the fixture.
		time.Sleep(20 * time.Millisecond)
		var body struct {
			Variables map[string]string `json:"variables"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		repo, ok := repos[body.Variables["name"]]
		if !ok {
			t.Errorf("a scan for %q, which is not a target", body.Variables["name"])
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if atomic.AddInt32(&repo.failFirst, -1) >= 0 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "502 Bad Gateway")
			return
		}
		if repo.status != 0 {
			w.WriteHeader(repo.status)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		def, nodes := "null", ""
		if repo.defaultBranch != "" {
			def = fmt.Sprintf(`{"name":%q}`, repo.defaultBranch)
			nodes = fmt.Sprintf(`{"name":%q,"target":{"oid":"c1","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t1"},"author":{"name":"a"},"committer":{"name":"a"}}}`, repo.defaultBranch)
		}
		fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/%s","url":"https://github.com/o/%s","isPrivate":%v,"defaultBranchRef":%s,"refs":{"totalCount":1,"nodes":[%s]}}}}`,
			body.Variables["name"], body.Variables["name"], repo.private, def, nodes)
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}, &peak
}

func TestSweepScan(t *testing.T) {
	prev := sweepBackoff
	sweepBackoff = 0
	t.Cleanup(func() { sweepBackoff = prev })

	c, _ := newSweepServer(t, map[string]*sweepRepo{
		"ok":    {defaultBranch: "main", private: true},
		"empty": {private: true},
		"gone":  {status: http.StatusNotFound},
		"flaky": {defaultBranch: "main", failFirst: 1},
	})
	targets := []SweepTarget{{Owner: "o", Name: "ok"}, {Owner: "o", Name: "empty"}, {Owner: "o", Name: "gone"}, {Owner: "o", Name: "flaky"}}
	got := c.SweepScan(context.Background(), targets, nil)

	if len(got) != 4 {
		t.Fatalf("results = %d, want one per target", len(got))
	}
	for i, r := range got {
		if r.Target != targets[i] {
			t.Errorf("result %d is for %v, want the targets' order", i, r.Target)
		}
		if (r.Scan == nil) == (r.NotScanned == "") {
			t.Errorf("%s: scan %v, not scanned %q; want exactly one", r.Target.Name, r.Scan != nil, r.NotScanned)
		}
	}
	if got[0].Scan == nil || got[0].Scan.Verdict != VerdictClean || !got[0].Scan.IsPrivate {
		t.Errorf("ok: %+v, want a clean scan that knows the repository is private", got[0])
	}
	if got[3].Scan != nil && got[3].Scan.IsPrivate {
		t.Errorf("flaky: a public repository read as private")
	}
	if got[1].NotScanned != "the repository has no commits yet" || !got[1].VisibilityKnown || !got[1].Private {
		t.Errorf("empty: %+v; want not scanned, its visibility kept for --public-only", got[1])
	}
	if got[2].VisibilityKnown {
		t.Errorf("gone: GitHub never answered, yet its visibility reads as known")
	}
	if got[2].Scan != nil || !strings.Contains(got[2].NotScanned, "Not Found") {
		t.Errorf("gone: %+v, want not scanned with GitHub's reason", got[2])
	}
	if got[3].Scan == nil {
		t.Errorf("flaky: %q; a 502 on the first try is retried, as the dashboard does", got[3].NotScanned)
	}
}

// The sweep is bounded: the measured shape is one probe per repository
// at watchedRepoConcurrency, never the whole account at once.
func TestSweepScanIsBounded(t *testing.T) {
	repos := map[string]*sweepRepo{}
	var targets []SweepTarget
	for i := 0; i < 3*watchedRepoConcurrency; i++ {
		name := fmt.Sprintf("r%d", i)
		repos[name] = &sweepRepo{defaultBranch: "main"}
		targets = append(targets, SweepTarget{Owner: "o", Name: name})
	}
	c, peak := newSweepServer(t, repos)
	got := c.SweepScan(context.Background(), targets, nil)
	if len(got) != len(targets) {
		t.Fatalf("results = %d", len(got))
	}
	if p := peak.Load(); p > int32(watchedRepoConcurrency) {
		t.Errorf("%d refs queries at once, past the bound of %d", p, watchedRepoConcurrency)
	}
}

// A sweep whose context is already cancelled starts nothing: every
// repository says it was not reached, and GitHub is asked nothing.
func TestSweepScanStopsWhenCancelled(t *testing.T) {
	repos := map[string]*sweepRepo{}
	var targets []SweepTarget
	for i := 0; i < 3*watchedRepoConcurrency; i++ {
		name := fmt.Sprintf("r%d", i)
		repos[name] = &sweepRepo{defaultBranch: "main"}
		targets = append(targets, SweepTarget{Owner: "o", Name: name})
	}
	c, peak := newSweepServer(t, repos)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range c.SweepScan(ctx, targets, nil) {
		if r.Scan != nil || !strings.Contains(r.NotScanned, "stopped") {
			t.Fatalf("%s: %+v; want it reported as not reached", r.Target.Name, r)
		}
	}
	if p := peak.Load(); p != 0 {
		t.Errorf("a cancelled sweep still queried GitHub (%d at once)", p)
	}
}

// newHookScanServer answers FetchRepoScan for a repository whose
// default branch, main, has an unsigned tip carrying a Claude session
// hook, beside a side branch whose tip carries a genuine author
// signature. The hook's blob answers blobStatus: 200 with a harmless
// body, or an error.
func newHookScanServer(t *testing.T, blobStatus int) *Client {
	return newHookScanServerSigned(t, blobStatus, false)
}

// newHookScanServerSigned is newHookScanServer with the side branch's
// signature made by GitHub when byGitHub is set — a web-UI commit, which
// says nothing about whether the maintainer signs.
func newHookScanServerSigned(t *testing.T, blobStatus int, byGitHub bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/graphql":
			_, _ = io.WriteString(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":2,"nodes":[
				{"name":"feature","target":{"oid":"c2","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t-feature"},"author":{"name":"me"},"committer":{"name":"me"},"signature":{"isValid":true,"state":"VALID","wasSignedByGitHub":`+fmt.Sprint(byGitHub)+`}}},
				{"name":"main","target":{"oid":"c1","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t-main"},"author":{"name":"me"},"committer":{"name":"me"},"signature":null}}]}}}}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t-main"):
			_, _ = io.WriteString(w, `{"tree":[{"path":".claude/settings.json","type":"blob","size":40,"sha":"b-hook"}],"truncated":false}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees/t-feature"):
			_, _ = io.WriteString(w, `{"tree":[{"path":"README.md","type":"blob","size":10,"sha":"b-readme"}],"truncated":false}`)
		case strings.HasSuffix(r.URL.Path, "/git/blobs/b-hook"):
			w.WriteHeader(blobStatus)
			_, _ = io.WriteString(w, `{"hooks":{}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}
}

// The sweep walks the default branch alone, but whether the repository
// signs its commits is a fact about every tip it listed: a signed side
// branch still makes an unsigned default tip with a hook an anomaly,
// as the full scan scores it.
func TestDefaultBranchOnlyKeepsTheSigningContext(t *testing.T) {
	unsignedDelta := func(s *RepoScan) bool {
		for _, f := range s.Findings {
			if f.Axis == AxisProvenance && strings.Contains(f.Reason, "unsigned while the repo otherwise signs") {
				return true
			}
		}
		return false
	}
	for name, opts := range map[string]ScanOptions{
		"full scan":           {},
		"default branch only": {DefaultBranchOnly: true},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := newHookScanServer(t, http.StatusOK).FetchRepoScan(context.Background(), "o", "r", opts)
			if err != nil {
				t.Fatalf("FetchRepoScan: %v", err)
			}
			if !unsignedDelta(s) {
				t.Errorf("score %d, findings %+v; want the unsigned default tip scored against the signed side branch", s.Score, s.Findings)
			}
		})
	}
}

// A side branch GitHub signed is a web-UI commit, not a maintainer who
// signs: it must not make the default tip's missing signature an
// anomaly, in the sweep as in the full scan.
func TestDefaultBranchOnlyIgnoresGitHubsSignature(t *testing.T) {
	s, err := newHookScanServerSigned(t, http.StatusOK, true).FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
	if err != nil {
		t.Fatalf("FetchRepoScan: %v", err)
	}
	for _, f := range s.Findings {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "otherwise signs") {
			t.Errorf("scored against a GitHub-signed side branch: %+v", f)
		}
	}
}

// A hook whose content never arrived is declared, whatever the verdict:
// its obfuscation was not checked, so a clean result is narrower.
func TestAnUnreadHookIsDeclared(t *testing.T) {
	s, err := newHookScanServer(t, http.StatusServiceUnavailable).FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
	if err != nil {
		t.Fatalf("FetchRepoScan: %v", err)
	}
	var got []UncheckedProbe
	for _, u := range s.Unchecked {
		if u.File {
			got = append(got, u)
		}
	}
	if len(got) != 1 || got[0].Name != ".claude/settings.json" || !strings.Contains(got[0].Reason, "not checked for obfuscation") {
		t.Errorf("unread files = %+v; want the hook declared", got)
	}

	read, err := newHookScanServer(t, http.StatusOK).FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
	if err != nil {
		t.Fatalf("FetchRepoScan: %v", err)
	}
	for _, u := range read.Unchecked {
		if u.File {
			t.Errorf("a hook that was read is declared unread: %+v", u)
		}
	}
}

// A hook whose bytes the lockfile pass fetched for its own reading was
// still never analysed by Axis 2, and is declared unread: Fetched is
// not the fact that matters, Analysed is.
func TestUnreadMeansNotAnalysed(t *testing.T) {
	rule, ok := matchIgnition(".claude/settings.json")
	if !ok {
		t.Fatal("the catalog no longer matches .claude/settings.json")
	}
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		Branches: []scanBranch{{
			Prov:    BranchProvenance{Name: "main", IsDefault: true},
			Matches: []ignitionMatch{{Path: ".claude/settings.json", Size: 40, BlobSHA: "shared", Rule: rule}},
		}},
		Blobs: map[string]blobAnalysis{"shared": {Size: 40, Fetched: true}},
	}
	declared := func(s *RepoScan) bool {
		for _, u := range s.Unchecked {
			if u.File && u.Name == ".claude/settings.json" {
				return true
			}
		}
		return false
	}
	if !declared(evaluateScan(in)) {
		t.Error("fetched by the lockfile pass but never analysed, and not declared")
	}
	in.Blobs["shared"] = blobAnalysis{Size: 40, Fetched: true, Analysed: true, IsText: true}
	if declared(evaluateScan(in)) {
		t.Error("an analysed hook is declared unread")
	}
}

// The sweep's signing context stops where the full scan's walk does:
// a signature on a tip past maxScanBranches is one the full scan never
// sees, and the sweep must not score it.
func TestSignedElsewhereStopsAtTheFullScansReach(t *testing.T) {
	for name, signedAt := range map[string]int{"within reach": 1, "past maxScanBranches": maxScanBranches + 1} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/graphql":
					nodes := []string{`{"name":"main","target":{"oid":"c0","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t-main"},"author":{"name":"me"},"committer":{"name":"me"},"signature":null}}`}
					for i := 1; i <= maxScanBranches+1; i++ {
						sig := "null"
						if i == signedAt {
							sig = `{"isValid":true,"state":"VALID","wasSignedByGitHub":false}`
						}
						nodes = append(nodes, fmt.Sprintf(`{"name":"b%02d","target":{"oid":"c%d","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":"t-side"},"author":{"name":"me"},"committer":{"name":"me"},"signature":%s}}`, i, i, sig))
					}
					fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":{"name":"main"},"refs":{"totalCount":%d,"nodes":[%s]}}}}`, len(nodes), strings.Join(nodes, ","))
				case strings.HasSuffix(r.URL.Path, "/git/trees/t-main"):
					_, _ = io.WriteString(w, `{"tree":[{"path":".claude/settings.json","type":"blob","size":40,"sha":"b-hook"}],"truncated":false}`)
				case strings.HasSuffix(r.URL.Path, "/git/blobs/b-hook"):
					_, _ = io.WriteString(w, `{"hooks":{}}`)
				default:
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"message":"Not Found"}`)
				}
			}))
			t.Cleanup(srv.Close)
			hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
			c := &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}
			s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
			if err != nil {
				t.Fatalf("FetchRepoScan: %v", err)
			}
			scored := false
			for _, f := range s.Findings {
				if f.Axis == AxisProvenance && strings.Contains(f.Reason, "otherwise signs") {
					scored = true
				}
			}
			if want := signedAt <= maxScanBranches-1; scored != want {
				t.Errorf("unsigned-tip finding = %v, want %v", scored, want)
			}
		})
	}
}
