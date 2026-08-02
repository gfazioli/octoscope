package github

import (
	"strings"
	"testing"
	"time"
)

func TestDetectPushBurst(t *testing.T) {
	base := time.Date(2026, 6, 3, 22, 38, 0, 0, time.UTC)
	mk := func(name string, offset time.Duration) Repo {
		return Repo{Name: name, PushedAt: base.Add(offset)}
	}

	tests := []struct {
		name      string
		repos     []Repo
		wantBurst bool
		wantCount int
	}{
		{
			name: "five repos within 49s is a burst",
			repos: []Repo{
				mk("a", 0),
				mk("b", 10*time.Second),
				mk("c", 20*time.Second),
				mk("d", 35*time.Second),
				mk("e", 49*time.Second),
			},
			wantBurst: true,
			wantCount: 5,
		},
		{
			name: "two repos never reaches the minimum",
			repos: []Repo{
				mk("a", 0),
				mk("b", 5*time.Second),
			},
			wantBurst: false,
		},
		{
			name: "three repos spread over a day is not a burst",
			repos: []Repo{
				mk("a", 0),
				mk("b", 8*time.Hour),
				mk("c", 20*time.Hour),
			},
			wantBurst: false,
		},
		{
			name: "tight trio inside a quiet history is a burst",
			repos: []Repo{
				mk("old1", -200*time.Hour),
				mk("old2", -100*time.Hour),
				mk("a", 0),
				mk("b", 15*time.Second),
				mk("c", 40*time.Second),
			},
			wantBurst: true,
			wantCount: 3,
		},
		{
			name: "repos with zero PushedAt are ignored",
			repos: []Repo{
				{Name: "never"},
				{Name: "never2"},
				mk("a", 0),
				mk("b", 10*time.Second),
			},
			wantBurst: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectPushBurst(tt.repos, pushBurstMinRepos, pushBurstWindow)
			if ok != tt.wantBurst {
				t.Fatalf("DetectPushBurst ok = %v, want %v (got %+v)", ok, tt.wantBurst, got)
			}
			if ok && got.Count != tt.wantCount {
				t.Errorf("burst count = %d, want %d", got.Count, tt.wantCount)
			}
		})
	}
}

func TestMatchIgnition(t *testing.T) {
	tests := []struct {
		path      string
		wantMatch bool
		wantClass ignitionClass
	}{
		{".github/setup.js", true, classDropper},
		{".claude/settings.json", true, classAgentHook},
		{".cursor/rules/setup.mdc", true, classAgentInstr},
		{".github/copilot-instructions.md", true, classAgentInstr},
		{".github/workflows/ci.yml", true, classCI},
		{".vscode/tasks.json", true, classEditorTask},
		{"package.json", true, classPackage},
		{".devcontainer/devcontainer.json", true, classDevcontain},
		// Nested path must NOT match a single-segment glob.
		{".github/workflows/nested/ci.yml", false, ""},
		{"src/index.js", false, ""},
		{"README.md", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rule, ok := matchIgnition(tt.path)
			if ok != tt.wantMatch {
				t.Fatalf("matchIgnition(%q) ok = %v, want %v", tt.path, ok, tt.wantMatch)
			}
			if ok && rule.Class != tt.wantClass {
				t.Errorf("class = %q, want %q", rule.Class, tt.wantClass)
			}
		})
	}
}

func TestShannonEntropy(t *testing.T) {
	if e := shannonEntropy(nil); e != 0 {
		t.Errorf("entropy(nil) = %v, want 0", e)
	}
	if e := shannonEntropy([]byte("aaaaaaaa")); e != 0 {
		t.Errorf("entropy(uniform byte) = %v, want 0", e)
	}
	// Two equally-likely symbols → 1 bit/byte.
	if e := shannonEntropy([]byte("abababab")); e < 0.99 || e > 1.01 {
		t.Errorf("entropy(two symbols) = %v, want ~1.0", e)
	}
}

