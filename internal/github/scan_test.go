package github

import (
	"fmt"
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

// The scored block reads heaviest-first, so the evidence that drove the
// verdict is the first thing seen rather than something to be found by
// comparing every number (#105). The engine adds axis by axis, which put a
// +1 above a +4.
func TestScoredFindingsAreRankedBySeverity(t *testing.T) {
	s := &RepoScan{Findings: []Finding{
		{Axis: AxisIgnition, Path: "a", Weight: 1},
		{Axis: AxisIgnition, Path: "b", Weight: 0}, // filtered out
		{Axis: AxisBlob, Path: "c", Weight: 4},
		{Axis: AxisProvenance, Path: "d", Weight: 3},
		{Axis: AxisCapability, Path: "e", Weight: 3},
		{Axis: AxisDelta, Path: "f", Weight: 2},
	}}

	got := s.ScoredFindings()
	var weights []int
	var paths []string
	for _, f := range got {
		weights = append(weights, f.Weight)
		paths = append(paths, f.Path)
	}
	if len(got) != 5 {
		t.Fatalf("scored = %d, want 5 (weight-0 filtered): %v", len(got), paths)
	}
	for i := 1; i < len(weights); i++ {
		if weights[i-1] < weights[i] {
			t.Errorf("weights %v are not descending at %d — a lighter finding reads above a heavier one", weights, i)
		}
	}

	if paths[1] != "d" || paths[2] != "e" {
		t.Errorf("equal weights reordered: got %v, want the +3 pair as d then e", paths)
	}

	// Presentation only: the engine's own slice keeps its order, because
	// ContextFindings walks it to keep related weight-0 lines together.
	if s.Findings[0].Path != "a" || s.Findings[2].Path != "c" {
		t.Errorf("ScoredFindings mutated s.Findings: %v", s.Findings)
	}
}

// Equal weights must keep engine order, so the same scan always renders
// identically — the determinism the report text already sorts for
// elsewhere. The sort therefore has to be *stable*, and asserting that
// needs an input where an unstable sort visibly disagrees.
//
// Measured rather than assumed: with the five-finding case above,
// sort.Slice and sort.SliceStable produce identical output, so swapping one
// for the other there proves nothing. Go's pdqsort is deterministic for a
// fixed input, and for weights cycling the way an axis-by-axis engine emits
// them the two first disagree at **thirteen** findings — a size a repo with
// several branches and workflows reaches easily. Hence the length.
func TestScoredFindingsSortIsStable(t *testing.T) {
	// The cycle mirrors the engine's own order — ignition, blob, provenance,
	// capability, delta — so the equal-weight pair is provenance next to
	// capability, which is exactly where an unstable sort would swap two
	// findings from different axes.
	weights := []int{1, 4, 3, 3, 2}
	var s RepoScan
	for i := 0; i < 13; i++ {
		s.Findings = append(s.Findings, Finding{
			Axis:   AxisIgnition,
			Path:   fmt.Sprintf("p%02d", i),
			Weight: weights[i%len(weights)],
		})
	}

	got := s.ScoredFindings()
	// Within each weight, paths must appear in ascending index order,
	// because that is the order the engine added them in.
	lastByWeight := map[int]string{}
	for _, f := range got {
		if prev, ok := lastByWeight[f.Weight]; ok && f.Path < prev {
			t.Errorf("weight %d reordered: %q came after %q — the sort is not stable", f.Weight, f.Path, prev)
		}
		lastByWeight[f.Weight] = f.Path
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

// chainScanInput builds a scan over several real workflow sources on one
// branch, parsed the way a scan would parse them, so the chain tests
// exercise parseWorkflow and the composition together rather than
// hand-built facts that could drift from what the parser really produces.
func chainScanInput(t *testing.T, files map[string]string) scanInput {
	t.Helper()
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches:      []scanBranch{{Prov: provBranch("main", true)}},
		Blobs:         map[string]blobAnalysis{},
		Now:           time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	}
	for path, src := range files {
		f := parseWorkflow([]byte(src))
		if f.Unparsed {
			t.Fatalf("%s did not parse as YAML", path)
		}
		sha := "sha-" + path
		in.Branches[0].Matches = append(in.Branches[0].Matches, ignitionMatch{
			Path: path, Size: 900, BlobSHA: sha,
			Rule: ignitionRule{Class: classCI, Weight: 0},
		})
		in.Blobs[sha] = blobAnalysis{Size: 900, Fetched: true, IsText: true, Workflow: &f}
	}
	return in
}

func findingFor(s *RepoScan, path string) (Finding, bool) {
	for _, f := range s.Findings {
		if f.Axis == AxisCapability && f.Path == path && f.Weight > 0 {
			return f, true
		}
	}
	return Finding{}, false
}

func reasonsFor(s *RepoScan, path string) []string {
	var out []string
	for _, f := range s.Findings {
		if f.Axis == AxisCapability && f.Path == path {
			out = append(out, f.Reason)
		}
	}
	return out
}

// The shape #106 exists for: a fork-triggered caller that holds nothing of
// its own, and a callee that reads a secret but whose only trigger is
// `workflow_call`. Read one file at a time neither is a finding. Composed,
// the callee is a fork-triggered path to a repository secret.
func TestScanScoresTheComposedChain(t *testing.T) {
	in := chainScanInput(t, map[string]string{
		".github/workflows/caller.yml": `
on: pull_request_target
permissions: {}
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets: inherit
`,
		".github/workflows/reusable.yml": `
on: workflow_call
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: deploy --token ${{ secrets.DEPLOY_TOKEN }}
`,
	})

	got := evaluateScan(in)
	f, ok := findingFor(got, ".github/workflows/reusable.yml")
	if !ok {
		t.Fatalf("the callee did not score: %v", reasonsFor(got, ".github/workflows/reusable.yml"))
	}
	if !strings.Contains(f.Reason, "pull_request_target") {
		t.Errorf("reason does not name the trigger that reaches it: %q", f.Reason)
	}
	if !strings.Contains(f.Reason, "reached through .github/workflows/caller.yml") {
		t.Errorf("reason does not say how an outsider reaches a workflow_call file: %q", f.Reason)
	}
	if !strings.Contains(f.Reason, "secrets") {
		t.Errorf("reason does not name what it holds: %q", f.Reason)
	}
}

// The other direction, and the one that keeps the composition from being a
// false-positive engine: a callee holds what its caller granted, not what
// it declares. `permissions: {}` in the caller confers nothing, and GitHub
// lets a callee only downgrade what it receives.
func TestScanDoesNotCreditACalleeWithUngrantedPower(t *testing.T) {
	in := chainScanInput(t, map[string]string{
		".github/workflows/caller.yml": `
on: pull_request_target
jobs:
  call:
    permissions: {}
    uses: ./.github/workflows/reusable.yml
`,
		".github/workflows/reusable.yml": `
on: workflow_call
permissions:
  contents: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: true
`,
	})
	// Even a permissive repository default must not reach it: the caller
	// declared a block, so the default never applies.
	in.Probes.DefaultWorkflowPerms = "write"

	got := evaluateScan(in)
	reasons := reasonsFor(got, ".github/workflows/reusable.yml")
	if len(reasons) != 1 {
		t.Fatalf("want one finding for the callee, got %v", reasons)
	}

	// The two halves have to be asserted together, because either alone is
	// satisfied by the pre-#106 behaviour for the wrong reason: without the
	// composition the callee simply has no outsider trigger, so "it did not
	// score" was already true. What proves the bound is the *pair* —
	// reachability composed in, power left out.
	if !strings.Contains(reasons[0], "pull_request_target") {
		t.Errorf("reachability did not reach the callee, so this asserts nothing about the bound: %q", reasons[0])
	}
	if !strings.Contains(reasons[0], "holds no secrets or write scopes") {
		t.Errorf("callee credited with power its caller never granted: %q", reasons[0])
	}
	if f, ok := findingFor(got, ".github/workflows/reusable.yml"); ok {
		t.Errorf("callee scored %d though `permissions: {}` conferred nothing: %q", f.Weight, f.Reason)
	}
	// Nothing in this pair holds anything: the caller's only job declares
	// `permissions: {}` and passes no secret, and the callee cannot hold
	// more than it was given. A composition that scored here would be
	// manufacturing power out of a chain that carries none.
	if got.Score != 0 {
		t.Errorf("score = %d, want 0 (findings %+v)", got.Score, got.Findings)
	}
}

// A callee-only file nobody calls: the scan does not know who invokes it,
// so it must claim neither power nor exposure. Before #106 this file read
// as "grants the repository's default write permission, reachable only from
// its own triggers" — two claims its caller actually decides.
func TestScanDoesNotInventACallerForAnOrphanCallee(t *testing.T) {
	in := chainScanInput(t, map[string]string{
		".github/workflows/orphan.yml": `
on: workflow_call
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: true
`,
	})
	in.Probes.DefaultWorkflowPerms = "write"

	got := evaluateScan(in)
	reasons := reasonsFor(got, ".github/workflows/orphan.yml")
	if len(reasons) != 1 {
		t.Fatalf("want exactly one disclosure for an orphan callee, got %v", reasons)
	}
	if !strings.Contains(reasons[0], "none in this repository calls it") {
		t.Errorf("reason does not disclose that no caller was found: %q", reasons[0])
	}
	if strings.Contains(reasons[0], "reachable only from its own triggers") {
		t.Errorf("reason still claims reachability a callee does not have: %q", reasons[0])
	}
}

// #106 scopes cross-repository resolution out; staying quiet about it is
// what would be wrong. The same-repository call written as a full name plus
// a ref is here too, so the more obscure spelling does not become the one
// that evades composition unnoticed.
func TestScanDisclosesAnUnfollowedChain(t *testing.T) {
	in := chainScanInput(t, map[string]string{
		".github/workflows/caller.yml": `
on: pull_request_target
permissions:
  contents: write
jobs:
  far:
    uses: octo-org/example/.github/workflows/a.yml@v1
    secrets: inherit
`,
	})

	got := evaluateScan(in)
	var disclosed bool
	for _, r := range reasonsFor(got, ".github/workflows/caller.yml") {
		if strings.Contains(r, "does not resolve") && strings.Contains(r, "octo-org/example") {
			disclosed = true
		}
	}
	if !disclosed {
		t.Errorf("the unfollowed chain was not disclosed: %v", reasonsFor(got, ".github/workflows/caller.yml"))
	}
}

// A chain adds contributors to a sum the axis promises to keep under
// tSuspicious, and the rule for that promise is to enumerate them rather
// than trust one term: composing caller and callee means one attack path
// can now emit an escalation finding per file in the chain, three of which
// would be 9 against a threshold of 5. The clamp has to absorb that.
func TestChainCannotBreachTheCapabilityCeiling(t *testing.T) {
	in := chainScanInput(t, map[string]string{
		".github/workflows/a.yml": `
on: [pull_request_target, issue_comment, issues]
permissions: write-all
jobs:
  call:
    uses: ./.github/workflows/b.yml
    secrets: inherit
`,
		".github/workflows/b.yml": `
on: workflow_call
jobs:
  call:
    uses: ./.github/workflows/c.yml
    secrets: inherit
  work:
    runs-on: [self-hosted, linux]
    steps:
      - run: echo ${{ secrets.A }}
`,
		".github/workflows/c.yml": `
on: workflow_call
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: deploy --token ${{ secrets.B }}
`,
	})
	in.Probes = capabilityProbes{
		SelfHostedRunners:    []string{"builder-1"},
		WriteDeployKeys:      []string{"ci deploy key"},
		OffPlatformHooks:     []string{"hooks.example.com"},
		DefaultWorkflowPerms: "write",
	}

	got := evaluateScan(in)
	total := 0
	for _, f := range capFindings(got) {
		total += f.Weight
	}
	if total > maxCapabilityScore {
		t.Errorf("axis total %d exceeds the ceiling %d — a chain multiplies findings the clamp must absorb", total, maxCapabilityScore)
	}
	if got.Verdict == VerdictSuspicious || got.Verdict == VerdictCompromised {
		t.Errorf("verdict = %v from capability alone, score %d", got.Verdict, got.Score)
	}
	// The clamp bounds the arithmetic, not the disclosure: every file in the
	// chain must still appear, or the report hides the path it just found.
	for _, p := range []string{".github/workflows/a.yml", ".github/workflows/b.yml", ".github/workflows/c.yml"} {
		if len(reasonsFor(got, p)) == 0 {
			t.Errorf("%s was clamped out of the report entirely", p)
		}
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
			wf:          &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}, UsesSecrets: true},
			wantWeight:  wCapEscalation,
			wantContain: "while holding the repository's secrets",
		},
		{
			name:        "fork trigger with write permissions scores",
			wf:          &workflowFacts{OutsiderTriggers: []string{"workflow_run"}, WritePerms: []string{"contents: write"}},
			wantWeight:  wCapEscalation,
			wantContain: "contents: write",
		},
		{
			// A fork trigger by itself is a label bot's normal life.
			name:        "bare fork trigger is inventory only",
			wf:          &workflowFacts{OutsiderTriggers: []string{"issue_comment"}},
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
			wf:          &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}, WritePerms: []string{"write-all"}},
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
	wf := &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}, UsesSecrets: true}
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
	// Every contributor this axis has, in its worst state, in one scan.
	// The defect this test exists to catch was a *sum* — one
	// fork-triggered secret-bearing workflow (3) plus a reachable
	// self-hosted runner (3) reaching Suspicious with no second axis
	// agreeing — and the first version of this test built a single
	// workflow, so it was green while the invariant was broken. One term
	// proves nothing about the total.
	worst := &workflowFacts{
		OutsiderTriggers: []string{"pull_request_target", "workflow_run", "issue_comment"},
		WritePerms:       []string{"write-all"},
		UsesSecrets:      true,
		SelfHostedJobs:   true, // what makes an attached runner reachable
	}
	in := capInput(".github/workflows/x.yml", worst)
	in.Probes = capabilityProbes{
		SelfHostedRunners: []string{"builder-1"},
		WriteDeployKeys:   []string{"ci deploy key"},
		OffPlatformHooks:  []string{"hooks.example.com"},
	}

	got := evaluateScan(in)
	if got.Verdict >= VerdictSuspicious {
		t.Errorf("capability alone reached %v with score %d — no single axis may do that", got.Verdict, got.Score)
	}
	// Exactly the ceiling, not merely non-zero: this axis is the only
	// scoring contributor in the fixture, so anything less means weight
	// went missing between the findings and the verdict — which a
	// non-zero check would wave through.
	if got.Score != maxCapabilityScore {
		t.Errorf("score %d, want the ceiling %d — the axis is the only scoring contributor here", got.Score, maxCapabilityScore)
	}

	// The ceiling must be what holds, not the weights happening to sum
	// low: with one workflow the total lands on 4, which is also the
	// ceiling, so that case cannot tell a clamped sum from an
	// uncoincidentally small one. Saturating the ceiling is the proof.
	// If this drops below, a contributor has been dropped or reweighted
	// and the case has quietly stopped being maximal.
	total := 0
	for _, f := range capFindings(got) {
		total += f.Weight
	}
	if total != maxCapabilityScore {
		t.Errorf("axis total %d, want the ceiling %d — the constructed case is no longer maximal", total, maxCapabilityScore)
	}

	// Clamped arithmetic must not become a clamped report. Counting
	// zero-weight findings would not show that: deploy keys and webhooks
	// are weight 0 by construction, so the count stays satisfied even if
	// the overflowing finding were dropped outright. The one the ceiling
	// actually clamps here is the runner escalation — it arrives asking
	// for wCapEscalation and is zeroed because the workflow already spent
	// the budget — so name it.
	const clampedReason = "can run on your own hardware"
	disclosed := false
	for _, f := range capFindings(got) {
		if !strings.Contains(f.Reason, clampedReason) {
			continue
		}
		disclosed = true
		if f.Weight != 0 {
			t.Errorf("the clamped runner escalation carries weight %d, want 0", f.Weight)
		}
	}
	if !disclosed {
		t.Error("the runner escalation the ceiling clamped is absent from the report — the arithmetic is clamped, not the disclosure")
	}

	t.Logf("worst capability shape: score %d, verdict %v, axis total %d across %d findings",
		got.Score, got.Verdict, total, len(capFindings(got)))
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

