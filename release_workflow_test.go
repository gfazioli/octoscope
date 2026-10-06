package main

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// release.yml's classify job decides once whether a tag is a prerelease,
// and every stable-only gate reads its answer (#193; the job's header has
// the story). The decision is a shell script inside YAML, so these tests
// run THAT script, extracted from the file the workflow runs. A copy of the
// logic in Go would test the copy.

const releaseWorkflow = ".github/workflows/release.yml"

// classifyStep returns the classify job and its `tag` step.
func classifyStep(t *testing.T) (workflowJob, workflowStep) {
	t.Helper()
	job := loadWorkflow(t, releaseWorkflow)["classify"]
	i := slices.IndexFunc(job.Steps, func(s workflowStep) bool { return s.ID == "tag" })
	if i < 0 || job.Steps[i].Run == "" {
		t.Fatalf("%s: no classify job with a `tag` step to run", releaseWorkflow)
	}
	return job, job.Steps[i]
}

// runClassify runs the script the way the runner runs a step that names no
// `shell:` — `bash -e`, per GitHub's workflow syntax reference; the script
// sets its own pipefail — with TAG in the environment and GITHUB_OUTPUT
// pointing at a file. It returns the prerelease output, or "refused" when
// the script exited non-zero.
func runClassify(t *testing.T, script, tag string) string {
	t.Helper()
	outFile := filepath.Join(t.TempDir(), "github_output")
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Env = append(os.Environ(), "TAG="+tag, "GITHUB_OUTPUT="+outFile)
	log, err := cmd.CombinedOutput()
	written, _ := os.ReadFile(outFile)
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("run classify for %q: %v", tag, err)
		}
		// A refused tag must leave no answer behind for a gate to read.
		if len(written) != 0 {
			t.Errorf("%q: refused, yet wrote %q to GITHUB_OUTPUT", tag, written)
		}
		return "refused"
	}
	lines := strings.Split(strings.TrimSpace(string(written)), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "prerelease=") {
		t.Fatalf("%q: want exactly one prerelease= line in GITHUB_OUTPUT, got %q (log: %s)", tag, written, log)
	}
	return strings.TrimPrefix(lines[0], "prerelease=")
}

// TestReleaseClassifiesTagsAsGoreleaserDoes pins the classification on the
// tags that matter: every tag shape this project cuts, the build-metadata
// case #193 is about, and the non-SemVer names `on.push.tags: v*` lets
// through, which classify refuses before anything is built.
//
// "As goreleaser does" was measured, not assumed: every tag below was run
// through Masterminds/semver v3.5.0's NewVersion and Prerelease — the parser
// goreleaser v2.18.1 uses for `prerelease: auto` — in a scratch program. The
// two agree on all fourteen tags classify accepts. Masterminds is the laxer
// of the two: it also takes 1.2.3, v1.2, v01.2.3 and v1.02.3, which classify
// refuses on purpose, since none of them is a name this project releases
// under.
func TestReleaseClassifiesTagsAsGoreleaserDoes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the classify script runs on ubuntu-latest; bash semantics are what is under test")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash on PATH")
	}
	_, step := classifyStep(t)

	cases := []struct {
		tag  string
		want string // "false", "true", or "refused"
	}{
		// Stable, including build metadata that carries hyphens (#193).
		{"v0.36.0", "false"},
		{"v1.2.3", "false"},
		{"v10.20.30", "false"},
		{"v1.2.3+build-1", "false"},
		{"v1.2.3+20260101.sha-5114f85", "false"},
		{"v1.0.0+0.build.1-rc.10000aaa-kk-0.1", "false"},

		// Prereleases, including hyphens inside the prerelease field and
		// build metadata after one.
		{"v0.37.0-rc.1", "true"},
		{"v1.2.3-alpha", "true"},
		{"v1.2.3-0.3.7", "true"},
		{"v1.2.3-x.7.z.92", "true"},
		{"v1.2.3-rc.1+build-2", "true"},
		{"v1.0.0-alpha-a.b-c-somethinglong+build.1-aef.1-its-okay", "true"},
		{"v1.2.3----RC-SNAPSHOT.12.9.1--.12+788", "true"},
		{"v1.0.0-0A.is.legal", "true"},

		// Not SemVer 2.0.0 behind a v: refused.
		{"", "refused"},
		{"main", "refused"}, // github.ref_name on a dispatch, were it ever read
		{"1.2.3", "refused"},
		{"V1.2.3", "refused"},
		{"vv1.2.3", "refused"},
		{"v1.2", "refused"},
		{"v1.2.3.4", "refused"},
		{"v01.2.3", "refused"},
		{"v1.02.3", "refused"},
		{"v1.2.3-", "refused"},
		{"v1.2.3+", "refused"},
		{"v1.2.3-01", "refused"},
		{"v1.2.3-rc..1", "refused"},
		{"v1.2.3+build..1", "refused"},
		{"v1.2.3-rc.1_2", "refused"},
	}
	for _, c := range cases {
		if got := runClassify(t, step.Run, c.tag); got != c.want {
			t.Errorf("classify(%q) = %s, want %s", c.tag, got, c.want)
		}
	}
}

