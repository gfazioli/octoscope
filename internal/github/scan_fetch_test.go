package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shurcooL/githubv4"
)

// scanRef is one branch of a fixture repository: its name and the tree
// its tip commit points at.
type scanRef struct{ name, tree string }

// newScanServer answers FetchRepoScan for one repository: the refs
// query from the branches given, a tree for every OID in trees (one
// README each), and 404 for anything else — the capability probes
// included, which the scan reports as unchecked rather than failing.
// It records the trees asked for.
func newScanServer(t *testing.T, defaultBranch string, refs []scanRef, trees map[string]bool) (*Client, func() []string) {
	t.Helper()
	var (
		mu    sync.Mutex
		asked []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			nodes := make([]string, 0, len(refs))
			for _, ref := range refs {
				nodes = append(nodes, fmt.Sprintf(`{"name":%q,"target":{"oid":"c%s","messageHeadline":"tip","committedDate":"2026-10-01T00:00:00Z","tree":{"oid":%q},"author":{"name":"a"},"committer":{"name":"a"}}}`, ref.name, ref.name, ref.tree))
			}
			def := "null"
			if defaultBranch != "" {
				def = fmt.Sprintf(`{"name":%q}`, defaultBranch)
			}
			fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","defaultBranchRef":%s,"refs":{"totalCount":%d,"nodes":[%s]}}}}`,
				def, len(refs), strings.Join(nodes, ","))
			return
		}
		if i := strings.Index(r.URL.Path, "/git/trees/"); i >= 0 {
			oid := r.URL.Path[i+len("/git/trees/"):]
			mu.Lock()
			asked = append(asked, oid)
			mu.Unlock()
			if trees[oid] {
				_, _ = io.WriteString(w, `{"tree":[{"path":"README.md","type":"blob","size":10,"sha":"b1"}],"truncated":false}`)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	c := &Client{gql: githubv4.NewClient(hc), rest: hc, authenticated: true}
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// A default branch whose commit has no files points at git's empty
// tree, which GitHub hands out and then answers 404 for. The scan used
// to fail on it with a bare 404; it is now an empty tree, walked.
func TestFetchRepoScanEmptyTree(t *testing.T) {
	c, asked := newScanServer(t, "main", []scanRef{{"main", emptyTreeOID}}, nil)
	s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{})
	if err != nil {
		t.Fatalf("FetchRepoScan on an empty default branch: %v", err)
	}
	if len(asked()) != 0 {
		t.Errorf("asked GitHub for trees %v; the empty tree is never fetched", asked())
	}
	if !s.ScannedDefault || s.BranchesScanned != 1 || s.Verdict != VerdictClean {
		t.Errorf("scan = scannedDefault %v, branches %d, verdict %s; want the empty branch walked and clean", s.ScannedDefault, s.BranchesScanned, s.Verdict)
	}
}

func TestFetchRepoScanDefaultBranchOnly(t *testing.T) {
	refs := []scanRef{{"feature", "t-feature"}, {"main", "t-main"}}
	trees := map[string]bool{"t-feature": true, "t-main": true}

	t.Run("walks the default branch and nothing else", func(t *testing.T) {
		c, asked := newScanServer(t, "main", refs, trees)
		s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
		if err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if got := asked(); len(got) != 1 || got[0] != "t-main" {
			t.Errorf("trees asked = %v, want only the default branch's", got)
		}
		if !s.ScannedDefault || s.BranchesScanned != 1 || s.BranchesTotal != 2 {
			t.Errorf("scannedDefault %v, scanned %d of %d; want 1 of 2, the rest counted", s.ScannedDefault, s.BranchesScanned, s.BranchesTotal)
		}
	})
	t.Run("without the option every branch is walked", func(t *testing.T) {
		c, asked := newScanServer(t, "main", refs, trees)
		if _, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{}); err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if got := asked(); len(got) != 2 {
			t.Errorf("trees asked = %v, want both branches", got)
		}
	})
	t.Run("a default branch the refs do not list is not scanned, and says so", func(t *testing.T) {
		c, asked := newScanServer(t, "trunk", refs, trees)
		s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
		if err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if s.ScannedDefault || len(asked()) != 0 {
			t.Errorf("scannedDefault %v, trees %v; want nothing walked and the fact recorded", s.ScannedDefault, asked())
		}
	})
	t.Run("a repository with no commits has no default branch to scan", func(t *testing.T) {
		c, _ := newScanServer(t, "", nil, nil)
		s, err := c.FetchRepoScan(context.Background(), "o", "r", ScanOptions{DefaultBranchOnly: true})
		if err != nil {
			t.Fatalf("FetchRepoScan: %v", err)
		}
		if s.ScannedDefault || s.DefaultBranch != "" {
			t.Errorf("scannedDefault %v, default %q", s.ScannedDefault, s.DefaultBranch)
		}
	})
}
