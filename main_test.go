package main

import "testing"

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