// The burst must not corroborate Axis 4. Capability describes
// configuration, not an event, which is why the axis carries its own
// ceiling at one below tSuspicious — and letting a burst corroborate it
// hands that ceiling straight back: 3 + 3 = 6 reaches Suspicious with no
// evidence that anything ran, forged or changed. The gate read
// `s.Score > 0`, which included Axis 4, against what both
// wPushBurstCorroboration's comment and DetectPushBurst's promised.
// Found by Codex reviewing #116, which widened how many repos score on
// Axis 4 and so how many could reach this.
func TestPushBurstDoesNotCorroborateCapabilityAlone(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	in := capInput(".github/workflows/x.yml", &workflowFacts{
		OutsiderTriggers: []string{"pull_request_target"},
		UsesSecrets:      true,
	})
	in.Now = now
	in.Burst = PushBurst{Count: 5, Span: 49 * time.Second, Newest: now.Add(-5 * time.Minute), Repos: []string{"r", "a", "b", "c", "d"}}
	in.BurstHit = true

	got := evaluateScan(in)
	if got.Score != wCapEscalation {
		t.Fatalf("score = %d, want %d — the capability finding alone should score (findings %+v)",
			got.Score, wCapEscalation, got.Findings)
	}
	f, ok := burstFinding(got)
	if !ok {
		t.Fatal("no push-burst finding: the burst must still be disclosed, just not scored")
	}
	if f.Weight != 0 {
		t.Errorf("push-burst weight = %d, want 0 — capability is not evidence for a burst to corroborate", f.Weight)
	}
	if got.Verdict == VerdictSuspicious {
		t.Errorf("verdict = %v: capability plus account-wide timing reached Suspicious with no Axis 1-3 evidence", got.Verdict)
	}
	// "No scored finding" would be a false sentence here — one did score.
	if strings.Contains(f.Reason, "no scored finding") {
		t.Errorf("reason claims nothing scored while the capability finding did: %q", f.Reason)
	}
	if !strings.Contains(f.Reason, "nothing to corroborate") {
		t.Errorf("reason does not say why the burst is unscored: %q", f.Reason)
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
		OutsiderTriggers: []string{"pull_request_target"},
		UsesSecrets:      true,
		WritePerms:       []string{"write-all"},
		SelfHostedJobs:   true,
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
			"danger": {Fetched: true, IsText: true, Workflow: &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}, UsesSecrets: true}},
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
		OutsiderTriggers: []string{"pull_request_target"}, // but runs on ubuntu-latest
	})
	unreachable.Probes = capabilityProbes{SelfHostedRunners: []string{"box"}}
	if got := evaluateScan(unreachable); got.Score != 0 {
		t.Errorf("runner scored without a self-hosted fork job: %d %+v", got.Score, capFindings(got))
	}

	reachable := capInput(".github/workflows/x.yml", &workflowFacts{
		OutsiderTriggers: []string{"pull_request_target"}, SelfHostedJobs: true,
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

// #107: a workflow declaring no permissions runs on the repository default,
// which is not in the file. The axis has to resolve it, and — the part that
// matters more — must not guess when it cannot.
func TestCapabilityResolvesDefaultWorkflowPerms(t *testing.T) {
	// The shape from the issue: a fork trigger and no permissions block at
	// all. Whether this is dangerous is a fact about the repository, not
	// about the file.
	bare := &workflowFacts{
		OutsiderTriggers:     []string{"pull_request_target"},
		InheritsDefaultPerms: true,
	}

	tests := map[string]struct {
		wf            *workflowFacts
		probes        capabilityProbes
		wantScored    bool
		wantUnchecked bool
		wantReason    string
	}{
		"default write makes it an escalation": {
			wf:         bare,
			probes:     capabilityProbes{DefaultWorkflowPerms: "write"},
			wantScored: true,
			wantReason: "default write permission",
		},
		"default read leaves it inventory": {
			wf:         bare,
			probes:     capabilityProbes{DefaultWorkflowPerms: "read"},
			wantScored: false,
			wantReason: "holds no secrets or write scopes",
		},
		// The honest case: unknown must not resolve to permissive, and must
		// not resolve to "holds nothing" either — that would be a claim the
		// scan cannot support.
		"an unknown default is declared, not assumed": {
			wf:            bare,
			probes:        capabilityProbes{DefaultPermsUnchecked: "the token lacks the scope this needs"},
			wantScored:    false,
			wantUnchecked: true,
			wantReason:    "could not be read",
		},
		// Declaring anything overrides the default, so a read-only block on
		// a permissive repository is genuinely safe and must not be flagged
		// — nor should the gap be declared, since nothing depends on it.
		"a declared block overrides a permissive default": {
			wf:            &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}},
			probes:        capabilityProbes{DefaultWorkflowPerms: "write"},
			wantScored:    false,
			wantUnchecked: false,
			wantReason:    "holds no secrets or write scopes",
		},
		// And the gap is not declared where it decides nothing: unknown
		// default, but this workflow declares its own permissions.
		"an unknown default is not declared when nothing inherits it": {
			wf:            &workflowFacts{OutsiderTriggers: []string{"pull_request_target"}},
			probes:        capabilityProbes{DefaultPermsUnchecked: "the token lacks the scope this needs"},
			wantScored:    false,
			wantUnchecked: false,
			wantReason:    "holds no secrets or write scopes",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			in := capInput(".github/workflows/x.yml", tt.wf)
			in.Probes = tt.probes
			got := evaluateScan(in)

			scored := false
			reasons := ""
			for _, f := range capFindings(got) {
				reasons += f.Reason + "\n"
				if f.Weight > 0 {
					scored = true
				}
			}
			if scored != tt.wantScored {
				t.Errorf("scored = %v, want %v (score %d)\n%s", scored, tt.wantScored, got.Score, reasons)
			}
			if !strings.Contains(reasons, tt.wantReason) {
				t.Errorf("no finding mentioning %q; got:\n%s", tt.wantReason, reasons)
			}

			declared := false
			for _, u := range got.Unchecked {
				if strings.Contains(u.Name, "default workflow permissions") {
					declared = true
				}
			}
			if declared != tt.wantUnchecked {
				t.Errorf("default-permission gap declared = %v, want %v", declared, tt.wantUnchecked)
			}
		})
	}
}
