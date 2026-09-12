package github

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A real v3 shape, trimmed. axios/axios carries exactly this pattern
// (measured 2026-09-08): 684 packages, two of them flagged, one of the
// two a nested duplicate of the other.
const lockfileV3 = `{
  "name": "app",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "app", "version": "1.0.0" },
    "node_modules/axios": {
      "version": "1.7.2",
      "resolved": "https://registry.npmjs.org/axios/-/axios-1.7.2.tgz",
      "integrity": "sha512-plain"
    },
    "node_modules/fsevents": {
      "version": "2.3.3",
      "resolved": "https://registry.npmjs.org/fsevents/-/fsevents-2.3.3.tgz",
      "integrity": "sha512-fsevents",
      "hasInstallScript": true
    },
    "node_modules/playwright/node_modules/fsevents": {
      "version": "2.3.3",
      "resolved": "https://registry.npmjs.org/fsevents/-/fsevents-2.3.3.tgz",
      "integrity": "sha512-fsevents",
      "hasInstallScript": true
    },
    "node_modules/esbuild": {
      "version": "0.28.1",
      "resolved": "https://registry.npmjs.org/esbuild/-/esbuild-0.28.1.tgz",
      "integrity": "sha512-esbuild",
      "hasInstallScript": true
    }
  }
}`

func TestParseLockfileExtractsOnlyTheInstallScriptSubset(t *testing.T) {
	f := parseLockfile([]byte(lockfileV3))

	if !f.Supported || f.Unparsed {
		t.Fatalf("a v3 lockfile must parse and be supported, got supported=%v unparsed=%v", f.Supported, f.Unparsed)
	}
	want := map[string]string{
		"fsevents@2.3.3": "sha512-fsevents",
		"esbuild@0.28.1": "sha512-esbuild",
	}
	if !reflect.DeepEqual(f.Packages, want) {
		t.Errorf("install-script surface = %v, want %v", f.Packages, want)
	}
	// The subset is the whole point: a package without the flag is an
	// ordinary dependency, and including it would put the entire
	// lockfile back into the delta — the noise this axis exists to
	// avoid.
	if _, ok := f.Packages["axios@1.7.2"]; ok {
		t.Error("a package with no install script must not appear in the surface")
	}
	if f.Note != "" {
		t.Errorf("an unambiguous lockfile owes no disclosure, got %q", f.Note)
	}
}

func TestANestedDuplicateIsTheSamePackage(t *testing.T) {
	f := parseLockfile([]byte(lockfileV3))

	// fsevents appears twice — once at the top level and once under
	// playwright — at the same version and integrity. It is one
	// dependency installed twice, not two, and counting it twice would
	// make a hoisting change read as a new install script.
	if len(f.Packages) != 2 {
		t.Fatalf("surface = %v, want 2 distinct packages", f.Packages)
	}
}

func TestTheRootProjectIsNotADependency(t *testing.T) {
	f := parseLockfile([]byte(`{
	  "lockfileVersion": 3,
	  "packages": {
	    "": { "name": "app", "version": "1.0.0", "hasInstallScript": true }
	  }
	}`))

	// The root entry is the scanned repository's own package.json. Its
	// lifecycle scripts are already Axis 1's business; reporting them
	// here would double-count the maintainer's own repo as a dependency
	// finding.
	if len(f.Packages) != 0 {
		t.Errorf("the root project must not appear as a dependency, got %v", f.Packages)
	}
	if !f.Supported {
		t.Error("a v3 lockfile with no flagged dependency is still supported")
	}
}

func TestAWorkspacePackageIsKept(t *testing.T) {
	f := parseLockfile([]byte(`{
	  "lockfileVersion": 3,
	  "packages": {
	    "": { "name": "monorepo", "version": "1.0.0" },
	    "packages/cli": { "version": "0.4.0", "hasInstallScript": true }
	  }
	}`))

	// Local source rather than a fetched tarball, so it has no
	// integrity — but a workspace package that starts running code at
	// install is exactly as interesting as a fetched one.
	//
	// Keyed "cli", not "packages/cli": npm omits `name` when it matches
	// the directory's basename, so the basename is the name (#158).
	if got, ok := f.Packages["cli@0.4.0"]; !ok || got != noIntegrity {
		t.Errorf("workspace surface = %v, want cli@0.4.0 recorded as %q", f.Packages, noIntegrity)
	}
}

