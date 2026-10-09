package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTrafficBuckets(t *testing.T) {
	end := day("2026-10-08")
	got := trafficBuckets([]github.TrafficDay{
		{Day: day("2026-09-25"), Count: 8}, // first day of the window
		{Day: day("2026-10-01"), Count: 3},
		{Day: day("2026-10-08"), Count: 10}, // last day
		{Day: day("2026-09-24"), Count: 99}, // one day too old: dropped
	}, end)
	want := []int{8, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 10}
	if len(got) != trafficDays {
		t.Fatalf("len = %d, want %d — one cell per day of GitHub's window", len(got), trafficDays)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buckets = %v, want %v (a missing day stays a quiet cell in its place)", got, want)
		}
	}
}

func TestRepoDetailTraffic(t *testing.T) {
	_ = applyTheme("octoscope", "")
	traffic := &github.Traffic{
		Views: 53, ViewsUnique: 17, Clones: 1701, ClonesUnique: 323,
		DailyViews: []github.TrafficDay{
			{Day: day("2026-09-25"), Count: 8},
			{Day: day("2026-10-08"), Count: 2},
		},
		DailyClones: []github.TrafficDay{
			{Day: day("2026-10-06"), Count: 1500},
		},
	}

	t.Run("the numbers, one row each", func(t *testing.T) {
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{TrafficAccess: github.AccessOK, Traffic: traffic}, 100))
		lines := strings.Split(out, "\n")
		if len(lines) != 3 || lines[0] != "Traffic (14 days)" {
			t.Fatalf("got:\n%s\nwant a heading and two rows", out)
		}
		if !strings.HasPrefix(lines[1], "views") || !strings.Contains(lines[1], "53 · 17 unique") {
			t.Errorf("views row = %q", lines[1])
		}
		if !strings.HasPrefix(lines[2], "clones") || !strings.Contains(lines[2], "1,701 · 323 unique") {
			t.Errorf("clones row = %q", lines[2])
		}
		// Both sparklines span the same 14 cells from the same column,
		// so a day lines up with the same day in the other row.
		if a, b := strings.IndexAny(lines[1], "·▁▂▃▄▅▆▇"), strings.IndexAny(lines[2], "·▁▂▃▄▅▆▇"); a != b {
			t.Errorf("sparklines start at bytes %d and %d, want the same column", a, b)
		}
		if spark := []rune(strings.Fields(lines[2])[1]); len(spark) != trafficDays {
			t.Errorf("clones spark = %q, want %d cells", string(spark), trafficDays)
		}
	})

	t.Run("a day sits in the same column in both rows", func(t *testing.T) {
		// The clones stop two days before the views: their busiest day
		// (10-06) has to land two cells before the end, not at it.
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{TrafficAccess: github.AccessOK, Traffic: traffic}, 100))
		lines := strings.Split(out, "\n")
		views := []rune(strings.Fields(lines[1])[1])
		clones := []rune(strings.Fields(lines[2])[1])
		if len(clones) != trafficDays || clones[11] != sparkBars[len(sparkBars)-1] || clones[12] != sparkBars[0] || clones[13] != sparkBars[0] {
			t.Errorf("clones spark = %q; want the 10-06 peak in cell 12 of 14 and two quiet days after it", string(clones))
		}
		// And the views keep their own days: 09-25 first, 10-08 last.
		if len(views) != trafficDays || views[0] == sparkBars[0] || views[13] == sparkBars[0] || views[6] != sparkBars[0] {
			t.Errorf("views spark = %q; want activity in the first and last cells and none between", string(views))
		}
	})

	t.Run("a narrow terminal drops the unique counts, not the days", func(t *testing.T) {
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{TrafficAccess: github.AccessOK, Traffic: traffic}, 40))
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > 38 {
				t.Errorf("line %d is %d cells, past the 38 a 40-column drill-in leaves: %q", i, w, l)
			}
		}
		// Each row decides for itself: the views fit with their unique
		// count (36 cells), the clones do not (42) and drop theirs.
		lines := strings.Split(out, "\n")
		if !strings.Contains(lines[1], "17 unique") || !strings.Contains(lines[2], "1,701") || strings.Contains(lines[2], "unique") {
			t.Errorf("want the views whole and the clones without their unique count at 40 columns:\n%s", out)
		}
	})

	t.Run("silent where GitHub is", func(t *testing.T) {
		for _, a := range []github.Access{github.AccessNotPermitted, github.AccessNotAsked} {
			if out := repoDetailTraffic(&github.RepoDetail{TrafficAccess: a}, 100); out != "" {
				t.Errorf("access %d rendered %q, want no section", a, out)
			}
		}
	})

	t.Run("a token short a permission says which one", func(t *testing.T) {
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{TrafficAccess: github.AccessTokenLacks}, 100))
		for _, want := range []string{"Traffic (14 days)", "Administration (read)", "repo scope"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})

	t.Run("a failure says why, without the HTML", func(t *testing.T) {
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{
			TrafficAccess: github.AccessFailed,
			TrafficErr:    errors.New("GitHub answered 502 body: <html>bad gateway</html>"),
		}, 100))
		if !strings.Contains(out, "502") || strings.Contains(out, "<html>") || !strings.Contains(out, "r reloads") {
			t.Errorf("got:\n%s", out)
		}
	})

	t.Run("a measured zero is said, not hidden", func(t *testing.T) {
		out := ansi.Strip(repoDetailTraffic(&github.RepoDetail{TrafficAccess: github.AccessOK, Traffic: &github.Traffic{}}, 100))
		if !strings.Contains(out, "No views or clones") {
			t.Errorf("got %q", out)
		}
	})

	t.Run("in the drill-in body", func(t *testing.T) {
		rd := newLoadedRepoDetail(&github.RepoDetail{
			Owner: "gfazioli", Name: "octoscope", URL: "https://github.com/gfazioli/octoscope",
			TrafficAccess: github.AccessOK, Traffic: traffic,
		})
		if body := ansi.Strip(rd.computeBody(100)); !strings.Contains(body, "Traffic (14 days)") || !strings.Contains(body, "1,701 · 323 unique") {
			t.Errorf("the drill-in does not paint the section:\n%s", body)
		}
	})
}
