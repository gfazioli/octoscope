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
	"sort"
	"strings"
)

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
	f.Supported = true

	// A key can repeat as name@version at two different install
	// locations — a nested duplicate, or the same version resolved from
	// two registries. Where their integrity disagrees the pair is
	// genuinely ambiguous, and the resolution has to be *deterministic*
	// rather than merely defensible: Go randomises map iteration, so
	// letting the last write win would make consecutive scans of an
	// unchanged lockfile disagree and manufacture a republish finding.
	// The smallest value wins, and the ambiguity is disclosed.
	ambiguous := map[string]bool{}

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

		if prev, seen := f.Packages[key]; seen && prev != value {
			ambiguous[key] = true
			if prev < value {
				value = prev
			}
		}
		f.Packages[key] = value
	}

	if len(ambiguous) > 0 {
		keys := make([]string, 0, len(ambiguous))
		for k := range ambiguous {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		f.Note = "the same version appears at more than one install location with different integrity (" +
			strings.Join(keys, ", ") + "); the lowest was recorded, so a change here is reported conservatively"
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
