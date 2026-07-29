package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestPRDetailChecksWiring covers the PR side of the shared checks
// renderer. The repo drill-in has its own tests and renderCheckList has
// unit tests, but neither would catch a PR-specific wiring regression:
// passing 0 where ChecksTotal belongs, or reading the wrong field, would
// leave both green while PR overflow counts and hyperlinks quietly broke.
func TestPRDetailChecksWiring(t *testing.T) {
	_ = applyTheme("octoscope", "")

	d := &github.PRDetail{
		ChecksState: "FAILURE",
		ChecksContexts: []github.CheckSummary{
			{Name: "lint", Conclusion: "SUCCESS"},
			{Name: "e2e", Conclusion: "FAILURE", URL: "https://github.com/o/r/actions/runs/5"},
		},
		ChecksTotal: 17,
	}

	if got := ansi.Strip(prDetailChecksSummary(d)); got != "failing" {
		t.Errorf("summary = %q, want %q", got, "failing")
	}

	out := prDetailChecks(d, 80)
	plain := ansi.Strip(out)

	if first := strings.SplitN(plain, "\n", 2)[0]; !strings.Contains(first, "e2e") {
		t.Errorf("the failing check should lead the list, got %q", first)
	}
	// 17 exist, 2 fetched and shown -> 15 hidden. Counting against the
	// fetched slice instead would say nothing at all.
	if !strings.Contains(plain, "+15 more") {
		t.Errorf("overflow must come from ChecksTotal, not len(ChecksContexts):\n%s", plain)
	}
	if !strings.Contains(out, ansi.SetHyperlink("https://github.com/o/r/actions/runs/5")) {
		t.Error("the failing check's run should be an OSC 8 target in the PR section too")
	}
}

// TestPRDetailChecksEmpty keeps the section droppable: no rollup means no
// summary, which is how the PR view decides to omit the heading.
func TestPRDetailChecksEmpty(t *testing.T) {
	_ = applyTheme("octoscope", "")

	d := &github.PRDetail{}
	if got := prDetailChecksSummary(d); got != "" {
		t.Errorf("summary = %q, want empty for a PR with no rollup", got)
	}
	if got := prDetailChecks(d, 80); got != "" {
		t.Errorf("list = %q, want empty for a PR with no checks", got)
	}
}
