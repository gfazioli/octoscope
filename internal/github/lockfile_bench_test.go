package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// syntheticLockfile builds a pathological lockfile of roughly n bytes:
// every entry carries an install script, which is the shape that costs the
// most to parse and the one the cap has to be defensible against. A real
// lockfile is far cheaper — WordPress/gutenberg's 1.81 MiB has 12 such
// entries out of thousands.
func syntheticLockfile(n int) string {
	var b strings.Builder
	b.WriteString(`{"lockfileVersion":3,"packages":{"":{"name":"x","version":"1.0.0"},`)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, `"node_modules/pkg%d":{"version":"1.0.%d","integrity":"sha512-%s","hasInstallScript":true},`,
			i, i, strings.Repeat("A", 64))
	}
	return strings.TrimSuffix(b.String(), ",") + "}}"
}

// BenchmarkLockfileFetchAndParse is where the allocation figures in
// maxLockfileScanBytes's comment come from, so they can be re-derived
// rather than believed:
//
//	go test ./internal/github/ -run '^$' -bench LockfileFetchAndParse -benchmem
//
// It exercises the real path — fetchBlob over an httptest server, then
// parseLockfile — at just under the cap, which is the worst case the cap
// admits. B/op is the number the comment quotes.
func BenchmarkLockfileFetchAndParse(b *testing.B) {
	body := syntheticLockfile(maxLockfileScanBytes - (64 << 10))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		content, err := c.fetchBlob(context.Background(), "o", "r", "sha", maxLockfileScanBytes)
		if err != nil {
			b.Fatal(err)
		}
		if f := parseLockfile(content); len(f.Packages) == 0 {
			b.Fatal("fixture parsed to nothing; the benchmark would measure the wrong path")
		}
	}
}
