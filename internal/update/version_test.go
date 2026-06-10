package update

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		// Plain upgrades.
		{"0.18.0", "0.19.0", true},
		{"0.18.0", "0.18.1", true},
		{"0.18.0", "1.0.0", true},
		// Equal / older — no prompt.
		{"0.18.0", "0.18.0", false},
		{"0.19.0", "0.18.0", false},
		{"1.0.0", "0.19.0", false},
		// The v-prefix must not matter.
		{"v0.18.0", "0.19.0", true},
		{"0.18.0", "v0.19.0", true},
		{"v0.18.0", "v0.18.0", false},
		// Numeric, not lexicographic: 0.9.0 < 0.10.0.
		{"0.9.0", "0.10.0", true},
		{"0.10.0", "0.9.0", false},
		// Missing components count as zero.
		{"0.19", "0.19.0", false},
		{"0.19.0", "0.19", false},
		{"0.19.0", "0.19.1", true},
		// Pre-releases rank below their core version.
		{"0.19.0-rc1", "0.19.0", true},
		{"0.19.0", "0.19.0-rc1", false},
		{"0.19.0-rc1", "0.19.0-rc2", true},
		// Dot-separated numeric pre-release identifiers compare
		// numerically, not lexicographically: rc.2 < rc.10.
		{"0.19.0-rc.2", "0.19.0-rc.10", true},
		{"0.19.0-rc.10", "0.19.0-rc.2", false},
		{"0.19.0-rc.2", "0.19.0-rc.2", false},
		// Build metadata is ignored for precedence (semver §10).
		{"0.19.0+build.5", "0.19.0", false},
		{"0.19.0", "0.19.0+build.5", false},
		{"0.19.0+a", "0.19.0+b", false},
		{"0.18.0+meta", "0.19.0", true},
		// Garbage in → conservative false (no spurious prompt).
		{"", "0.19.0", false},
		{"0.18.0", "", false},
		{"0.18.0", "not-a-version", false},
		{"dev", "0.19.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
