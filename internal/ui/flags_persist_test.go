package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
)

// flagRun launches a model the way main does for
//
//	octoscope --theme amber --refresh 5m --compact --public-only --no-sponsor
//
// against a config file that says the opposite of every one of them, and
// returns it with a dashboard on screen and a way to read the file back.
// fromFlags is what main reports of the command line: pass the zero value
// for the same settings read from the file instead, the control.
func flagRun(t *testing.T, fromFlags FromFlags) (Model, func() config.Config) {
	t.Helper()
	return flagRunOver(t, fromFlags, asSeeded)
}

// flagRunOver is flagRun against a file that holds onDisk.
func flagRunOver(t *testing.T, fromFlags FromFlags, onDisk stored) (Model, func() config.Config) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	path := filepath.Join(t.TempDir(), "config.toml")
	seed := config.Defaults()
	seed.Theme, seed.RefreshInterval, seed.Compact, seed.PublicOnly, seed.ShowSponsor = onDisk.theme, onDisk.interval, onDisk.compact, onDisk.publicOnly, onDisk.showSponsor
	if err := config.Save(path, seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	client, err := github.New("octocat", github.Options{PublicOnly: true})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "test", Options{
		ConfigPath: path, Theme: "amber", Interval: 5 * time.Minute, Compact: true, ShowSponsor: false,
		SeasonalTheme: true, Now: time.Date(2026, time.July, 1, 12, 0, 0, 0, time.Local),
		FromFlags: fromFlags,
	})
	m.stats = &github.Stats{}
	return m, func() config.Config {
		t.Helper()
		got, err := config.Load(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		return got
	}
}

var everyFlag = FromFlags{Interval: true, Compact: true, PublicOnly: true, Theme: true, ShowSponsor: true}

// stored is the five keys a flag can override, as the file holds them.
type stored struct {
	theme       string
	interval    time.Duration
	compact     bool
	publicOnly  bool
	showSponsor bool
}

func storedIn(c config.Config) stored {
	return stored{c.Theme, c.RefreshInterval, c.Compact, c.PublicOnly, c.ShowSponsor}
}

var (
	asSeeded = stored{"octoscope", time.Minute, false, false, true}
	asFlags  = stored{"amber", 5 * time.Minute, true, true, false}
)

