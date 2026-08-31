package config

// Scan baselines are octoscope's only piece of *machine-written* state,
// and they deliberately do not live in config.toml. That file is
// hand-edited and round-tripped carefully to preserve the user's own
// formatting and lists; filling it with blob OIDs and timestamps would
// wreck it. They get a sibling JSON file instead, which the user is
// never expected to open.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// BaselineFingerprint is one repository's recorded auto-execution
// surface, as captured by a previous scan. A later scan diffs against it
// to answer the question the content axes cannot: did something that
// auto-executes *change*?
//
// Keys in Ignition are "branch\x00path" — see BaselineKey. Signed maps a
// branch name to whether its tip carried a genuine author signature.
type BaselineFingerprint struct {
	CapturedAt time.Time `json:"captured_at"`

	// Verdict is the scan verdict at the moment of capture. Recorded so
	// a later report can admit that the baseline was taken when the repo
	// was *already* flagged — a "no change since last scan" on top of a
	// compromised baseline means nothing has improved, not that all is
	// well.
	Verdict string `json:"verdict"`

	Ignition map[string]string `json:"ignition"`
	Signed   map[string]bool   `json:"signed"`

	// Seen is the unbounded history: for each Ignition key, every
	// distinct blob OID ever observed there and the date it was first
	// observed. Ignition answers "what was here last time"; this answers
	// "has this exact content ever been here before".
	//
	// **Unbounded on purpose, and retention is not the same knob as
	// scoring.** baselineMaxAge stops a stale delta from moving the
	// verdict, which is right: past a month most of a diff is legitimate
	// churn and scoring it teaches the reader to ignore the axis. That is
	// an argument about scoring, and it was quietly deciding retention
	// too. A fixed lookback is a *published dwell time*, and patience is
	// the whole point of the attack this axis exists to notice — anything
	// willing to revert itself quietly is willing to wait.
	//
	// Growth is bounded by **distinct contents**, not by scans: a file
	// that oscillates between two versions forever holds two entries, and
	// a file nobody touches adds none. A path with real churn accumulates
	// one entry per version, which is its edit history — the unavoidable
	// price of being able to recognise a return to any of them.
	Seen map[string]map[string]time.Time `json:"seen,omitempty"`
}

// Baselines is the whole store, keyed by "owner/name".
//
// A repository rename produces a new key, so the first scan after a
// rename reads as "no baseline yet" and simply starts a fresh history.
// That loses continuity but never invents a delta, which is the right
// way round for a security signal.
type Baselines struct {
	Repos map[string]BaselineFingerprint `json:"repos"`
}

// BaselinePathFor puts the store next to whichever config file is
// actually in use, so `--config /somewhere/else.toml` keeps its state
// together with it instead of writing back into the default directory.
// An empty configPath falls back to BaselinePath.
func BaselinePathFor(configPath string) string {
	if configPath == "" {
		return BaselinePath()
	}
	return filepath.Join(filepath.Dir(configPath), "scan-baselines.json")
}

// BaselinePath returns the baseline store's location, alongside the
// config file and honouring $XDG_CONFIG_HOME the same way. Returns ""
// when no usable directory exists, which callers treat as "no
// persistence available" rather than an error.
func BaselinePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "octoscope", "scan-baselines.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "octoscope", "scan-baselines.json")
}

// LoadBaselines reads the store. A missing file yields an empty store
// and no error — that is the ordinary first-run state.
//
// A *malformed* file also yields an empty store and no error, unlike
// config.Load which fails loudly. The asymmetry is deliberate: a broken
// config is a user mistake worth stopping for, whereas this file is
// machine-written and the user never touches it. Refusing to scan
// because a cache got corrupted would trade a security tool for a
// bookkeeping problem. The next successful scan rewrites it.
func LoadBaselines(path string) Baselines {
	empty := Baselines{Repos: map[string]BaselineFingerprint{}}
	if path == "" {
		return empty
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var out Baselines
	if err := json.Unmarshal(b, &out); err != nil {
		return empty
	}
	if out.Repos == nil {
		out.Repos = map[string]BaselineFingerprint{}
	}
	return out
}

// SaveBaselines writes the store, creating the directory if needed. The
// write goes to a temp file and is renamed into place so an interrupted
// save cannot leave a half-written store behind.
func SaveBaselines(path string, b Baselines) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".scan-baselines-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// The cleanup calls below run when an error is already in hand and
	// nothing useful could be done with a second one, so their returns
	// are discarded explicitly rather than silently.
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
