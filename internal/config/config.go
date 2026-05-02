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

	// Theme picks one of the built-in palettes. Empty (zero value)
	// means "use the default theme" — Defaults() sets it to
	// "octoscope". The config package doesn't validate the name to
	// avoid importing the ui package (would create a cycle); main.go
	// validates against ui.IsValidTheme before booting the model.
	Theme string `toml:"theme"`

	// AccentColor optionally overrides only the Accent slot of the
	// active theme. Accepts anything lipgloss takes (hex like
	// "#FF0080", ANSI 256 numbers like "201"). Empty disables the
	// override. The other palette slots stay on the named theme.
	AccentColor string `toml:"accent_color"`
}

// Defaults returns the values octoscope uses when no config file
// exists (or a present file leaves keys unset).
func Defaults() Config {
	return Config{
		RefreshInterval: 60 * time.Second,
		PublicOnly:      false,
		Compact:         false,
		Theme:           "octoscope",
		AccentColor:     "",
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
		Theme           string `toml:"theme"`
		AccentColor     string `toml:"accent_color"`
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
	if raw.Theme != "" {
		cfg.Theme = raw.Theme
	}
	if raw.AccentColor != "" {
		cfg.AccentColor = raw.AccentColor
	}

	return cfg, nil
}

// Save serialises cfg to TOML and writes it to path atomically: we
// write to `path.tmp` first, then rename onto `path`. The rename is
// atomic on Unix and Windows, so a crash mid-write can never leave
// the user with a half-written config file.
//
// Parent directories are created if needed (mkdir -p), so callers
// don't have to ensure ~/.config/octoscope/ exists before saving.
//
// The output keeps human-readable comments matching the example in
// the README so a saved file remains pleasant to hand-edit later.
func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config: empty path")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Render the optional accent_color line only when set, so a
	// pristine config file doesn't carry a meaningless empty key.
	accentLine := ""
	if cfg.AccentColor != "" {
		accentLine = fmt.Sprintf("\n# Override the active theme's accent colour. Hex (\"#FF0080\")\n# or ANSI 256 (\"201\"). Leave unset to use the theme's default.\naccent_color = %q\n", cfg.AccentColor)
	}

	body := fmt.Sprintf(`# octoscope configuration
# Auto-saved by octoscope. Edit by hand or via the in-app settings
# panel (press ',' while running).

# Auto-refresh interval. Go duration syntax: "30s", "1m", "5m", "1h".
refresh_interval = %q

# Hide private repositories, PRs and issues from the list tabs.
public_only = %t

# Use the dense card layout in the Overview tab.
compact = %t

# Visual theme. Built-in: octoscope (default), high-contrast,
# terminal, monochrome, stranger-things, phosphor, amber.
theme = %q
%s`, cfg.RefreshInterval.String(), cfg.PublicOnly, cfg.Compact, cfg.Theme, accentLine)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup; the rename failure is the primary error.
		_ = os.Remove(tmp)
		return fmt.Errorf("config: %w", err)
	}
	return nil
}
