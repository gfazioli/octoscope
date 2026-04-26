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
	if url == "" {
		return errors.New("browse: empty url")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		return errors.New("browse: unsupported platform: " + runtime.GOOS)
	}
	return cmd.Start()
}
