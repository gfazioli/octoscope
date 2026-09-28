package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
)

// Coverage note: these tests pin the fetchMsg/tickMsg HANDLER logic and
// the interval-change interleave. Every tick the package arms is built
// by tickCmd, which recordTicks swaps for a recorder, so what was armed
// — its delay and its generation — is read directly rather than
// inferred from a batch being non-nil, which the spinner tick alone
// makes it.

// armedTick is one tick read off the tickCmd seam: its delay is what
// the automatic refresh waits, its gen whether it is still the live one.
type armedTick struct {
	d   time.Duration
	gen int
}

// recordTicks swaps tickCmd for a recorder and restores it on cleanup.
// The recorded command fires at once rather than after d, so a test that
// runs a returned batch never waits out a back-off.
func recordTicks(t *testing.T) *[]armedTick {
	t.Helper()
	armed := new([]armedTick)
	orig := tickCmd
	tickCmd = func(d time.Duration, gen int) tea.Cmd {
		*armed = append(*armed, armedTick{d, gen})
		return func() tea.Msg { return tickMsg{gen: gen} }
	}
	t.Cleanup(func() { tickCmd = orig })
	return armed
}

// recordFetches swaps fetchCmd for a counter and restores it on cleanup.
// CodeRabbit on #155: `cmd != nil` is satisfied by the spinner tick
// alone, so a dispatched fetch has to be observed directly. The recorded
// command never reaches the network.
func recordFetches(t *testing.T) *int {
	t.Helper()
	n := new(int)
	orig := fetchCmd
	fetchCmd = func(*github.Client) tea.Cmd {
		*n++
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { fetchCmd = orig })
	return n
}

// TestStaleTickIgnored pins the generation guard: a tick from a
// superseded chain (gen != refreshGen) must NOT trigger a fetch and
// must NOT reschedule — it self-terminates, leaving one chain alive.
func TestStaleTickIgnored(t *testing.T) {
	m := newTestModel(t, "", false, nil)
	m.refreshGen = 1 // current chain is gen 1

	updated, cmd := m.Update(tickMsg{gen: 0}) // a leftover gen-0 tick
	m2 := updated.(Model)
	if m2.loading {
		t.Error("a stale tick must not start a fetch (loading should stay false)")
	}
	if cmd != nil {
		t.Error("a stale tick must not reschedule (cmd should be nil)")
	}
}

// TestCurrentGenTickFetches confirms the live tick still fetches — and
// arms nothing itself: the fetch's answer does, from what it says.
func TestCurrentGenTickFetches(t *testing.T) {
	armed := recordTicks(t)
	fetches := recordFetches(t)
	m := newTestModel(t, "", false, nil) // refreshGen == 0
	updated, _ := m.Update(tickMsg{gen: 0})
	if !updated.(Model).loading {
		t.Error("the live tick should start a fetch (loading=true)")
	}
	if *fetches != 1 {
		t.Errorf("the live tick dispatched %d fetches, want 1", *fetches)
	}
	if len(*armed) != 0 {
		t.Errorf("the tick armed %v before its fetch answered", *armed)
	}
}

// TestTickDuringAFetchLeavesTheTimerToIt pins that the automatic refresh
// never starts a second fetch while one is in flight — a manual `r`, or
// a settings refetch the timer caught up with. Two racing fetches let
// whichever answered first clear the loading state while the other was
// still out, so the spinner and the header mascot stopped mid-refresh,
// and let an older answer landing last replace newer stats. Nor does the
// tick re-arm itself: the delay it would use predates the answer in
// flight (#202). That answer arms the next tick, which is what keeps the
// automatic refresh from stopping for good.
func TestTickDuringAFetchLeavesTheTimerToIt(t *testing.T) {
	armed := recordTicks(t)
	m := newTestModel(t, "", false, nil) // refreshGen == 0
	m.loading = true                     // a manual fetch is in flight

	updated, cmd := m.Update(tickMsg{gen: 0})
	m = updated.(Model)
	if cmd != nil {
		t.Error("a tick during a fetch must do nothing — a fetch here races the one in flight")
	}
	if !m.loading {
		t.Error("the fetch in flight still owns the loading state")
	}
	if len(*armed) != 0 {
		t.Errorf("the tick re-armed itself %v, from a delay older than the answer in flight", *armed)
	}

	updated, _ = m.Update(fetchMsg{at: time.Now()})
	m = updated.(Model)
	if len(*armed) != 1 || (*armed)[0].gen != m.refreshGen {
		t.Fatalf("the answer must arm exactly one live tick, got %v (refreshGen %d)", *armed, m.refreshGen)
	}
}

