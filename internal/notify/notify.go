// Package notify delivers system notifications with an octoscope icon
// and a click-through URL. It exists because the standard `beeep`
// path on macOS routes through `osascript`, which silently drops both
// the icon argument and any click action.
//
// Strategy:
//
//   - macOS: shell out to `terminal-notifier` if present in PATH. It
//     supports `-appIcon` and `-open URL` natively and is brew-installable
//     (`brew install terminal-notifier`). When the binary is missing,
//     fall back to plain `beeep` so users without it get the same
//     iconless / clickless behaviour they had on v0.7.x.
//   - Linux / Windows: stay on `beeep`, but pass the embedded icon
//     path so the notification carries the octoscope mark.
//
// The icon is embedded at compile time and written to a temp file
// once per process. Failures are non-fatal: a missing icon path or
// a backend that rejects the call leaves the notification silent
// rather than crashing the TUI.
package notify

import (
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/gen2brain/beeep"
)

//go:embed icon.png
var iconBytes []byte

var (
	iconOnce sync.Once
	iconPath string
)

// resolveIcon writes the embedded PNG to a process-scoped temp file
// and returns its path. Subsequent calls are O(1). Returns "" if the
// write failed — callers should treat that as "no icon", not as fatal.
func resolveIcon() string {
	iconOnce.Do(func() {
		path := filepath.Join(os.TempDir(), "octoscope-icon.png")
		if err := os.WriteFile(path, iconBytes, 0o644); err != nil {
			return
		}
		iconPath = path
	})
	return iconPath
}

// Send fires a desktop notification with the given title and message.
// clickURL, if non-empty, is the page that should open when the user
// activates the notification (supported on macOS via terminal-notifier
// only — beeep on the other platforms ignores it for now).
//
// Returns the underlying error if all backends failed; nil otherwise.
// Callers in octoscope discard the error: a failed notification is
// not worth surfacing in the TUI.
func Send(title, message, clickURL string) error {
	icon := resolveIcon()

	if runtime.GOOS == "darwin" {
		if err := sendViaTerminalNotifier(title, message, icon, clickURL); err == nil {
			return nil
		}
		// Fall through to beeep so we still ring + show *something*
		// when terminal-notifier isn't installed.
	}

	return beeep.Notify(title, message, icon)
}

// sendViaTerminalNotifier shells out to the brew-installable
// `terminal-notifier` CLI. Returns an error if the binary is missing
// (so the caller can fall back) or if the launch fails.
func sendViaTerminalNotifier(title, message, icon, clickURL string) error {
	bin, err := exec.LookPath("terminal-notifier")
	if err != nil {
		return errors.New("notify: terminal-notifier not found")
	}

	args := []string{
		"-title", title,
		"-message", message,
	}
	// We deliberately do NOT pass -appIcon here. On modern macOS
	// (Big Sur+) the system not only ignores the override but causes
	// terminal-notifier to silently drop the entire notification when
	// the flag is present — confirmed on macOS Sequoia. The notification
	// banner will show terminal-notifier's own icon, which is the
	// correct fallback for now. Restoring the icon on macOS would
	// require packaging octoscope as a signed .app bundle.
	//
	// We also skip `-sender com.apple.Terminal`: it labels the banner
	// as coming from Terminal but makes macOS reject -open (a
	// sender-impersonation safeguard).
	_ = icon
	if clickURL != "" {
		args = append(args, "-open", clickURL)
	}

	// Run synchronously and discard output. terminal-notifier returns
	// quickly (it dispatches to the Notification Center daemon and
	// exits) so there's no benefit to backgrounding it.
	return exec.Command(bin, args...).Run()
}

// Beep plays the system bell. Thin wrapper kept here so the rest of
// the codebase imports a single notification package instead of
// importing beeep directly.
func Beep() error {
	return beeep.Beep(beeep.DefaultFreq, beeep.DefaultDuration)
}
