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

	// Equal core: a version WITH a pre-release ranks below one
	// without (1.0.0-rc1 < 1.0.0). Between two pre-releases, fall
	// back to a plain string compare for a stable, if approximate,
	// ordering.
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "" && bp != "":
		return 1
	case ap != "" && bp == "":
		return -1
	case ap < bp:
		return -1
	case ap > bp:
		return 1
	default:
		return 0
	}
}

// parse splits "v1.2.3-rc1" into ([]int{1,2,3}, "rc1", true). The bool
// is false when no numeric component can be read at all.
func parse(v string) (core []int, pre string, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil, "", false
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, "", false
		}
		core = append(core, n)
	}
	if len(core) == 0 {
		return nil, "", false
	}
	return core, pre, true
}