// stableGate is the term every stable-only condition carries.
const stableGate = "needs.classify.outputs.prerelease == 'false'"

// gatesOnStable reports whether an `if:` expression can be true only when
// classify called the tag stable: stableGate is one of its top-level `&&`
// terms and nothing at top level is `||`-ed beside it. `x || y` inside
// parentheses is fine — verify-cask's dispatch-or-promoted clause is one.
//
// One `${{ }}` around the whole condition, or none. A partial one — text
// beside it, or two of them — makes GitHub interpolate the condition into
// a string, which is truthy whatever the classify term says.
func gatesOnStable(expr string) bool {
	e := strings.TrimSpace(expr)
	if strings.HasPrefix(e, "${{") && strings.HasSuffix(e, "}}") {
		e = strings.TrimSpace(e[3 : len(e)-2])
	}
	if strings.Contains(e, "${{") || strings.Contains(e, "}}") {
		return false
	}
	isGate := func(term string) bool { return strings.Join(strings.Fields(term), " ") == stableGate }
	found := false
	depth, quoted, start := 0, false, 0
	for i := 0; i < len(e); i++ {
		switch c := e[i]; {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(e[i:], "||"):
			return false
		case depth == 0 && strings.HasPrefix(e[i:], "&&"):
			found = found || isGate(e[start:i])
			start, i = i+2, i+1
		}
	}
	return found || isGate(e[start:])
}

// TestGatesOnStable pins the reader the gate test relies on: `|| true`
// beside the comparison, a partial `${{ }}`, and a negation all fail it,
// while an `||` inside parentheses or a quoted string does not.
func TestGatesOnStable(t *testing.T) {
	cases := map[string]bool{
		"${{ needs.classify.outputs.prerelease == 'false' }}":                                                true,
		"needs.classify.outputs.prerelease == 'false'":                                                       true,
		"${{ !cancelled() && github.event_name == 'push' && needs.classify.outputs.prerelease == 'false' }}": true,
		"${{ a && (b || c) && needs.classify.outputs.prerelease == 'false' }}":                               true,
		"${{ needs.classify.outputs.prerelease == 'false' || true }}":                                        false,
		"${{ a || needs.classify.outputs.prerelease == 'false' }}":                                           false,
		"${{ needs.classify.outputs.prerelease != 'true' }}":                                                 false,
		"${{ !(needs.classify.outputs.prerelease == 'false') }}":                                             false,
		"${{ (needs.classify.outputs.prerelease == 'false' || true) }}":                                      false,
		"${{ a == '||' && needs.classify.outputs.prerelease == 'false' }}":                                   true,
		"needs.classify.outputs.prerelease == 'false' && ${{ true }}":                                        false,
		"${{ needs.classify.outputs.prerelease == 'false' }} && ${{ true }}":                                 false,
		"": false,
	}
	for expr, want := range cases {
		if got := gatesOnStable(expr); got != want {
			t.Errorf("gatesOnStable(%q) = %v, want %v", expr, got, want)
		}
	}
}

// TestReleaseClassifyReadsTheTagAndPublishesItsAnswer pins the two ends of
// the script the test above runs with a TAG it supplies itself: where the
// workflow takes the tag from, and where the answer goes. On a dispatch
// github.ref_name is the branch the run was launched from, so a TAG read
// from it alone would classify "main" and refuse every rehearsal.
func TestReleaseClassifyReadsTheTagAndPublishesItsAnswer(t *testing.T) {
	job, step := classifyStep(t)
	if got, want := step.Env["TAG"], "${{ inputs.tag || github.ref_name }}"; got != want {
		t.Errorf("classify reads TAG from %q, want %q", got, want)
	}
	if got, want := job.Outputs["prerelease"], "${{ steps.tag.outputs.prerelease }}"; got != want {
		t.Errorf("classify publishes prerelease as %q, want %q", got, want)
	}
}

// TestReleasePromoteChecksBeforePublishing holds promote's agreement check
// where it can still stop something: unconditional, reading classify's
// answer, and before the steps that publish the release and push the cask.
func TestReleasePromoteChecksBeforePublishing(t *testing.T) {
	promote := loadWorkflow(t, releaseWorkflow)["promote"]
	index := func(name string) int {
		i := stepIndex(promote, name)
		if i < 0 {
			t.Fatalf("%s: no promote step %q", releaseWorkflow, name)
		}
		return i
	}
	check := index("classify and goreleaser agree on what this release is")
	for _, later := range []string{"Publish the release", "Push it to the tap"} {
		if index(later) < check {
			t.Errorf("promote runs %q before the agreement check", later)
		}
	}
	s := promote.Steps[check]
	if s.If != "" {
		t.Errorf("the agreement check runs only if %q; it has to run every time", s.If)
	}
	if got, want := s.Env["WANT"], "${{ needs.classify.outputs.prerelease }}"; got != want {
		t.Errorf("the agreement check compares against %q, want %q", got, want)
	}
}

// where names a job, or one of its steps, in a failure message.
func where(job, step string) string {
	if step == "" {
		return "job " + job
	}
	return fmt.Sprintf("%s step %q", job, step)
}

