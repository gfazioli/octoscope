package github

import (
	"strings"
	"testing"
)

// chainIndex parses a set of workflow sources into the path-keyed index
// composeChain consumes, so the tests read as the YAML they are about.
func chainIndex(t *testing.T, files map[string]string) map[string]*workflowFacts {
	t.Helper()
	index := make(map[string]*workflowFacts, len(files))
	for path, src := range files {
		f := parseWorkflow([]byte(src))
		if f.Unparsed {
			t.Fatalf("%s did not parse as YAML", path)
		}
		index[path] = &f
	}
	return index
}

const calleeReadsSecret = `
on: workflow_call
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: deploy --token ${{ secrets.DEPLOY_TOKEN }}
`

func TestComposeChainReachesCalleeSecret(t *testing.T) {
	// The shape #106 was opened for. Read one file at a time the caller
	// holds nothing and the callee is not outsider-reachable, so neither
	// scores; composed, they are a fork-triggered path to a secret.
	tests := []struct {
		name        string
		caller      string
		wantSecrets bool
		wantWhy     string
	}{
		{
			name: "secrets: inherit hands over the whole set",
			caller: `
on: pull_request_target
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets: inherit
`,
			wantSecrets: true,
		},
		{
			name: "a secret passed by name counts too",
			caller: `
on: pull_request_target
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets:
      token: ${{ secrets.DEPLOY_TOKEN }}
`,
			wantSecrets: true,
		},
		{
			// The false positive the by-name check must not produce: a
			// mapping is not automatically a secret.
			name: "a by-name mapping carrying no secret passes nothing",
			caller: `
on: pull_request_target
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets:
      who: ${{ github.actor }}
`,
			wantSecrets: false,
			wantWhy:     "the caller hands over no secret, so the callee's reference resolves to nothing",
		},
		{
			name: "no secrets key at all passes nothing",
			caller: `
on: pull_request_target
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
`,
			wantSecrets: false,
			wantWhy:     "the caller hands over no secret, so the callee's reference resolves to nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := composeChain(chainIndex(t, map[string]string{
				".github/workflows/caller.yml":   tt.caller,
				".github/workflows/reusable.yml": calleeReadsSecret,
			}))
			callee := got[".github/workflows/reusable.yml"]
			if callee == nil {
				t.Fatal("callee missing from the composed set")
			}
			// Reachability accumulates regardless of what was handed over.
			if !contains(callee.Triggers, "pull_request_target") {
				t.Errorf("callee triggers = %v, want pull_request_target carried in from the caller", callee.Triggers)
			}
			if !contains(callee.ViaCallers, ".github/workflows/caller.yml") {
				t.Errorf("callee ViaCallers = %v, want the caller named so the report can say how", callee.ViaCallers)
			}
			if callee.Secrets != tt.wantSecrets {
				t.Errorf("callee secrets = %v, want %v — %s", callee.Secrets, tt.wantSecrets, tt.wantWhy)
			}
		})
	}
}

func TestComposeChainBoundsPowerByTheGiver(t *testing.T) {
	// A callee can only downgrade what it receives, never widen it, so
	// what it declares is not what it holds.
	calleeDeclaresWrite := `
on: workflow_call
permissions:
  contents: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: true
`
	tests := []struct {
		name            string
		callerPerms     string
		wantPerms       []string
		wantInheritsDef bool
	}{
		{
			// The case a per-file reading gets wrong in the dangerous
			// direction: `permissions: {}` grants nothing elevated, yet it
			// *is* a declaration, so the repository default never applies.
			name:            "an empty permissions block confers nothing and overrides the default",
			callerPerms:     "permissions: {}",
			wantPerms:       nil,
			wantInheritsDef: false,
		},
		{
			name:            "a read-only block confers nothing elevated",
			callerPerms:     "permissions:\n  contents: read",
			wantPerms:       nil,
			wantInheritsDef: false,
		},
		{
			name:            "declared write is conferred",
			callerPerms:     "permissions:\n  id-token: write",
			wantPerms:       []string{"id-token: write"},
			wantInheritsDef: false,
		},
		{
			// Nothing declared anywhere above it, so what reaches the
			// callee is the repository setting only #107's probe can read.
			name:            "declaring nothing passes the repository default along",
			callerPerms:     "",
			wantPerms:       nil,
			wantInheritsDef: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := "on: pull_request_target\n" + tt.callerPerms + `
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
`
			got := composeChain(chainIndex(t, map[string]string{
				".github/workflows/caller.yml":   caller,
				".github/workflows/reusable.yml": calleeDeclaresWrite,
			}))
			callee := got[".github/workflows/reusable.yml"]
			if strings.Join(callee.Write.Perms, ",") != strings.Join(tt.wantPerms, ",") {
				t.Errorf("callee write = %v, want %v — a callee holds the caller's grant, not its own declaration",
					callee.Write.Perms, tt.wantPerms)
			}
			if callee.Write.InheritsDefault != tt.wantInheritsDef {
				t.Errorf("callee InheritsDefault = %v, want %v", callee.Write.InheritsDefault, tt.wantInheritsDef)
			}
		})
	}
}

