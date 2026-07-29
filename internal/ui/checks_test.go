package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestRenderCheckListFailuresFirst is the ordering guarantee that makes
// the section useful: with the visible list capped, a failure buried in
// a long build matrix must still show, because a red check is the reason
// someone drilled in at all.
func TestRenderCheckListFailuresFirst(t *testing.T) {
	_ = applyTheme("octoscope", "")

	var checks []github.CheckSummary
	for i := 0; i < checksMaxVisible; i++ {
		checks = append(checks, github.CheckSummary{
			Name:       "passing-job-" + string(rune('a'+i)),
			Conclusion: "SUCCESS",
		})
	}
	// The one that matters: last in API order, past the cap.
	checks = append(checks, github.CheckSummary{
		Name:       "govulncheck",
		Conclusion: "FAILURE",
		URL:        "https://github.com/o/r/actions/runs/1",
	})

	out := ansi.Strip(renderCheckList(checks, len(checks), 60))
	if !strings.Contains(out, "govulncheck") {
		t.Fatalf("the failing check fell off the visible list:\n%s", out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "govulncheck") {
		t.Errorf("failing check is not on the first line, got %q", first)
	}
	if !strings.Contains(out, "+1 more") {
		t.Errorf("overflow count missing — the total stops being honest:\n%s", out)
	}
}

// TestRenderCheckListStablePerBucket asserts the API's order survives
// within a bucket, so a workflow's jobs stay grouped rather than being
// shuffled by the sort.
func TestRenderCheckListStablePerBucket(t *testing.T) {
	_ = applyTheme("octoscope", "")

	checks := []github.CheckSummary{
		{Name: "lint", Conclusion: "SUCCESS"},
		{Name: "test", Conclusion: "SUCCESS"},
		{Name: "build", Conclusion: "SUCCESS"},
	}
	out := ansi.Strip(renderCheckList(checks, len(checks), 60))
	if strings.Index(out, "lint") > strings.Index(out, "test") ||
		strings.Index(out, "test") > strings.Index(out, "build") {
		t.Errorf("stable order lost within a bucket:\n%s", out)
	}
}

// TestRenderCheckListLinksOnlyVouchedURLs keeps the security gate wired
// into the render path: a github.com run URL becomes clickable, a
// third-party CI URL stays inert text. The visible output is identical
// either way — only the escapes differ.
func TestRenderCheckListLinksOnlyVouchedURLs(t *testing.T) {
	_ = applyTheme("octoscope", "")

	checks := []github.CheckSummary{
		{Name: "github-job", Conclusion: "FAILURE", URL: "https://github.com/o/r/actions/runs/1"},
		{Name: "vendor-job", Conclusion: "FAILURE", URL: "https://circleci.com/gh/o/r/1"},
		{Name: "no-url-job", Conclusion: "FAILURE"},
	}
	out := renderCheckList(checks, len(checks), 60)

	if !strings.Contains(out, ansi.SetHyperlink("https://github.com/o/r/actions/runs/1")) {
		t.Error("the github.com run URL should be an OSC 8 target")
	}
	if strings.Contains(out, "circleci.com") {
		t.Error("a non-github.com URL leaked into the output — it must render as plain text only")
	}
	for _, name := range []string{"github-job", "vendor-job", "no-url-job"} {
		if !strings.Contains(ansi.Strip(out), name) {
			t.Errorf("check %q missing from the visible output", name)
		}
	}
}

// TestRenderCheckListOverflowCountsTheRealTotal is the honesty
// guarantee for the "+N more" line. The rollup can hold more checks than
// the query fetches — charmbracelet/bubbletea runs 41 — so counting
// overflow against the fetched slice would under-report. Here 50 are
// fetched out of a real 41+9: with 8 shown, the line has to say what is
// actually hidden, not what happens to be in memory.
func TestRenderCheckListOverflowCountsTheRealTotal(t *testing.T) {
	_ = applyTheme("octoscope", "")

	var checks []github.CheckSummary
	for i := 0; i < 20; i++ {
		checks = append(checks, github.CheckSummary{Name: "job", Conclusion: "SUCCESS"})
	}

	// 41 exist, 20 came back, 8 are shown -> 33 hidden.
	out := ansi.Strip(renderCheckList(checks, 41, 60))
	if !strings.Contains(out, "+33 more") {
		t.Errorf("overflow must count against the rollup total (41), not the fetched slice (20):\n%s", out)
	}

	// A total that under-reports the slice must not produce a negative
	// or missing count — the fetched length is the floor.
	out = ansi.Strip(renderCheckList(checks, 0, 60))
	if !strings.Contains(out, "+12 more") {
		t.Errorf("a stale total must fall back to the fetched length:\n%s", out)
	}
}

// TestRenderCheckListEmpty keeps the caller's "no section at all"
// contract intact: an empty list renders nothing, so the repo drill-in
// can omit the heading instead of printing an orphan.
func TestRenderCheckListEmpty(t *testing.T) {
	if got := renderCheckList(nil, 0, 60); got != "" {
		t.Errorf("renderCheckList(nil) = %q, want empty", got)
	}
}

// TestChecksRollupSummary covers the states GitHub actually returns,
// including EXPECTED — a required check that has not reported yet, which
// reads as pending rather than as an unknown state.
func TestChecksRollupSummary(t *testing.T) {
	_ = applyTheme("octoscope", "")

	tests := []struct {
		state string
		want  string
	}{
		{"", ""},
		{"SUCCESS", "all passing"},
		{"FAILURE", "failing"},
		{"PENDING", "pending"},
		{"EXPECTED", "pending"},
		{"ERROR", "errored"},
		{"SOMETHING_NEW", "something_new"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := ansi.Strip(checksRollupSummary(tt.state)); got != tt.want {
				t.Errorf("checksRollupSummary(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestCheckMarkerGlyphsAreDistinct is the monochromatic-theme
// guarantee: the outcomes must be distinguishable by glyph alone, since
// under monochrome / phosphor / amber every style resolves to one tone
// and colour carries no information at all.
func TestCheckMarkerGlyphsAreDistinct(t *testing.T) {
	for _, theme := range []string{"octoscope", "monochrome"} {
		t.Run(theme, func(t *testing.T) {
			_ = applyTheme(theme, "")

			glyphs := map[string]string{}
			for name, c := range map[string]github.CheckSummary{
				"passed":  {Conclusion: "SUCCESS"},
				"failed":  {Conclusion: "FAILURE"},
				"startup": {Conclusion: "STARTUP_FAILURE"},
				"neutral": {Conclusion: "SKIPPED"},
				"running": {Status: "IN_PROGRESS"},
			} {
				glyphs[name] = ansi.Strip(checkMarker(c))
				if glyphs[name] == "" {
					t.Errorf("%s produced no glyph", name)
				}
			}

			if glyphs["passed"] == glyphs["failed"] {
				t.Error("pass and fail share a glyph — indistinguishable without colour")
			}
			if glyphs["failed"] != glyphs["startup"] {
				t.Errorf("STARTUP_FAILURE (%q) should read as a failure (%q), not as pending",
					glyphs["startup"], glyphs["failed"])
			}
			if glyphs["running"] == glyphs["neutral"] {
				t.Error("in-flight and neutral share a glyph")
			}
		})
	}
}
