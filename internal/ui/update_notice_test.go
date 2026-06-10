package ui

import (
	"strings"
	"testing"

	"github.com/gfazioli/octoscope/internal/update"
)

// TestRenderUpdateNotice checks the update-available line carries the
// version and the channel-specific upgrade command, so the user always
// sees both what's new and how to get it.
func TestRenderUpdateNotice(t *testing.T) {
	_ = applyTheme("octoscope", "")

	cases := []struct {
		ch       update.Channel
		wantCmd  string
		chanName string
	}{
		{update.ChannelHomebrew, "brew upgrade gfazioli/tap/octoscope", "homebrew"},
		{update.ChannelGo, "go install github.com/gfazioli/octoscope@latest", "go"},
		{update.ChannelGhExtension, "gh extension upgrade octoscope", "gh"},
		{update.ChannelManual, "https://github.com/gfazioli/octoscope/releases/latest", "manual"},
	}
	for _, c := range cases {
		out := renderUpdateNotice("v0.19.0", c.ch)
		if !strings.Contains(out, "v0.19.0") {
			t.Errorf("[%s] notice missing version: %q", c.chanName, out)
		}
		if !strings.Contains(out, "available") {
			t.Errorf("[%s] notice missing 'available': %q", c.chanName, out)
		}
		if !strings.Contains(out, c.wantCmd) {
			t.Errorf("[%s] notice missing upgrade command %q: %q", c.chanName, c.wantCmd, out)
		}
	}
}
