package ui

import (
	"reflect"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
)

// The domain shape and the on-disk shape are two field lists that have
// to stay in step, and they live in two packages neither of which can
// see the other. A field added to one and forgotten in the other is a
// scan that silently stops recording something — it compiles, it passes
// every other test, and the loss only shows up as a delta that never
// fires. So the parity is asserted by name rather than trusted.
func TestTheTwoFingerprintShapesStayInStep(t *testing.T) {
	names := func(v any) map[string]bool {
		typ := reflect.TypeOf(v)
		out := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			out[typ.Field(i).Name] = true
		}
		return out
	}
	domain := names(github.ScanFingerprint{})
	disk := names(config.BaselineFingerprint{})

	for f := range domain {
		if !disk[f] {
			t.Errorf("github.ScanFingerprint.%s has no counterpart in config.BaselineFingerprint — it will never be persisted", f)
		}
	}
	for f := range disk {
		if !domain[f] {
			t.Errorf("config.BaselineFingerprint.%s has no counterpart in github.ScanFingerprint — it will never be read back", f)
		}
	}
}

// And parity of names is not parity of wiring: both conversions have to
// actually carry every field. The fixture is checked for having no zero
// field first, because a round trip of an empty struct is byte-perfect
// and proves nothing at all.
func TestFingerprintConversionCarriesEveryField(t *testing.T) {
	when := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	in := github.ScanFingerprint{
		CapturedAt: when,
		Verdict:    "watch",
		// Deliberately not the current version: the round trip has to
		// carry whatever was recorded, including a format this binary no
		// longer writes, or an old baseline would read back as current
		// and be diffed against keys it does not share (#169).
		DepsKeyVersion: 7,
		Ignition:       map[string]string{"main\x00.claude/settings.json": "abc123"},
		Signed:         map[string]bool{"main": true},
		Seen:           map[string]map[string]time.Time{"main\x00.claude/settings.json": {"abc123": when}},
		Deps:           map[string]map[string]string{"main\x00package-lock.json": {"fsevents@2.3.3": "sha512-a"}},
	}

	v := reflect.ValueOf(in)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Fatalf("fixture leaves %s zero; a round trip that carries nothing would still pass",
				v.Type().Field(i).Name)
		}
	}

	if got := baselineToFingerprint(fingerprintToBaseline(in)); !reflect.DeepEqual(got, in) {
		t.Errorf("a field was dropped in the round trip:\n got %+v\nwant %+v", got, in)
	}
}
