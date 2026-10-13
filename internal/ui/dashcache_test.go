package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/dashcache"
	"github.com/gfazioli/octoscope/internal/github"
)

const cacheTestVersion = "test"

func cacheTestClient(t *testing.T, publicOnly bool) *github.Client {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, err := github.New("octocat", github.Options{PublicOnly: publicOnly})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	return client
}

// seedSnapshot writes s as the client's snapshot, taken at `at`.
func seedSnapshot(t *testing.T, dir string, client *github.Client, s *github.Stats, at time.Time) {
	t.Helper()
	data, err := dashcache.Encode(client.CacheKey(), cacheTestVersion, s, at)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := dashcache.Write(dir, client.CacheKey(), data, at); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func cachedStats() *github.Stats {
	return &github.Stats{Login: "octocat", Name: "Cached Octocat", TotalStars: 42, Followers: 7}
}

// newCachedModel is a model whose first paint came from a snapshot taken
// a little over three hours ago.
func newCachedModel(t *testing.T) (Model, string, *github.Client) {
	t.Helper()
	dir := t.TempDir()
	client := cacheTestClient(t, false)
	seedSnapshot(t, dir, client, cachedStats(), time.Now().Add(-3*time.Hour-time.Minute))
	m := NewModel(client, cacheTestVersion, Options{DashboardCacheDir: dir})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	return u.(Model), dir, client
}

// runCmd executes cmd and, through any batches, every command it holds,
// so the work a handler scheduled actually happens. Callers record the
// tick seam first, so nothing here waits out a timer.
func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(c)
		}
	}
}

func networkError() error {
	return &github.FetchError{Reason: github.ReasonNetwork, Err: errors.New("dial tcp: no route to host")}
}

// The launch paints the last snapshot at once and says it is a cache
// while the refresh runs, instead of a launch screen waiting on GitHub.
func TestCachedFirstPaint(t *testing.T) {
	m, _, _ := newCachedModel(t)

	if m.stats == nil || m.stats.Name != "Cached Octocat" {
		t.Fatalf("first paint stats = %+v, want the snapshot", m.stats)
	}
	if !m.fromCache || !m.loading {
		t.Errorf("fromCache=%v loading=%v, want both true until the live answer", m.fromCache, m.loading)
	}
	v := m.View()
	if !strings.Contains(v, "cached 3h ago · refreshing…") {
		t.Errorf("the footer does not say the dashboard is cached:\n%s", lastLines(v, 3))
	}
	if !strings.Contains(v, "refresh") || !strings.Contains(v, "settings") {
		t.Errorf("the dashboard footer is missing; still on the launch screen?\n%s", lastLines(v, 3))
	}
}

// Screenshot mode never reads the snapshot, even when one is there.
func TestPublicOnlyNeverPaintsFromCache(t *testing.T) {
	dir := t.TempDir()
	full := cacheTestClient(t, false)
	seedSnapshot(t, dir, full, cachedStats(), time.Now().Add(-time.Hour))

	public := cacheTestClient(t, true)
	if public.CacheKey() != full.CacheKey() {
		t.Fatal("the two clients name different snapshots; the test would pass for the wrong reason")
	}
	m := NewModel(public, cacheTestVersion, Options{DashboardCacheDir: dir})
	if m.stats != nil || m.fromCache {
		t.Errorf("a public-only launch painted the snapshot: %+v", m.stats)
	}
}

// With the cache off (no directory) nothing is read.
func TestNoCacheDirNoPaint(t *testing.T) {
	m := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
	if m.stats != nil || m.fromCache {
		t.Errorf("a launch without a cache directory painted %+v", m.stats)
	}
}

// The first live answer replaces the snapshot, without pulsing cards for
// what changed since the last session, and becomes the next snapshot.
func TestLiveAnswerReplacesTheCacheQuietly(t *testing.T) {
	recordTicks(t)
	m, dir, client := newCachedModel(t)

	live := &github.Stats{Login: "octocat", Name: "Live Octocat", TotalStars: 50, Followers: 9}
	at := time.Now()
	u, cmd := m.Update(fetchMsg{stats: live, at: at})
	got := u.(Model)

	if got.stats != live || got.fromCache {
		t.Errorf("stats=%p fromCache=%v, want the live answer and no cache flag", got.stats, got.fromCache)
	}
	if !got.lastFetch.Equal(at) {
		t.Errorf("lastFetch = %v, want the live answer's %v", got.lastFetch, at)
	}
	if len(got.pulseMap) != 0 {
		t.Errorf("the cache-to-live switch pulsed %v; that is a change since the last session, not one seen live", got.pulseMap)
	}
	if v := got.View(); strings.Contains(v, "cached") {
		t.Errorf("the footer still says cached after the live answer:\n%s", lastLines(v, 3))
	}

	runCmd(cmd)
	saved, savedAt, ok := dashcache.Load(dir, client.CacheKey(), cacheTestVersion, time.Now())
	if !ok || saved.Name != "Live Octocat" {
		t.Fatalf("the live answer was not written as the next snapshot: ok=%v %+v", ok, saved)
	}
	if !savedAt.Equal(at) {
		t.Errorf("snapshot taken at %v, want the fetch's time %v", savedAt, at)
	}
}