// TestEveryAnswerArmsTheNextRefresh pins #202's rule: the answer to any
// fetch arms the next automatic refresh — the startup paint, the timer's
// own, `r` and a settings refetch all end in the same fetchMsg — and
// supersedes the tick pending before, so exactly one stays live however
// many answers land.
func TestEveryAnswerArmsTheNextRefresh(t *testing.T) {
	armed := recordTicks(t)
	m := newTestModel(t, "", false, nil) // interval 60s, refreshGen 0

	for i := 1; i <= 3; i++ {
		updated, _ := m.Update(fetchMsg{at: time.Now()})
		m = updated.(Model)
		if len(*armed) != i {
			t.Fatalf("after %d answers, %d ticks armed — want one per answer", i, len(*armed))
		}
		if a := (*armed)[i-1]; a.gen != m.refreshGen || a.d != m.interval {
			t.Errorf("answer %d armed %+v, want the interval %v under the live gen %d", i, a, m.interval, m.refreshGen)
		}
	}
	// The tick pending before the first answer (gen 0), and every one
	// armed before the last, are stale now: none may fetch.
	stale := []int{0}
	for _, a := range (*armed)[:len(*armed)-1] {
		stale = append(stale, a.gen)
	}
	for _, gen := range stale {
		if u, c := m.Update(tickMsg{gen: gen}); c != nil || u.(Model).loading {
			t.Errorf("superseded tick gen %d still acted", gen)
		}
	}
}

// TestARateLimitedRefreshMovesTheTimer is #202's sequence. The last
// automatic fetch was healthy, so the next tick is due one interval
// later; then `r` comes back with a primary rate limit whose reset is
// far away. The answer must move the timer to the reset, and the tick
// still due at the old cadence must not spend one more request against
// the limit.
func TestARateLimitedRefreshMovesTheTimer(t *testing.T) {
	armed := recordTicks(t)
	fetches := recordFetches(t)
	m := newTestModel(t, "", false, nil) // interval 60s; the pending tick is gen 0
	m.lastRateLimit = &github.RateLimit{ResetAt: time.Now().Add(40 * time.Minute)}

	// Through the key itself, not a hand-made answer: if `r` stopped
	// dispatching a fetch, the rest of this test would be moot.
	updated, _ := m.Update(key("r"))
	m = updated.(Model)
	if *fetches != 1 || !m.loading {
		t.Fatalf("r dispatched %d fetches (loading=%v), want 1", *fetches, m.loading)
	}
	if len(*armed) != 0 {
		t.Errorf("r armed %v before its fetch answered", *armed)
	}

	// Its answer: a primary rate limit.
	updated, _ = m.Update(fetchMsg{
		err: &github.FetchError{Reason: github.ReasonRateLimitPrimary, Err: errors.New("API rate limit exceeded")},
		at:  time.Now(),
	})
	m = updated.(Model)
	if len(*armed) != 1 {
		t.Fatalf("the answer armed %d ticks, want 1", len(*armed))
	}
	if a := (*armed)[0]; a.d < 39*time.Minute || a.gen != m.refreshGen {
		t.Errorf("armed %+v, want the back-off to the reset (~40m) under the live gen %d", a, m.refreshGen)
	}

	updated, cmd := m.Update(tickMsg{gen: 0})
	if cmd != nil || updated.(Model).loading {
		t.Error("the tick due at the old cadence fetched — one more request against the limit")
	}
}

// TestInitLeavesTheTimerToTheFirstAnswer pins that startup arms nothing
// of its own: a tick armed at launch would fire at the interval whatever
// the startup fetch answered, a rate limit included — #202 again — so
// the first answer arms the timer, like every answer after it.
func TestInitLeavesTheTimerToTheFirstAnswer(t *testing.T) {
	armed := recordTicks(t)
	fetches := recordFetches(t)
	m := newTestModel(t, "", false, nil)
	m.Init()
	if *fetches != 1 {
		t.Fatalf("Init dispatched %d fetches, want the first one", *fetches)
	}
	if len(*armed) != 0 {
		t.Errorf("Init armed %v — the startup fetch's answer does that", *armed)
	}
}

