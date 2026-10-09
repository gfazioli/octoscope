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

	t.Run("gives up after attempts on persistent 5xx, with the last error", func(t *testing.T) {
		// A distinct error per attempt, so returning the first one
		// instead of the last would show.
		errs := []error{
			&FetchError{Reason: ReasonServer, Err: errors.New("502 #1")},
			&FetchError{Reason: ReasonServer, Err: errors.New("502 #2")},
			&FetchError{Reason: ReasonServer, Err: errors.New("502 #3")},
		}
		calls := 0
		_, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", errs[calls-1]
		}, 3, 0, time.Second)
		if err != errs[2] {
			t.Errorf("err = %v, want the third attempt's error", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (all attempts used)", calls)
		}
	})

	// Every classified reason but ReasonServer is tried exactly once: a
	// retried rate limit deepens the penalty, a retried refusal or
	// not-found can never succeed, and a deadline is not transient.
	for _, reason := range []FetchErrorReason{
		ReasonAuth, ReasonAuthScope, ReasonRateLimitPrimary, ReasonRateLimitSecondary,
		ReasonNotFound, ReasonNetwork, ReasonUnknown,
	} {
		t.Run(fmt.Sprintf("does NOT retry reason %d", reason), func(t *testing.T) {
			want := &FetchError{Reason: reason, Err: errors.New("refused")}
			calls := 0
			_, err := RetryTransient(func(context.Context) (string, error) {
				calls++
				return "", want
			}, 3, 0, time.Second)
			if err != want {
				t.Errorf("err = %v, want the error unchanged", err)
			}
			if calls != 1 {
				t.Errorf("calls = %d, want 1", calls)
			}
		})
	}

	t.Run("waits the backoff, doubled, between attempts", func(t *testing.T) {
		const backoff = 30 * time.Millisecond
		start := time.Now()
		_, _ = RetryTransient(func(context.Context) (string, error) {
			return "", serverErr
		}, 3, backoff, time.Second)
		if elapsed := time.Since(start); elapsed < 3*backoff {
			t.Errorf("three attempts took %v, want at least %v (backoff then twice the backoff)", elapsed, 3*backoff)
		}
	})

	t.Run("finds a wrapped 5xx, and leaves an unclassified error alone", func(t *testing.T) {
		// A caller may wrap the FetchError on its way out, so the policy
		// has to see through a wrap rather than type-assert.
		calls := 0
		_, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", fmt.Errorf("inbox: %w", serverErr)
		}, 3, 0, time.Second)
		if calls != 3 || !errors.Is(err, serverErr) {
			t.Errorf("wrapped 5xx: calls = %d, err = %v; want 3 calls and the wrapped 5xx", calls, err)
		}
		calls = 0
		plain := errors.New("502 but not a FetchError")
		_, err = RetryTransient(func(context.Context) (string, error) {
			calls++
			return "", plain
		}, 3, 0, time.Second)
		if calls != 1 || err != plain {
			t.Errorf("unclassified error: calls = %d, err = %v; want 1 call and the error unchanged — only a classified ReasonServer is transient", calls, err)
		}
	})

	t.Run("success on first try makes one call", func(t *testing.T) {
		calls := 0
		got, err := RetryTransient(func(context.Context) (string, error) {
			calls++
			return "ok", nil
		}, 3, 0, time.Second)
		if err != nil || got != "ok" || calls != 1 {
			t.Errorf("got %q, calls = %d, err = %v; want \"ok\", 1 call, no error", got, calls, err)
		}
	})

	t.Run("every attempt gets its own deadline", func(t *testing.T) {
		// A shared deadline would hand the third attempt whatever the first
		// two left over; each one has to start with the full budget.
		// Each attempt burns a fifth of the budget, so under a shared one
		// the third would start with ~60% of it — well below the bound,
		// which leaves a fifth of the budget (400ms) for scheduling slack.
		const timeout = 2 * time.Second
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
			if r < timeout*8/10 || r > timeout {
				t.Errorf("attempt %d started with %v left, want ~%v", i+1, r, timeout)
			}
		}
	})
}
