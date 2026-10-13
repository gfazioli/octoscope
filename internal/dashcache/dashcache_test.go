package dashcache

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

const (
	testKey     = "0123456789abcdef01234567"
	otherKey    = "fedcba9876543210fedcba98"
	testVersion = "9.9.9"
)

var t0 = time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)

// fill sets every field reachable from v to a non-zero value, walking
// into structs, slices and pointers. It fails on an unexported field or
// a kind JSON cannot carry: either would be dropped by the cache
// without a word, and the first paint would show a zero that reads
// like a real count. A new Stats field of that kind fails here first.
func fill(t *testing.T, v reflect.Value, path string, n *int) {
	t.Helper()
	*n++
	switch v.Kind() {
	case reflect.String:
		v.SetString("s" + strings.Repeat("x", *n%5))
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(int64(*n))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		fill(t, v.Elem(), path+"*", n)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			fill(t, s.Index(i), path+"[]", n)
		}
		v.Set(s)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(t0.Add(time.Duration(*n) * time.Minute)))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				t.Fatalf("%s.%s is unexported: the cache would drop it silently", path, f.Name)
			}
			fill(t, v.Field(i), path+"."+f.Name, n)
		}
	default:
		t.Fatalf("%s has kind %s, which this test (and maybe JSON) cannot carry", path, v.Kind())
	}
}

func fullStats(t *testing.T) *github.Stats {
	t.Helper()
	s := &github.Stats{}
	n := 0
	fill(t, reflect.ValueOf(s).Elem(), "Stats", &n)
	return s
}

func save(t *testing.T, dir, key string, s *github.Stats, at time.Time) {
	t.Helper()
	data, err := Encode(key, testVersion, s, at)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := Write(dir, key, data, at); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// Every field of the dashboard survives the round trip, except the
// rate-limit snapshot, which is dropped on purpose.
func TestRoundTripKeepsEveryFieldButTheRateLimit(t *testing.T) {
	dir := t.TempDir()
	s := fullStats(t)
	save(t, dir, testKey, s, t0)

	got, at, ok := Load(dir, testKey, testVersion, t0.Add(time.Hour))
	if !ok {
		t.Fatal("a fresh snapshot was not loaded")
	}
	if !at.Equal(t0) {
		t.Errorf("saved at %v, want %v", at, t0)
	}
	if got.RateLimit != nil {
		t.Errorf("the rate-limit snapshot was cached: %+v", got.RateLimit)
	}
	want := *s
	want.RateLimit = nil
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("round trip changed the stats\n got: %+v\nwant: %+v", *got, want)
	}
	// Encode works on a copy: the caller's stats keep their rate limit.
	if s.RateLimit == nil {
		t.Error("Encode cleared the rate limit on the caller's stats")
	}
}

func TestLoadMisses(t *testing.T) {
	s := &github.Stats{Login: "octocat", TotalStars: 42}

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string)
		key   string
		ver   string
		now   time.Time
	}{
		{"no file", func(*testing.T, string) {}, testKey, testVersion, t0},
		{"another version wrote it", func(t *testing.T, dir string) {
			save(t, dir, testKey, s, t0)
		}, testKey, "0.0.1", t0.Add(time.Minute)},
		{"exactly MaxAge old", func(t *testing.T, dir string) {
			save(t, dir, testKey, s, t0)
		}, testKey, testVersion, t0.Add(MaxAge)},
		{"saved in the future (the clock moved back)", func(t *testing.T, dir string) {
			save(t, dir, testKey, s, t0)
		}, testKey, testVersion, t0.Add(-time.Second)},
		{"another key's file under this key's name", func(t *testing.T, dir string) {
			save(t, dir, otherKey, s, t0)
			data, err := os.ReadFile(path(dir, otherKey))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path(dir, testKey), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}, testKey, testVersion, t0.Add(time.Minute)},
		{"corrupt", func(t *testing.T, dir string) {
			if err := os.WriteFile(path(dir, testKey), []byte(`{"version":`), 0o600); err != nil {
				t.Fatal(err)
			}
		}, testKey, testVersion, t0},
		{"no stats", func(t *testing.T, dir string) {
			body := `{"version":"` + testVersion + `","key":"` + testKey + `","saved_at":"2026-10-13T09:30:00Z"}`
			if err := os.WriteFile(path(dir, testKey), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}, testKey, testVersion, t0.Add(time.Minute)},
		{"stats with no login", func(t *testing.T, dir string) {
			save(t, dir, testKey, &github.Stats{TotalStars: 42}, t0)
		}, testKey, testVersion, t0.Add(time.Minute)},
		{"a key that is not one", func(t *testing.T, dir string) {
			save(t, dir, testKey, s, t0)
		}, "../" + testKey, testVersion, t0.Add(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if got, _, ok := Load(dir, tc.key, tc.ver, tc.now); ok || got != nil {
				t.Errorf("loaded %+v, want a miss", got)
			}
		})
	}
}