// The defect #158 names, end to end at the parser: the same workspace,
// the same install script, moved. Before the fix the two lockfiles shared
// no key at all, and the delta read one dependency that started running
// code at install and one that stopped.
func TestMovingAWorkspaceDoesNotChangeItsKey(t *testing.T) {
	lock := func(path, name string) map[string]string {
		decl := ""
		if name != "" {
			decl = `"name": "` + name + `", `
		}
		return parseLockfile([]byte(`{
		  "lockfileVersion": 3,
		  "packages": {
		    "": { "name": "monorepo", "version": "1.0.0" },
		    "` + path + `": { ` + decl + `"version": "0.4.0", "hasInstallScript": true }
		  }
		}`)).Packages
	}

	// Declared name: carried by npm precisely because it differs from the
	// directory, which is the case the issue proposed fixing.
	//
	// The two directories differ in their LAST segment as well, and the
	// expected key is asserted outright — with both paths ending in "cli"
	// this half passed even with the declared-name branch removed, since
	// the basename fallback answered "cli" on both sides.
	before, after := lock("tools/cli", "@scope/tools"), lock("apps/renamed", "@scope/tools")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("a declared-name workspace moved and its surface changed:\n before %v\n after  %v", before, after)
	}
	if got, ok := before["@scope/tools@0.4.0"]; !ok || got != noIntegrity {
		t.Errorf("surface = %v, want the DECLARED name @scope/tools@0.4.0", before)
	}

	// No declared name, because the package is called after its directory.
	// This is the half a path fallback would have missed — four of the
	// seven real install-script workspaces measured.
	if before, after := lock("packages/cli", ""), lock("apps/cli", ""); !reflect.DeepEqual(before, after) {
		t.Errorf("a basename-named workspace moved and its surface changed:\n before %v\n after  %v", before, after)
	}
}

func TestLockfileVersion1IsUnsupportedNotEmpty(t *testing.T) {
	f := parseLockfile([]byte(`{"lockfileVersion": 1, "dependencies": {"fsevents": {"version": "2.3.3"}}}`))

	// The file read fine; its schema cannot answer the question. An
	// empty result with no disclosure would read as "no dependency runs
	// code at install", which is a claim this file cannot make.
	if f.Supported {
		t.Error("lockfileVersion 1 declares no install scripts and must not be reported as supported")
	}
	if f.Unparsed {
		t.Error("a v1 lockfile parsed correctly; it is unsupported, not unreadable")
	}
	if f.Note == "" {
		t.Error("an unsupported schema owes the reader a disclosure")
	}
}

func TestMalformedContentIsDeclaredNotClean(t *testing.T) {
	f := parseLockfile([]byte("\x00not json at all{{"))

	if !f.Unparsed || f.Supported {
		t.Fatalf("undecodable content must be declared unparsed, got supported=%v unparsed=%v", f.Supported, f.Unparsed)
	}
	if len(f.Packages) != 0 {
		t.Errorf("nothing may be claimed from content we could not read, got %v", f.Packages)
	}
	if f.Note == "" {
		t.Error("an unreadable lockfile owes the reader a disclosure")
	}
}

func TestAMissingIntegrityFallsBackAndNeverReadsAsAChange(t *testing.T) {
	// A git dependency carries no integrity hash. `resolved` still pins
	// content there — it holds the commit SHA — so it is the fallback,
	// and the sentinel is last.
	git := `{
	  "lockfileVersion": 3,
	  "packages": {
	    "node_modules/tool": {
	      "version": "1.0.0",
	      "resolved": "git+ssh://git@github.com/o/r.git#abc123",
	      "hasInstallScript": true
	    },
	    "node_modules/linked": { "version": "0.0.0", "hasInstallScript": true }
	  }
	}`
	f := parseLockfile([]byte(git))

	if got := f.Packages["tool@1.0.0"]; got != "git+ssh://git@github.com/o/r.git#abc123" {
		t.Errorf("a git dependency must fall back to resolved, got %q", got)
	}
	if got := f.Packages["linked@0.0.0"]; got != noIntegrity {
		t.Errorf("an entry with neither integrity nor resolved must record %q, got %q", noIntegrity, got)
	}

	// The delta compares these values for equality across scans. A
	// missing value must compare equal to itself, or an unchanged
	// dependency would be reported as republished — the one false
	// positive that would discredit the sharpest case this axis has.
	again := parseLockfile([]byte(git))
	if !reflect.DeepEqual(f.Packages, again.Packages) {
		t.Errorf("two parses of the same lockfile disagree: %v vs %v", f.Packages, again.Packages)
	}
}

