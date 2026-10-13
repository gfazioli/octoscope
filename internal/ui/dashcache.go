package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/dashcache"
)

// The dashboard's side of the on-disk snapshot (#65). The rules about
// *what* is a usable snapshot live in internal/dashcache; this file
// decides *when* the dashboard reads and writes one, and how the footer
// says that what is on screen came from it.

// paintFromCache puts the last snapshot on screen before the first
// fetch answers, when the cache is on, the session is not public-only
// and dashcache vouches for one. Public-only never reads it: that is the
// screenshot mode, and the rule the update-check cache already follows
// keeps such sessions hermetic.
func (m *Model) paintFromCache(now time.Time) {
	if m.cacheDir == "" || m.client.PublicOnly() {
		return
	}
	s, at, ok := dashcache.Load(m.cacheDir, m.client.CacheKey(), m.version, now)
	if !ok {
		return
	}
	m.stats = s
	m.lastFetch = at
	m.fromCache = true
}

// cacheWriteCmd stores a successful answer as the next launch's first
// paint. Nil when there is nothing to store, when the cache is off, and
// while public-only is on — checked at write time, so pressing p stops
// the writes from the next answer on.
//
// The snapshot is encoded here, on the update loop, and only the bytes
// travel to the background write: once Update returns, the model owns
// the stats again, and nothing outside it should be reading them.
func (m Model) cacheWriteCmd(msg fetchMsg) tea.Cmd {
	if msg.err != nil || msg.stats == nil || m.cacheDir == "" || m.client.PublicOnly() {
		return nil
	}
	key := m.client.CacheKey()
	data, err := dashcache.Encode(key, m.version, msg.stats, msg.at)
	if err != nil {
		return nil
	}
	dir := m.cacheDir
	return func() tea.Msg {
		// Best-effort: a snapshot that could not be written just means
		// the next launch waits for the network, as every launch did
		// before #65.
		_ = dashcache.Write(dir, key, data, time.Now())
		return nil
	}
}

// dataAgeLabel says how old the dashboard on screen is, for the footer
// whenever it cannot say "Updated …": while a cached first paint waits
// for its refresh, and after a refresh failed. A cached snapshot is
// usually hours old, so it reads in coarse units ("cached 3h ago"); a
// live answer keeps the seconds the "Updated …" label always shows.
// humanDuration rounds anything under a minute up to "1m", which would
// age a dashboard reopened seconds after quitting, so that case says so.
func dataAgeLabel(m Model, now time.Time) string {
	age := now.Sub(m.lastFetch)
	if m.fromCache {
		if age < time.Minute {
			return "cached just now"
		}
		return "cached " + humanDuration(age) + " ago"
	}
	return fmt.Sprintf("updated %s ago", age.Truncate(time.Second))
}
