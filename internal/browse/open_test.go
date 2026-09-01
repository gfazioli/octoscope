package browse

import (
	"runtime"
	"strings"
	"testing"
)

// TestMakeOpenCmdForEveryPlatform pins the launcher argv for every OS
// octoscope ships to, from whichever OS is running the suite.
//
// It exists because this package had no test at all and CI ran only on
// Ubuntu, so the Windows branch had shipped in every release without
// once being compiled on Windows, let alone exercised. Reading the OS
// through a parameter instead of runtime.GOOS is what makes the other
// two rows checkable here — with the switch on the constant, a mutation
// to the Windows argv survived the suite on every non-Windows host.
func TestMakeOpenCmdForEveryPlatform(t *testing.T) {
	const url = "https://example.com"
	cases := []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open", url}},
		{"linux", []string{"xdg-open", url}},
		// The empty third argument is load-bearing: `start` reads a
		// quoted first argument as the console title, so dropping it
		// makes Windows open a titled window and no page.
		{"windows", []string{"cmd", "/c", "start", "", url}},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			cmd, err := makeOpenCmdFor(c.goos, url)
			if err != nil {
				t.Fatalf("makeOpenCmdFor(%q): %v", c.goos, err)
			}
			if got := cmd.Args; !equal(got, c.want) {
				t.Errorf("argv = %q, want %q", got, c.want)
			}
		})
	}
}

// TestMakeOpenCmdForRejectsUnknown keeps the default arm honest: an OS
// with no launcher must produce an error naming it, not a command that
// fails obscurely at Start.
func TestMakeOpenCmdForRejectsUnknown(t *testing.T) {
	cmd, err := makeOpenCmdFor("plan9", "https://example.com")
	if err == nil {
		t.Fatalf("makeOpenCmdFor(plan9) = %v, want an error", cmd)
	}
	if !strings.Contains(err.Error(), "plan9") {
		t.Errorf("error %q does not name the platform", err)
	}
}

// TestMakeOpenCmdForRejectsEmpty keeps the guard ahead of the switch:
// an empty URL must not reach a launcher on any platform. On Windows
// `start ""` with no target opens a blank console rather than failing,
// so the check has to happen here.
func TestMakeOpenCmdForRejectsEmpty(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if cmd, err := makeOpenCmdFor(goos, ""); err == nil {
			t.Errorf("makeOpenCmdFor(%q, \"\") = %v, want an error", goos, cmd)
		} else if !strings.Contains(err.Error(), "empty url") {
			t.Errorf("error on %s is %q, which does not name the cause", goos, err)
		}
	}
}

// TestMakeOpenCmdUsesTheHost is the seam check: the exported path must
// actually delegate with runtime.GOOS, or the table above would be
// pinning a function nothing calls.
func TestMakeOpenCmdUsesTheHost(t *testing.T) {
	const url = "https://example.com"
	got, gotErr := makeOpenCmd(url)
	want, wantErr := makeOpenCmdFor(runtime.GOOS, url)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("makeOpenCmd err = %v, makeOpenCmdFor(%s) err = %v", gotErr, runtime.GOOS, wantErr)
	}
	if gotErr == nil && !equal(got.Args, want.Args) {
		t.Errorf("makeOpenCmd argv = %q, want %q", got.Args, want.Args)
	}
}

func equal(a, b []string) bool {
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