func TestAnAmbiguousDuplicateIsDeterministicAndDisclosed(t *testing.T) {
	// The same version at two install locations with *different*
	// integrity. Go randomises map iteration, so "last write wins" would
	// make consecutive scans of an unchanged lockfile disagree — and a
	// disagreement here is a republish finding, the most alarming thing
	// this axis can say. It must be stable.
	src := `{
	  "lockfileVersion": 3,
	  "packages": {
	    "node_modules/dup": { "version": "1.0.0", "integrity": "sha512-bbb", "hasInstallScript": true },
	    "node_modules/nested/node_modules/dup": { "version": "1.0.0", "integrity": "sha512-aaa", "hasInstallScript": true }
	  }
	}`
	first := parseLockfile([]byte(src))
	for i := 0; i < 50; i++ {
		got := parseLockfile([]byte(src))
		if !reflect.DeepEqual(first.Packages, got.Packages) {
			t.Fatalf("parse is not deterministic: %v vs %v", first.Packages, got.Packages)
		}
	}
	if got := first.Packages["dup@1.0.0"]; got != "sha512-aaa"+ambiguousSep+"sha512-bbb" {
		t.Errorf("an ambiguous key must record every value, sorted, got %q", got)
	}
	if first.Note == "" {
		t.Error("an ambiguous duplicate owes the reader a disclosure")
	}
}

// The first version of this kept only the lowest of the conflicting
// values, which is deterministic and lossy: a change confined to any
// other location left the recorded value untouched. That silently drops
// a republish, which is the sharpest thing this axis can report, so it
// is worth its own test rather than trust in the reduction.
func TestAChangeAtAnyLocationOfAnAmbiguousKeyIsVisible(t *testing.T) {
	src := func(second string) []byte {
		return []byte(`{
		  "lockfileVersion": 3,
		  "packages": {
		    "node_modules/dup": { "version": "1.0.0", "integrity": "sha512-aaa", "hasInstallScript": true },
		    "node_modules/nested/node_modules/dup": { "version": "1.0.0", "integrity": "` + second + `", "hasInstallScript": true }
		  }
		}`)
	}
	// "sha512-zzz" sorts ABOVE "sha512-aaa", so a reduction that keeps
	// the minimum returns the identical value for both of these.
	before := parseLockfile(src("sha512-bbb")).Packages["dup@1.0.0"]
	after := parseLockfile(src("sha512-zzz")).Packages["dup@1.0.0"]

	if before == after {
		t.Errorf("a republish at the non-minimum location is invisible: both parses recorded %q", before)
	}
}

// A packages map is necessary but not sufficient. hasInstallScript was
// measured at lockfileVersion 2 and 3; a schema nobody has looked at
// must not be answered from a field whose meaning was assumed.
func TestAnUnmeasuredLockfileVersionIsNotClaimedAsSupported(t *testing.T) {
	f := parseLockfile([]byte(`{
	  "lockfileVersion": 4,
	  "packages": {
	    "": { "name": "app", "version": "1.0.0" },
	    "node_modules/fsevents": { "version": "2.3.3", "integrity": "sha512-x", "hasInstallScript": true }
	  }
	}`))

	if f.Supported {
		t.Error("lockfileVersion 4 has not been measured and must not be reported as supported")
	}
	if f.Unparsed {
		t.Error("the file decoded fine; it is unmeasured, not unreadable")
	}
	if len(f.Packages) != 0 {
		t.Errorf("nothing may be claimed from a schema we do not vouch for, got %v", f.Packages)
	}
	if !strings.Contains(f.Note, "4") {
		t.Errorf("the disclosure must name the version it declined, got %q", f.Note)
	}
}

