package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

// TestMakeCopyCmdForFixedPlatforms pins the two helpers that are a
// property of the OS rather than of the host's installed packages.
// Same reason as browse's twin: this package had no test, CI was
// Ubuntu-only, and the Windows arm (`clip`) shipped unexercised in
// every release. Taking the OS as a parameter is what lets a macOS or
// Linux run catch a mutation to it.
//
// Asserted on Args[0] rather than cmd.Path: exec.Command resolves Path
// through LookPath, so it is an absolute path where the helper exists
// and the bare name where it does not. Args[0] is always the name that
// was asked for, which is the decision under test.
func TestMakeCopyCmdForFixedPlatforms(t *testing.T) {
	cases := []struct{ goos, want string }{
		{"darwin", "pbcopy"},
		{"windows", "clip"},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			cmd, err := makeCopyCmdFor(c.goos)
			if err != nil {
				t.Fatalf("makeCopyCmdFor(%q): %v", c.goos, err)
			}
			if got := cmd.Args[0]; got != c.want {
				t.Errorf("helper = %q, want %q", got, c.want)
			}
			if len(cmd.Args) != 1 {
				t.Errorf("argv = %q, want the bare helper with no arguments", cmd.Args)
			}
		})
	}
}

// TestMakeCopyCmdForSearchesOnUnixLikes covers the arm that cannot be
// parameterised away: which of the three helpers exists is a fact about
// the host. Both outcomes are correct — what must hold is that a
// returned command is one of the three and that the absent case is
// ErrNoHelper with a nil command, never a nil/nil pair.
func TestMakeCopyCmdForSearchesOnUnixLikes(t *testing.T) {
	cmd, err := makeCopyCmdFor("linux")
	if err != nil {
		if !errors.Is(err, ErrNoHelper) {
			t.Fatalf("makeCopyCmdFor(linux) = %v, want ErrNoHelper", err)
		}
		if cmd != nil {
			t.Error("ErrNoHelper returned alongside a non-nil command")
		}
		return
	}
	if cmd == nil {
		t.Fatal("nil command with a nil error")
	}
	switch cmd.Args[0] {
	case "wl-copy", "xclip", "xsel":
	default:
		t.Errorf("helper = %q, want one of wl-copy/xclip/xsel", cmd.Args[0])
	}
}

// TestMakeCopyCmdForPriority pins the search order documented in
// makeCopyCmdFor by stating which helpers exist rather than asking the
// host. The earlier version of this test skipped unless wl-copy was
// installed, which is to say it skipped on every machine that runs the
// suite — and a mutation swapping wl-copy and xclip survived it.
func TestMakeCopyCmdForPriority(t *testing.T) {
	// The full argv, not just the binary: xclip and xsel both default
	// to the X11 PRIMARY selection (middle-click paste) unless told
	// otherwise, so "-selection clipboard" is the flag that makes Copy
	// mean what the user pressed. Asserting only Args[0] let a mutation
	// to it survive.
	var (
		wayland = []string{"wl-copy"}
		xclip   = []string{"xclip", "-selection", "clipboard"}
		xsel    = []string{"xsel", "--clipboard", "--input"}
	)
	cases := []struct {
		name      string
		installed []string
		want      []string
	}{
		{"all three -> wayland wins", []string{"wl-copy", "xclip", "xsel"}, wayland},
		{"xclip and xsel -> xclip", []string{"xclip", "xsel"}, xclip},
		{"xsel alone", []string{"xsel"}, xsel},
		{"wayland alone", []string{"wl-copy"}, wayland},
		// X11 compatibility is the case the order exists for: a Wayland
		// session commonly has xclip too, and picking it would work but
		// go through the compatibility layer rather than the native path.
		{"wayland session with xclip present", []string{"xclip", "wl-copy"}, wayland},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withHelpers(t, c.installed)
			cmd, err := makeCopyCmdFor("linux")
			if err != nil {
				t.Fatalf("makeCopyCmdFor(linux): %v", err)
			}
			if got := cmd.Args; !equalArgs(got, c.want) {
				t.Errorf("argv = %q, want %q", got, c.want)
			}
		})
	}
}

// TestMakeCopyCmdForNoHelper pins the empty case: nothing installed
// must be ErrNoHelper with a nil command, so Copy can surface the
// "clipboard not available" toast rather than dereferencing nil.
func TestMakeCopyCmdForNoHelper(t *testing.T) {
	withHelpers(t, nil)
	cmd, err := makeCopyCmdFor("linux")
	if !errors.Is(err, ErrNoHelper) {
		t.Fatalf("err = %v, want ErrNoHelper", err)
	}
	if cmd != nil {
		t.Error("ErrNoHelper returned alongside a non-nil command")
	}
}

// withHelpers replaces the lookPath seam for one test, reporting only
// the named binaries as present, and restores it afterwards.
func withHelpers(t *testing.T, installed []string) {
	t.Helper()
	prev := lookPath
	t.Cleanup(func() { lookPath = prev })
	lookPath = func(bin string) (string, error) {
		for _, b := range installed {
			if b == bin {
				return "/usr/bin/" + bin, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

// TestMakeCopyCmdUsesTheHost is the seam check: the caller Copy uses
// must delegate with runtime.GOOS, or the table above pins a function
// nothing reaches.
func TestMakeCopyCmdUsesTheHost(t *testing.T) {
	got, gotErr := makeCopyCmd()
	want, wantErr := makeCopyCmdFor(runtime.GOOS)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("makeCopyCmd err = %v, makeCopyCmdFor(%s) err = %v", gotErr, runtime.GOOS, wantErr)
	}
	if gotErr == nil && got.Args[0] != want.Args[0] {
		t.Errorf("makeCopyCmd helper = %q, want %q", got.Args[0], want.Args[0])
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
