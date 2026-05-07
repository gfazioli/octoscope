// Package clipboard wraps the host system's clipboard so the rest
// of octoscope can write text without caring whether the user is on
// macOS, Linux (X11 or Wayland), or Windows. Reading isn't needed
// today — octoscope only ever writes (Copy URL action).
//
// Implementation strategy: shell out to the platform's standard
// clipboard helper (pbcopy / clip / xclip / xsel / wl-copy) rather
// than pulling in a CGO-dependency-heavy clipboard library. The
// trade-off is that the helper has to exist on the host: macOS and
// Windows both ship one in the base install, while a stripped-down
// Linux container may not. In that case Copy returns an error and
// the caller surfaces a "clipboard not available" toast — better
// than crashing.
package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoHelper is returned when no clipboard utility is available on
// the host. Linux flavours that ship without xclip / xsel / wl-copy
// will hit this; we leave installation up to the user rather than
// guessing distros.
var ErrNoHelper = errors.New("no clipboard helper found (install xclip, xsel, or wl-copy on Linux)")

// Copy writes `text` to the system clipboard. Blocks until the
// helper has consumed the input or fails — typically <50ms.
//
// On Linux we try wl-copy first (Wayland), then xclip, then xsel.
// First found wins; absence of all three returns ErrNoHelper. On
// macOS and Windows the helper is part of the base install so it's
// always there.
func Copy(text string) error {
	cmd, err := makeCopyCmd()
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		// Wrap the helper's exit error so callers can show the
		// underlying reason (e.g. "xclip: Can't open display") if
		// they want, while still matching against ErrNoHelper for
		// the "tool absent" case via errors.Is.
		return err
	}
	return nil
}

// makeCopyCmd picks the right clipboard helper for the current OS
// and returns it ready to receive stdin. Pure construction — never
// runs the command.
func makeCopyCmd() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pbcopy"), nil
	case "windows":
		return exec.Command("clip"), nil
	}
	// Linux / *BSD: try the three helpers in priority order.
	// Wayland's wl-copy comes first because Wayland sessions often
	// also have an xclip/xsel installed via X11 compatibility, but
	// wl-copy is the native choice when both are present.
	for _, c := range []struct {
		bin  string
		args []string
	}{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	} {
		if _, err := exec.LookPath(c.bin); err == nil {
			return exec.Command(c.bin, c.args...), nil
		}
	}
	return nil, ErrNoHelper
}
