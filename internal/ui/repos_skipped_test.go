package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestRenderReposTabSkippedNotice pins the #37 notice: skipped
// watch_repos entries surface as a muted line naming the refs, the
// line survives the empty-rows state (all watched entries stale and
// no owned repos — exactly when the user most needs the signal), and
// zero skipped renders nothing.
func TestRenderReposTabSkippedNotice(t *testing.T) {
	_ = applyTheme("octoscope", "")

	t.Run("notice lists the refs under the table", func(t *testing.T) {
		stats := &github.Stats{
			Repositories:   []github.Repo{mkRepo("gfazioli", "octoscope", 100)},
			WatchedSkipped: []string{"owner/gone", "bad-entry"},
		}
		out := ansi.Strip(ReposModel{}.renderReposTab(stats, 120, 40, nil))
		if !strings.Contains(out, "2 watched entries skipped: owner/gone, bad-entry") {
			t.Errorf("notice missing or wrong:\n%s", out)
		}
	})

	t.Run("notice survives the empty state", func(t *testing.T) {
		stats := &github.Stats{WatchedSkipped: []string{"owner/gone"}}
		out := ansi.Strip(ReposModel{}.renderReposTab(stats, 120, 40, nil))
		if !strings.Contains(out, "1 watched entry skipped: owner/gone") {
			t.Errorf("notice must render even with zero rows:\n%s", out)
		}
	})

	t.Run("zero skipped renders no notice", func(t *testing.T) {
		stats := &github.Stats{Repositories: []github.Repo{mkRepo("gfazioli", "octoscope", 100)}}
		out := ansi.Strip(ReposModel{}.renderReposTab(stats, 120, 40, nil))
		if strings.Contains(out, "skipped") {
			t.Errorf("no skipped entries but a notice rendered:\n%s", out)
		}
	})

	t.Run("long ref list truncates to the available width", func(t *testing.T) {
		stats := &github.Stats{
			Repositories: []github.Repo{mkRepo("gfazioli", "octoscope", 100)},
			WatchedSkipped: []string{
				"owner/very-long-repository-name-one",
				"owner/very-long-repository-name-two",
				"owner/very-long-repository-name-three",
			},
		}
		out := ansi.Strip(ReposModel{}.renderReposTab(stats, 60, 40, nil))
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "skipped") && len([]rune(line)) > 60 {
				t.Errorf("notice line wider than available (%d runes): %q", len([]rune(line)), line)
			}
		}
	})
}
