package github

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A redirect is followed on its own host only: the token rides every
// request the client makes, a redirected one included.
func TestSameHostRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	hc := &http.Client{CheckRedirect: sameHostRedirects}
	resp, err := hc.Get(origin.URL + "/repos/o/r/dependabot/alerts")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || elsewhere.Load() != 0 {
		t.Errorf("status %d, %d requests elsewhere; want the 302 itself and none", resp.StatusCode, elsewhere.Load())
	}

	// Same host over https is followed: GitHub's own renames.
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repositories/1/x", nil)
	via, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/o/r/x", nil)
	if err := sameHostRedirects(req, []*http.Request{via}); err != nil {
		t.Errorf("a same-host https redirect was refused: %v", err)
	}
}

// The client New builds carries the policy, for GraphQL and REST alike.
func TestNewClientRefusesCrossHostRedirects(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-real")
	c, err := New("", Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.rest.CheckRedirect == nil {
		t.Fatal("the REST client has no redirect policy")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://elsewhere.example/x", nil)
	via, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	if err := c.rest.CheckRedirect(req, []*http.Request{via}); err != http.ErrUseLastResponse {
		t.Errorf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
}
