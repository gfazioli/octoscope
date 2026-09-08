package github

// Axis 1b — the dependency install surface (#108).
//
// The delta axis notices when something that auto-executes *in the
// repository's own source* changes. It cannot see a dependency that
// starts executing on install: that is not a code change and not a new
// file, it is a lockfile diff — the one part of a pull request nobody
// reads line by line, and one that branch protection and required
// reviews do nothing about, because there is nothing a reviewer would
// recognise as suspicious.
//
// Like workflow.go, this file only *extracts facts*. The scoring lives
// in evaluateScan with the other axes, so the same pure-function
// discipline applies.
//
// npm only, and that is a measured decision rather than a starting
// point nobody revisited (2026-09-08):
//
//   - package-lock.json v2/v3 and npm-shrinkwrap.json declare the signal
//     themselves, per entry, beside the resolved version and integrity
//     hash: "hasInstallScript": true.
//   - package-lock.json v1 has no packages map and no such flag.
//   - pnpm-lock.yaml carried `requiresBuild` at lockfileVersion 6 and
//     **dropped it at 9**, so building on it would ship a rule that
//     decays. `hasBin` is not a substitute: a bin entry runs when the
//     developer chooses to run it, which is not this threat.
//   - yarn.lock (v1 and berry) declares nothing equivalent;
//     dependenciesMeta.built is a package.json opt-out, not a
//     declaration.
//   - go.sum is not applicable: the Go toolchain runs no install scripts.
//
// The report must therefore say npm, and never read as ecosystem-agnostic
// coverage.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The lockfile schema versions whose per-entry hasInstallScript flag has
// actually been measured (2026-09-08); npm 11 writes 3. A `packages` map
// is necessary but not sufficient to trust the flag, so the version is
// checked rather than merely decoded.
const (
	lockfileVersionMin = 2
	lockfileVersionMax = 3
)

// ambiguousSep joins the conflicting values of one ambiguous key. It
// cannot occur inside an integrity — a space-separated list of
// `alg-base64` tokens, none of which is a bare "+" — nor inside a
// resolved URL, so the composite stays splittable and can never collide
// with a real single value.
const ambiguousSep = " + "

// noIntegrity marks a package whose lockfile entry carries neither an
// integrity hash nor a resolved URL — git and `link:` dependencies,
// mainly.
//
// It is a stable non-empty sentinel on purpose. The delta compares these
// values for equality, so "no integrity recorded" must compare equal to
// itself across scans; an empty string would work today and invites the
// first refactor that treats "" as absent to manufacture a republish
// finding out of a dependency that never moved.
const noIntegrity = "-"

// lockfileFacts is what one lockfile says about which dependencies run
// code at install time.
//
// Supported and Unparsed are separate because they are different claims
// and only one of them is a failure. A v1 lockfile is a file we read
// correctly and whose schema cannot answer the question; a malformed one
// is a file we could not read at all. Both mean "not compared", and
// neither may be allowed to render as "nothing found" — the honesty
// contract workflowFacts.Unparsed carries for Axis 4.
type lockfileFacts struct {
	// Packages maps "name@version" to the integrity of each dependency
	// that carries an install script.
	//
	// The key holds the version because that is what separates an
	// ordinary bump from a republish. Same key, different integrity, is
	// the same version shipping different content — the sharp case. A
	// new key for a name that had none is a dependency that started
	// executing. A new key for a name already present is a bump, which
	// is ordinary churn.
	Packages map[string]string

	// Supported reports whether this file's schema can express install
	// scripts at all.
	Supported bool

	// Unparsed reports that the content could not be decoded, so the
	// report can say the file was not understood rather than imply it
	// was checked and found clean.
	Unparsed bool

	// Note is the one-line reason, for the disclosure the report owes
	// the reader whenever Supported is false, Unparsed is true, or the
	// file was ambiguous. Empty when there is nothing to disclose.
	Note string
}

