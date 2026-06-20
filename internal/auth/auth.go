// Package auth resolves a GitHub token from the environment or the
// gh CLI, so users don't have to configure one just to use octoscope.
package auth

import (
	"os"
	"os/exec"
	"strings"
)

// ghTokenOutput runs `gh auth token` and returns its raw stdout. It is a
// package var so tests can stub the gh CLI fallback without invoking the
// real binary (which would make the test depend on the host's gh login).
var ghTokenOutput = func() ([]byte, error) {
	return exec.Command("gh", "auth", "token").Output()
}

// Token returns a GitHub personal access token by, in order:
//
//  1. $GITHUB_TOKEN env var
//  2. `gh auth token` output, if the GitHub CLI is installed and logged in
//  3. empty string — the caller should fall back to unauthenticated requests
//     (GitHub gives 60 req/h in that case, which is enough for an occasional
//     demo but not for a 60-second polling loop)
func Token() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	out, err := ghTokenOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
