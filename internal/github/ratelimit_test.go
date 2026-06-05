package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// reasonOf unwraps the FetchError classification for assertions.
func reasonOf(err error) FetchErrorReason {
	var fe *FetchError
	if errors.As(err, &fe) {
		return fe.Reason
	}
	return ReasonUnknown
}

// Tests pin the contract of the rate-limit mergers introduced
// for the parallel-fetch design (v0.10.1 for the 2-way, v0.13.0
// for the 3-way). Most-pessimistic Remaining wins among
// envelopes that actually carry a Limit; envelopes with Limit==0
// (empty / cached-response) are ignored so they can't drag the
// reported budget to zero.

func envelope(limit, remaining, cost int) rateLimitFields {
	return rateLimitFields{
		Limit:     githubv4.Int(limit),
		Remaining: githubv4.Int(remaining),
		Cost:      githubv4.Int(cost),
		ResetAt:   githubv4.DateTime{Time: time.Unix(int64(remaining), 0)}, // arbitrary distinct timestamp
	}
}

func TestMergeRateLimit3(t *testing.T) {
	tests := []struct {
		name          string
		a, b, c       rateLimitFields
		wantLimit     int
		wantRemaining int
		wantCost      int
	}{
		{
			name:          "all valid — smallest Remaining wins",
			a:             envelope(5000, 4800, 1),
			b:             envelope(5000, 4500, 2),
			c:             envelope(5000, 4700, 1),
			wantLimit:     5000,
			wantRemaining: 4500,
			wantCost:      4,
		},
		{
			name:          "a is empty — pick still finds the smallest among b/c",
			a:             envelope(0, 0, 0),
			b:             envelope(5000, 4500, 2),
			c:             envelope(5000, 4200, 1),
			wantLimit:     5000,
			wantRemaining: 4200, // regression: previously the empty `a` would win and report 0
			wantCost:      3,
		},
		{
			name:          "a and b empty — c provides the answer",
			a:             envelope(0, 0, 0),
			b:             envelope(0, 0, 0),
			c:             envelope(5000, 4800, 1),
			wantLimit:     5000,
			wantRemaining: 4800,
			wantCost:      1,
		},
		{
			name:          "all empty — Limit/Remaining stay zero, cost still sums",
			a:             envelope(0, 0, 0),
			b:             envelope(0, 0, 0),
			c:             envelope(0, 0, 0),
			wantLimit:     0,
			wantRemaining: 0,
			wantCost:      0,
		},
		{
			name:          "cost sums regardless of which envelope wins",
			a:             envelope(5000, 4900, 3),
			b:             envelope(5000, 4800, 5),
			c:             envelope(5000, 4950, 2),
			wantLimit:     5000,
			wantRemaining: 4800,
			wantCost:      10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeRateLimit3(tt.a, tt.b, tt.c)
			if got.Limit != tt.wantLimit {
				t.Errorf("Limit = %d, want %d", got.Limit, tt.wantLimit)
			}
			if got.Remaining != tt.wantRemaining {
				t.Errorf("Remaining = %d, want %d", got.Remaining, tt.wantRemaining)
			}
			if got.Cost != tt.wantCost {
				t.Errorf("Cost = %d, want %d", got.Cost, tt.wantCost)
			}
		})
	}
}

// orderRateResources must lead with the budgets octoscope spends
// (graphql, core, search — in that order) and append the rest
// alphabetically, regardless of map iteration order.
func TestOrderRateResources(t *testing.T) {
	raw := map[string]restRateEntry{
		"integration_manifest": {Limit: 5000},
		"core":                 {Limit: 5000, Used: 3, Remaining: 4997},
		"actions_runner":       {Limit: 10},
		"graphql":              {Limit: 5000, Used: 120, Remaining: 4880},
		"search":               {Limit: 30},
	}
	got := orderRateResources(raw)
	wantOrder := []string{"graphql", "core", "search", "actions_runner", "integration_manifest"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len = %d, want %d", len(got), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got[i].Name != want {
			t.Errorf("resource[%d] = %q, want %q", i, got[i].Name, want)
		}
	}
	if got[0].Used != 120 || got[0].Remaining != 4880 {
		t.Errorf("graphql counters not carried over: %+v", got[0])
	}
}

// FetchRateLimits must decode the /rate_limit payload (including
// the unix-seconds reset) and classify HTTP failures through the
// shared FetchError reasons.
func TestFetchRateLimits(t *testing.T) {
	t.Run("decodes and orders", func(t *testing.T) {
		reset := time.Now().Add(30 * time.Minute).Unix()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/rate_limit" {
				t.Errorf("path = %q, want /rate_limit", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"resources":{` +
				`"core":{"limit":5000,"used":10,"remaining":4990,"reset":` + strconv.FormatInt(reset, 10) + `},` +
				`"graphql":{"limit":5000,"used":200,"remaining":4800,"reset":` + strconv.FormatInt(reset, 10) + `},` +
				`"search":{"limit":30,"used":1,"remaining":29,"reset":` + strconv.FormatInt(reset, 10) + `}},` +
				`"rate":{"limit":5000,"used":10,"remaining":4990,"reset":` + strconv.FormatInt(reset, 10) + `}}`))
		}))
		defer srv.Close()
		c := &Client{rest: srv.Client()}
		c.rest.Transport = &rewriteHost{base: srv.Client().Transport, host: srv.URL}

		got, err := c.FetchRateLimits(context.Background())
		if err != nil {
			t.Fatalf("FetchRateLimits err = %v", err)
		}
		if len(got.Resources) != 3 {
			t.Fatalf("resources = %d, want 3 (the legacy top-level rate must not duplicate core)", len(got.Resources))
		}
		if got.Resources[0].Name != "graphql" || got.Resources[0].Remaining != 4800 {
			t.Errorf("first resource = %+v, want graphql/4800", got.Resources[0])
		}
		if got.Resources[0].Reset.Unix() != reset {
			t.Errorf("reset = %v, want unix %d", got.Resources[0].Reset, reset)
		}
	})

	t.Run("401 classifies as auth", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := &Client{rest: srv.Client()}
		c.rest.Transport = &rewriteHost{base: srv.Client().Transport, host: srv.URL}

		_, err := c.FetchRateLimits(context.Background())
		if reasonOf(err) != ReasonAuth {
			t.Errorf("reason = %v, want ReasonAuth (err: %v)", reasonOf(err), err)
		}
	})

	t.Run("500 classifies as server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		c := &Client{rest: srv.Client()}
		c.rest.Transport = &rewriteHost{base: srv.Client().Transport, host: srv.URL}

		_, err := c.FetchRateLimits(context.Background())
		if reasonOf(err) != ReasonServer {
			t.Errorf("reason = %v, want ReasonServer (err: %v)", reasonOf(err), err)
		}
	})
}
