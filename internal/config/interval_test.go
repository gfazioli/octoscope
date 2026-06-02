package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeInterval(t *testing.T) {
	cases := []struct {
		name     string
		in, want time.Duration
	}{
		{"zero -> default", 0, DefaultRefreshInterval},
		{"negative -> default", -5 * time.Minute, DefaultRefreshInterval},
		{"1ns -> min", time.Nanosecond, MinRefreshInterval},
		{"500ms -> min", 500 * time.Millisecond, MinRefreshInterval},
		{"exactly min -> min", MinRefreshInterval, MinRefreshInterval},
		{"30s passes through", 30 * time.Second, 30 * time.Second},
		{"1h passes through", time.Hour, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeInterval(c.in); got != c.want {
				t.Errorf("NormalizeInterval(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}

	// Invariant: the result is never <= 0 and never a positive below the
	// floor, for any input — so a future case can't slip a busy-loop value through.
	for _, in := range []time.Duration{-time.Hour, 0, time.Nanosecond, time.Second, MinRefreshInterval, time.Minute} {
		if got := NormalizeInterval(in); got <= 0 || got < MinRefreshInterval {
			t.Errorf("NormalizeInterval(%v) = %v violates the floor invariant", in, got)
		}
	}
}

// TestLoadFloorsInterval is the end-to-end regression for the
// busy-loop / persist path: a bad refresh_interval on disk is floored
// at Load (so it neither busy-loops nor round-trips back to disk).
func TestLoadFloorsInterval(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) Config {
		p := filepath.Join(dir, "c.toml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("load %q: %v", body, err)
		}
		return cfg
	}

	if got := load(`refresh_interval = "0s"`).RefreshInterval; got != DefaultRefreshInterval {
		t.Errorf("0s -> %v, want default %v", got, DefaultRefreshInterval)
	}
	if got := load(`refresh_interval = "-1m"`).RefreshInterval; got != DefaultRefreshInterval {
		t.Errorf("-1m -> %v, want default %v", got, DefaultRefreshInterval)
	}
	if got := load(`refresh_interval = "1ms"`).RefreshInterval; got != MinRefreshInterval {
		t.Errorf("1ms -> %v, want min %v", got, MinRefreshInterval)
	}
	if got := load(`refresh_interval = "30s"`).RefreshInterval; got != 30*time.Second {
		t.Errorf("30s -> %v, want 30s", got)
	}
}
