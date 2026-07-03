// Package auth resolves a GitHub token from the environment or the
// gh CLI, so users don't have to configure one just to use octoscope.
package auth

import (
	"os"
	"os/exec"
	"strings"
)

// Source identifies where a resolved token came from, so auth-error
// surfaces can name the right fix (regenerate the PAT vs re-run
// `gh auth login`) without ever echoing the token itself.
type Source int

const (
	// SourceNone means no token was found — the session is unauthenticated.
	SourceNone Source = iota
	// SourceEnv means the token came from the $GITHUB_TOKEN env var.
	SourceEnv
	// SourceGHCLI means the token came from `gh auth token`.
	SourceGHCLI
)

// ghTokenOutput runs `gh auth token` and returns its raw stdout. It is a
// package var so tests can stub the gh CLI fallback without invoking the
// real binary (which would make the test depend on the host's gh login).
var ghTokenOutput = func() ([]byte, error) {
	return exec.Command("gh", "auth", "token").Output()
}

// TokenSource resolves a token like Token and also reports where it
// came from, so callers can tailor "your token was rejected" guidance
// to the source actually in use.
func TokenSource() (string, Source) {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, SourceEnv
	}
	out, err := ghTokenOutput()
	if err != nil {
		return "", SourceNone
	}
	if t := strings.TrimSpace(string(out)); t != "" {
		return t, SourceGHCLI
	}
	return "", SourceNone
}

// Token returns a GitHub personal access token by, in order:
//
//  1. $GITHUB_TOKEN env var
//  2. `gh auth token` output, if the GitHub CLI is installed and logged in
//  3. empty string — the caller should fall back to unauthenticated requests
//     (GitHub gives 60 req/h in that case, which is enough for an occasional
//     demo but not for a 60-second polling loop)
func Token() string {
	t, _ := TokenSource()
	return t
}
