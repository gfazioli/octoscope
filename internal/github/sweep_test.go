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
		fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/%s","url":"https://github.com/o/%s","defaultBranchRef":%s,"refs":{"totalCount":1,"nodes":[%s]}}}}`,
			body.Variables["name"], body.Variables["name"], def, nodes)
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
		"ok":    {defaultBranch: "main"},
		"empty": {},
		"gone":  {status: http.StatusNotFound},
		"flaky": {defaultBranch: "main", failFirst: 1},
	})
	targets := []SweepTarget{{"o", "ok", ""}, {"o", "empty", ""}, {"o", "gone", ""}, {"o", "flaky", ""}}
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
	if got[0].Scan == nil || got[0].Scan.Verdict != VerdictClean {
		t.Errorf("ok: %+v, want a clean scan", got[0])
	}
	if got[1].NotScanned != "the repository has no commits yet" {
		t.Errorf("empty: %q", got[1].NotScanned)
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
		targets = append(targets, SweepTarget{"o", name, ""})
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
