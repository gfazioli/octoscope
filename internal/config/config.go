// Package config loads octoscope's user-facing configuration from a
// TOML file at ~/.config/octoscope/config.toml (or
// $XDG_CONFIG_HOME/octoscope/config.toml when set).
//
// Precedence is the standard Unix order, applied by callers:
//
//	CLI flags > config file > built-in defaults
//
// All keys are optional. A missing file is not an error — the caller
// just gets defaults. A malformed file is an error: octoscope exits
// loudly rather than silently masking a bad user config.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk shape of ~/.config/octoscope/config.toml.
//
// Fields use Go duration / bool primitives so callers can drop them
// straight into github.Options and ui.NewModel without conversions.
// Zero-valued struct (Defaults()) is what an empty / missing file
// produces.
type Config struct {
	// RefreshInterval controls how often octoscope re-fetches from the
	// GitHub GraphQL API. Accepts Go duration syntax: "30s", "1m",
	// "5m", "1h". No floor enforced — pick a value that fits your
	// rate-limit budget (5000/h authenticated, 60/h unauthenticated).
	RefreshInterval time.Duration `toml:"refresh_interval"`

	// PublicOnly hides private repositories, PRs and issues from the
	// list tabs. Useful if you screenshot or screencast octoscope
	// often. Global counters (PRs Authored, Issues Authored) stay
	// complete since they're aggregate numbers, not titles.
	PublicOnly bool `toml:"public_only"`

	// Compact uses a denser card layout: smaller card width,
	// abbreviated labels. Fits more onto narrow terminals.
	Compact bool `toml:"compact"`
}

// Defaults returns the values octoscope uses when no config file
// exists (or a present file leaves keys unset).
func Defaults() Config {
	return Config{
		RefreshInterval: 60 * time.Second,
		PublicOnly:      false,
		Compact:         false,
	}
}

// DefaultPath returns the file octoscope looks for absent
// `--config PATH`. Honours $XDG_CONFIG_HOME when set; falls back to
// ~/.config/octoscope/config.toml otherwise. Returns "" when neither
// $HOME nor $XDG_CONFIG_HOME yields a usable directory — extremely
// rare but worth handling so we don't panic.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "octoscope", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "octoscope", "config.toml")
}

// Load reads `path` and returns the parsed Config merged onto
// Defaults(). Missing file → Defaults() with err == nil. Malformed
// file → defaults plus a non-nil error so callers can surface the
// reason and exit.
//
// The TOML library decodes into the receiver in place, so any keys
// not in the file simply keep their default zero values — except
// time.Duration, which TOML doesn't know natively. We post-process
// that one field below.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, nil
	}

	// Stat first so we can distinguish "no file" (fine) from "file
	// exists but unreadable" (real error).
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: %w", err)
	}

	// Decode into a parallel struct that uses string for the
	// duration field; we can't tag time.Duration directly because
	// BurntSushi/toml doesn't know it.
	var raw struct {
		RefreshInterval string `toml:"refresh_interval"`
		PublicOnly      *bool  `toml:"public_only"`
		Compact         *bool  `toml:"compact"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}

	if raw.RefreshInterval != "" {
		d, err := time.ParseDuration(raw.RefreshInterval)
		if err != nil {
			return cfg, fmt.Errorf("config %s: refresh_interval %q: %w",
				path, raw.RefreshInterval, err)
		}
		cfg.RefreshInterval = d
	}
	if raw.PublicOnly != nil {
		cfg.PublicOnly = *raw.PublicOnly
	}
	if raw.Compact != nil {
		cfg.Compact = *raw.Compact
	}

	return cfg, nil
}
