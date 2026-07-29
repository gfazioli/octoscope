package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gfazioli/octoscope/internal/github"
)

// Rendering for CI status-check lists, shared by the PR drill-in
// (checks on the head commit) and the repo drill-in (checks on the
// default branch's tip). Both answer the same question — "what is the
// state of CI, and if it's red, which check is red" — so they render
// identically rather than drifting into two dialects.

// checksMaxVisible caps how many check rows a section paints. Deep
// enough to cover a normal matrix build, shallow enough that the
// section stays a summary rather than becoming the whole view; the
// overflow count keeps the total honest.
const checksMaxVisible = 8

// checkMarker renders the per-check status glyph. SUCCESS = ✓ (ok);
// failure-flavoured conclusions = ✗; muted neutrals = ·; everything
// pending or in flight = ⏳.
//
// The failure-flavoured set spans both CheckRun conclusions (FAILURE /
// TIMED_OUT / CANCELLED / ACTION_REQUIRED / STARTUP_FAILURE) and
// StatusContext states (FAILURE / ERROR). Keeping them in one case
// avoids treating STARTUP_FAILURE as "still pending" — a bot whose CI
// startup blew up is firmly in the red zone.
//
// The glyphs carry the meaning, not the colour, which is what makes
// this readable under the monochromatic themes: ✓ / ✗ / · / ⏳ stay
// distinct when every style resolves to the same tone.
func checkMarker(c github.CheckSummary) string {
	switch checkOutcome(c) {
	case checkPassed:
		return okStyle.Render("✓")
	case checkFailed:
		return errorStyle.Render("✗")
	case checkNeutral:
		return mutedStyle.Render("·")
	default:
		return warnStyle.Render("⏳")
	}
}

// checkOutcome collapses a check's conclusion / status pair into the
// four buckets the UI actually distinguishes. Conclusion wins when
// present (a completed check), status is the fallback (still running).
func checkOutcome(c github.CheckSummary) checkBucket {
	state := c.Conclusion
	if state == "" {
		state = c.Status
	}
	switch state {
	case "SUCCESS":
		return checkPassed
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return checkFailed
	case "NEUTRAL", "SKIPPED", "STALE":
		return checkNeutral
	default: // PENDING, IN_PROGRESS, QUEUED, WAITING, REQUESTED, COMPLETED-with-no-conclusion
		return checkRunning
	}
}

type checkBucket int

// Ordered by how much the user needs to see it: a failure first, a
// still-running check next, then the ones that need no attention. The
// ordering is load-bearing — see renderCheckList.
const (
	checkFailed checkBucket = iota
	checkRunning
	checkNeutral
	checkPassed
)

// checksRollupSummary renders the inline summary that sits next to a
// "Checks" heading: the aggregate rollup state. Returns "" when the
// repo or PR has no checks at all, so the caller can drop the whole
// section instead of heading an empty list.
func checksRollupSummary(state string) string {
	switch state {
	case "":
		return ""
	case "SUCCESS":
		return okStyle.Render("all passing")
	case "FAILURE":
		return errorStyle.Render("failing")
	case "PENDING", "EXPECTED":
		return warnStyle.Render("pending")
	case "ERROR":
		return errorStyle.Render("errored")
	default:
		return mutedStyle.Render(strings.ToLower(state))
	}
}

// renderCheckList renders one line per check: status glyph, then the
// check name linked to wherever that check reports.
//
// Failures are floated to the top. That is not cosmetic — with the
// visible list capped, a red check on a repo with a 20-job matrix
// could otherwise land in the "+N more" tail, which would hide the one
// row the user drilled in to find. Within a bucket the API's order is
// preserved (a stable sort), since that order groups a workflow's jobs
// together.
//
// Names are linked through githubHyperlink, which silently declines
// anything that isn't an https github.com URL — a third-party CI
// provider reporting through the Checks API points at its own
// dashboard, and those render as plain text rather than becoming
// one-click targets from a GitHub-looking row.
func renderCheckList(checks []github.CheckSummary, width int) string {
	if len(checks) == 0 {
		return ""
	}

	ordered := make([]github.CheckSummary, len(checks))
	copy(ordered, checks)
	sort.SliceStable(ordered, func(i, j int) bool {
		return checkOutcome(ordered[i]) < checkOutcome(ordered[j])
	})

	overflow := 0
	if len(ordered) > checksMaxVisible {
		overflow = len(ordered) - checksMaxVisible
		ordered = ordered[:checksMaxVisible]
	}

	var lines []string
	for _, c := range ordered {
		// width-12 leaves room for the two-space indent, the glyph
		// and its separator; truncate happens before linking so the
		// OSC 8 escapes never get cut mid-sequence.
		name := truncate(c.Name, width-12)
		lines = append(lines, "  "+checkMarker(c)+"  "+githubHyperlink(c.URL, name))
	}
	if overflow > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("    +%d more", overflow)))
	}
	return strings.Join(lines, "\n")
}