// TestAFlagIsForTheRun holds "Flags override the config file for that
// run" to its last three words. Until 0.41.0 the model could not tell a
// flag's value from the file's, so the next config write, for any reason,
// carried every flag with it: `--theme amber`, pin a repository, and amber
// was the stored theme (#260). A write now leaves a key the run took from
// a flag as the file has it, one flag at a time, and still writes the
// same values when they came from the file.
func TestAFlagIsForTheRun(t *testing.T) {
	// Every flag, then a write that is about none of them.
	m, file := flagRun(t, everyFlag)
	if err := m.persistConfig(); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}
	if got := storedIn(file()); got != asSeeded {
		t.Errorf("after an unrelated write the file holds %+v, want it as seeded %+v", got, asSeeded)
	}

	// The control: the same values, read from the file, are the user's.
	m, file = flagRun(t, FromFlags{})
	if err := m.persistConfig(); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}
	if got := storedIn(file()); got != asFlags {
		t.Errorf("with no flag the file holds %+v, want the model's %+v", got, asFlags)
	}

	// One flag at a time: only that key keeps the file's value.
	for _, c := range []struct {
		name string
		flag FromFlags
		want stored
	}{
		{"--refresh", FromFlags{Interval: true}, stored{"amber", time.Minute, true, true, false}},
		{"--compact", FromFlags{Compact: true}, stored{"amber", 5 * time.Minute, false, true, false}},
		{"--public-only", FromFlags{PublicOnly: true}, stored{"amber", 5 * time.Minute, true, false, false}},
		{"--theme", FromFlags{Theme: true}, stored{"octoscope", 5 * time.Minute, true, true, false}},
		{"--no-sponsor", FromFlags{ShowSponsor: true}, stored{"amber", 5 * time.Minute, true, true, true}},
	} {
		m, file := flagRun(t, c.flag)
		if err := m.persistConfig(); err != nil {
			t.Fatalf("%s: persistConfig: %v", c.name, err)
		}
		if got := storedIn(file()); got != c.want {
			t.Errorf("%s: the file holds %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestAChoiceMadeInTheAppIsSaved is the other half: a setting changed
// during the run is a choice, flag or no flag, and is written; the flags
// nobody touched still are not.
func TestAChoiceMadeInTheAppIsSaved(t *testing.T) {
	// `p` twice, the sequence #260 was measured with: public-only is the
	// user's now, and ends where --public-only had put it.
	m, file := flagRun(t, everyFlag)
	u, _ := m.Update(key("p"))
	if got := storedIn(file()); got != asSeeded {
		t.Errorf("after one p the file holds %+v, want %+v (public-only off, as seeded, and no flag written)", got, asSeeded)
	}
	_, _ = u.(Model).Update(key("p"))
	want := asSeeded
	want.publicOnly = true
	if got := storedIn(file()); got != want {
		t.Errorf("after p twice the file holds %+v, want %+v", got, want)
	}

	// The settings panel, saved with one row changed: that row is written.
	m, file = flagRun(t, everyFlag)
	m.settings = m.settings.Open(m.interval, false, m.client.PublicOnly(), m.theme, m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
	_ = m.applySettingsAndClose()
	if got := storedIn(file()); got != asSeeded {
		t.Errorf("compact switched back off in the panel: the file holds %+v, want %+v", got, asSeeded)
	}
	m, file = flagRun(t, everyFlag)
	m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), "phosphor", m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
	_ = m.applySettingsAndClose()
	want = asSeeded
	want.theme = "phosphor"
	if got := storedIn(file()); got != want {
		t.Errorf("a theme chosen in the panel: the file holds %+v, want %+v", got, want)
	}
	// And saved with nothing changed, it writes no flag.
	m, file = flagRun(t, everyFlag)
	m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
	_ = m.applySettingsAndClose()
	if got := storedIn(file()); got != asSeeded {
		t.Errorf("the panel saved as it opened: the file holds %+v, want %+v", got, asSeeded)
	}
	// One row at a time, over a file that already agrees with the flags,
	// so the row that was changed is the only key that can differ, and a
	// change that did not reach the file shows.
	for _, c := range []struct {
		row  string
		open func(m Model) SettingsModel
		want stored
	}{
		{"refresh", func(m Model) SettingsModel {
			return m.settings.Open(30*time.Second, m.compact, m.client.PublicOnly(), m.theme, m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
		}, stored{"amber", 30 * time.Second, true, true, false}},
		{"compact", func(m Model) SettingsModel {
			return m.settings.Open(m.interval, false, m.client.PublicOnly(), m.theme, m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
		}, stored{"amber", 5 * time.Minute, false, true, false}},
		{"public-only", func(m Model) SettingsModel {
			return m.settings.Open(m.interval, m.compact, false, m.theme, m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
		}, stored{"amber", 5 * time.Minute, true, false, false}},
		{"theme", func(m Model) SettingsModel {
			return m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), "phosphor", m.seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
		}, stored{"phosphor", 5 * time.Minute, true, true, false}},
		{"sponsor", func(m Model) SettingsModel {
			return m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, m.seasonal, m.accentColor, true, m.repos.commitCounts)
		}, stored{"amber", 5 * time.Minute, true, true, true}},
	} {
		m, file := flagRunOver(t, everyFlag, asFlags)
		m.settings = c.open(m)
		_ = m.applySettingsAndClose()
		if got := storedIn(file()); got != c.want {
			t.Errorf("the %s row alone changed in the panel: the file holds %+v, want %+v", c.row, got, c.want)
		}
	}
	// Every row changed, over a file that already agreed with the flags,
	// so a row left unwritten shows: every one is written.
	m, file = flagRunOver(t, everyFlag, asFlags)
	m.settings = m.settings.Open(30*time.Second, false, false, "phosphor", m.seasonal, m.accentColor, true, m.repos.commitCounts)
	_ = m.applySettingsAndClose()
	if got, want := storedIn(file()), (stored{"phosphor", 30 * time.Second, false, false, true}); got != want {
		t.Errorf("every row changed in the panel: the file holds %+v, want %+v", got, want)
	}
}
