package main

import "testing"

// TestParseArgsNoColor pins the --no-color flag plumbing: present →
// cli.noColor non-nil (main() forces the monochrome theme), absent →
// nil (config/--theme/default wins). Also confirms it composes with a
// username arg, mirroring the --no-sponsor coverage.
func TestParseArgsNoColor(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--no-color"})
		if !ok {
			t.Fatal("parseArgs returned !ok for a valid flag")
		}
		if cli.noColor == nil {
			t.Error("--no-color should set cli.noColor (got nil)")
		}
	})

	t.Run("absent", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{})
		if !ok {
			t.Fatal("parseArgs returned !ok for empty args")
		}
		if cli.noColor != nil {
			t.Errorf("absent --no-color should leave cli.noColor nil, got %v", *cli.noColor)
		}
	})

	t.Run("composes with username", func(t *testing.T) {
		login, _, cli, ok := parseArgs([]string{"--no-color", "torvalds"})
		if !ok {
			t.Fatal("parseArgs returned !ok")
		}
		if cli.noColor == nil {
			t.Error("--no-color should be set alongside a username")
		}
		if login != "torvalds" {
			t.Errorf("username = %q, want torvalds", login)
		}
	})
}

// TestNoColorActive pins the NO_COLOR resolution rules: the flag always
// triggers; the env var triggers only when present and non-empty (per
// the no-color.org convention, its value — "0", "false", etc. — is
// irrelevant), so an explicit empty string must NOT trigger.
func TestNoColorActive(t *testing.T) {
	cases := []struct {
		name    string
		flagSet bool
		env     string
		want    bool
	}{
		{"nothing set", false, "", false},
		{"flag only", true, "", true},
		{"env empty string is ignored", false, "", false},
		{"env zero still disables colour", false, "0", true},
		{"env one", false, "1", true},
		{"env arbitrary value", false, "false", true},
		{"flag and env both", true, "1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := noColorActive(tc.flagSet, tc.env); got != tc.want {
				t.Errorf("noColorActive(%v, %q) = %v, want %v",
					tc.flagSet, tc.env, got, tc.want)
			}
		})
	}
}

// TestParseArgsNoSponsor pins the --no-sponsor flag plumbing: present →
// cli.noSponsor non-nil (main() forces ShowSponsor off), absent → nil
// (config/default wins). Also confirms it composes with a username arg.
func TestParseArgsNoSponsor(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--no-sponsor"})
		if !ok {
			t.Fatal("parseArgs returned !ok for a valid flag")
		}
		if cli.noSponsor == nil {
			t.Error("--no-sponsor should set cli.noSponsor (got nil)")
		}
	})

	t.Run("absent", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{})
		if !ok {
			t.Fatal("parseArgs returned !ok for empty args")
		}
		if cli.noSponsor != nil {
			t.Errorf("absent --no-sponsor should leave cli.noSponsor nil, got %v", *cli.noSponsor)
		}
	})

	t.Run("composes with username", func(t *testing.T) {
		login, _, cli, ok := parseArgs([]string{"--no-sponsor", "torvalds"})
		if !ok {
			t.Fatal("parseArgs returned !ok")
		}
		if cli.noSponsor == nil {
			t.Error("--no-sponsor should be set alongside a username")
		}
		if login != "torvalds" {
			t.Errorf("username = %q, want torvalds", login)
		}
	})
}