// A live-to-live refresh still pulses: the quiet switch above must not
// switch pulses off for good.
func TestLiveRefreshStillPulses(t *testing.T) {
	recordTicks(t)
	m := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
	u, _ := m.Update(fetchMsg{stats: cachedStats(), at: time.Now()})
	u, _ = u.(Model).Update(fetchMsg{stats: &github.Stats{Login: "octocat", TotalStars: 50, Followers: 7}, at: time.Now()})
	if len(u.(Model).pulseMap) == 0 {
		t.Error("a change seen between two live answers did not pulse")
	}
}

// A failed first fetch leaves the snapshot up and says how old it is —
// the offline-first case: open it with no network and see the last state.
func TestFailedFetchKeepsTheCachedPaint(t *testing.T) {
	recordTicks(t)
	m, _, _ := newCachedModel(t)
	before, at := m.stats, m.lastFetch

	u, _ := m.Update(fetchMsg{err: networkError(), at: time.Now()})
	got := u.(Model)
	if got.stats != before || !got.fromCache || !got.lastFetch.Equal(at) {
		t.Errorf("a failed fetch changed the cached paint: stats kept=%v fromCache=%v lastFetch moved=%v",
			got.stats == before, got.fromCache, !got.lastFetch.Equal(at))
	}
	v := got.View()
	if strings.Contains(v, "Network error") {
		t.Errorf("the error screen replaced the cached dashboard")
	}
	if !strings.Contains(v, "offline · retrying") || !strings.Contains(v, "cached 3h ago") {
		t.Errorf("the footer does not say offline and cached:\n%s", lastLines(v, 3))
	}
}

// The same holds for a failed refresh in a live session. Until #65 any
// failed refresh replaced the stats with the failure's nil and showed
// the error screen, so the footer's offline chip was unreachable.
func TestFailedRefreshKeepsTheLiveDashboard(t *testing.T) {
	recordTicks(t)
	m := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	u, _ = u.(Model).Update(fetchMsg{stats: cachedStats(), at: time.Now().Add(-2 * time.Minute)})
	before := u.(Model).stats

	u, _ = u.(Model).Update(fetchMsg{err: networkError(), at: time.Now()})
	got := u.(Model)
	if got.stats != before {
		t.Fatal("a failed refresh dropped the dashboard")
	}
	v := got.View()
	if strings.Contains(v, "Network error") || !strings.Contains(v, "offline · retrying") {
		t.Errorf("want the dashboard with the offline chip:\n%s", lastLines(v, 3))
	}
	if !strings.Contains(v, "updated 2m") {
		t.Errorf("the footer does not say how old the data is:\n%s", lastLines(v, 3))
	}
}

// A token GitHub rejects does not keep the dashboard it fetched: neither
// a cached paint nor live data stays up, badged "authenticated", for a
// token that can no longer read it. The error screen names the fix.
func TestRejectedTokenDropsTheDashboard(t *testing.T) {
	recordTicks(t)
	for _, reason := range []github.FetchErrorReason{github.ReasonAuth, github.ReasonAuthScope} {
		rejected := &github.FetchError{Reason: reason, Err: errors.New("401 Bad credentials")}

		cached, _, _ := newCachedModel(t)
		u, _ := cached.Update(fetchMsg{err: rejected, at: time.Now()})
		if got := u.(Model); got.stats != nil || got.fromCache {
			t.Errorf("reason %v: the cached paint survived a rejected token", reason)
		}

		live := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
		u, _ = live.Update(fetchMsg{stats: cachedStats(), at: time.Now()})
		u, _ = u.(Model).Update(fetchMsg{err: rejected, at: time.Now()})
		if u.(Model).stats != nil {
			t.Errorf("reason %v: live stats survived a rejected token", reason)
		}
	}
}

