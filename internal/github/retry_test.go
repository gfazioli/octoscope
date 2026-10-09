package github

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestRetryTransient pins the retry policy: a transient 5xx (ReasonServer)
// is retried up to `attempts`, while success and every other error class
// surface immediately (no wasted retries).
func TestRetryTransient(t *testing.T) {
	serverErr := &FetchError{Reason: ReasonServer, Err: errors.New("502 bad gateway")}
	authErr := &FetchError{Reason: ReasonAuth, Err: errors.New("bad credentials")}

	t.Run("retries a transient 5xx then succeeds", func(t *testing.T) {
		calls := 0
		got, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			if calls < 3 {
				return "", serverErr
			}
			return "ok", nil
		}, 3, 0, time.Second)
		if err != nil || got != "ok" {
			t.Errorf("got %q, %v; want success after retries", got, err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (two 5xx + one success)", calls)
		}
	})

	t.Run("gives up after attempts on persistent 5xx", func(t *testing.T) {
		calls := 0
		_, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", serverErr
		}, 3, 0, time.Second)
		if !errors.Is(err, serverErr) {
			t.Errorf("err = %v, want the 5xx after exhausting retries", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (all attempts used)", calls)
		}
	})

	t.Run("does NOT retry a non-transient error", func(t *testing.T) {
		calls := 0
		_, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", authErr
		}, 3, 0, time.Second)
		if !errors.Is(err, authErr) {
			t.Errorf("err = %v, want the auth error", err)
		}
		if calls != 1 {
			t.Errorf("auth error should NOT be retried; calls = %d, want 1", calls)
		}
	})

	t.Run("finds a wrapped 5xx, and leaves an unclassified error alone", func(t *testing.T) {
		// The non-interactive inbox path wraps its FetchError in a hint,
		// so the policy has to see through a wrap rather than type-assert.
		calls := 0
		_, _ = RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", fmt.Errorf("inbox: %w", serverErr)
		}, 3, 0, time.Second)
		if calls != 3 {
			t.Errorf("wrapped 5xx: calls = %d, want 3", calls)
		}
		calls = 0
		_, _ = RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", errors.New("502 but not a FetchError")
		}, 3, 0, time.Second)
		if calls != 1 {
			t.Errorf("unclassified error: calls = %d, want 1 — only a classified ReasonServer is transient", calls)
		}
	})

	t.Run("success on first try makes one call", func(t *testing.T) {
		calls := 0
		if _, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "ok", nil
		}, 3, 0, time.Second); err != nil || calls != 1 {
			t.Errorf("calls = %d err = %v, want 1 call no error", calls, err)
		}
	})

	t.Run("every attempt gets its own deadline", func(t *testing.T) {
		// A shared deadline would hand the third attempt whatever the first
		// two left over; each one has to start with the full budget.
		// Each attempt burns a fifth of the budget, so under a shared one
		// the third would start with ~60% of it — well below the bound.
		const timeout = time.Second
		var remaining []time.Duration
		_, _ = RetryTransient(func(ctx context.Context) (string, error) {
			d, ok := ctx.Deadline()
			if !ok {
				t.Fatal("attempt ran without a deadline")
			}
			remaining = append(remaining, time.Until(d))
			time.Sleep(timeout / 5)
			return "", serverErr
		}, 3, 0, timeout)
		if len(remaining) != 3 {
			t.Fatalf("attempts = %d, want 3", len(remaining))
		}
		for i, r := range remaining {
			if r < timeout*9/10 || r > timeout {
				t.Errorf("attempt %d started with %v left, want ~%v", i+1, r, timeout)
			}
		}
	})
}
