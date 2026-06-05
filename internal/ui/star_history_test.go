package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
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

// cumulate must produce the running sum, preserving length and
// leaving leading zero weeks at zero (they render as axis ticks).
func TestCumulate(t *testing.T) {
	tests := []struct {
		name    string
		buckets []int
		want    []int
	}{
		{"empty", []int{}, []int{}},
		{"all zero stays zero", []int{0, 0, 0}, []int{0, 0, 0}},
		{"running sum", []int{0, 2, 0, 3, 1}, []int{0, 2, 2, 5, 6}},
		{"single spike plateaus", []int{0, 5, 0, 0}, []int{0, 5, 5, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cumulate(tt.buckets)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("bucket %d = %d, want %d", i, got[i], tt.want[i])
				}
			}
			// Monotone non-decreasing by construction.
			for i := 1; i < len(got); i++ {
				if got[i] < got[i-1] {
					t.Errorf("cumulative bucket %d (%d) < bucket %d (%d)", i, got[i], i-1, got[i-1])
				}
			}
		})
	}
}

// The cumulative sparkline must be a monotone curve ending at the
// top glyph, and the mode must be visible in the section heading
// (the `v` toggle needs feedback even when the curves look alike).
func TestStarHistoryCumulativeMode(t *testing.T) {
	// A spike 3 weeks ago then silence: density shows one bar,
	// cumulative shows a plateau from the spike onward.
	week := 7 * 24 * time.Hour
	stars := []time.Time{
		time.Now().Add(-3 * week),
		time.Now().Add(-3*week - time.Hour),
	}

	density := renderStarSparkline(stars, false, StarModeDensity)
	cumulative := renderStarSparkline(stars, false, StarModeCumulative)
	if density == "" || cumulative == "" {
		t.Fatal("both modes should render for a starred repo")
	}
	if density == cumulative {
		t.Error("density and cumulative should differ for a spike-then-silence history")
	}

	d := &github.RepoDetail{StarHistory: stars}
	if got := repoDetailStarHistory(d, StarModeCumulative); !strings.Contains(got, "cumulative") {
		t.Errorf("cumulative heading should name the mode, got:\n%s", got)
	}
	if got := repoDetailStarHistory(d, StarModeDensity); strings.Contains(got, "cumulative") {
		t.Errorf("density heading should not mention cumulative, got:\n%s", got)
	}
}

// The `v` key cycles the star-history mode only when the loaded
// detail has stars; the title hint follows the same condition.
func TestStarModeToggleKey(t *testing.T) {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}

	t.Run("toggles when stars present", func(t *testing.T) {
		rd := RepoDetailModel{open: true}.applyFetched(
			&github.RepoDetail{StarHistory: []time.Time{time.Now()}}, nil)
		rd, _ = rd.Update(key, nil, 80, 24)
		if rd.starMode != StarModeCumulative {
			t.Errorf("after v, starMode = %v, want cumulative", rd.starMode)
		}
		rd, _ = rd.Update(key, nil, 80, 24)
		if rd.starMode != StarModeDensity {
			t.Errorf("second v should cycle back to density, got %v", rd.starMode)
		}
		if !strings.Contains(ansi.Strip(rd.renderTitle()), "v star view") {
			t.Error("title should hint the v key when star history is present")
		}
	})

	t.Run("inert without stars", func(t *testing.T) {
		rd := RepoDetailModel{open: true}.applyFetched(&github.RepoDetail{}, nil)
		rd, _ = rd.Update(key, nil, 80, 24)
		if rd.starMode != StarModeDensity {
			t.Errorf("v on a star-less detail should not change mode, got %v", rd.starMode)
		}
		if strings.Contains(ansi.Strip(rd.renderTitle()), "star view") {
			t.Error("title must not advertise v when there is no star history")
		}
	})
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
