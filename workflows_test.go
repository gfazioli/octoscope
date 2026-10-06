package main

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workflowStep is the part of a GitHub Actions step these tests read.
type workflowStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
}

// workflowJob is the part of a GitHub Actions job these tests read.
type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

// loadWorkflow parses a workflow file into its jobs. Parsed, not grepped:
// a pattern over the text also matches comments and misses quoted values,
// which is how the first version of the check below could pass a commented
// pin standing in for the tag that actually ran.
func loadWorkflow(t *testing.T, path string) map[string]workflowJob {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var wf struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatalf("%s: no jobs parsed", path)
	}
	return wf.Jobs
}

// stepsUsing returns every step in the workflow whose `uses` names action,
// whatever ref follows the @.
func stepsUsing(jobs map[string]workflowJob, action string) []workflowStep {
	var out []workflowStep
	for _, name := range slices.Sorted(maps.Keys(jobs)) {
		for _, s := range jobs[name].Steps {
			if strings.HasPrefix(s.Uses, action+"@") {
				out = append(out, s)
			}
		}
	}
	return out
}

var shaPinned = regexp.MustCompile(`@[0-9a-f]{40}$`)

// TestCIHandoffRunsTheReleaseActions holds ci.yml's cask-handoff to the
// handoff it stands in for. release.yml carries goreleaser's cask from the
// release job to promote through artifact storage, which runs only at tag
// time; ci.yml runs the same trip on every pull request so that a bump of
// either action is measured on its own PR rather than on a release. That is
// evidence only while the two run the same thing:
//
//   - the same action at the same commit, pinned by SHA in both, so a
//     Dependabot bump has to move them together and a tag cannot stand in;
//   - the same `with` inputs, so release.yml cannot gain an input that
//     changes the trip — upload-artifact v7's `archive: false` names the
//     artifact after the file and would break promote's download by name —
//     while CI keeps passing without it.
//
// retention-days is the one input allowed to differ. release.yml keeps the
// artifact 90 days because it is the only copy of a cask a failed promote
// can still publish; CI's is read once, by the next job.
func TestCIHandoffRunsTheReleaseActions(t *testing.T) {
	ci := loadWorkflow(t, ".github/workflows/ci.yml")
	release := loadWorkflow(t, ".github/workflows/release.yml")

	for _, action := range []string{"actions/upload-artifact", "actions/download-artifact"} {
		ciSteps, relSteps := stepsUsing(ci, action), stepsUsing(release, action)
		if len(ciSteps) != 1 || len(relSteps) != 1 {
			t.Errorf("%s: want exactly one step in each workflow, got %d in ci.yml and %d in release.yml",
				action, len(ciSteps), len(relSteps))
			continue
		}
		c, r := ciSteps[0], relSteps[0]

		for file, s := range map[string]workflowStep{"ci.yml": c, "release.yml": r} {
			if !shaPinned.MatchString(s.Uses) {
				t.Errorf("%s: %q is not pinned to a commit SHA", file, s.Uses)
			}
		}
		if c.Uses != r.Uses {
			t.Errorf("%s: ci.yml runs %q, release.yml runs %q — the handoff in CI is not the release's",
				action, c.Uses, r.Uses)
		}

		ciWith, relWith := maps.Clone(c.With), maps.Clone(r.With)
		delete(ciWith, "retention-days")
		delete(relWith, "retention-days")
		if !maps.Equal(ciWith, relWith) {
			t.Errorf("%s: inputs differ — ci.yml %v, release.yml %v (retention-days aside)",
				action, ciWith, relWith)
		}
	}
}