// TestSettingsIntervalChangeSupersedesChain pins that changing the
// refresh interval in the settings panel bumps the generation (old
// chain dies) and returns a fresh chain — no doubling.
func TestSettingsIntervalChangeSupersedesChain(t *testing.T) {
	m := newTestModel(t, "", false, nil) // interval 60s, refreshGen 0
	// Stage a different interval in the settings panel.
	m.settings = m.settings.Open(30*time.Second, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor, m.repos.commitCounts)

	cmd := m.applySettingsAndClose()
	if m.refreshGen != 1 {
		t.Errorf("interval change should bump refreshGen to 1, got %d", m.refreshGen)
	}
	if cmd == nil {
		t.Error("interval change should return a fresh tick chain")
	}
	if m.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", m.interval)
	}

	// A leftover tick from the old (gen-0) chain is now ignored — check
	// the UPDATED model's loading flag, not the pre-Update copy.
	u, c := m.Update(tickMsg{gen: 0})
	if c != nil || u.(Model).loading {
		t.Error("the superseded gen-0 tick should be dropped after the interval change")
	}
}

// TestAnAnswerAfterAnIntervalChangeKeepsOneTick is the interleave that
// once needed a fetch to remember the generation it started under: the
// interval changes while a fetch is in flight, then that fetch answers.
// Both arm; the later arming supersedes the earlier, at the new
// interval, so one tick stays live and no superseded chain comes back.
func TestAnAnswerAfterAnIntervalChangeKeepsOneTick(t *testing.T) {
	armed := recordTicks(t)
	m := newTestModel(t, "", false, nil) // interval 60s, refreshGen 0
	m.loading = true                     // a fetch is in flight
	m.settings = m.settings.Open(30*time.Second, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor, m.repos.commitCounts)
	m.applySettingsAndClose()

	updated, _ := m.Update(fetchMsg{at: time.Now()})
	m = updated.(Model)
	if len(*armed) != 2 {
		t.Fatalf("armed %v, want the interval change's tick and the answer's", *armed)
	}
	if a := (*armed)[1]; a.gen != m.refreshGen || a.d != 30*time.Second {
		t.Errorf("the answer armed %+v, want 30s under the live gen %d", a, m.refreshGen)
	}
	for _, gen := range []int{0, (*armed)[0].gen} {
		if u, c := m.Update(tickMsg{gen: gen}); c != nil || u.(Model).loading {
			t.Errorf("tick gen %d still acted after the answer re-armed", gen)
		}
	}
}

// TestIntervalChangeKeepsTheBackOff pins the same gap on the settings
// path: re-arming at the new interval while a primary rate limit is
// still in force would send a request against it at the new cadence.
func TestIntervalChangeKeepsTheBackOff(t *testing.T) {
	armed := recordTicks(t)
	m := newTestModel(t, "", false, nil)
	m.errReason = github.ReasonRateLimitPrimary
	m.lastRateLimit = &github.RateLimit{ResetAt: time.Now().Add(40 * time.Minute)}
	m.settings = m.settings.Open(30*time.Second, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor, m.repos.commitCounts)

	m.applySettingsAndClose()
	if len(*armed) != 1 {
		t.Fatalf("the interval change armed %d ticks, want 1", len(*armed))
	}
	if a := (*armed)[0]; a.d < 39*time.Minute {
		t.Errorf("the interval change re-armed at %v while rate-limited until the reset (~40m)", a.d)
	}
}

// TestSettingsIntervalFloored pins that the settings-save path floors a
// sub-minimum interval (NormalizeInterval wiring), not just the
// standalone helper: a panel-entered 0 becomes the default.
func TestSettingsIntervalFloored(t *testing.T) {
	m := newTestModel(t, "", false, nil)
	m.interval = 30 * time.Second // a non-default current value, so the change is observable
	m.settings = m.settings.Open(0, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor, m.repos.commitCounts)

	_ = m.applySettingsAndClose()
	if m.interval != config.DefaultRefreshInterval {
		t.Errorf("a 0 interval entered in the settings panel should floor to %v, got %v",
			config.DefaultRefreshInterval, m.interval)
	}
}
