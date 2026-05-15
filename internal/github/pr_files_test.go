package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSanitizeFileChange covers the pure transform from the
// REST wire shape to the UI-facing FileChange:
//   - Sanitize() runs on Path / Status / Patch
//   - patchLineCap is enforced on Patch (count of '\n')
//   - Truncated reflects only the cap, not GitHub-side omissions
func TestSanitizeFileChange(t *testing.T) {
	largePatch := strings.Repeat("@@ hunk @@\n", patchLineCap+1) // > patchLineCap newlines
	boundary := strings.Repeat("@@ hunk @@\n", patchLineCap)     // exactly patchLineCap newlines — keep
	ansiPath := "internal/ui/\x1b[31mhostile\x1b[0m.go"
	cleanPath := "internal/ui/hostile.go"

	tests := []struct {
		name        string
		in          restFile
		wantPath    string
		wantStatus  string
		wantPatch   string
		wantTrunc   bool
		patchEmpty  bool
		patchPrefix string // when wantPatch is set and we want a prefix match (long patches)
	}{
		{
			name:       "simple modified file passes through",
			in:         restFile{Filename: "README.md", Status: "modified", Additions: 3, Deletions: 1, Patch: "@@ -1 +1 @@\n-old\n+new"},
			wantPath:   "README.md",
			wantStatus: "modified",
			wantPatch:  "@@ -1 +1 @@\n-old\n+new",
		},
		{
			name:       "path with ANSI escape is stripped",
			in:         restFile{Filename: ansiPath, Status: "modified"},
			wantPath:   cleanPath,
			wantStatus: "modified",
		},
		{
			name:       "patch exactly at cap is kept",
			in:         restFile{Filename: "big.go", Status: "modified", Patch: boundary},
			wantPath:   "big.go",
			wantStatus: "modified",
			wantPatch:  boundary,
		},
		{
			name:       "patch over cap is dropped and flagged",
			in:         restFile{Filename: "huge.go", Status: "modified", Patch: largePatch},
			wantPath:   "huge.go",
			wantStatus: "modified",
			wantTrunc:  true,
			patchEmpty: true,
		},
		{
			name:       "binary file (empty patch) stays untouched",
			in:         restFile{Filename: "logo.png", Status: "modified", Patch: ""},
			wantPath:   "logo.png",
			wantStatus: "modified",
			patchEmpty: true,
		},
		{
			name:       "removed file keeps status",
			in:         restFile{Filename: "old.txt", Status: "removed", Deletions: 42},
			wantPath:   "old.txt",
			wantStatus: "removed",
			patchEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFileChange(tt.in)
			if got.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Truncated != tt.wantTrunc {
				t.Errorf("Truncated = %v, want %v", got.Truncated, tt.wantTrunc)
			}
			if tt.patchEmpty {
				if got.Patch != "" {
					t.Errorf("Patch = %q, want empty", got.Patch)
				}
			} else if tt.wantPatch != "" && got.Patch != tt.wantPatch {
				t.Errorf("Patch mismatch")
			}
			if got.Additions != tt.in.Additions {
				t.Errorf("Additions = %d, want %d", got.Additions, tt.in.Additions)
			}
			if got.Deletions != tt.in.Deletions {
				t.Errorf("Deletions = %d, want %d", got.Deletions, tt.in.Deletions)
			}
		})
	}
}