func TestLooksObfuscated(t *testing.T) {
	benign := []byte(`{"hooks":{"SessionStart":"echo hello"}}`)
	if m := looksObfuscated(benign); len(m) != 0 {
		t.Errorf("benign config flagged with markers %v", m)
	}

	packed := []byte(`const x = eval(atob("ZWNobyBwd25lZA==")); require("child_process").execSync(x)`)
	m := looksObfuscated(packed)
	joined := strings.Join(m, " | ")
	for _, want := range []string{"eval", "base64", "child process"} {
		if !strings.Contains(joined, want) {
			t.Errorf("packed payload missing marker %q (got %v)", want, m)
		}
	}

	longLine := []byte("var a=" + strings.Repeat("0123456789", 250) + ";")
	if m := looksObfuscated(longLine); len(m) == 0 {
		t.Errorf("very long single line not flagged")
	}
}

func TestIsTextContent(t *testing.T) {
	if !isTextContent([]byte("plain ascii config\n")) {
		t.Error("ascii not recognised as text")
	}
	if !isTextContent(nil) {
		t.Error("empty content should count as text")
	}
	if isTextContent([]byte{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe}) {
		t.Error("binary content recognised as text")
	}
}

// --- engine: verdict matrix ---------------------------------------------

func provBranch(name string, isDefault bool) BranchProvenance {
	return BranchProvenance{
		Name:           name,
		IsDefault:      isDefault,
		TipOID:         "deadbeefcafebabe",
		Signed:         true,
		SignedByGitHub: false,
	}
}

