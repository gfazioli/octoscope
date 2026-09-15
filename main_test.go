package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

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

// TestParseArgsThemeList covers the --theme list run mode (#63): the
// literal value "list" selects the print-and-exit mode without picking
// a theme, while a real name still sets cli.theme as before.
func TestParseArgsThemeList(t *testing.T) {
	t.Run("list selects the run mode", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--theme", "list"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --theme list")
		}
		if !cli.themeList {
			t.Error("--theme list should set cli.themeList")
		}
		if cli.theme != nil {
			t.Errorf("--theme list must not select a theme, got %q", *cli.theme)
		}
	})

	t.Run("a real name still selects a theme", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--theme", "phosphor"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --theme phosphor")
		}
		if cli.themeList {
			t.Error("--theme phosphor must not trip the list mode")
		}
		if cli.theme == nil || *cli.theme != "phosphor" {
			t.Errorf("--theme phosphor should set cli.theme, got %v", cli.theme)
		}
	})

	t.Run("a real name after list clears the mode (last --theme wins)", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--theme", "list", "--theme", "phosphor"})
		if !ok {
			t.Fatal("parseArgs returned !ok")
		}
		if cli.themeList {
			t.Error("--theme phosphor after --theme list should clear list mode")
		}
		if cli.theme == nil || *cli.theme != "phosphor" {
			t.Errorf("last --theme should win, got %v", cli.theme)
		}
	})
}

// The one flag combination parseArgs refuses. Exercised through the pure
// helper rather than parseArgs itself, because the rejection path calls
// os.Exit — same reason noColorActive is its own function.
func TestActivityWithoutOutputMode(t *testing.T) {
	cases := []struct {
		activity, plain, json bool
		want                  bool
	}{
		{false, false, false, false}, // nothing passed
		{false, true, false, false},  // --plain alone
		{false, false, true, false},  // --json alone
		{true, true, false, false},   // --activity --plain
		{true, false, true, false},   // --activity --json
		{true, true, true, false},    // parseArgs rejects --plain --json earlier
		{true, false, false, true},   // --activity alone: the one refusal
		{false, true, true, false},   // --plain --json, rejected earlier, not here
	}
	for _, c := range cases {
		if got := activityWithoutOutputMode(c.activity, c.plain, c.json); got != c.want {
			t.Errorf("activityWithoutOutputMode(%v, %v, %v) = %v, want %v",
				c.activity, c.plain, c.json, got, c.want)
		}
	}
}

func TestParseArgsActivity(t *testing.T) {
	t.Run("accepted with an output mode", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--json", "--activity"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --json --activity")
		}
		if !cli.activity || !cli.json {
			t.Fatalf("activity=%v json=%v, want both true", cli.activity, cli.json)
		}
	})
	t.Run("off unless asked for", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--json"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --json")
		}
		if cli.activity {
			t.Fatal("activity defaulted to true; the extra request must be opt-in")
		}
	})
}

// fakeSource counts what runNonInteractive actually asks for. Both review
// passes on #186 made the same point about the first version of these
// tests: they called AttachEvents directly, so deleting the FetchEvents
// block from runNonInteractive would have left every one of them green.
// Counting the calls is what makes the wiring the thing under test.
type fakeSource struct {
	stats      *github.Stats
	statsCalls int
	events     []github.Event
	eventCalls int
	eventLogin string
	eventsErr  error
	publicOnly bool
}

func (f *fakeSource) FetchStats(context.Context) (*github.Stats, error) {
	f.statsCalls++
	return f.stats, nil
}

func (f *fakeSource) FetchEvents(_ context.Context, login string) ([]github.Event, error) {
	f.eventCalls++
	f.eventLogin = login
	return f.events, f.eventsErr
}

func (f *fakeSource) PublicOnly() bool { return f.publicOnly }

func newFakeSource() *fakeSource {
	ts := time.Date(2026, 9, 15, 11, 55, 21, 0, time.UTC)
	return &fakeSource{
		stats: &github.Stats{Login: "gfazioli", Name: "G", Authenticated: true},
		events: []github.Event{
			{ID: "21190576879", Type: "PushEvent", Repo: "gfazioli/octoscope", CreatedAt: ts, IsPublic: true, Ref: "main"},
		},
	}
}