// v2 is the other end of the measured range, and it is a real npm 7
// lockfile shape.
func TestLockfileVersion2IsSupported(t *testing.T) {
	f := parseLockfile([]byte(`{
	  "lockfileVersion": 2,
	  "packages": {
	    "node_modules/esbuild": { "version": "0.28.1", "integrity": "sha512-e", "hasInstallScript": true }
	  }
	}`))
	if !f.Supported {
		t.Errorf("lockfileVersion 2 must be supported, note %q", f.Note)
	}
	if got := f.Packages["esbuild@0.28.1"]; got != "sha512-e" {
		t.Errorf("surface = %v, want esbuild@0.28.1", f.Packages)
	}
}

func TestEmptyContentIsUnparsed(t *testing.T) {
	f := parseLockfile(nil)
	if !f.Unparsed {
		t.Error("empty content is not a lockfile with no install scripts; it is unreadable")
	}
}

// A lockfile is attacker-controlled input, so the parser's job is to be
// dull on shapes npm never writes. A trailing slash used to produce an
// empty name and drop the entry — a dependency that runs code at install
// vanishing from the surface without a word, which is the one outcome
// this axis refuses.
func TestNonCanonicalPathsStillName(t *testing.T) {
	tests := []struct{ path, declared, want string }{
		{"packages/cli/", "", "cli"},
		{"node_modules/pkg/", "", "pkg"},
		{"packages//cli", "", "cli"},
		{"/cli", "", "cli"},
	}
	for _, tc := range tests {
		if got := packageNameOf(tc.path, tc.declared); got != tc.want {
			t.Errorf("packageNameOf(%q, %q) = %q, want %q", tc.path, tc.declared, got, tc.want)
		}
	}
}

// Keying a workspace by its name means two nameless workspaces sharing a
// basename and a version land in one bucket. npm refuses two workspaces
// with one name, so that only happens in a lockfile written to produce
// it — and the point of this test is that producing it buys nothing: a
// workspace records the sentinel whatever its entry claims, so the bucket
// cannot hold two integrity-shaped values and the delta cannot be made to
// report "same version, different bytes" at weight 4.
func TestACraftedWorkspaceCollisionCannotForgeARepublish(t *testing.T) {
	f := parseLockfile([]byte(`{
	  "lockfileVersion": 3,
	  "packages": {
	    "": { "name": "monorepo", "version": "1.0.0" },
	    "packages/cli": { "version": "1.0.0", "hasInstallScript": true,
	                      "integrity": "sha512-AAAA" },
	    "apps/cli":     { "version": "1.0.0", "hasInstallScript": true,
	                      "integrity": "sha512-BBBB" }
	  }
	}`))

	got, ok := f.Packages["cli@1.0.0"]
	if !ok {
		t.Fatalf("surface = %v, want the colliding workspaces recorded under cli@1.0.0", f.Packages)
	}
	if got != noIntegrity {
		t.Errorf("colliding workspaces recorded %q, want the sentinel %q — two hash-shaped values here are what the delta scores as a republish", got, noIntegrity)
	}
}

func TestPackageNameOf(t *testing.T) {
	tests := []struct {
		path, declared, want string
	}{
		// A fetched dependency: the install location is the identity.
		{"node_modules/fsevents", "", "fsevents"},
		{"node_modules/@scope/pkg", "", "@scope/pkg"},
		{"node_modules/playwright/node_modules/esbuild", "", "esbuild"},
		// An aliased install declares another name; the location still
		// wins, deliberately — see packageNameOf.
		{"node_modules/react-is-18", "react-is", "react-is-18"},

		// A workspace: the path is where it lives, not what it is.
		{"packages/cli", "@scope/cli", "@scope/cli"},
		{"packages/playwright-chromium", "", "playwright-chromium"},
		// The same package after a monorepo reorganisation — the key must
		// not move with it. This is #158.
		{"apps/cli", "@scope/cli", "@scope/cli"},
		{"apps/playwright-chromium", "", "playwright-chromium"},
		// A single-segment workspace path, and the root's own sibling.
		{"cli", "", "cli"},
	}
	for _, tc := range tests {
		if got := packageNameOf(tc.path, tc.declared); got != tc.want {
			t.Errorf("packageNameOf(%q, %q) = %q, want %q", tc.path, tc.declared, got, tc.want)
		}
	}
}

// --- the second review round --------------------------------------------

