package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gfazioli/octoscope/internal/auth"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestRetryTransient pins the retry policy: a transient 5xx (ReasonServer)
// is retried up to `attempts`, while success and every other error class
// surface immediately (no wasted retries).
func TestRetryTransient(t *testing.T) {
	serverErr := &github.FetchError{Reason: github.ReasonServer, Err: errors.New("502 bad gateway")}
	authErr := &github.FetchError{Reason: github.ReasonAuth, Err: errors.New("bad credentials")}

	t.Run("retries a transient 5xx then succeeds", func(t *testing.T) {
		calls := 0
		_, err := retryTransient(func(context.Context) (*github.Stats, error) {
			calls++
			if calls < 3 {
				return nil, serverErr
			}
			return &github.Stats{}, nil
		}, 3, 0)
		if err != nil {
			t.Errorf("expected success after retries, got %v", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (two 5xx + one success)", calls)
		}
	})

	t.Run("gives up after attempts on persistent 5xx", func(t *testing.T) {
		calls := 0
		_, err := retryTransient(func(context.Context) (*github.Stats, error) {
			calls++
			return nil, serverErr
		}, 3, 0)
		if err == nil {
			t.Error("expected the 5xx error after exhausting retries")
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (all attempts used)", calls)
		}
	})

	t.Run("does NOT retry a non-transient error", func(t *testing.T) {
		calls := 0
		_, err := retryTransient(func(context.Context) (*github.Stats, error) {
			calls++
			return nil, authErr
		}, 3, 0)
		if err == nil {
			t.Error("expected the auth error")
		}
		if calls != 1 {
			t.Errorf("auth error should NOT be retried; calls = %d, want 1", calls)
		}
	})

	t.Run("success on first try makes one call", func(t *testing.T) {
		calls := 0
		if _, err := retryTransient(func(context.Context) (*github.Stats, error) {
			calls++
			return &github.Stats{}, nil
		}, 3, 0); err != nil || calls != 1 {
			t.Errorf("calls = %d err = %v, want 1 call no error", calls, err)
		}
	})
}

// TestFetchErrorMessage pins that the full-screen error view shows a
// clean, human message — and NEVER the raw HTML 5xx body.
func TestFetchErrorMessage(t *testing.T) {
	rawHTML := errors.New(`non-200 OK status code: 502 Bad Gateway body: "<html>\r\n<head><title>502 Bad Gateway</title></head>"`)

	t.Run("5xx shows a friendly message, no HTML", func(t *testing.T) {
		title, detail := fetchErrorMessage(github.ReasonServer, rawHTML, nil, auth.SourceNone)
		if title == "" {
			t.Error("expected a title")
		}
		for _, bad := range []string{"<html>", "<head>", "<title>", "body:"} {
			if strings.Contains(title+detail, bad) {
				t.Errorf("error message leaked raw HTML (%q):\n%s\n%s", bad, title, detail)
			}
		}
		if !strings.Contains(strings.ToLower(detail), "retr") {
			t.Errorf("5xx detail should mention the automatic retry:\n%s", detail)
		}
	})

	t.Run("cleanErr strips HTML and truncates", func(t *testing.T) {
		got := cleanErr(rawHTML)
		if strings.Contains(got, "<") || strings.Contains(got, "body:") {
			t.Errorf("cleanErr left markup: %q", got)
		}
		if !strings.Contains(got, "502") {
			t.Errorf("cleanErr should keep the human part: %q", got)
		}
	})

	t.Run("cleanErr handles markup at index 0 (no raw leak, no empty)", func(t *testing.T) {
		got := cleanErr(errors.New("<html><head><title>502</title></head></html>"))
		if strings.Contains(got, "<") {
			t.Errorf("leading-markup error leaked angle brackets: %q", got)
		}
		if got == "" {
			t.Error("cleanErr should fall back to a message, not empty")
		}
	})

	t.Run("cleanErr strips ANSI / C0 control sequences", func(t *testing.T) {
		got := cleanErr(errors.New("\x1b[2J\x1b[31mboom\x07\r evil"))
		if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\x07') || strings.ContainsRune(got, '\r') {
			t.Errorf("cleanErr leaked a control sequence: %q", got)
		}
		if !strings.Contains(got, "boom") {
			t.Errorf("cleanErr dropped the readable text: %q", got)
		}
	})

	t.Run("cleanErr on nil and plain", func(t *testing.T) {
		if cleanErr(nil) == "" {
			t.Error("nil err should yield a message")
		}
		if got := cleanErr(errors.New("just a plain error")); got != "just a plain error" {
			t.Errorf("plain error mangled: %q", got)
		}
	})
}

