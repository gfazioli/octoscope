package ui

import (
	"testing"
	"time"
)

// bucketStars must distribute timestamps across n weekly
// buckets with index 0 = oldest, n-1 = most recent. Pins the
// contract that fuels the sparkline renderer.
func TestBucketStars(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour

	tests := []struct {
		name  string
		stars []time.Time
		n     int
		want  []int
	}{
		{
			name:  "empty input",
			stars: nil,
			n:     4,
			want:  []int{0, 0, 0, 0},
		},
		{
			name: "all in most-recent bucket",
			stars: []time.Time{
				now,
				now.Add(-2 * time.Hour),
			},
			n:    4,
			want: []int{0, 0, 0, 2},
		},
		{
			name: "spread across last 3 weeks",
			stars: []time.Time{
				now.Add(-1 * week / 2),      // bucket n-1
				now.Add(-(1*week + week/2)), // bucket n-2
				now.Add(-(2*week + week/2)), // bucket n-3
			},
			n:    4,
			want: []int{0, 1, 1, 1},
		},
		{
			name: "out-of-window dropped",
			stars: []time.Time{
				now.Add(-1 * time.Hour), // n-1
				now.Add(-100 * week),    // way out, dropped
			},
			n:    4,
			want: []int{0, 0, 0, 1},
		},
		{
			name:  "n=0 returns empty",
			stars: []time.Time{now},
			n:     0,
			want:  []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bucketStars(tt.stars, now, tt.n)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("bucket %d = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// sparklineString maps integer buckets to 8 block glyphs. Pins
// the corner cases: all-empty input collapses to empty string,
// scaling stays monotonic, single non-zero bucket renders at
// the highest level.
func TestSparklineString(t *testing.T) {
	tests := []struct {
		name    string
		buckets []int
		want    string
	}{
		{
			name:    "all empty → empty string",
			buckets: []int{0, 0, 0},
			want:    "",
		},
		{
			name:    "single bucket scales to max",
			buckets: []int{0, 0, 5, 0},
			// Empty weeks render as muted ticks (`·`), one
			// per bucket, so the timeline axis stays
			// visible even when most weeks are empty.
			want: "··▇·",
		},
		{
			name:    "monotonic ramp produces increasing bars",
			buckets: []int{0, 1, 2, 3, 4, 5, 6, 7},
			// First is empty (count 0 → muted tick), others
			// scale 1..max=7.
			want: "·▁▂▃▄▅▆▇",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sparklineString(tt.buckets)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if got != "" && len([]rune(got)) != len(tt.buckets) {
				t.Errorf("length mismatch: got %d runes, want %d", len([]rune(got)), len(tt.buckets))
			}
		})
	}
}
