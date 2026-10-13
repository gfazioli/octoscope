package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// With dashboard_cache off, the launch deletes the snapshots already on
// disk and hands the dashboard no directory; with it on, the directory
// passes through untouched. A directory that cannot be resolved turns
// the feature off instead of failing the launch.
func TestDashboardCacheDir(t *testing.T) {
	seed := func(t *testing.T) (string, string) {
		dir := t.TempDir()
		snap := filepath.Join(dir, "dashboard-0123456789abcdef01234567.json")
		if err := os.WriteFile(snap, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir, snap
	}

	t.Run("off deletes and disables", func(t *testing.T) {
		dir, snap := seed(t)
		if got := dashboardCacheDir(false, func() (string, error) { return dir, nil }); got != "" {
			t.Errorf("got %q, want no directory", got)
		}
		if _, err := os.Stat(snap); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the snapshot survived turning the cache off: %v", err)
		}
	})

	t.Run("on keeps and passes the directory", func(t *testing.T) {
		dir, snap := seed(t)
		if got := dashboardCacheDir(true, func() (string, error) { return dir, nil }); got != dir {
			t.Errorf("got %q, want %q", got, dir)
		}
		if _, err := os.Stat(snap); err != nil {
			t.Errorf("an enabled cache lost its snapshot: %v", err)
		}
	})

	t.Run("unresolvable directory disables", func(t *testing.T) {
		if got := dashboardCacheDir(true, func() (string, error) { return "", errors.New("no home") }); got != "" {
			t.Errorf("got %q, want no directory", got)
		}
	})
}
