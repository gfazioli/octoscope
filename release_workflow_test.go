package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The release workflow decides once whether a tag is a prerelease, in the
// classify job of release.yml, and every stable-only step reads that answer
// (#193). Before, each gate asked whether the tag contained a hyphen while
// goreleaser parsed the tag's prerelease field, and the two disagreed on
// build metadata: `v1.2.3+build-1` would have published a stable release
// with the cask and the mirror skipped, every job green.
//
// The decision is a shell script inside YAML, so these tests run THAT
// script, extracted from the file the workflow runs. A copy of the logic in
// Go would test the copy.

const releaseWorkflow = ".github/workflows/release.yml"

// classifyScript returns the run block of the classify job's `tag` step.
func classifyScript(t *testing.T) string {
	t.Helper()
	for _, s := range loadWorkflow(t, releaseWorkflow)["classify"].Steps {
		if s.ID == "tag" && s.Run != "" {
			return s.Run
		}
	}
	t.Fatalf("%s: no classify job with a `tag` step to run", releaseWorkflow)
	return ""
}

// runClassify runs the script the way the runner does — bash with -e and
// pipefail, TAG in the environment, GITHUB_OUTPUT pointing at a file — and
// returns the prerelease output, or refused=true when the script exited
// non-zero.
func runClassify(t *testing.T, script, tag string) (prerelease string, refused bool) {
	t.Helper()
	outFile := filepath.Join(t.TempDir(), "github_output")
	cmd := exec.Command("bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
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
		return "", true
	}
	lines := strings.Split(strings.TrimSpace(string(written)), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "prerelease=") {
		t.Fatalf("%q: want exactly one prerelease= line in GITHUB_OUTPUT, got %q (log: %s)", tag, written, log)
	}
	return strings.TrimPrefix(lines[0], "prerelease="), false
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
	script := classifyScript(t)

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
		got, refused := runClassify(t, script, c.tag)
		if refused {
			got = "refused"
		}
		if got != c.want {
			t.Errorf("classify(%q) = %s, want %s", c.tag, got, c.want)
		}
	}
}

// TestReleaseGatesReadClassify holds every stable-only gate to the one
// answer. Three ways back to #193, each silent at run time:
//
//   - a gate that tests the tag string again, the way all of them used to;
//   - a gate that negates 'true', which an empty output — classify failed,
//     or the job never ran — satisfies, so stable-only work runs on a tag
//     nobody classified;
//   - a job that reads needs.classify without listing classify in its
//     `needs`. The needs context holds only a job's listed dependencies, so
//     the answer would read as empty, the gate would compare false, and the
//     stable-only work would be skipped with every job green. actionlint
//     catches it; nothing in CI runs actionlint.
func TestReleaseGatesReadClassify(t *testing.T) {
	raw, err := os.ReadFile(releaseWorkflow)
	if err != nil {
		t.Fatalf("read %s: %v", releaseWorkflow, err)
	}
	text := string(raw)

	if m := regexp.MustCompile(`contains\((github\.ref_name|inputs\.tag)`).FindString(text); m != "" {
		t.Errorf("%s still tests the tag string (%q); read needs.classify.outputs.prerelease instead", releaseWorkflow, m)
	}

	// Gates are `if:` lines. promote also reads the answer as a value, to
	// compare it with goreleaser's, and that read is not a gate.
	read := regexp.MustCompile(`needs\.classify\.outputs\.prerelease(\s*[!=]=\s*'\w*')?`)
	gates := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "if:") {
			continue
		}
		for _, r := range read.FindAllStringSubmatch(line, -1) {
			gates++
			if strings.Join(strings.Fields(r[1]), " ") != "== 'false'" {
				t.Errorf("%s: gate %q; compare with == 'false', so an empty answer skips the stable-only work", releaseWorkflow, strings.TrimSpace(line))
			}
		}
	}
	if gates == 0 {
		t.Fatalf("%s: no gate reads needs.classify.outputs.prerelease", releaseWorkflow)
	}

	var wf struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", releaseWorkflow, err)
	}
	for name, job := range wf.Jobs {
		body, err := yaml.Marshal(&job)
		if err != nil {
			t.Fatalf("re-encode job %s: %v", name, err)
		}
		if !strings.Contains(string(body), "needs.classify") {
			continue
		}
		var j struct {
			Needs yaml.Node `yaml:"needs"`
		}
		if err := job.Decode(&j); err != nil {
			t.Fatalf("decode job %s: %v", name, err)
		}
		var needs []string
		switch j.Needs.Kind {
		case yaml.ScalarNode:
			needs = []string{j.Needs.Value}
		case yaml.SequenceNode:
			if err := j.Needs.Decode(&needs); err != nil {
				t.Fatalf("decode %s.needs: %v", name, err)
			}
		}
		if !slices.Contains(needs, "classify") {
			t.Errorf("job %s reads needs.classify but its needs are %v: the answer would arrive empty", name, needs)
		}
	}
}
