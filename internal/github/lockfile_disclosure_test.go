package github

import (
	"strings"
	"testing"
	"time"
)

func packageJSONMatch() ignitionMatch {
	return ignitionMatch{Path: "package.json", Size: 900, BlobSHA: "pkg",
		Rule: ignitionRule{Glob: "package.json", Class: classPackage, Weight: 0}}
}

// disclosureScan runs one scan of a default branch with the given
// surface and returns every weight-0 delta reason it produced, joined.
// No baseline, so the only other line is the "no previous scan" one the
// engine has always emitted.
func disclosureScan(matches []ignitionMatch, blobs map[string]blobAnalysis, foreign []string) string {
	s := evaluateScan(scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1,
		Branches: []scanBranch{{
			Prov: provBranch("main", true), Matches: matches, OtherLockfiles: foreign,
		}},
		Blobs: blobs, Now: time.Now(),
	})
	var out []string
	for _, f := range s.Findings {
		if f.Axis == AxisDelta && f.Weight == 0 {
			out = append(out, f.Reason)
		}
	}
	return strings.Join(out, "\n")
}

// Every reason a lockfile can go unread has to reach the reader, because
// the alternative is silence and silence is indistinguishable from
// "nothing here runs code at install". Two of these reasons cannot be
// re-derived at report time, which is why the gather records them.
func TestEveryUnreadLockfileSaysWhy(t *testing.T) {
	cases := []struct {
		name   string
		unread lockfileUnread
		want   string
	}{
		{"oversized", lockUnreadOversized, "larger than the scan reads"},
		{"fetch failed", lockUnreadFetchFailed, "content could not be fetched"},
		{"npm ignores it", lockUnreadNotAuthoritative, "higher-precedence lockfile"},
		{"reason missing", "", "recorded no reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureScan(
				[]ignitionMatch{lockMatch("package-lock.json", "l1", 400)},
				map[string]blobAnalysis{"l1": {Size: 400, LockfileUnread: tc.unread}},
				nil)

			if !strings.Contains(got, "was not compared") {
				t.Errorf("an unread lockfile must say the surface was not compared; got:\n%s", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("the disclosure must carry the reason %q; got:\n%s", tc.want, got)
			}
		})
	}
}

// A lockfile that WAS read but had something to declare — a schema that
// cannot answer, content that would not decode, an ambiguous duplicate.
// The sentence is written where the fact was established and passed
// through, rather than re-derived here where it would drift.
func TestAReadLockfileWithSomethingToDeclareSaysIt(t *testing.T) {
	cases := map[string]lockfileFacts{
		"unsupported schema": {Note: "lockfileVersion 1 ...", Supported: false},
		"undecodable":        {Note: "could not be decoded as JSON", Unparsed: true},
		"ambiguous":          {Note: "the same version appears at more than one install location", Supported: true},
	}
	for name, lf := range cases {
		t.Run(name, func(t *testing.T) {
			facts := lf
			got := disclosureScan(
				[]ignitionMatch{lockMatch("package-lock.json", "l1", 400)},
				map[string]blobAnalysis{"l1": {Size: 400, Fetched: true, Lockfile: &facts}},
				nil)

			if !strings.Contains(got, facts.Note) {
				t.Errorf("the parser's own note must reach the reader verbatim; got:\n%s", got)
			}
			if !strings.Contains(got, "package-lock.json") {
				t.Errorf("the disclosure must name the file; got:\n%s", got)
			}
		})
	}
}

// A lockfile that parsed cleanly and had nothing to declare says
// nothing. A line on every scan of every healthy repository is how a
// reader learns to skip an axis.
func TestACleanlyReadLockfileIsSilent(t *testing.T) {
	facts := lockfileFacts{Supported: true, Packages: map[string]string{"fsevents@2.3.3": "sha512-a"}}
	got := disclosureScan(
		[]ignitionMatch{lockMatch("package-lock.json", "l1", 400)},
		map[string]blobAnalysis{"l1": {Size: 400, Fetched: true, Lockfile: &facts}},
		nil)

	if strings.Contains(got, "was not compared") {
		t.Errorf("a lockfile that was read owes no disclosure; got:\n%s", got)
	}
}

// The likeliest place for this axis to look like coverage when it is
// not: a JavaScript repository whose lockfile belongs to a package
// manager whose format does not declare install-time execution.
func TestAForeignLockfileIsNamedRatherThanIgnored(t *testing.T) {
	got := disclosureScan(
		[]ignitionMatch{packageJSONMatch()},
		map[string]blobAnalysis{},
		[]string{"pnpm-lock.yaml"})

	if !strings.Contains(got, "pnpm-lock.yaml") || !strings.Contains(got, "npm lockfiles only") {
		t.Errorf("a pnpm lockfile must be named, with what the scan does read; got:\n%s", got)
	}
	// And it must not ALSO claim there is no lockfile: that case has
	// just been stated, more precisely.
	if strings.Contains(got, "commits no npm lockfile") {
		t.Errorf("the two lines say the same thing; only the precise one belongs:\n%s", got)
	}
}