func TestRunNonInteractiveOnlyFetchesEventsWhenAsked(t *testing.T) {
	t.Run("without --activity nothing asks for events", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, true, false); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.eventCalls != 0 {
			t.Errorf("made %d event requests, want 0 — the extra call must be opt-in", f.eventCalls)
		}
		if strings.Contains(buf.String(), "recent_activity") {
			t.Errorf("recent_activity must be absent, got:\n%s", buf.String())
		}
	})

	t.Run("with --activity it makes exactly one, and it lands in the document", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, true, true); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.eventCalls != 1 {
			t.Errorf("made %d event requests, want exactly 1", f.eventCalls)
		}
		// The endpoint has no `viewer` form, so the login FetchStats
		// resolved is the one that has to be passed on.
		if f.eventLogin != "gfazioli" {
			t.Errorf("fetched events for %q, want the login the stats resolved", f.eventLogin)
		}
		var doc struct {
			RecentActivity []map[string]any `json:"recent_activity"`
		}
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, buf.String())
		}
		if len(doc.RecentActivity) != 1 || doc.RecentActivity[0]["id"] != "21190576879" {
			t.Errorf("the fetched event did not reach the document: %#v", doc.RecentActivity)
		}
	})

	t.Run("--plain --activity takes the same path", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, false, true); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.eventCalls != 1 {
			t.Errorf("made %d event requests, want exactly 1", f.eventCalls)
		}
		if !strings.Contains(buf.String(), "Recent activity") {
			t.Errorf("plain output has no activity section:\n%s", buf.String())
		}
	})

	t.Run("a failed feed fails the run rather than silently omitting it", func(t *testing.T) {
		f := newFakeSource()
		f.eventsErr = errors.New("403 rate limited")
		var buf bytes.Buffer
		err := runNonInteractive(&buf, f, true, true)
		if err == nil {
			t.Fatal("want an error: absent means \"nobody asked\", so a failed fetch must not render as absent")
		}
		if buf.Len() != 0 {
			t.Errorf("wrote a partial document alongside the error:\n%s", buf.String())
		}
	})
}

// parseArgs refuses --activity without an output mode by calling
// os.Exit(2), which cannot be observed in-process — so the table test
// above covers only the predicate, and would stay green if parseArgs
// stopped calling it. Both review passes on #186 said so.
//
// The standard re-exec idiom closes that: the test runs itself as a child
// with an env marker, the child calls the real parseArgs and lets it exit,
// and the parent reads the status. It exercises the branch rather than
// the helper it delegates to.
// reexecEnv marks a process as the child half of the re-exec test below.
// Its value is "<pid of the parent>:<comma-separated args>", and the child
// checks that pid against its own PPID.
//
// The pid is not decoration. A first version keyed on the variable being
// non-empty, so an inherited OCTOSCOPE_TEST_PARSEARGS in somebody's
// environment made `go test ./...` exit 0 having run none of this package's
// tests; a second required a "reexec:" prefix, which a review pass pointed
// out only makes the collision contrived rather than impossible. The PPID
// is the one part of the marker the parent knows and an exported variable
// cannot guess, so a stale value from any other process simply does not
// match and the suite runs normally.
const reexecEnv = "OCTOSCOPE_TEST_PARSEARGS"

func TestMain(m *testing.M) {
	if v := os.Getenv(reexecEnv); v != "" {
		if pid, args, ok := strings.Cut(v, ":"); ok && pid == strconv.Itoa(os.Getppid()) {
			// Exits by itself when the arguments are refused; if it
			// returns, the refusal did not happen and 0 says so.
			parseArgs(strings.Split(args, ","))
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestParseArgsRejectsActivityWithoutAnOutputMode(t *testing.T) {
	cases := []struct {
		name string
		args string
		want int
	}{
		{"--activity alone is refused", "--activity", 2},
		{"--activity --json is accepted", "--activity,--json", 0},
		{"--activity --plain is accepted", "--activity,--plain", 0},
	}
	// A marker this process did not set must not be mistaken for a child
	// run. Both shapes that fooled earlier versions are covered: a bare
	// value, and one carrying a pid that is not ours.
	for _, bogus := range []struct{ name, value string }{
		{"a bare marker", "--activity"},
		{"an old prefixed marker", "reexec:--activity"},
		{"a marker naming another process", "999999:--activity"},
	} {
		t.Run(bogus.name+" does not hijack the package", func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestParseArgsNoColor")
			cmd.Env = append(os.Environ(), reexecEnv+"="+bogus.value)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%q changed the run: %v\n%s", bogus.value, err, out)
			}
			if !strings.Contains(string(out), "PASS") {
				t.Errorf("expected the normal suite to run, got:\n%s", out)
			}
		})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestParseArgsRejectsActivityWithoutAnOutputMode")
			cmd.Env = append(os.Environ(), reexecEnv+"="+strconv.Itoa(os.Getpid())+":"+c.args)
			out, err := cmd.CombinedOutput()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("running the child: %v", err)
			}
			if code != c.want {
				t.Errorf("octoscope %s exited %d, want %d\noutput: %s",
					strings.ReplaceAll(c.args, ",", " "), code, c.want, out)
			}
			if c.want == 2 && !strings.Contains(string(out), "--activity needs --plain or --json") {
				t.Errorf("the refusal did not say why:\n%s", out)
			}
		})
	}
}
