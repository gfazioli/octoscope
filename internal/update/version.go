// Package update powers octoscope's in-app "a newer version is
// available" check: comparing the running version against the latest
// published release, detecting how the binary was installed so the
// right upgrade command can be suggested, and caching the last check
// on disk so the network isn't hit on every launch.
//
// It is deliberately decoupled from the github client (the HTTP fetch
// lives in internal/github) and from the TUI (orchestration lives in
// internal/ui) — this package is pure logic plus a small on-disk cache.
package update

import (
	"strconv"
	"strings"
)

// IsNewer reports whether latest is a strictly newer version than
// current. Both accept an optional leading "v" (so "0.19.0" and
// "v0.19.0" compare equal). Comparison is numeric per dotted component
// — crucially NOT lexicographic, which would wrongly rank "0.9.0" above
// "0.10.0". A pre-release suffix ("-rc1", "-beta") makes a version
// lower than the same core version without one (0.19.0-rc1 < 0.19.0),
// matching semver precedence closely enough for an upgrade hint.
//
// Unparseable input is treated conservatively: if either side can't be
// read as a version, IsNewer returns false (no spurious "update
// available" prompt on garbage).
func IsNewer(current, latest string) bool {
	return compare(current, latest) < 0
}

// compare returns -1 if a < b, 0 if equal, +1 if a > b. Returns 0
// (treated as "not newer") when either side is unparseable.
func compare(a, b string) int {
	ac, ap, aok := parse(a)
	bc, bp, bok := parse(b)
	if !aok || !bok {
		return 0
	}

	// Compare the numeric core component by component. Missing
	// components count as 0 so "0.19" == "0.19.0".
	n := len(ac)
	if len(bc) > n {
		n = len(bc)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(ac) {
			av = ac[i]
		}
		if i < len(bc) {
			bv = bc[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}

	// Equal core: precedence is decided by the pre-release identifiers
	// (build metadata was already dropped in parse — semver ignores it).
	return comparePre(ap, bp)
}

// comparePre orders two pre-release identifier lists by semver
// precedence: a version WITHOUT a pre-release outranks one WITH
// (1.0.0 > 1.0.0-rc1). Identifiers compare left to right — numeric ones
// numerically (so rc.2 < rc.10), a numeric identifier below an
// alphanumeric one, and a longer list wins when all shared identifiers
// are equal.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0: // no pre-release outranks a pre-release
		return 1
	case len(b) == 0:
		return -1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ai, aNum := atoi(a[i])
		bi, bNum := atoi(b[i])
		switch {
		case aNum && bNum:
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		case aNum != bNum:
			// A numeric identifier has lower precedence than an
			// alphanumeric one.
			if aNum {
				return -1
			}
			return 1
		default:
			if a[i] != b[i] {
				if a[i] < b[i] {
					return -1
				}
				return 1
			}
		}
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
}

// atoi reports whether s is all digits and, if so, its integer value.
func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parse splits "v1.2.3-rc.1+build" into ([]int{1,2,3},
// []string{"rc","1"}, true). Build metadata (the "+…" part) is dropped
// entirely — semver ignores it for precedence, so 1.0.0+a == 1.0.0+b ==
// 1.0.0. The bool is false when no numeric core can be read.
func parse(v string) (core []int, pre []string, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil, nil, false
	}
	// Drop build metadata before anything else (ignored for precedence).
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	// Split off the pre-release (everything after the first '-').
	var preStr string
	if i := strings.IndexByte(v, '-'); i >= 0 {
		preStr = v[i+1:]
		v = v[:i]
	}
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, nil, false
		}
		core = append(core, n)
	}
	if len(core) == 0 {
		return nil, nil, false
	}
	if preStr != "" {
		pre = strings.Split(preStr, ".")
	}
	return core, pre, true
}
