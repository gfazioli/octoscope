package main

import (
	"cmp"
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
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

// workflowJob is the part of a GitHub Actions job these tests read.
type workflowJob struct {
	If      string            `yaml:"if"`
	Needs   workflowNeeds     `yaml:"needs"`
	Outputs map[string]string `yaml:"outputs"`
	Steps   []workflowStep    `yaml:"steps"`

	// node is the job as written, for mentions.
	node yaml.Node
}

// UnmarshalYAML decodes the fields above and keeps the job's node.
func (j *workflowJob) UnmarshalYAML(n *yaml.Node) error {
	type fields workflowJob // no methods, so Decode does not come back here
	if err := n.Decode((*fields)(j)); err != nil {
		return err
	}
	j.node = *n
	return nil
}

// mentions reports whether any value in the job, typed above or not,
// contains s. Comments are not values, so a comment quoting an expression
// does not count.
func (j workflowJob) mentions(s string) bool {
	var in func(*yaml.Node) bool
	in = func(n *yaml.Node) bool {
		return n.Kind == yaml.ScalarNode && strings.Contains(n.Value, s) ||
			slices.ContainsFunc(n.Content, in)
	}
	return in(&j.node)
}

// workflowNeeds is a job's `needs`, which YAML lets be one name or a list.
type workflowNeeds []string

// UnmarshalYAML accepts both forms.
func (w *workflowNeeds) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*w = workflowNeeds{n.Value}
		return nil
	}
	return n.Decode((*[]string)(w))
}

// stepIndex returns the index of the job's step named name, or -1.
func stepIndex(job workflowJob, name string) int {
	return slices.IndexFunc(job.Steps, func(s workflowStep) bool { return s.Name == name })
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

// stepsUsing returns every step of one job whose `uses` names action,
// whatever ref follows the @.
func stepsUsing(job workflowJob, action string) []workflowStep {
	var out []workflowStep
	for _, s := range job.Steps {
		if strings.HasPrefix(s.Uses, action+"@") {
			out = append(out, s)
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
//
// Compared as written, so an input may not be an expression: the same
// `${{ ... }}` in both files evaluates against a pull request in one and a
// tag push in the other. And the CI half may not be conditional, its jobs
// or any of their steps: an `if:` there could skip the trip, or the check
// at its end, on every pull request and leave this test green. Only the four jobs that make the trip are read, so an
// unrelated upload elsewhere is not mistaken for it.
func TestCIHandoffRunsTheReleaseActions(t *testing.T) {
	ci := loadWorkflow(t, ".github/workflows/ci.yml")
	release := loadWorkflow(t, ".github/workflows/release.yml")

	for _, leg := range []struct {
		action        string
		ciJob, relJob string
	}{
		{"actions/upload-artifact", "goreleaser", "release"},
		{"actions/download-artifact", "cask-handoff", "promote"},
	} {
		action := leg.action
		ciJob, okCI := ci[leg.ciJob]
		relJob, okRel := release[leg.relJob]
		if !okCI || !okRel {
			t.Errorf("%s: want job %s in ci.yml and job %s in release.yml", action, leg.ciJob, leg.relJob)
			continue
		}
		ciSteps, relSteps := stepsUsing(ciJob, action), stepsUsing(relJob, action)
		if len(ciSteps) != 1 || len(relSteps) != 1 {
			t.Errorf("%s: want exactly one step in each of ci.yml's %s and release.yml's %s, got %d and %d",
				action, leg.ciJob, leg.relJob, len(ciSteps), len(relSteps))
			continue
		}
		c, r := ciSteps[0], relSteps[0]

		// The whole job, not only the artifact step: an `if:` on the step
		// that records the digest, or on the one that compares it, would
		// leave the trip running and its check skipped.
		if ciJob.If != "" {
			t.Errorf("ci.yml's %s job runs only if %q; the trip has to run on every pull request", leg.ciJob, ciJob.If)
		}
		for _, s := range ciJob.Steps {
			if s.If != "" {
				t.Errorf("ci.yml's %s step %q runs only if %q; every step of the trip has to run on every pull request",
					leg.ciJob, cmp.Or(s.Name, s.Uses), s.If)
			}
		}

		for file, s := range map[string]workflowStep{"ci.yml": c, "release.yml": r} {
			if !shaPinned.MatchString(s.Uses) {
				t.Errorf("%s: %q is not pinned to a commit SHA", file, s.Uses)
			}
		}
		if c.Uses != r.Uses {
			t.Errorf("%s: ci.yml runs %q, release.yml runs %q — the handoff in CI is not the release's",
				action, c.Uses, r.Uses)
		}

		// Every input, retention-days included, before it is set aside for
		// the comparison below.
		for file, with := range map[string]map[string]string{"ci.yml": c.With, "release.yml": r.With} {
			for _, k := range slices.Sorted(maps.Keys(with)) {
				if strings.Contains(with[k], "${{") {
					t.Errorf("%s: %s sets %s to the expression %q, which evaluates per event",
						action, file, k, with[k])
				}
			}
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
