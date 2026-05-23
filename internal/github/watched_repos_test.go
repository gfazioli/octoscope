package github

import "testing"

// splitOwnerNameKey is the bare-bones parser used by
// FetchWatchedRepos; the strict sanitisation pass happens
// upstream in config.SanitizeRepoList. Pins the contract that
// no double-slash, leading-slash or trailing-slash entry can
// produce a half-valid (owner, name) pair the caller would
// then issue against GitHub.
func TestSplitOwnerNameKey(t *testing.T) {
	tests := []struct {
		in        string
		wantOwner string
		wantName  string
	}{
		{"gfazioli/octoscope", "gfazioli", "octoscope"},
		{"a/b", "a", "b"},
		{"", "", ""},
		{"no-slash", "", ""},
		{"/leading", "", ""},
		{"trailing/", "", ""},
		{"three/segments/here", "", ""},
		{"//", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			gotO, gotN := splitOwnerNameKey(tt.in)
			if gotO != tt.wantOwner || gotN != tt.wantName {
				t.Errorf("splitOwnerNameKey(%q) = (%q, %q), want (%q, %q)",
					tt.in, gotO, gotN, tt.wantOwner, tt.wantName)
			}
		})
	}
}