// Sanitize strips terminal-control escapes and deliberately keeps
// newlines, which is right for a commit message and wrong for a package
// name interpolated into a line-oriented report: a name carrying "\n"
// forges an extra visual finding in a security report. Flattened at the
// parse boundary, so nothing downstream has to remember.
func TestALockfileCannotSmuggleANewlineIntoTheReport(t *testing.T) {
	f := parseLockfile([]byte("{\n  \"lockfileVersion\": 3,\n  \"packages\": {\n" +
		"    \"node_modules/evil\\nCRITICAL: everything is fine\": " +
		"{\"version\": \"1.0\\n0.0\", \"integrity\": \"sha512-a\\nb\", \"hasInstallScript\": true}\n  }\n}"))

	for k, v := range f.Packages {
		if strings.ContainsAny(k, "\n\r\t") {
			t.Errorf("the recorded key still spans lines: %q", k)
		}
		if strings.ContainsAny(v, "\n\r\t") {
			t.Errorf("the recorded value still spans lines: %q", v)
		}
	}
	if len(f.Packages) != 1 {
		t.Fatalf("surface = %v, want the one entry", f.Packages)
	}
}

// A republish finding is a claim about bytes, and only a hash supports
// it. A `resolved` URL says where a dependency came FROM.
func TestIsIntegrity(t *testing.T) {
	yes := []string{"sha512-abc", "sha256-abc", "sha1-abc", "sha384-abc",
		"sha512-a" + ambiguousSep + "sha512-b"}
	for _, v := range yes {
		if !isIntegrity(v) {
			t.Errorf("%q is a content hash", v)
		}
	}
	no := []string{"", noIntegrity, "https://registry.npmjs.org/x/-/x-1.0.0.tgz",
		"git+ssh://git@github.com/o/r.git#abc123",
		// one hash and one URL is not a hash: the composite is only as
		// strong as its weakest part.
		"sha512-a" + ambiguousSep + "https://example.com/x.tgz"}
	for _, v := range no {
		if isIntegrity(v) {
			t.Errorf("%q is not a content hash", v)
		}
	}
}

// The ambiguity note becomes one finding's text, and the per-path
// finding cap does not reach inside a single Reason — so this list is
// the one place a crafted lockfile could still write an unbounded line
// into the report.
func TestTheAmbiguityDisclosureIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"lockfileVersion": 3, "packages": {`)
	for i := 0; i < maxAmbiguousListed*5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"node_modules/p%03d": {"version":"1.0.0","integrity":"sha512-a","hasInstallScript":true},`, i)
		fmt.Fprintf(&b, `"node_modules/nested/node_modules/p%03d": {"version":"1.0.0","integrity":"sha512-b","hasInstallScript":true}`, i)
	}
	b.WriteString("}}")

	f := parseLockfile([]byte(b.String()))
	if n := strings.Count(f.Note, "p0"); n > maxAmbiguousListed {
		t.Errorf("the note names %d keys, over the %d cap", n, maxAmbiguousListed)
	}
	if !strings.Contains(f.Note, "more") {
		t.Errorf("a truncated list must say how many were left out: %q", f.Note)
	}
}

// The set reduction is NOT lossless, and this pins the miss rather than
// letting a future reader discover it. Two locations swapping their
// contents reduce to the same composite. Recording the location instead
// would key on the install path, which hoisting rearranges constantly —
// trading a rare miss for routine noise is the trade this axis refuses.
// Documented in docs/design/supply-chain-scan.md, Honest gaps.
func TestASwapBetweenTwoInstallLocationsIsInvisible(t *testing.T) {
	src := func(a, b string) []byte {
		return []byte(`{"lockfileVersion": 3, "packages": {
		  "node_modules/dup": {"version":"1.0.0","integrity":"` + a + `","hasInstallScript":true},
		  "node_modules/nested/node_modules/dup": {"version":"1.0.0","integrity":"` + b + `","hasInstallScript":true}
		}}`)
	}
	before := parseLockfile(src("sha512-aaa", "sha512-bbb")).Packages["dup@1.0.0"]
	after := parseLockfile(src("sha512-bbb", "sha512-aaa")).Packages["dup@1.0.0"]

	if before != after {
		t.Fatalf("the reduction has become location-sensitive (%q vs %q) — that is a "+
			"behaviour change worth a decision, not a silent one: hoisting will now "+
			"produce findings", before, after)
	}
}