func TestComposeChainTransitiveAndCyclic(t *testing.T) {
	t.Run("reachability travels the whole chain", func(t *testing.T) {
		got := composeChain(chainIndex(t, map[string]string{
			".github/workflows/a.yml": `
on: issue_comment
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
`,
			".github/workflows/c.yml": calleeReadsSecret,
		}))
		c := got[".github/workflows/c.yml"]
		if !contains(c.Triggers, "issue_comment") {
			t.Errorf("c triggers = %v, want issue_comment carried two hops", c.Triggers)
		}
		if !c.Secrets {
			t.Error("c should reach secrets: both hops forwarded them")
		}
		if !c.Write.InheritsDefault {
			t.Errorf("c InheritsDefault = false: nothing in the chain declared permissions, so the repository default reaches it")
		}
	})

	t.Run("a cycle terminates", func(t *testing.T) {
		// Not valid Actions, but a scan reads whatever is in the tree and
		// must not hang on it. The fixpoint makes this terminate by
		// construction rather than by a visited set.
		got := composeChain(chainIndex(t, map[string]string{
			".github/workflows/a.yml": "on: pull_request_target\njobs:\n  x:\n    uses: ./.github/workflows/b.yml\n    secrets: inherit\n",
			".github/workflows/b.yml": "on: workflow_call\njobs:\n  y:\n    uses: ./.github/workflows/a.yml\n    secrets: inherit\n",
		}))
		if !contains(got[".github/workflows/b.yml"].Triggers, "pull_request_target") {
			t.Error("b should still inherit reachability from a")
		}
	})
}

func TestComposeChainDoesNotInventCallers(t *testing.T) {
	// A callee nobody in this branch calls: the scan does not know who
	// invokes it, so it must claim neither power nor exposure. Before
	// #106 this file read as "declares no permissions, so it runs with the
	// repository default" — a claim its caller actually decides.
	got := composeChain(chainIndex(t, map[string]string{
		".github/workflows/orphan.yml": calleeReadsSecret,
	}))
	c := got[".github/workflows/orphan.yml"]
	if c.CallerFound {
		t.Error("CallerFound = true with no caller in the index")
	}
	if len(c.Triggers) != 0 {
		t.Errorf("triggers = %v, want none: workflow_call is not untrusted input on its own", c.Triggers)
	}
	if c.Secrets {
		t.Error("secrets = true: the reference resolves to nothing until a caller passes them")
	}
	if c.Write.InheritsDefault || len(c.Write.Perms) > 0 {
		t.Errorf("write = %+v, want empty: a callee's permissions are its caller's to grant", c.Write)
	}
}

// Every outsider trigger carries its own reason. Composition made the
// multi-trigger case ordinary — a chain unions its callers' triggers, and
// so does the merge across branches — and the names sort alphabetically, so
// explaining only the first handed `issue_comment` the explanation while
// `pull_request_target` was listed with none. Copilot, #117.
func TestDescribeTriggersExplainsEachOne(t *testing.T) {
	got := describeTriggers([]string{"issue_comment", "pull_request_target"})
	for _, ev := range []string{"issue_comment", "pull_request_target"} {
		why := outsiderTriggers[ev]
		if !strings.Contains(got, ev+" ("+why+")") {
			t.Errorf("%s is listed without its own reason: %q", ev, got)
		}
	}
	if one := describeTriggers([]string{"issues"}); one != "issues ("+outsiderTriggers["issues"]+")" {
		t.Errorf("single trigger = %q, want no list punctuation", one)
	}
	if describeTriggers(nil) != "" {
		t.Error("no triggers should render as nothing, not as stray punctuation")
	}
}

// Two jobs calling the same workflow is one unfollowed chain, not two. The
// value is joined into report text, so a duplicate reads as a second
// target. Copilot, #117.
func TestComposeChainDedupesUnfollowed(t *testing.T) {
	got := composeChain(chainIndex(t, map[string]string{
		".github/workflows/c.yml": `
on: pull_request_target
jobs:
  one:
    uses: octo-org/example/.github/workflows/a.yml@v1
  two:
    uses: octo-org/example/.github/workflows/a.yml@v1
`,
	}))
	if u := got[".github/workflows/c.yml"].Unfollowed; len(u) != 1 {
		t.Errorf("unfollowed = %v, want the target once", u)
	}
}

// A `./` call the scan cannot resolve is a chain leading somewhere unread —
// most usefully when the callee's content never arrived, over
// maxBlobScanBytes or past the fetch budget. The doc comment claimed this
// was recorded while the code skipped it. Copilot, #117.
func TestComposeChainDisclosesAMissingLocalCallee(t *testing.T) {
	got := composeChain(chainIndex(t, map[string]string{
		".github/workflows/c.yml": `
on: pull_request_target
jobs:
  one:
    uses: ./.github/workflows/gone.yml
    secrets: inherit
`,
	}))
	u := got[".github/workflows/c.yml"].Unfollowed
	if !contains(u, "./.github/workflows/gone.yml") {
		t.Errorf("unfollowed = %v, want the unresolvable local call disclosed", u)
	}
}

func TestComposeChainDisclosesUnfollowed(t *testing.T) {
	// #106 scopes cross-repository resolution out. Staying quiet about it
	// is the part that would be wrong — and a same-repository call written
	// as a full name plus a ref must not become the spelling that evades
	// composition unnoticed.
	got := composeChain(chainIndex(t, map[string]string{
		".github/workflows/caller.yml": `
on: pull_request_target
jobs:
  far:
    uses: octo-org/example/.github/workflows/a.yml@v1
    secrets: inherit
  byname:
    uses: gfazioli/octoscope/.github/workflows/b.yml@main
    secrets: inherit
  action:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`,
	}))
	c := got[".github/workflows/caller.yml"]
	if len(c.Unfollowed) != 2 {
		t.Fatalf("unfollowed = %v, want both workflow calls disclosed", c.Unfollowed)
	}
	for _, u := range c.Unfollowed {
		if strings.Contains(u, "actions/checkout") {
			t.Errorf("a step-level action was read as a reusable-workflow call: %q", u)
		}
	}
}