// eslint/eslint and expressjs/express both commit no lockfile at all
// (measured 2026-09-08). A common state, not an anomaly — and still one
// line, because "no findings" and "nothing to look at" differ.
func TestAnNpmProjectWithNoLockfileSaysSo(t *testing.T) {
	got := disclosureScan([]ignitionMatch{packageJSONMatch()}, map[string]blobAnalysis{}, nil)

	if !strings.Contains(got, "commits no npm lockfile") {
		t.Errorf("a package.json with no lockfile owes the reader a line; got:\n%s", got)
	}
}

// ...but a repository that is not an npm project at all owes nothing.
// "No npm lockfile" on a Go or Rust repository is noise, and noise is
// what this axis spends its whole design avoiding.
func TestARepositoryThatIsNotAnNpmProjectIsNotToldAboutNpm(t *testing.T) {
	got := disclosureScan([]ignitionMatch{
		{Path: ".github/workflows/ci.yml", Size: 900, BlobSHA: "w",
			Rule: ignitionRule{Glob: ".github/workflows/*.yml", Class: classCI, Weight: 0}},
	}, map[string]blobAnalysis{}, nil)

	if strings.Contains(got, "npm lockfile") {
		t.Errorf("a non-npm repository must not be told about npm lockfiles; got:\n%s", got)
	}
}

// A side branch's lockfile is out of scope by design, and saying so once
// per branch would drown the axis on a fork-heavy repository.
func TestASideBranchLockfileOwesNoDisclosure(t *testing.T) {
	s := evaluateScan(scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 2,
		Branches: []scanBranch{
			{Prov: provBranch("main", true)},
			{Prov: provBranch("next", false), Matches: []ignitionMatch{
				lockMatch("package-lock.json", "l1", 400)}},
		},
		Blobs: map[string]blobAnalysis{"l1": {Size: 400, LockfileUnread: lockUnreadSideBranch}},
		Now:   time.Now(),
	})
	for _, f := range s.Findings {
		if f.Axis == AxisDelta && strings.Contains(f.Reason, "not compared") {
			t.Errorf("a side-branch lockfile must not produce a disclosure: %q", f.Reason)
		}
	}
}

func TestIsForeignLockfile(t *testing.T) {
	yes := []string{"pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"}
	for _, p := range yes {
		if !isForeignLockfile(p) {
			t.Errorf("%q is a lockfile this scan does not read and must be observed", p)
		}
	}
	// The ones we DO read must never arrive here — they are ignition
	// matches, and counting them twice would disclose that the file we
	// just compared was not compared.
	no := []string{"package-lock.json", "npm-shrinkwrap.json", "go.sum", "Cargo.lock", "src/yarn.lock"}
	for _, p := range no {
		if isForeignLockfile(p) {
			t.Errorf("%q must not be observed as a foreign lockfile", p)
		}
	}
}

// npm-shrinkwrap.json is usually a byte-identical COPY of
// package-lock.json, so in the common case the two share one git blob
// SHA and one blobAnalysis. Reading authority out of that shared entry
// made the ignored file look measured: its surface was recorded a second
// time under its own path, and it got no "npm ignores it" line. A later
// identical change then produced two findings and twice the score for
// one underlying event.
func TestTwoLockfilesWithIdenticalContentDoNotCountTwice(t *testing.T) {
	const sha = "same"
	facts := lockfileFacts{Supported: true, Packages: map[string]string{"fsevents@2.3.3": "sha512-a"}}
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1,
		Branches: []scanBranch{{Prov: provBranch("main", true), Matches: []ignitionMatch{
			lockMatch("package-lock.json", sha, 400),
			lockMatch("npm-shrinkwrap.json", sha, 400),
		}}},
		Blobs: map[string]blobAnalysis{sha: {Size: 400, Fetched: true, Lockfile: &facts}},
		Now:   time.Now(),
	}
	s := evaluateScan(in)

	if n := len(s.Fingerprint.Deps); n != 1 {
		t.Errorf("Deps recorded %d surfaces, want 1 — npm reads one of these files: %v", n, s.Fingerprint.Deps)
	}
	if _, ok := s.Fingerprint.Deps[fingerprintKey("main", "npm-shrinkwrap.json")]; !ok {
		t.Errorf("the recorded surface must be the shrinkwrap's: %v", s.Fingerprint.Deps)
	}

	var told bool
	for _, f := range s.Findings {
		if f.Path == "package-lock.json" && strings.Contains(f.Reason, "higher-precedence") {
			told = true
		}
	}
	if !told {
		t.Error("the ignored file must still be disclosed as ignored, even sharing a blob with the one that was read")
	}
}
