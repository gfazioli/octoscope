package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

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
		title, detail := fetchErrorMessage(github.ReasonServer, rawHTML, nil)
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

	t.Run("cleanErr on nil and plain", func(t *testing.T) {
		if cleanErr(nil) == "" {
			t.Error("nil err should yield a message")
		}
		if got := cleanErr(errors.New("just a plain error")); got != "just a plain error" {
			t.Errorf("plain error mangled: %q", got)
		}
	})
}
