// Package dashcache keeps the dashboard's last successful snapshot on
// disk, so a launch can paint at once and refetch in the background
// (#65). It is a cache, never a source of truth: anything it cannot
// vouch for — another octoscope version, another identity, an age past
// MaxAge, a file that does not parse — is a miss, and a miss is exactly
// the first-run experience.
//
// What it stores is github.Stats minus the rate-limit snapshot, which
// describes a budget at one instant and is meaningless on the next
// launch. The feed, the inbox, GitHub's service status and the drill-ins
// are not cached; they load as they always have.
//
// Callers decide *when* it applies. The dashboard never reads or writes
// it in public-only mode (the rule the update-check cache already
// follows, so screenshot sessions stay hermetic), and non-interactive
// runs never touch it: a script wants the live answer.
package dashcache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// MaxAge is how old a snapshot may be and still paint the first screen.
// A week covers someone who opens the dashboard on Mondays; older than
// that, the picture misleads more than it helps, and the launch waits
// for the network as it always did.
const MaxAge = 7 * 24 * time.Hour

const (
	filePrefix = "dashboard-"
	fileSuffix = ".json"
	tempSuffix = ".tmp"
)

// validKey is what github.Client.CacheKey produces: 24 lowercase hex
// digits. The key becomes a file name, so anything else is refused
// rather than joined into a path.
var validKey = regexp.MustCompile(`^[0-9a-f]{24}$`)

// ErrInvalidKey is returned by Write for a key that is not one
// CacheKey could have produced.
var ErrInvalidKey = errors.New("dashcache: invalid key")

// entry is the file's shape. Key and Version are repeated inside the
// file, not only implied by its name and the binary reading it, so a
// file copied or renamed by hand still cannot paint the wrong account
// or decode into a struct that has since changed.
type entry struct {
	Version string        `json:"version"`
	Key     string        `json:"key"`
	SavedAt time.Time     `json:"saved_at"`
	Stats   *github.Stats `json:"stats"`
}

// Dir is <user cache dir>/octoscope — the directory update.json already
// lives in. Honours $XDG_CACHE_HOME on Linux via os.UserCacheDir.
func Dir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "octoscope"), nil
}

func path(dir, key string) string {
	return filepath.Join(dir, filePrefix+key+fileSuffix)
}

// Load returns the snapshot stored for key, and when it was taken, if
// it was written by this octoscope version for this key and is younger
// than MaxAge at now. Every other case — no file, unreadable, corrupt,
// another version, another key, too old, saved "in the future" by a
// clock that has since moved back — is a miss, reported as ok=false.
func Load(dir, key, version string, now time.Time) (*github.Stats, time.Time, bool) {
	if !validKey.MatchString(key) {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(path(dir, key))
	if err != nil {
		return nil, time.Time{}, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, time.Time{}, false
	}
	if e.Version != version || e.Key != key || e.Stats == nil || e.Stats.Login == "" {
		return nil, time.Time{}, false
	}
	age := now.Sub(e.SavedAt)
	if e.SavedAt.IsZero() || age < 0 || age >= MaxAge {
		return nil, time.Time{}, false
	}
	return e.Stats, e.SavedAt, true
}

// Encode serialises s as the snapshot for key, taken at now. It drops
// the rate-limit snapshot on a copy and leaves s untouched.
//
// Encoding is split from Write so the caller can run it where nothing
// else touches s — the dashboard encodes on its update loop and hands
// only the bytes to a background write.
func Encode(key, version string, s *github.Stats, now time.Time) ([]byte, error) {
	if s == nil {
		return nil, errors.New("dashcache: nil stats")
	}
	cp := *s
	cp.RateLimit = nil
	return json.Marshal(entry{Version: version, Key: key, SavedAt: now, Stats: &cp})
}

// Write stores data as the snapshot for key. The file is created 0600
// (it holds private repository names) and replaces the previous one
// atomically, so a reader never sees half a snapshot. Windows has no
// mode bits; there the per-user ACL that %LocalAppData% passes down to
// the cache directory is what keeps the file private. Then it prunes:
// other snapshots and abandoned temp files older than MaxAge can never
// be loaded again — a rotated token leaves exactly that behind.
func Write(dir, key string, data []byte, now time.Time) error {
	if !validKey.MatchString(key) {
		return ErrInvalidKey
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filePrefix+"*"+tempSuffix)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// os.CreateTemp already creates 0600; asserting it here keeps that a
	// property of this function rather than of the standard library.
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path(dir, key)); err != nil {
		os.Remove(tmpName)
		return err
	}
	prune(dir, key, now)
	return nil
}

// prune removes snapshots other than key's, and temp files, last
// modified more than MaxAge before now. Best-effort: a file it cannot
// stat or remove is left for the next write.
func prune(dir, key string, now time.Time) {
	keep := path(dir, key)
	for _, pattern := range []string{filePrefix + "*" + fileSuffix, filePrefix + "*" + tempSuffix} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			if m == keep {
				continue
			}
			info, err := os.Lstat(m)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if now.Sub(info.ModTime()) > MaxAge {
				os.Remove(m)
			}
		}
	}
}

// Purge removes every dashboard snapshot and temp file in dir, whatever
// its key or age. It runs when the user turns the cache off: a cache
// they no longer want should not stay behind holding their private
// repository names. A missing directory is not an error.
func Purge(dir string) error {
	var first error
	for _, pattern := range []string{filePrefix + "*" + fileSuffix, filePrefix + "*" + tempSuffix} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil && first == nil {
			first = err
		}
		for _, m := range matches {
			info, err := os.Lstat(m)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
				first = err
			}
		}
	}
	return first
}