// workflowCondition is one `if:` in a workflow and where it sits.
type workflowCondition struct{ at, cond string }

// conditions lists every job's and every step's `if:`, in a slice rather
// than keyed by label: step names are optional and need not be unique, so
// a map would let an unnamed or same-named step overwrite another's
// condition — the job's own, for an unnamed one — and the check would skip
// it. Steps are labelled by position as well as by name.
func conditions(jobs map[string]workflowJob) []workflowCondition {
	var out []workflowCondition
	for _, name := range slices.Sorted(maps.Keys(jobs)) {
		job := jobs[name]
		out = append(out, workflowCondition{where(name, ""), job.If})
		for i, s := range job.Steps {
			label := cmp.Or(s.Name, s.Uses, "unnamed")
			out = append(out, workflowCondition{fmt.Sprintf("%s step %d (%s)", name, i+1, label), s.If})
		}
	}
	return out
}

// TestConditionsKeepsEveryOne pins conditions on the shapes a map lost:
// an unnamed step beside its job's own `if:`, and two steps sharing a name.
func TestConditionsKeepsEveryOne(t *testing.T) {
	jobs := map[string]workflowJob{"j": {
		If: "job-if",
		Steps: []workflowStep{
			{Run: "true", If: "unnamed-if"},
			{Name: "same", If: "first-if"},
			{Name: "same", If: "second-if"},
		},
	}}
	var got []string
	for _, c := range conditions(jobs) {
		got = append(got, c.cond)
	}
	if want := []string{"job-if", "unnamed-if", "first-if", "second-if"}; !slices.Equal(got, want) {
		t.Errorf("conditions = %q, want %q", got, want)
	}
}

// TestReleaseGatesReadClassify holds every stable-only gate to the one
// answer. Three ways back to #193, each silent at run time:
//
//   - a gate that tests the tag string again, the way all of them used to;
//   - a gate that negates 'true', which an empty output — classify failed,
//     or the job never ran — satisfies, so stable-only work runs on a tag
//     nobody classified;
//   - a gate with `|| <anything>` beside the comparison, which makes it no
//     gate at all, or a stable-only step whose gate was simply deleted.
//
// Conditions are read as parsed `if:` values: a folded `if: >-` puts the
// expression on the next line, and a comment quoting the old condition is
// not a condition.
func TestReleaseGatesReadClassify(t *testing.T) {
	jobs := loadWorkflow(t, releaseWorkflow)

	// The stable-only work, by job and step name; "" is the job itself.
	// Named, so a gate cannot vanish by being deleted rather than broken.
	stableOnly := map[string][]string{
		"release":     {"Does the published binary start?", "Can a stranger pull the image?"},
		"promote":     {"Fetch the cask goreleaser rendered", "The artifact is goreleaser's cask, for this tag", "Push it to the tap"},
		"verify-cask": {""},
		"mirror":      {""},
	}
	for _, jobName := range slices.Sorted(maps.Keys(stableOnly)) {
		job, ok := jobs[jobName]
		if !ok {
			t.Errorf("%s: no job %s", releaseWorkflow, jobName)
			continue
		}
		for _, stepName := range stableOnly[jobName] {
			cond := job.If
			if stepName != "" {
				i := stepIndex(job, stepName)
				if i < 0 {
					t.Errorf("%s: no step %q in job %s", releaseWorkflow, stepName, jobName)
					continue
				}
				cond = job.Steps[i].If
			}
			if !gatesOnStable(cond) {
				t.Errorf("%s runs only for a stable tag, so its if has to carry %s as a top-level && term, with no top-level ||; it is %q",
					where(jobName, stepName), stableGate, cond)
			}
		}
	}

	// And every condition in the file: none tests the tag string, and any
	// that reads classify reads it the same way.
	tagString := regexp.MustCompile(`contains\((github\.ref_name|inputs\.tag)`)
	for _, c := range conditions(jobs) {
		if m := tagString.FindString(c.cond); m != "" {
			t.Errorf("%s still tests the tag string (%q); read needs.classify.outputs.prerelease instead", c.at, m)
		}
		if strings.Contains(c.cond, "needs.classify") && !gatesOnStable(c.cond) {
			t.Errorf("%s reads classify as %q; carry %s as a top-level && term instead", c.at, c.cond, stableGate)
		}
	}
}

// TestReleaseJobsThatReadClassifyNeedIt fails on a job that reads
// needs.classify anywhere — a gate, an env value, a script — without
// listing classify in its `needs`. The needs context holds only a job's
// listed dependencies, so the answer would read as empty, a gate would
// compare false, and the stable-only work would be skipped with every job
// green. actionlint catches it; nothing in CI runs actionlint.
func TestReleaseJobsThatReadClassifyNeedIt(t *testing.T) {
	jobs := loadWorkflow(t, releaseWorkflow)
	for _, name := range slices.Sorted(maps.Keys(jobs)) {
		job := jobs[name]
		if job.mentions("needs.classify") && !slices.Contains(job.Needs, "classify") {
			t.Errorf("job %s reads needs.classify but its needs are %v: the answer would arrive empty", name, job.Needs)
		}
	}
}