// A key carrying a path cannot reach a file outside the dashboard-
// prefix, even one whose contents name that same key.
func TestLoadRefusesAKeyWithAPathInIt(t *testing.T) {
	dir := t.TempDir()
	sneaky := "x/../" + testKey
	data, err := Encode(sneaky, testVersion, &github.Stats{Login: "octocat"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	// dashboard-x/../<key>.json cleans to <key>.json in dir.
	if err := os.WriteFile(filepath.Join(dir, testKey+fileSuffix), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := Load(dir, sneaky, testVersion, t0.Add(time.Minute)); ok {
		t.Errorf("a key with a path in it loaded %+v", got)
	}
}

// The snapshot just written is never pruned, whatever the clock says.
func TestWriteNeverPrunesItsOwnSnapshot(t *testing.T) {
	dir := t.TempDir()
	data, err := Encode(testKey, testVersion, &github.Stats{Login: "octocat"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, testKey, data, time.Now().Add(2*MaxAge)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path(dir, testKey)); err != nil {
		t.Errorf("Write pruned the snapshot it had just written: %v", err)
	}
}

// One tick inside the window still paints: the boundary above is the
// window's edge, not an off-by-one that refuses every snapshot.
func TestLoadHitsJustInsideMaxAge(t *testing.T) {
	dir := t.TempDir()
	save(t, dir, testKey, &github.Stats{Login: "octocat"}, t0)
	if _, _, ok := Load(dir, testKey, testVersion, t0.Add(MaxAge-time.Nanosecond)); !ok {
		t.Error("a snapshot one nanosecond inside MaxAge was refused")
	}
}

func TestWriteIsPrivateAndAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "octoscope")
	save(t, dir, testKey, &github.Stats{Login: "octocat"}, t0)

	info, err := os.Stat(path(dir, testKey))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX mode bits: Stat reports 0666 for any writable
	// file and Chmod only toggles read-only. There the file is private
	// because the cache directory, under %LocalAppData%, inherits an ACL
	// that grants only the user (and SYSTEM and Administrators).
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("snapshot mode %o, want 600: it holds private repository names", perm)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"+tempSuffix))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestWriteRefusesAKeyThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"", "../escape", strings.ToUpper(testKey), testKey + "0"} {
		if err := Write(dir, key, []byte("{}"), t0); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Write(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a refused key still wrote: %v", entries)
	}
}

// A write prunes what can never be loaded again — other snapshots and
// temp files older than MaxAge — and touches nothing else.
func TestWritePrunesOnlyWhatCanNeverLoadAgain(t *testing.T) {
	dir := t.TempDir()
	old := t0.Add(-MaxAge - time.Hour)
	recent := t0.Add(-time.Hour)

	files := map[string]time.Time{
		"dashboard-" + otherKey + ".json":         old,    // a rotated token's: goes
		"dashboard-aaaaaaaaaaaaaaaaaaaaaaaa.json": recent, // another account's, current: stays
		"dashboard-123.tmp":                       old,    // abandoned write: goes
		"dashboard-456.tmp":                       recent, // maybe in flight: stays
		"update.json":                             old,    // not ours: stays
	}
	for name, mtime := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	save(t, dir, testKey, &github.Stats{Login: "octocat"}, t0)

	for name, gone := range map[string]bool{
		"dashboard-" + otherKey + ".json":         true,
		"dashboard-aaaaaaaaaaaaaaaaaaaaaaaa.json": false,
		"dashboard-123.tmp":                       true,
		"dashboard-456.tmp":                       false,
		"update.json":                             false,
		"dashboard-" + testKey + ".json":          false,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if gone && err == nil {
			t.Errorf("%s survived the prune", name)
		}
		if !gone && err != nil {
			t.Errorf("%s was pruned: %v", name, err)
		}
	}
}

func TestPurgeRemovesEverySnapshotAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	save(t, dir, testKey, &github.Stats{Login: "octocat"}, t0)
	save(t, dir, otherKey, &github.Stats{Login: "hubot"}, t0)
	for _, name := range []string{"dashboard-9.tmp", "update.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := Purge(dir); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "dashboard-*"))
	if len(left) != 0 {
		t.Errorf("Purge left %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "update.json")); err != nil {
		t.Errorf("Purge removed a file that is not a snapshot: %v", err)
	}
	if err := Purge(filepath.Join(dir, "missing")); err != nil {
		t.Errorf("Purge of a missing directory = %v, want nil", err)
	}
}

func TestEncodeRefusesNilStats(t *testing.T) {
	if _, err := Encode(testKey, testVersion, nil, t0); err == nil {
		t.Error("Encode(nil) succeeded")
	}
}