// lockPackage is one entry of the v2/v3 `packages` map. Only the four
// fields this axis reads are declared; everything else in a real entry
// is ignored by encoding/json.
type lockPackage struct {
	Version          string `json:"version"`
	Resolved         string `json:"resolved"`
	Integrity        string `json:"integrity"`
	HasInstallScript bool   `json:"hasInstallScript"`
}

// lockfileDoc is the subset of the lockfile schema this axis decodes.
type lockfileDoc struct {
	LockfileVersion int                    `json:"lockfileVersion"`
	Packages        map[string]lockPackage `json:"packages"`
}

// parseLockfile extracts the install-script surface from one npm
// lockfile.
//
// Callers must cap the content length before calling — the scan already
// does, via maxBlobScanBytes — because this hands attacker-controlled
// bytes to a JSON decoder.
func parseLockfile(content []byte) lockfileFacts {
	f := lockfileFacts{Packages: map[string]string{}}

	var doc lockfileDoc
	if err := json.Unmarshal(content, &doc); err != nil {
		f.Unparsed = true
		f.Note = "the lockfile could not be decoded as JSON, so the dependency install surface was not compared"
		return f
	}

	// v1 (npm 6) has a `dependencies` tree and no per-entry flag. The
	// file is fine; the schema cannot answer the question. Reported as
	// such rather than as an empty result, which would read as "no
	// dependency runs code at install" — a claim this file cannot make.
	if doc.Packages == nil {
		f.Note = "this lockfile format does not declare install-time execution (lockfileVersion 1 or an unrecognised schema), so the dependency install surface was not compared"
		return f
	}
	// A `packages` map is necessary but not sufficient. hasInstallScript
	// was measured at lockfileVersion 2 and 3 and nowhere else, so a
	// future schema that happens to carry a packages map would decode
	// cleanly here and be answered from a field nobody has checked still
	// means what it meant. Declining a version we have not measured
	// costs a disclosure the reader can act on; accepting one costs a
	// silent wrong answer, and this axis is built on never trading the
	// second for the first.
	if doc.LockfileVersion < lockfileVersionMin || doc.LockfileVersion > lockfileVersionMax {
		f.Note = fmt.Sprintf(
			"lockfileVersion %d is not one this scan has measured (it reads %d and %d), so the dependency install surface was not compared",
			doc.LockfileVersion, lockfileVersionMin, lockfileVersionMax)
		return f
	}
	f.Supported = true

	// A key can repeat as name@version at two different install
	// locations — a nested duplicate, or the same version resolved from
	// two registries. Every distinct value is collected first and the
	// key reduced afterwards, for two separate reasons.
	//
	// Determinism: Go randomises map iteration, so letting the last
	// write win would make consecutive scans of an unchanged lockfile
	// disagree and manufacture a republish finding out of nothing.
	//
	// Fidelity: keeping only ONE of the conflicting values — the lowest,
	// as this first did — hides every change confined to the others. Two
	// locations at aaa and bbb still reduce to aaa after bbb becomes
	// zzz, so the sharpest case this axis has would be dropped in
	// silence. The reduction is the sorted set instead, which moves
	// whenever any location moves.
	values := map[string]map[string]bool{}

	for path, p := range doc.Packages {
		if !p.HasInstallScript {
			continue
		}
		// The root entry ("") is the *scanned* repository's own
		// package.json, not a dependency. Its lifecycle scripts are Axis
		// 1's business already, through the "package.json" row of
		// ignitionCatalog.
		if path == "" {
			continue
		}
		name := packageNameFromPath(path)
		if name == "" {
			continue
		}
		key := name + "@" + p.Version

		value := p.Integrity
		if value == "" {
			// A git or link dependency has no integrity hash. `resolved`
			// still pins content for the git case (it carries the commit
			// SHA), so it is the better fallback; the sentinel is last.
			value = p.Resolved
		}
		if value == "" {
			value = noIntegrity
		}

		if values[key] == nil {
			values[key] = map[string]bool{}
		}
		values[key][value] = true
	}

	var ambiguous []string
	for key, set := range values {
		if len(set) == 1 {
			for v := range set {
				f.Packages[key] = v
			}
			continue
		}
		vs := make([]string, 0, len(set))
		for v := range set {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		f.Packages[key] = strings.Join(vs, ambiguousSep)
		ambiguous = append(ambiguous, key)
	}

	if len(ambiguous) > 0 {
		sort.Strings(ambiguous)
		f.Note = "the same version appears at more than one install location with different integrity (" +
			strings.Join(ambiguous, ", ") + "); every value is recorded, joined by \"" + ambiguousSep +
			"\", so the entry is a composite rather than an integrity to quote — and a change at any one location still moves it"
	}

	return f
}

// packageNameFromPath turns a lockfile entry path into the package name.
//
// Entries are keyed by install location, so a nested copy reads as
// "node_modules/playwright/node_modules/fsevents" and the name is what
// follows the last node_modules segment. A workspace entry
// ("packages/cli") has no such segment and is its own name — it is local
// source rather than a fetched dependency, and it is kept, because a
// workspace package that gains an install script is exactly as
// interesting as a fetched one.
func packageNameFromPath(path string) string {
	const seg = "node_modules/"
	if i := strings.LastIndex(path, seg); i >= 0 {
		return path[i+len(seg):]
	}
	return path
}

// foreignLockfiles are the lockfiles of the other JavaScript package
// managers. They are deliberately NOT in ignitionCatalog — a catalog row
// is a claim that a path auto-executes, and these carry no such claim,
// which is why TestMatchIgnition asserts they do not match.
//
// They are observed anyway, and only for this: the difference between an
// explicit "this repository's dependency install surface was not
// compared" and silence. Silence is the failure mode this whole axis is
// built to avoid, and a pnpm or Yarn repository is the single most
// likely place to hit it.
//
// The list is JavaScript-only because install scripts are an
// npm-ecosystem concept; go.sum and Cargo.lock belong to toolchains that
// run nothing at install, so naming them would be noise rather than
// disclosure.
var foreignLockfiles = map[string]string{
	"pnpm-lock.yaml": "pnpm",
	"yarn.lock":      "Yarn",
	"bun.lockb":      "Bun",
	"bun.lock":       "Bun",
}

// isForeignLockfile reports whether a repo-root-relative path is a
// lockfile from an ecosystem this scan does not read.
func isForeignLockfile(p string) bool {
	_, ok := foreignLockfiles[p]
	return ok
}

// lockfileReadOrder keeps the lockfile matches of one branch, most
// authoritative first, and drops everything that is not a lockfile.
//
// The order decides which file the scan sees, because
// maxLockfileFetches bounds how many are read — and npm's own answer to
// a repository carrying both is unambiguous: "If both package-lock.json
// and npm-shrinkwrap.json are present in the root of a project,
// npm-shrinkwrap.json will take precedence and package-lock.json will
// be ignored" (docs.npmjs.com/cli/v11/configuring-npm/package-lock-json,
// read 2026-09-08). Reading the ignored file would describe an install
// surface npm never uses.
//
// The tie-break on path is not decoration. Tree order is GitHub's to
// choose, and a pick that flipped between two scans would replace the
// recorded dependency set wholesale — manufacturing a republish finding
// for every entry in it, which is the one false positive this axis is
// weighted to avoid.
func lockfileReadOrder(matches []ignitionMatch) []ignitionMatch {
	var out []ignitionMatch
	for _, m := range matches {
		if m.Rule.Class == classLockfile {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := lockfileRank(out[i].Path), lockfileRank(out[j].Path)
		if ri != rj {
			return ri < rj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// lockfileRank orders the known lockfile names by npm's own precedence.
// A name with no rank sorts last rather than silently outranking a file
// npm would actually use — so adding a catalog row without thinking
// about precedence degrades to "read after the ones we understand"
// instead of to a wrong answer.
func lockfileRank(p string) int {
	switch p {
	case "npm-shrinkwrap.json":
		return 0
	case "package-lock.json":
		return 1
	}
	return 2
}
