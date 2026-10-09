package github

import (
	"context"
	"errors"
	"time"
)

// TransientAttempts / TransientBackoff bound the transient-5xx retry.
// The GraphQL gateway intermittently 502s the heavy dashboard query on
// busy accounts (the complexity-ceiling scar); those clear in moments,
// so a couple of quick retries keep the TUI on its loading spinner, and
// a scheduled --json run on its way, instead of failing on the first
// blip. One policy for both, so the dashboard and the non-interactive
// report cannot drift apart on what counts as transient.
const (
	TransientAttempts = 3
	TransientBackoff  = 800 * time.Millisecond
)

// RetryTransient runs fetch up to `attempts` times, retrying ONLY a
// transient ReasonServer error (5xx — typically a 502 — or an HTTP/2
// transport failure) after `backoff`, doubled each round. Success and
// every other error class (auth, rate-limit, not-found, network/timeout,
// unknown) return immediately — retrying those is pointless. Each
// attempt gets its own `timeout`: a 5xx comes back fast, so retries cost
// only the backoff, not a full timeout each. (A 5xx that instead hangs
// to the per-attempt deadline isn't really transient; that's the worst
// case, ~attempts×timeout, and the deadline itself classifies as
// ReasonNetwork, so it is not retried again.)
//
// The retry is for a fine request on a bad moment. A query that times
// out on its own weight is not transient, and each retry of it deducts
// more from GitHub's hour-long timeout penalty: fix the query rather
// than lean on this.
func RetryTransient[T any](fetch func(context.Context) (T, error), attempts int, backoff, timeout time.Duration) (T, error) {
	var (
		v   T
		err error
	)
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		v, err = fetch(ctx)
		cancel()
		if err == nil {
			return v, nil
		}
		var fe *FetchError
		if !errors.As(err, &fe) || fe.Reason != ReasonServer {
			return v, err
		}
		if attempt < attempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return v, err
}