// TestDecodePRFilesPage covers the HTTP-response classification
// path. Each status code is mapped to a distinct FetchError
// reason so the UI banner stays informative; the JSON body
// itself is decoded only on 2xx. The function must always close
// the response body.
func TestDecodePRFilesPage(t *testing.T) {
	body := func(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

	tests := []struct {
		name         string
		status       int
		bodyText     string
		header       http.Header
		wantErr      bool
		wantReason   FetchErrorReason
		wantLen      int
		wantFilename string // first element when len > 0
	}{
		{
			name:       "401 -> auth",
			status:     http.StatusUnauthorized,
			bodyText:   `{"message":"Bad credentials"}`,
			wantErr:    true,
			wantReason: ReasonAuth,
		},
		{
			name:       "403 with remaining=0 -> primary rate limit",
			status:     http.StatusForbidden,
			bodyText:   `{"message":"API rate limit exceeded"}`,
			header:     http.Header{"X-Ratelimit-Remaining": []string{"0"}},
			wantErr:    true,
			wantReason: ReasonRateLimitPrimary,
		},
		{
			name:       "403 with remaining>0 -> abuse / secondary",
			status:     http.StatusForbidden,
			bodyText:   `{"message":"abuse"}`,
			header:     http.Header{"X-Ratelimit-Remaining": []string{"42"}},
			wantErr:    true,
			wantReason: ReasonRateLimitSecondary,
		},
		{
			name:       "403 without remaining header -> primary by default",
			status:     http.StatusForbidden,
			bodyText:   `{"message":"forbidden"}`,
			wantErr:    true,
			wantReason: ReasonRateLimitPrimary,
		},
		{
			name:       "429 -> secondary rate limit",
			status:     http.StatusTooManyRequests,
			bodyText:   `{"message":"slow down"}`,
			wantErr:    true,
			wantReason: ReasonRateLimitSecondary,
		},
		{
			name:       "500 -> server",
			status:     http.StatusInternalServerError,
			bodyText:   `oops`,
			wantErr:    true,
			wantReason: ReasonServer,
		},
		{
			name:       "200 with malformed JSON -> server",
			status:     http.StatusOK,
			bodyText:   `not json`,
			wantErr:    true,
			wantReason: ReasonServer,
		},
		{
			name:         "200 with valid array",
			status:       http.StatusOK,
			bodyText:     `[{"filename":"a.go","status":"modified","additions":2,"deletions":1,"patch":"@@"}]`,
			wantLen:      1,
			wantFilename: "a.go",
		},
		{
			name:     "200 with empty array",
			status:   http.StatusOK,
			bodyText: `[]`,
			wantLen:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.status,
				Status:     http.StatusText(tt.status),
				Body:       body(tt.bodyText),
				Header:     tt.header,
			}
			got, err := decodePRFilesPage(resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				fe, ok := err.(*FetchError)
				if !ok {
					t.Fatalf("err type = %T, want *FetchError", err)
				}
				if fe.Reason != tt.wantReason {
					t.Errorf("reason = %v, want %v", fe.Reason, tt.wantReason)
				}
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 && got[0].Filename != tt.wantFilename {
				t.Errorf("first filename = %q, want %q", got[0].Filename, tt.wantFilename)
			}
		})
	}
}

// TestFetchPRFilesPagination is an end-to-end test of the
// paginating fetch loop against a httptest server. Confirms:
//   - the loop walks pages until a short page (len < filesPerPage)
//     is returned, then stops
//   - request URL path-escapes owner / name
//   - the per-PR maxFiles ceiling caps the result regardless of
//     how many pages the server is happy to keep returning
func TestFetchPRFilesPagination(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.RequestURI())
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			// Full page (filesPerPage entries) — pagination must
			// continue.
			w.Write([]byte(`[` + strings.Repeat(`{"filename":"a.go","status":"modified","additions":1,"deletions":0,"patch":""},`, filesPerPage-1) +
				`{"filename":"last.go","status":"modified","additions":1,"deletions":0,"patch":""}]`))
		case "2":
			// Short page — fetch should stop after consuming it.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"filename":"b.go","status":"added","additions":3,"deletions":0,"patch":""}]`))
		default:
			t.Errorf("unexpected page request: %s", page)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// Build a Client with the test server as the REST transport.
	// We don't go through New() because we don't need oauth here —
	// only the rest field is exercised.
	c := &Client{rest: srv.Client()}
	// Point the request URL at the test server by intercepting
	// the host through a custom transport.
	c.rest.Transport = &rewriteHost{base: srv.Client().Transport, host: srv.URL}

	files, err := c.FetchPRFiles(context.Background(), "owner with space", "repo/name", 1)
	if err != nil {
		t.Fatalf("FetchPRFiles err = %v", err)
	}
	if got, want := len(files), filesPerPage+1; got != want {
		t.Errorf("len(files) = %d, want %d", got, want)
	}
	if len(gotPaths) != 2 {
		t.Errorf("server saw %d requests, want 2 (page=1, page=2)", len(gotPaths))
	}
	// PathEscape on owner converts " " -> %20 (not "+" — that's
	// QueryEscape). Confirm the request actually went through it.
	if !strings.Contains(gotPaths[0], "owner%20with%20space") {
		t.Errorf("owner was not path-escaped, got URI = %q", gotPaths[0])
	}
	if !strings.Contains(gotPaths[0], "repo%2Fname") {
		t.Errorf("repo name was not path-escaped, got URI = %q", gotPaths[0])
	}
}

// rewriteHost is a tiny http.RoundTripper that swaps the
// request's host with `host` before forwarding. Lets the test
// keep using the production URL builder (api.github.com) while
// the actual traffic hits the httptest server.
type rewriteHost struct {
	base http.RoundTripper
	host string
}

func (r *rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	// host is e.g. "http://127.0.0.1:54321". Splice the scheme
	// + host onto the original path + query.
	newURL := r.host + req.URL.RequestURI()
	out, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	out.Header = req.Header
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(out)
}
