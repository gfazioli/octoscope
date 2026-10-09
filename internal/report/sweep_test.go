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
		{Target: github.SweepTarget{Owner: "me", Name: "hooked", URL: "https://github.com/me/hooked"}, Scan: scan(github.VerdictWatch, 2,
			github.Finding{Axis: github.AxisIgnition, Path: ".claude/settings.json", Weight: 2, Reason: "AI-agent / editor session hook — Claude session hooks run on SessionStart"})},
		{Target: github.SweepTarget{Owner: "me", Name: "empty", URL: "https://github.com/me/empty"}, NotScanned: "the repository has no commits yet"},
		{Target: github.SweepTarget{Owner: "acme", Name: "lib", URL: "https://github.com/acme/lib"}, NotScanned: "github rest 404 Not Found:\n<html>…</html>"},
	}
}

func TestFromSweep(t *testing.T) {
	s := FromSweep(sweepFixture(), "0.38.0", time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC), 15900*time.Millisecond, false)
	if s.Summary != (SweepSummary{Repositories: 5, Suspicious: 1, Watch: 1, Clean: 1, NotScanned: 2}) {
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
	gone := s.Repositories[4]
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
	empty := repos[3].(map[string]any)
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
		"5 repositories · default branch only · public-only",
		"  suspicious            1",
		"  not scanned           2",
		"Suspicious\n  me/flagged   score 5\n    +5 provenance  tip abc1234",
		"Watch\n  me/hooked   score 2\n    +2 ignition  .claude/settings.json: AI-agent / editor session hook",
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

// The JSON's verdict is the summary's key for it, so a script matches
// "likely_compromised" in both places; the plain report keeps the words.
func TestSweepVerdictToken(t *testing.T) {
	results := []github.SweepResult{{
		Target: github.SweepTarget{Owner: "me", Name: "worm"},
		Scan:   &github.RepoScan{DefaultBranch: "main", ScannedDefault: true, Verdict: github.VerdictCompromised, Score: 12},
	}}
	s := FromSweep(results, "0.38.0", time.Now(), time.Second, false)
	if got := s.Repositories[0].Verdict; got != "likely_compromised" {
		t.Errorf("verdict = %q, want likely_compromised", got)
	}
	var buf bytes.Buffer
	if err := RenderSweepPlain(&buf, s); err != nil {
		t.Fatalf("RenderSweepPlain: %v", err)
	}
	if !strings.Contains(buf.String(), "\nLikely compromised\n  me/worm   score 12") {
		t.Errorf("plain report:\n%s", buf.String())
	}
}