// A retry after a failure shows that it is running: the footer swaps the
// last error for the spinner and the age of what is on screen, instead
// of reading "offline · retrying" as if nothing happened.
func TestRetryAfterAFailureShowsItIsRunning(t *testing.T) {
	recordTicks(t)
	fetches := recordFetches(t)
	m, _, _ := newCachedModel(t)
	u, _ := m.Update(fetchMsg{err: networkError(), at: time.Now()})
	// The real retry path: r on the dashboard dispatches a fetch.
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	got := u.(Model)
	if *fetches != 1 || !got.loading {
		t.Fatalf("r after a failure dispatched %d fetches, loading=%v; want one running", *fetches, got.loading)
	}

	v := got.View()
	if strings.Contains(v, "offline · retrying") {
		t.Errorf("a running retry still shows the last error:\n%s", lastLines(v, 3))
	}
	if !strings.Contains(v, "cached 3h ago · refreshing…") {
		t.Errorf("a running retry does not say what is on screen:\n%s", lastLines(v, 3))
	}

	// The same for live data a failed refresh left up.
	live := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
	u, _ = live.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	u, _ = u.(Model).Update(fetchMsg{stats: cachedStats(), at: time.Now().Add(-2 * time.Minute)})
	u, _ = u.(Model).Update(fetchMsg{err: networkError(), at: time.Now()})
	retry := u.(Model)
	retry.loading = true
	if v := retry.View(); !strings.Contains(v, "updated 2m") || !strings.Contains(v, "refreshing…") {
		t.Errorf("a retry over live data does not say how old it is:\n%s", lastLines(v, 3))
	}
}

// On a terminal too narrow for both, the reason wins over the age, and
// the footer keeps to its two lines.
func TestNarrowFooterKeepsTheReasonAndDropsTheAge(t *testing.T) {
	recordTicks(t)
	m, _, _ := newCachedModel(t)
	u, _ := m.Update(fetchMsg{err: networkError(), at: time.Now()})
	u, _ = u.(Model).Update(tea.WindowSizeMsg{Width: 36, Height: 45})
	foot := renderFooterBar(u.(Model))
	if !strings.Contains(foot, "offline · retrying") {
		t.Errorf("the narrow footer lost the reason:\n%s", foot)
	}
	if strings.Contains(foot, "cached") {
		t.Errorf("the narrow footer kept an age that does not fit:\n%s", foot)
	}
	wide, _ := u.(Model).Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	if !strings.Contains(renderFooterBar(wide.(Model)), "cached 3h ago") {
		t.Error("a wide footer dropped the age it has room for")
	}
}

// With nothing to show, a failure is still the error screen.
func TestFailedFetchWithNothingToShowIsTheErrorScreen(t *testing.T) {
	recordTicks(t)
	m := NewModel(cacheTestClient(t, false), cacheTestVersion, Options{})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	u, _ = u.(Model).Update(fetchMsg{err: networkError(), at: time.Now()})
	if v := u.(Model).View(); !strings.Contains(v, "Network error") {
		t.Errorf("no stats and a failed fetch did not show the error screen:\n%s", v)
	}
}

// Writes follow the mode at write time: none while public-only is on,
// none for a failure, none with the cache off.
func TestCacheWrites(t *testing.T) {
	m, _, client := newCachedModel(t)
	ok := fetchMsg{stats: cachedStats(), at: time.Now()}

	if m.cacheWriteCmd(ok) == nil {
		t.Error("a successful answer was not written")
	}
	if m.cacheWriteCmd(fetchMsg{stats: cachedStats(), err: networkError(), at: time.Now()}) != nil {
		t.Error("a failure was written")
	}
	client.SetPublicOnly(true)
	if m.cacheWriteCmd(ok) != nil {
		t.Error("an answer was written while public-only was on")
	}
	client.SetPublicOnly(false)
	m.cacheDir = ""
	if m.cacheWriteCmd(ok) != nil {
		t.Error("an answer was written with the cache off")
	}
}

func TestDataAgeLabel(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		age       time.Duration
		fromCache bool
		want      string
	}{
		{20 * time.Second, true, "cached just now"},
		{time.Minute, true, "cached 1m ago"},
		{3*time.Hour + 5*time.Minute, true, "cached 3h ago"},
		{50 * time.Hour, true, "cached 2d ago"},
		{2*time.Minute + 5*time.Second, false, "updated 2m5s ago"},
	} {
		m := Model{lastFetch: now.Add(-tc.age), fromCache: tc.fromCache}
		if got := dataAgeLabel(m, now); got != tc.want {
			t.Errorf("age %v fromCache=%v: got %q, want %q", tc.age, tc.fromCache, got, tc.want)
		}
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
