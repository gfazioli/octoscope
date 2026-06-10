package update

import (
	"os"
	"path/filepath"
	"strings"
)

// Channel is how the running octoscope binary was most likely
// installed. It drives the upgrade command suggested in the
// update-available notice — octoscope must never overwrite itself,
// because the package manager owns the binary (auto-update would fight
// brew/gh/go and break the read-only trust model).
type Channel int

const (
	ChannelUnknown Channel = iota
	ChannelHomebrew
	ChannelGo
	ChannelGhExtension
	ChannelManual
)

// DetectChannel infers the install channel from the running
// executable's path (and how it was invoked). Best-effort: any
// ambiguity falls back to ChannelManual, whose hint just points at the
// releases page — always correct, if generic.
func DetectChannel() Channel {
	// argv[0] basename: a gh extension is invoked as "gh-octoscope".
	if base := filepath.Base(os.Args[0]); strings.HasPrefix(base, "gh-") {
		return ChannelGhExtension
	}

	exe, err := os.Executable()
	if err != nil {
		return ChannelManual
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	p := filepath.ToSlash(exe)

	switch {
	case strings.Contains(p, "/gh/extensions/"), strings.Contains(p, "/gh-octoscope/"):
		return ChannelGhExtension
	case strings.Contains(p, "/Cellar/"), strings.Contains(p, "/homebrew/"), strings.Contains(p, "/Homebrew/"):
		return ChannelHomebrew
	case strings.Contains(p, "/go/bin/"), strings.HasSuffix(filepath.Dir(p), "/bin") && goPathBin(p):
		return ChannelGo
	default:
		return ChannelManual
	}
}

// goPathBin reports whether p sits under $GOPATH/bin or $GOBIN, the
// install targets used by `go install`.
func goPathBin(p string) bool {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		if strings.HasPrefix(p, filepath.ToSlash(gobin)+"/") {
			return true
		}
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		if strings.HasPrefix(p, filepath.ToSlash(filepath.Join(gopath, "bin"))+"/") {
			return true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if strings.HasPrefix(p, filepath.ToSlash(filepath.Join(home, "go", "bin"))+"/") {
			return true
		}
	}
	return false
}

// UpgradeCommand returns the shell command (or instruction) that
// upgrades octoscope for the given channel. The string is rendered
// verbatim in the update-available notice.
func UpgradeCommand(ch Channel) string {
	switch ch {
	case ChannelHomebrew:
		return "brew upgrade gfazioli/tap/octoscope"
	case ChannelGo:
		return "go install github.com/gfazioli/octoscope@latest"
	case ChannelGhExtension:
		return "gh extension upgrade octoscope"
	default:
		return "github.com/gfazioli/octoscope/releases/latest"
	}
}
