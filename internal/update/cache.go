package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Cache is the on-disk record of the last update check. It lets a
// fresh launch reuse a recent result instead of hitting the network
// every time octoscope starts — the in-session hourly tick keeps it
// current after that.
type Cache struct {
	// LastCheck is when the Releases API was last queried.
	LastCheck time.Time `json:"last_check"`
	// LatestTag is the tag returned by that query (e.g. "v0.19.0").
	LatestTag string `json:"latest_tag"`
}

// cachePath is <user cache dir>/octoscope/update.json. Honours
// $XDG_CACHE_HOME on Linux via os.UserCacheDir.
func cachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "octoscope", "update.json"), nil
}

// LoadCache reads the cached check. A missing or unreadable file
// returns a zero Cache and no error — the caller treats a zero
// LastCheck as "never checked", which is the correct first-run state.
func LoadCache() Cache {
	path, err := cachePath()
	if err != nil {
		return Cache{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Cache{}
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}
	}
	return c
}

// SaveCache persists the check result, creating the parent directory
// if needed. Errors are returned but are non-fatal for the caller — a
// failed cache write just means the next launch re-checks over the
// network.
func SaveCache(c Cache) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Fresh reports whether c was checked within d of now — i.e. recent
// enough to reuse without re-querying the network.
func (c Cache) Fresh(d time.Duration) bool {
	if c.LastCheck.IsZero() {
		return false
	}
	return time.Since(c.LastCheck) < d
}