// TestFetchErrorMessageAuth pins the #36 error UX: a rejected token
// names its source and the matching fix, an under-scoped token names
// the missing scope, and neither path ever echoes the raw error body
// (which could ride alongside a token-shaped string).
func TestFetchErrorMessageAuth(t *testing.T) {
	rejected := errors.New(`non-200 OK status code: 401 Unauthorized body: "{\"message\":\"Bad credentials\"}" ghp_deadbeef`)

	t.Run("env token points at $GITHUB_TOKEN and the tokens page", func(t *testing.T) {
		title, detail := fetchErrorMessage(github.ReasonAuth, rejected, nil, auth.SourceEnv)
		if !strings.Contains(strings.ToLower(title), "expired or revoked") {
			t.Errorf("title should say expired/revoked: %q", title)
		}
		if !strings.Contains(detail, "$GITHUB_TOKEN") || !strings.Contains(detail, tokensSettingsURL) {
			t.Errorf("detail should name the env var and the regen URL: %q", detail)
		}
	})

	t.Run("gh CLI token points at gh auth, not the tokens page", func(t *testing.T) {
		_, detail := fetchErrorMessage(github.ReasonAuth, rejected, nil, auth.SourceGHCLI)
		if !strings.Contains(detail, "gh auth") {
			t.Errorf("detail should point at gh auth refresh/login: %q", detail)
		}
		if strings.Contains(detail, "settings/tokens") {
			t.Errorf("regenerating a PAT does not fix a gh login — drop the URL: %q", detail)
		}
	})

	t.Run("no token falls back to the generic guidance", func(t *testing.T) {
		_, detail := fetchErrorMessage(github.ReasonAuth, rejected, nil, auth.SourceNone)
		if !strings.Contains(detail, "$GITHUB_TOKEN") || !strings.Contains(detail, "gh auth login") {
			t.Errorf("fallback should offer both auth paths: %q", detail)
		}
	})

	t.Run("under-scoped token names the missing scope", func(t *testing.T) {
		scopeErr := errors.New("Your token has not been granted the required scopes to execute this query. The 'starredRepositories' field requires one of the following scopes: ['read:user'], but your token has only been granted the: ['repo'] scopes.")
		title, detail := fetchErrorMessage(github.ReasonAuthScope, scopeErr, nil, auth.SourceEnv)
		if !strings.Contains(strings.ToLower(title), "scope") {
			t.Errorf("title should mention the scope problem: %q", title)
		}
		if !strings.Contains(detail, "read:user") || !strings.Contains(detail, tokensSettingsURL) {
			t.Errorf("detail should name the scope and the regen URL: %q", detail)
		}
	})

	t.Run("auth messages never echo the raw error", func(t *testing.T) {
		for _, src := range []auth.Source{auth.SourceNone, auth.SourceEnv, auth.SourceGHCLI} {
			title, detail := fetchErrorMessage(github.ReasonAuth, rejected, nil, src)
			if strings.Contains(title+detail, "ghp_deadbeef") {
				t.Errorf("source %d echoed the raw error (token-shaped string leaked)", src)
			}
		}
	})
}
