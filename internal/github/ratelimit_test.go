package github

import (
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

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