func TestEvaluateScanClean(t *testing.T) {
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: provBranch("main", true),
				Matches: []ignitionMatch{
					{Path: "package.json", Size: 1200, BlobSHA: "p", Rule: ignitionRule{Glob: "package.json", Class: classPackage, Weight: 0}},
					{Path: ".vscode/tasks.json", Size: 800, BlobSHA: "v", Rule: ignitionRule{Glob: ".vscode/tasks.json", Class: classEditorTask, Weight: 0}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"p": {Size: 1200, Fetched: true, IsText: true},
			"v": {Size: 800, Fetched: true, IsText: true},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictClean {
		t.Fatalf("verdict = %v, want clean (score %d, findings %+v)", got.Verdict, got.Score, got.Findings)
	}
	if got.Score != 0 {
		t.Errorf("score = %d, want 0", got.Score)
	}
	if len(got.IgnitionInventory()) != 2 {
		t.Errorf("inventory = %d, want 2", len(got.IgnitionInventory()))
	}
	if len(got.ScoredFindings()) != 0 {
		t.Errorf("scored findings = %d, want 0", len(got.ScoredFindings()))
	}
}

func TestEvaluateScanWatch(t *testing.T) {
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: provBranch("main", true),
				Matches: []ignitionMatch{
					{Path: ".claude/settings.json", Size: 400, BlobSHA: "c", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
					{Path: ".gemini/settings.json", Size: 300, BlobSHA: "g", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"c": {Size: 400, Fetched: true, IsText: true},
			"g": {Size: 300, Fetched: true, IsText: true},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictWatch {
		t.Fatalf("verdict = %v, want watch (score %d)", got.Verdict, got.Score)
	}
}

// --- Axis 4: capability escalation ---------------------------------------

// capInput builds a scan of one CI workflow with the given facts.
func capInput(path string, wf *workflowFacts) scanInput {
	return scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{{
			Prov: provBranch("main", true),
			Matches: []ignitionMatch{
				{Path: path, Size: 900, BlobSHA: "w", Rule: ignitionRule{Class: classCI, Weight: 0}},
			},
		}},
		Blobs: map[string]blobAnalysis{
			"w": {Size: 900, Fetched: true, IsText: true, Workflow: wf},
		},
		Now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}
}

func capFindings(s *RepoScan) []Finding {
	var out []Finding
	for _, f := range s.Findings {
		if f.Axis == AxisCapability {
			out = append(out, f)
		}
	}
	return out
}

func TestEvaluateScanCapability(t *testing.T) {
	tests := []struct {
		name        string
		wf          *workflowFacts
		wantWeight  int
		wantContain string
	}{
		{
			// The false positive this axis must not produce: octoscope's
			// own release workflow. Powerful, secret-reading, correct.
			name:        "power on a trusted trigger is inventory only",
			wf:          &workflowFacts{WritePerms: []string{"contents: write"}, UsesSecrets: true},
			wantWeight:  0,
			wantContain: "reachable only from its own triggers",
		},
		{
			name:        "fork trigger with secrets scores",
			wf:          &workflowFacts{ForkTriggers: []string{"pull_request_target"}, UsesSecrets: true},
			wantWeight:  wCapEscalation,
			wantContain: "while holding the repository's secrets",
		},
		{
			name:        "fork trigger with write permissions scores",
			wf:          &workflowFacts{ForkTriggers: []string{"workflow_run"}, WritePerms: []string{"contents: write"}},
			wantWeight:  wCapEscalation,
			wantContain: "contents: write",
		},
		{
			// A fork trigger by itself is a label bot's normal life.
			name:        "bare fork trigger is inventory only",
			wf:          &workflowFacts{ForkTriggers: []string{"issue_comment"}},
			wantWeight:  0,
			wantContain: "holds no secrets or write scopes",
		},
		{
			// write-all on a trusted trigger is untidy, not dangerous.
			// Scoring it broke the capability-alone rule: five such
			// workflows reached Suspicious between them.
			name:        "write-all on a trusted trigger is inventory",
			wf:          &workflowFacts{WritePerms: []string{"write-all"}},
			wantWeight:  0,
			wantContain: "write-all rather than the scopes it needs",
		},
		{
			name:        "write-all a fork trigger can reach does score",
			wf:          &workflowFacts{ForkTriggers: []string{"pull_request_target"}, WritePerms: []string{"write-all"}},
			wantWeight:  wCapEscalation + wCapWriteAll,
			wantContain: "write-all rather than the scopes it needs",
		},
		{
			// Not understood must never pass for checked-and-clean.
			name:        "unparseable workflow says it was not checked",
			wf:          &workflowFacts{Unparsed: true},
			wantWeight:  0,
			wantContain: "could not be parsed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateScan(capInput(".github/workflows/x.yml", tt.wf))
			fs := capFindings(got)
			if len(fs) == 0 {
				t.Fatalf("no capability finding produced")
			}
			total, joined := 0, ""
			for _, f := range fs {
				total += f.Weight
				joined += f.Reason + "\n"
			}
			if total != tt.wantWeight {
				t.Errorf("capability weight = %d, want %d (%s)", total, tt.wantWeight, joined)
			}
			if !strings.Contains(joined, tt.wantContain) {
				t.Errorf("reason %q does not mention %q", joined, tt.wantContain)
			}
		})
	}
}

// The same workflow on many branches is one fact about the repository,
// not one per branch — otherwise a repo with ten branches scores ten
// times for a single file.
func TestCapabilityReportedOncePerPath(t *testing.T) {
	wf := &workflowFacts{ForkTriggers: []string{"pull_request_target"}, UsesSecrets: true}
	in := capInput(".github/workflows/x.yml", wf)
	extra := scanBranch{
		Prov: provBranch("next", false),
		Matches: []ignitionMatch{
			{Path: ".github/workflows/x.yml", Size: 900, BlobSHA: "w", Rule: ignitionRule{Class: classCI, Weight: 0}},
		},
	}
	in.Branches = append(in.Branches, extra)
	in.BranchesTotal = 2

	got := evaluateScan(in)
	if n := len(capFindings(got)); n != 1 {
		t.Errorf("capability findings = %d, want 1: %+v", n, capFindings(got))
	}
}

// The invariant #67 asks to preserve: no single axis can reach a high
// tier alone. Capability is the newest and therefore the one at risk of
// having thresholds loosened to make it visible.
func TestCapabilityAloneCannotReachSuspicious(t *testing.T) {
	// The worst a workflow can do on this axis: a fork trigger holding
	// secrets AND write-all.
	worst := &workflowFacts{
		ForkTriggers: []string{"pull_request_target", "workflow_run"},
		WritePerms:   []string{"write-all"},
		UsesSecrets:  true,
	}
	got := evaluateScan(capInput(".github/workflows/x.yml", worst))
	if got.Verdict >= VerdictSuspicious {
		t.Errorf("capability alone reached %v with score %d — no single axis may do that", got.Verdict, got.Score)
	}
	if got.Score == 0 {
		t.Error("the worst capability shape should still score something")
	}
	t.Logf("worst capability-only shape: score %d, verdict %v", got.Score, got.Verdict)
}

// --- delta: what changed since the recorded baseline ---------------------

// deltaInput builds a scan of one branch carrying one *scoring* ignition
// path, so the delta axis has something it will actually diff.
func deltaInput(branch, path, blobSHA string, signed bool, baseline *ScanFingerprint, now time.Time) scanInput {
	prov := provBranch(branch, true)
	prov.Signed = signed
	prov.SignedByGitHub = false
	return scanInput{
		Owner: "o", Name: "r", DefaultBranch: branch,
		BranchesTotal: 1,
		Branches: []scanBranch{{
			Prov: prov,
			Matches: []ignitionMatch{
				{Path: path, Size: 400, BlobSHA: blobSHA, Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
			},
		}},
		Blobs:    map[string]blobAnalysis{blobSHA: {Size: 400, Fetched: true, IsText: true}},
		Now:      now,
		Baseline: baseline,
	}
}

func deltaFindings(s *RepoScan) []Finding {
	var out []Finding
	for _, f := range s.Findings {
		if f.Axis == AxisDelta {
			out = append(out, f)
		}
	}
	return out
}

func TestEvaluateScanDelta(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-48 * time.Hour)
	ancient := now.Add(-120 * 24 * time.Hour)
	const branch, path = "main", ".claude/settings.json"
	key := fingerprintKey(branch, path)

	base := func(capturedAt time.Time, ign map[string]string, signed map[string]bool, verdict string) *ScanFingerprint {
		return &ScanFingerprint{CapturedAt: capturedAt, Verdict: verdict, Ignition: ign, Signed: signed}
	}

	tests := []struct {
		name        string
		in          scanInput
		wantCount   int
		wantWeight  int // total weight contributed by delta findings
		wantContain string
	}{
		{
			// First ever scan: must SAY it has nothing to compare
			// against, so silence can never read as "nothing changed".
			name:        "no baseline announces itself",
			in:          deltaInput(branch, path, "aaa", true, nil, now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "no previous scan",
		},
		{
			name: "unchanged surface produces no delta",
			in: deltaInput(branch, path, "aaa", true,
				base(fresh, map[string]string{key: "aaa"}, map[string]bool{branch: true}, "clean"), now),
			wantCount:  0,
			wantWeight: 0,
		},
		{
			// The headline signal: something that auto-executes appeared.
			name: "new ignition path scores",
			in: deltaInput(branch, path, "aaa", true,
				base(fresh, map[string]string{}, map[string]bool{branch: true}, "clean"), now),
			wantCount:   1,
			wantWeight:  wDeltaNewIgnition,
			wantContain: "appeared",
		},
		{
			name: "changed content on a known path scores",
			in: deltaInput(branch, path, "bbb", true,
				base(fresh, map[string]string{key: "aaa"}, map[string]bool{branch: true}, "clean"), now),
			wantCount:   1,
			wantWeight:  wDeltaChangedIgnition,
			wantContain: "changed",
		},
		{
			name: "signature regression scores",
			in: deltaInput(branch, path, "aaa", false,
				base(fresh, map[string]string{key: "aaa"}, map[string]bool{branch: true}, "clean"), now),
			wantCount:   1,
			wantWeight:  wDeltaSignedRegressed,
			wantContain: "no longer is",
		},
		{
			// Gaining a signature is an improvement, not a signal.
			name: "signature improvement is silent",
			in: deltaInput(branch, path, "aaa", true,
				base(fresh, map[string]string{key: "aaa"}, map[string]bool{branch: false}, "clean"), now),
			wantCount:  0,
			wantWeight: 0,
		},
		{
			// A branch the baseline never saw is new work, not an
			// appearance — otherwise every new feature branch alarms.
			name: "path on a branch the baseline never saw is ignored",
			in: deltaInput(branch, path, "aaa", true,
				base(fresh, map[string]string{}, map[string]bool{"other": true}, "clean"), now),
			wantCount:  0,
			wantWeight: 0,
		},
		{
			// Stale baselines still report, but must not score: a
			// months-old diff is mostly legitimate drift.
			name: "stale baseline reports without scoring",
			in: deltaInput(branch, path, "aaa", true,
				base(ancient, map[string]string{}, map[string]bool{branch: true}, "clean"), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "without affecting the verdict",
		},
		{
			// A store entry with no captured_at decodes to the zero
			// time, and measuring an age from it yields ~739969 days.
			// That must not be dressed up as a very old baseline: it is
			// an unknown age, and saying so is the honest reading.
			name: "baseline with no capture time says the age is unknown",
			in: deltaInput(branch, path, "aaa", true,
				base(time.Time{}, map[string]string{}, map[string]bool{branch: true}, "clean"), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "age is unknown",
		},
		{
			// "Nothing changed" on top of an already-bad baseline is not
			// a clean bill of health, and the report has to say so.
			name: "baseline taken while already flagged is disclosed",
			in: deltaInput(branch, path, "aaa", true,
				base(fresh, map[string]string{key: "aaa"}, map[string]bool{branch: true}, "likely compromised"), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "already",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateScan(tt.in)
			ds := deltaFindings(got)
			if len(ds) != tt.wantCount {
				t.Fatalf("delta findings = %d, want %d: %+v", len(ds), tt.wantCount, ds)
			}
			total := 0
			joined := ""
			for _, d := range ds {
				total += d.Weight
				joined += d.Reason + "\n"
			}
			if total != tt.wantWeight {
				t.Errorf("delta weight = %d, want %d (%s)", total, tt.wantWeight, joined)
			}
			if tt.wantContain != "" && !strings.Contains(joined, tt.wantContain) {
				t.Errorf("delta reason %q does not mention %q", joined, tt.wantContain)
			}
		})
	}
}

// The fingerprint the caller persists must describe this scan: scoring
// ignition paths with their blob OIDs, the per-branch signature state,
// and the verdict reached — the last so a later scan can tell it was
// baselined against an already-flagged repo.
func TestScanFingerprintIsRecorded(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	in := deltaInput("main", ".claude/settings.json", "abc123", true, nil, now)
	// A ubiquitous, weight-0 surface must stay out: diffing package.json
	// would bury real signal under ordinary commits.
	in.Branches[0].Matches = append(in.Branches[0].Matches, ignitionMatch{
		Path: "package.json", Size: 900, BlobSHA: "noise",
		Rule: ignitionRule{Class: classPackage, Weight: 0},
	})
	in.Blobs["noise"] = blobAnalysis{Size: 900, Fetched: true, IsText: true}

	got := evaluateScan(in)
	fp := got.Fingerprint
	if !fp.CapturedAt.Equal(now) {
		t.Errorf("CapturedAt = %v, want %v", fp.CapturedAt, now)
	}
	if fp.Verdict != got.Verdict.String() {
		t.Errorf("Verdict = %q, want %q", fp.Verdict, got.Verdict.String())
	}
	if oid := fp.Ignition[fingerprintKey("main", ".claude/settings.json")]; oid != "abc123" {
		t.Errorf("scoring path OID = %q, want abc123", oid)
	}
	if _, ok := fp.Ignition[fingerprintKey("main", "package.json")]; ok {
		t.Error("weight-0 surface must not be fingerprinted")
	}
	if !fp.Signed["main"] {
		t.Error("branch signature state not recorded")
	}
}

// --- burst membership: identity, not bare name ---------------------------

func TestRepoInBurst(t *testing.T) {
	// The viewer's own repos, two of which are in the burst.
	own := []Repo{
		{Name: "octoscope", URL: "https://github.com/gfazioli/octoscope"},
		{Name: "findergit", URL: "https://github.com/gfazioli/findergit"},
		{Name: "netfox", URL: "https://github.com/gfazioli/netfox"},
	}
	burst := PushBurst{Count: 3, Repos: []string{"octoscope", "findergit", "netfox"}}

	tests := []struct {
		name        string
		repos       []Repo
		owner, repo string
		want        bool
	}{
		{"own repo genuinely in the burst", own, "gfazioli", "octoscope", true},
		{"owner casing is irrelevant", own, "GFAZIOLI", "OctoScope", true},
		{
			// The defect this function exists for: a watched repo from
			// another owner that happens to share a bare name must not
			// inherit the viewer's own fan-out.
			name:  "same bare name, different owner, must not match",
			repos: own, owner: "someorg", repo: "octoscope", want: false,
		},
		{"repo absent from the account list", own, "gfazioli", "unrelated", false},
		{
			// Present in the account but not part of the cluster.
			name: "account repo outside the burst",
			repos: append(append([]Repo{}, own...),
				Repo{Name: "quiet", URL: "https://github.com/gfazioli/quiet"}),
			owner: "gfazioli", repo: "quiet", want: false,
		},
		{"empty account list", nil, "gfazioli", "octoscope", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repoInBurst(tt.repos, burst, tt.owner, tt.repo); got != tt.want {
				t.Errorf("repoInBurst(%q/%q) = %v, want %v", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}

// --- engine: Axis 4, the account-wide push burst -------------------------

// burstInput builds a scan input that either scores on Axis 1 (two agent
// hooks → Watch) or is clean (a weight-0 surface), so the tests can watch
// what the burst does to each.
func burstInput(scored bool, hit bool, newest, now time.Time) scanInput {
	rule := ignitionRule{Class: classPackage, Weight: 0}
	if scored {
		rule = ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}
	}
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{{
			Prov: provBranch("main", true),
			Matches: []ignitionMatch{
				{Path: ".claude/settings.json", Size: 400, BlobSHA: "c", Rule: rule},
				{Path: ".gemini/settings.json", Size: 300, BlobSHA: "g", Rule: rule},
			},
		}},
		Blobs: map[string]blobAnalysis{
			"c": {Size: 400, Fetched: true, IsText: true},
			"g": {Size: 300, Fetched: true, IsText: true},
		},
		Now: now,
	}
	if !newest.IsZero() {
		in.Burst = PushBurst{Count: 5, Span: 49 * time.Second, Newest: newest, Repos: []string{"r", "a", "b", "c", "d"}}
		in.BurstHit = hit
	}
	return in
}

func burstFinding(s *RepoScan) (Finding, bool) {
	for _, f := range s.Findings {
		if f.Axis == AxisPushBurst {
			return f, true
		}
	}
	return Finding{}, false
}

func TestEvaluateScanPushBurst(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		in          scanInput
		wantFinding bool
		wantWeight  int
		wantVerdict ScanVerdict
	}{
		{
			// The corroboration case: Axis 1 already scored 2 (Watch),
			// the burst adds 3 → 5, which is exactly tSuspicious.
			name:        "recent burst corroborates a scored repo",
			in:          burstInput(true, true, now.Add(-5*time.Minute), now),
			wantFinding: true,
			wantWeight:  wPushBurstCorroboration,
			wantVerdict: VerdictSuspicious,
		},
		{
			// The rule that matters: a burst must never create a verdict
			// on its own. Reported for transparency at weight 0.
			name:        "recent burst alone scores nothing",
			in:          burstInput(false, true, now.Add(-5*time.Minute), now),
			wantFinding: true,
			wantWeight:  0,
			wantVerdict: VerdictClean,
		},
		{
			// Without the recency gate this is the bug that got the
			// original banner pulled: a months-old batch push re-alarming
			// on every run.
			name:        "stale burst is ignored entirely",
			in:          burstInput(true, true, now.Add(-72*time.Hour), now),
			wantFinding: false,
			wantVerdict: VerdictWatch,
		},
		{
			name:        "burst this repo is not part of is ignored",
			in:          burstInput(true, false, now.Add(-5*time.Minute), now),
			wantFinding: false,
			wantVerdict: VerdictWatch,
		},
		{
			// A caller with no repo list (accountRepos nil) must degrade
			// to Axis 1-3 rather than mis-score.
			name:        "no burst context at all",
			in:          burstInput(true, false, time.Time{}, now),
			wantFinding: false,
			wantVerdict: VerdictWatch,
		},
		{
			// A zero Now would otherwise make every burst look ancient
			// *or* current depending on sign; assert it is simply skipped.
			name:        "zero clock is skipped",
			in:          burstInput(true, true, now.Add(-5*time.Minute), time.Time{}),
			wantFinding: false,
			wantVerdict: VerdictWatch,
		},
		{
			// Mild clock skew must not throw away a genuine detection.
			name:        "slightly future push still counts as recent",
			in:          burstInput(true, true, now.Add(30*time.Second), now),
			wantFinding: true,
			wantWeight:  wPushBurstCorroboration,
			wantVerdict: VerdictSuspicious,
		},
		{
			// The one-sided-comparison trap: now.Sub(future) is negative
			// and would satisfy `age <= recency` forever, so a repo with
			// a bogus future timestamp would look freshly burst on every
			// single scan.
			name:        "absurd future push is not recent",
			in:          burstInput(true, true, now.AddDate(4, 0, 0), now),
			wantFinding: false,
			wantVerdict: VerdictWatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateScan(tt.in)
			f, ok := burstFinding(got)
			if ok != tt.wantFinding {
				t.Fatalf("push-burst finding present = %v, want %v (findings %+v)", ok, tt.wantFinding, got.Findings)
			}
			if ok && f.Weight != tt.wantWeight {
				t.Errorf("push-burst weight = %d, want %d", f.Weight, tt.wantWeight)
			}
			if got.Verdict != tt.wantVerdict {
				t.Errorf("verdict = %v, want %v (score %d)", got.Verdict, tt.wantVerdict, got.Score)
			}
		})
	}
}

// The weight-0 burst finding must stay out of ScoredFindings so the
// report's scored-evidence section doesn't imply it moved the needle.
func TestPushBurstAloneIsNotScoredEvidence(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	got := evaluateScan(burstInput(false, true, now.Add(-time.Minute), now))
	if _, ok := burstFinding(got); !ok {
		t.Fatal("expected the burst to be reported for transparency")
	}
	for _, f := range got.ScoredFindings() {
		if f.Axis == AxisPushBurst {
			t.Error("a weight-0 burst must not appear among scored findings")
		}
	}
}

func TestEvaluateScanCompromised(t *testing.T) {
	prov := provBranch("main", true)
	prov.Signed = false
	prov.SignedByGitHub = false
	prov.Bot = true
	prov.AuthorName = "github-actions"

	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: prov,
				Matches: []ignitionMatch{
					{Path: ".github/setup.js", Size: 4500000, BlobSHA: "d", Rule: ignitionRule{Class: classDropper, Weight: wIgnitionNamedIOC}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"d": {Size: 4500000, Fetched: false, Markers: []string{"eval() call", "base64 decode at runtime"}},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictCompromised {
		t.Fatalf("verdict = %v, want compromised (score %d, findings %+v)", got.Verdict, got.Score, got.Findings)
	}
	// Expect: ignition(4) + oversized(4) + obfuscated(5) + spoof(5) + combined(4) = 22.
	if got.Score < tCompromised {
		t.Errorf("score = %d, want >= %d", got.Score, tCompromised)
	}
	var sawSpoof, sawCombined bool
	for _, f := range got.Findings {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "forged") {
			sawSpoof = true
		}
		if strings.Contains(f.Reason, "coincide") {
			sawCombined = true
		}
	}
	if !sawSpoof {
		t.Error("expected a spoofed-identity finding")
	}
	if !sawCombined {
		t.Error("expected a combined smoking-gun finding")
	}
}

func TestEvaluateScanUnsignedDelta(t *testing.T) {
	// Default branch signed; a side branch carrying an agent hook is
	// unsigned → unsigned-delta + divergence fire.
	main := provBranch("main", true)
	main.Signed = true
	side := provBranch("next", false)
	side.Signed = false

	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 2,
		Branches: []scanBranch{
			{Prov: main},
			{
				Prov: side,
				Matches: []ignitionMatch{
					{Path: ".claude/settings.json", Size: 500, BlobSHA: "c", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{"c": {Size: 500, Fetched: true, IsText: true}},
	}
	got := evaluateScan(in)
	var sawUnsigned, sawDivergence bool
	for _, f := range got.Findings {
		if strings.Contains(f.Reason, "unsigned while the repo otherwise signs") {
			sawUnsigned = true
		}
		if strings.Contains(f.Reason, "divergence") {
			sawDivergence = true
		}
	}
	if !sawUnsigned {
		t.Error("expected an unsigned-delta finding")
	}
	if !sawDivergence {
		t.Error("expected a side-branch divergence finding")
	}
}

// TestBranchCoverageDisclosure pins the #85 coverage math: the gap
// between real and scanned branch counts, and whether the scan should
// admit partial coverage (unscanned branches or a bounded tree walk).
func TestBranchCoverageDisclosure(t *testing.T) {
	cases := []struct {
		name           string
		scanned, total int
		truncated      bool
		wantNotScanned int
		wantPartial    bool
	}{
		{"all scanned", 5, 5, false, 0, false},
		{"enumeration cap: 20 of 250", 20, 250, true, 230, true},
		{"bounded fan-out only", 20, 30, true, 10, true},
		{"deep-tree truncation, no branch gap", 5, 5, true, 0, true},
		{"under-reported total floors at zero", 5, 0, false, 0, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := &RepoScan{BranchesScanned: tt.scanned, BranchesTotal: tt.total, Truncated: tt.truncated}
			if got := s.BranchesNotScanned(); got != tt.wantNotScanned {
				t.Errorf("BranchesNotScanned() = %d, want %d", got, tt.wantNotScanned)
			}
			if got := s.PartialCoverage(); got != tt.wantPartial {
				t.Errorf("PartialCoverage() = %v, want %v", got, tt.wantPartial)
			}
		})
	}
}

// --- regressions found by the Codex review of #103 -----------------------

// Bounding each finding is not enough. One fork-triggered secret-bearing
// workflow (3) plus a reachable self-hosted runner (3) summed to 6 and
// reached Suspicious with no other axis agreeing.
func TestCapabilityAggregateIsClamped(t *testing.T) {
	in := capInput(".github/workflows/x.yml", &workflowFacts{
		ForkTriggers:   []string{"pull_request_target"},
		UsesSecrets:    true,
		WritePerms:     []string{"write-all"},
		SelfHostedJobs: true,
	})
	in.Probes = capabilityProbes{SelfHostedRunners: []string{"box"}}

	got := evaluateScan(in)
	if got.Verdict >= VerdictSuspicious {
		t.Errorf("axis 4 aggregate reached %v (score %d) with no other axis", got.Verdict, got.Score)
	}
	if got.Score > maxCapabilityScore {
		t.Errorf("capability contributed %d, above the %d ceiling", got.Score, maxCapabilityScore)
	}
	// Clamping the arithmetic must not hide the evidence.
	if len(capFindings(got)) < 3 {
		t.Errorf("clamping dropped findings: %+v", capFindings(got))
	}
}

// Deduping by path alone let a safe copy on the default branch mask a
// dangerous variant at the same path on a side branch — the exact
// divergence pattern the scan exists to catch.
func TestCapabilityDedupeIsContentKeyed(t *testing.T) {
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 2,
		Branches: []scanBranch{
			{Prov: provBranch("main", true), Matches: []ignitionMatch{
				{Path: ".github/workflows/ci.yml", BlobSHA: "safe", Rule: ignitionRule{Class: classCI}}}},
			{Prov: provBranch("next", false), Matches: []ignitionMatch{
				{Path: ".github/workflows/ci.yml", BlobSHA: "danger", Rule: ignitionRule{Class: classCI}}}},
		},
		Blobs: map[string]blobAnalysis{
			"safe":   {Fetched: true, IsText: true, Workflow: &workflowFacts{}},
			"danger": {Fetched: true, IsText: true, Workflow: &workflowFacts{ForkTriggers: []string{"pull_request_target"}, UsesSecrets: true}},
		},
		Now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}
	got := evaluateScan(in)
	if got.Score == 0 {
		t.Errorf("the dangerous side-branch variant was skipped: %+v", capFindings(got))
	}
}

// A runner is reachable only if a fork-triggered workflow actually
// targets self-hosted. Coexistence alone made the report claim
// outsider code could run on your hardware when it could not.
func TestRunnerScoresOnlyWhenReachable(t *testing.T) {
	unreachable := capInput(".github/workflows/x.yml", &workflowFacts{
		ForkTriggers: []string{"pull_request_target"}, // but runs on ubuntu-latest
	})
	unreachable.Probes = capabilityProbes{SelfHostedRunners: []string{"box"}}
	if got := evaluateScan(unreachable); got.Score != 0 {
		t.Errorf("runner scored without a self-hosted fork job: %d %+v", got.Score, capFindings(got))
	}

	reachable := capInput(".github/workflows/x.yml", &workflowFacts{
		ForkTriggers: []string{"pull_request_target"}, SelfHostedJobs: true,
	})
	reachable.Probes = capabilityProbes{SelfHostedRunners: []string{"box"}}
	if got := evaluateScan(reachable); got.Score == 0 {
		t.Error("a fork-triggered self-hosted job with a runner attached must score")
	}
}

// A workflow whose content never arrived (over the size cap, or past
// the fetch budget) must be declared, not silently treated as examined.
func TestUnretrievedWorkflowIsDeclared(t *testing.T) {
	in := capInput(".github/workflows/x.yml", nil) // Workflow facts absent
	in.Blobs["w"] = blobAnalysis{Size: 900}        // never fetched
	got := evaluateScan(in)
	found := false
	for _, u := range got.Unchecked {
		if strings.Contains(u.Name, "x.yml") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unretrieved workflow was not declared: %+v", got.Unchecked)
	}
}
