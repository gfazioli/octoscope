package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

func sweepFixture() []github.SweepResult {
	scan := func(v github.ScanVerdict, score int, findings ...github.Finding) *github.RepoScan {
		return &github.RepoScan{DefaultBranch: "main", ScannedDefault: true, Verdict: v, Score: score, Findings: findings}
	}
	return []github.SweepResult{
		{Target: github.SweepTarget{Owner: "me", Name: "clean", URL: "https://github.com/me/clean"}, Scan: scan(github.VerdictClean, 0)},
		{Target: github.SweepTarget{Owner: "me", Name: "flagged", URL: "https://github.com/me/flagged"}, Scan: scan(github.VerdictSuspicious, 5,
			github.Finding{Axis: github.AxisProvenance, Weight: 5, Reason: "tip abc1234 forged as \"github-actions[bot]\" but not signed by GitHub"},
			github.Finding{Axis: github.AxisDelta, Weight: 0, Reason: "no previous scan to compare against"})},
		{Target: github.SweepTarget{Owner: "me", Name: "empty", URL: "https://github.com/me/empty"}, NotScanned: "the repository has no commits yet"},
		{Target: github.SweepTarget{Owner: "acme", Name: "lib", URL: "https://github.com/acme/lib"}, NotScanned: "github rest 404 Not Found:\n<html>…</html>"},
	}
}

func TestFromSweep(t *testing.T) {
	s := FromSweep(sweepFixture(), "0.38.0", time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC), 15900*time.Millisecond, false)
	if s.Summary != (SweepSummary{Repositories: 4, Suspicious: 1, Clean: 1, NotScanned: 2}) {
		t.Errorf("summary = %+v", s.Summary)
	}
	if got := s.Summary.LikelyCompromised + s.Summary.Suspicious + s.Summary.Watch + s.Summary.Clean + s.Summary.NotScanned; got != s.Summary.Repositories {
		t.Errorf("counts add to %d, want %d: every repository in exactly one", got, s.Summary.Repositories)
	}
	if s.Scope != "default_branch" || s.ElapsedSeconds != 15.9 {
		t.Errorf("scope %q, elapsed %v", s.Scope, s.ElapsedSeconds)
	}
	flagged := s.Repositories[1]
	if len(flagged.Findings) != 1 || flagged.Findings[0].Weight != 5 {
		t.Errorf("findings = %+v; want the scored evidence only, not the weight-0 context", flagged.Findings)
	}
	gone := s.Repositories[3]
	if gone.Scanned || gone.Score != nil || gone.Verdict != "" || strings.Contains(gone.Reason, "\n") {
		t.Errorf("unscanned = %+v; want no verdict, no score, and a one-line reason", gone)
	}
}

func TestRenderSweepJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderSweepJSON(&buf, FromSweep(sweepFixture(), "0.38.0", time.Now(), time.Second, false)); err != nil {
		t.Fatalf("RenderSweepJSON: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "null") {
		t.Errorf("a list came out null:\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	repos := doc["repositories"].([]any)
	clean := repos[0].(map[string]any)
	if clean["score"] != float64(0) || clean["verdict"] != "clean" {
		t.Errorf("a clean repository = %v; want score 0 present, not omitted", clean)
	}
	empty := repos[2].(map[string]any)
	if _, ok := empty["score"]; ok {
		t.Errorf("an unscanned repository carries a score: %v", empty)
	}
}

func TestRenderSweepPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderSweepPlain(&buf, FromSweep(sweepFixture(), "0.38.0", time.Now(), time.Second, true)); err != nil {
		t.Fatalf("RenderSweepPlain: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"4 repositories · default branch only · public-only",
		"  suspicious            1",
		"  not scanned           2",
		"Suspicious\n  me/flagged   score 5\n    +5 provenance  tip abc1234",
		"Not scanned",
		"me/empty", "the repository has no commits yet",
		"no comparison with earlier scans",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "me/clean") {
		t.Errorf("a clean repository is listed; it should only be counted:\n%s", out)
	}
	if strings.Contains(out, "no previous scan") {
		t.Errorf("weight-0 context reached the plain report:\n%s", out)
	}
}
