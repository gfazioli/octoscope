// Package browse opens URLs in the user's default browser.
//
// Used by the Repos / PRs / Issues tabs so pressing Enter on a list
// row jumps to the corresponding GitHub page. Aligned with octoscope's
// "one GraphQL query per refresh" design principle: instead of fetching
// per-row detail data on demand, we hand the user off to GitHub itself.
package browse

import (
	"errors"
	"os/exec"
	"runtime"
)

// OpenURL launches the user's default browser pointing at url.
// Returns an error if the platform is unsupported or the launcher
// command fails to start. The caller is expected to surface or
// ignore the error — octoscope ignores it (failure is non-fatal).
func OpenURL(url string) error {
	cmd, err := makeOpenCmd(url)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// makeOpenCmd picks the right launcher for the host OS and returns it
// ready to start. Pure construction — never runs the command, which is
// what lets a test assert the argv without opening a browser on the
// machine running the suite.
func makeOpenCmd(url string) (*exec.Cmd, error) {
	return makeOpenCmdFor(runtime.GOOS, url)
}

// makeOpenCmdFor takes the OS as an argument rather than reading
// runtime.GOOS, so every platform's argv is assertable from every
// platform. That is the whole point: with the switch reading the
// constant directly, the Windows branch could only ever be checked by a
// Windows machine — and a mutation to it survived the suite everywhere
// else, silently. The launcher for an OS is a fact about that OS, not
// about the host, so it does not need the host to be one.
//
// The Windows form needs its empty third argument: `start` reads a
// first quoted argument as the new console's *title*, so `start <url>`
// with a quoted URL sets a title and opens nothing.
func makeOpenCmdFor(goos, url string) (*exec.Cmd, error) {
	if url == "" {
		return nil, errors.New("browse: empty url")
	}

	switch goos {
	case "darwin":
		return exec.Command("open", url), nil
	case "linux":
		return exec.Command("xdg-open", url), nil
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url), nil
	}
	return nil, errors.New("browse: unsupported platform: " + goos)
}
