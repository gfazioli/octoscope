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

// The one flag combination parseArgs refuses, for --activity and --inbox
// alike. Exercised through the pure helper rather than parseArgs itself,
// because the rejection path calls os.Exit — same reason noColorActive is
// its own function.
func TestReportFlagWithoutOutputMode(t *testing.T) {
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
		if got := reportFlagWithoutOutputMode(c.activity, c.plain, c.json); got != c.want {
			t.Errorf("reportFlagWithoutOutputMode(%v, %v, %v) = %v, want %v",
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

func TestParseArgsInbox(t *testing.T) {
	t.Run("accepted with an output mode", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--plain", "--inbox"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --plain --inbox")
		}
		if !cli.inbox || !cli.plain || cli.activity {
			t.Fatalf("inbox=%v plain=%v activity=%v, want true true false", cli.inbox, cli.plain, cli.activity)
		}
	})
	t.Run("off unless asked for", func(t *testing.T) {
		_, _, cli, ok := parseArgs([]string{"--json", "--activity"})
		if !ok {
			t.Fatal("parseArgs returned !ok for --json --activity")
		}
		if cli.inbox {
			t.Fatal("inbox defaulted to true; the extra request must be opt-in")
		}
	})
}

// fakeSource counts what runNonInteractive actually asks for. Both review
// passes on #186 made the same point about the first version of these
// tests: they called AttachEvents directly, so deleting the FetchEvents
// block from runNonInteractive would have left every one of them green.
// Counting the calls is what makes the wiring the thing under test.
//
// The *Fail queues are answered first, one error per call, before the
// fake settles into its steady answer: that is how a test makes the
// first attempts of a request fail and a later one succeed.
type fakeSource struct {
	stats      *github.Stats
	statsCalls int
	statsFail  []error
	events     []github.Event
	eventCalls int
	eventLogin string
	eventsErr  error
	eventsFail []error
	inbox      []github.Notification
	inboxCalls int
	inboxErr   error
	inboxFail  []error
	publicOnly bool
}

// popFail answers the next queued failure, if any.
func popFail(q *[]error) error {
	if len(*q) == 0 {
		return nil
	}
	err := (*q)[0]
	*q = (*q)[1:]
	return err
}

func (f *fakeSource) FetchStats(context.Context) (*github.Stats, error) {
	f.statsCalls++
	if err := popFail(&f.statsFail); err != nil {
		return nil, err
	}
	return f.stats, nil
}

func (f *fakeSource) FetchEvents(_ context.Context, login string) ([]github.Event, error) {
	f.eventCalls++
	f.eventLogin = login
	if err := popFail(&f.eventsFail); err != nil {
		return nil, err
	}
	return f.events, f.eventsErr
}

func (f *fakeSource) FetchNotifications(context.Context) ([]github.Notification, error) {
	f.inboxCalls++
	if err := popFail(&f.inboxFail); err != nil {
		return nil, err
	}
	return f.inbox, f.inboxErr
}

func (f *fakeSource) PublicOnly() bool { return f.publicOnly }

func newFakeSource() *fakeSource {
	ts := time.Date(2026, 9, 15, 11, 55, 21, 0, time.UTC)
	return &fakeSource{
		stats: &github.Stats{Login: "gfazioli", Name: "G", Authenticated: true},
		events: []github.Event{
			{ID: "21190576879", Type: "PushEvent", Repo: "gfazioli/octoscope", CreatedAt: ts, IsPublic: true, Ref: "main"},
		},
		inbox: []github.Notification{
			{ID: "1", Reason: "review_requested", Type: "PullRequest", Title: "Public PR", Repo: "acme/lib", Unread: true, UpdatedAt: ts},
			{ID: "2", Reason: "mention", Type: "Issue", Title: "Private issue", Repo: "acme/secret", Unread: true, UpdatedAt: ts, IsPrivate: true},
		},
	}
}

func TestRunNonInteractiveOnlyFetchesEventsWhenAsked(t *testing.T) {
	t.Run("without --activity nothing asks for events", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true}); err != nil {
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
		if err := runNonInteractive(&buf, f, reportRequest{json: true, activity: true}); err != nil {
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
		if err := runNonInteractive(&buf, f, reportRequest{activity: true}); err != nil {
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
		err := runNonInteractive(&buf, f, reportRequest{json: true, activity: true})
		if err == nil {
			t.Fatal("want an error: absent means \"nobody asked\", so a failed fetch must not render as absent")
		}
		if buf.Len() != 0 {
			t.Errorf("wrote a partial document alongside the error:\n%s", buf.String())
		}
	})
}

// The same wiring test for --inbox, for the reason fakeSource exists:
// a test that called AttachInbox directly would stay green with the
// FetchNotifications call deleted from runNonInteractive.
func TestRunNonInteractiveOnlyFetchesInboxWhenAsked(t *testing.T) {
	inboxIDs := func(t *testing.T, out []byte) []string {
		t.Helper()
		var doc struct {
			Inbox *[]struct {
				ID string `json:"id"`
			} `json:"inbox"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, out)
		}
		if doc.Inbox == nil {
			return nil
		}
		ids := []string{}
		for _, n := range *doc.Inbox {
			ids = append(ids, n.ID)
		}
		return ids
	}

	t.Run("without --inbox nothing asks for it", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true, activity: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.inboxCalls != 0 {
			t.Errorf("made %d inbox requests, want 0 — the extra call must be opt-in", f.inboxCalls)
		}
		if strings.Contains(buf.String(), `"inbox"`) {
			t.Errorf("inbox must be absent, got:\n%s", buf.String())
		}
	})

	t.Run("with --inbox it makes exactly one, and it lands in the document", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true, inbox: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.inboxCalls != 1 || f.eventCalls != 0 {
			t.Errorf("made %d inbox and %d event requests, want 1 and 0", f.inboxCalls, f.eventCalls)
		}
		if got := inboxIDs(t, buf.Bytes()); strings.Join(got, ",") != "1,2" {
			t.Errorf("inbox ids = %v, want [1 2]", got)
		}
	})

	t.Run("--public-only leaves private threads out", func(t *testing.T) {
		f := newFakeSource()
		f.publicOnly = true
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true, inbox: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if got := inboxIDs(t, buf.Bytes()); strings.Join(got, ",") != "1" {
			t.Errorf("inbox ids = %v, want only the public thread [1]", got)
		}
	})

	t.Run("--plain --inbox takes the same path", func(t *testing.T) {
		f := newFakeSource()
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{inbox: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.inboxCalls != 1 {
			t.Errorf("made %d inbox requests, want exactly 1", f.inboxCalls)
		}
		if !strings.Contains(buf.String(), "Inbox (2)") {
			t.Errorf("plain output has no inbox section:\n%s", buf.String())
		}
	})

	t.Run("a failed inbox fails the run, and a scope refusal names the token type", func(t *testing.T) {
		f := newFakeSource()
		f.inboxErr = &github.FetchError{Reason: github.ReasonAuthScope, Err: errors.New("GitHub answered 403 for the notifications inbox")}
		var buf bytes.Buffer
		err := runNonInteractive(&buf, f, reportRequest{json: true, inbox: true})
		if err == nil {
			t.Fatal("want an error: absent means \"nobody asked\", so a failed fetch must not render as absent")
		}
		if buf.Len() != 0 {
			t.Errorf("wrote a partial document alongside the error:\n%s", buf.String())
		}
		if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "fine-grained token cannot read it") {
			t.Errorf("error = %q, want the status and the token-type hint", err)
		}
		var fe *github.FetchError
		if !errors.As(err, &fe) {
			t.Error("the hint must wrap the FetchError, not replace it")
		}
	})

	t.Run("any other failure passes through unadorned", func(t *testing.T) {
		zeroBackoff(t) // a persistent 502 is retried before it surfaces
		f := newFakeSource()
		f.inboxErr = &github.FetchError{Reason: github.ReasonServer, Err: errors.New("GitHub answered 502 for the notifications inbox")}
		err := runNonInteractive(&bytes.Buffer{}, f, reportRequest{json: true, inbox: true})
		if err == nil || strings.Contains(err.Error(), "fine-grained") {
			t.Errorf("error = %v, want the 502 without the token hint", err)
		}
	})
}

// parseArgs refuses --activity or --inbox without an output mode, and
// --inbox beside a username, by calling os.Exit(2), which cannot be
// observed in-process — so the table test
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
// is the one part of the marker a STALE value cannot carry: a variable
// left in somebody's environment names no pid of ours, so it does not
// match and the suite runs normally.
//
// It is not spoof-proof, and a review pass was right to say so. A launcher
// that sets OCTOSCOPE_TEST_PARSEARGS="$$:--activity" and then execs `go
// test` makes its own pid the test binary's parent, and would still hijack
// the run. Defending against a caller who is deliberately trying to break
// the harness is not what this is for; defending against an inherited
// variable is, and that is what it does.
const reexecEnv = "OCTOSCOPE_TEST_PARSEARGS"

// zeroBackoff removes the waits between retries for one test, so a test
// that drives a transient 5xx does not sleep through 0.8 s and 1.6 s.
func zeroBackoff(t *testing.T) {
	t.Helper()
	prev := transientBackoff
	transientBackoff = 0
	t.Cleanup(func() { transientBackoff = prev })
}

// TestRunNonInteractiveRetriesTransientErrors pins #224: the report a
// cron job runs rides out the 502 the dashboard rides out, on the same
// policy, for every request it makes — and still fails on anything that
// is not transient, after exactly one try.
func TestRunNonInteractiveRetriesTransientErrors(t *testing.T) {
	bad502 := func(what string) error {
		return &github.FetchError{Reason: github.ReasonServer, Err: errors.New("GitHub answered 502 for " + what)}
	}

	t.Run("uses the dashboard's backoff", func(t *testing.T) {
		if transientBackoff != github.TransientBackoff {
			t.Errorf("transientBackoff = %v, want github.TransientBackoff (%v)", transientBackoff, github.TransientBackoff)
		}
	})

	t.Run("waits the backoff between attempts", func(t *testing.T) {
		// The variable has to be what the report actually waits on, not
		// only what it is initialised to.
		prev := transientBackoff
		transientBackoff = 30 * time.Millisecond
		t.Cleanup(func() { transientBackoff = prev })
		f := newFakeSource()
		f.statsFail = []error{bad502("the dashboard"), bad502("the dashboard")}
		start := time.Now()
		if err := runNonInteractive(&bytes.Buffer{}, f, reportRequest{json: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
			t.Errorf("two retries took %v, want at least 90ms (30ms, then 60ms)", elapsed)
		}
	})

	t.Run("two 502s on the dashboard fetch, then the document", func(t *testing.T) {
		zeroBackoff(t)
		f := newFakeSource()
		f.statsFail = []error{bad502("the dashboard"), bad502("the dashboard")}
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true}); err != nil {
			t.Fatalf("runNonInteractive: %v — a transient 502 must be retried, not fail the run", err)
		}
		if f.statsCalls != 3 {
			t.Errorf("FetchStats calls = %d, want 3 (two 502s and the answer)", f.statsCalls)
		}
		if !strings.Contains(buf.String(), `"gfazioli"`) {
			t.Errorf("the document is missing after the retries:\n%s", buf.String())
		}
	})

	t.Run("a 502 that persists fails after the last attempt, with nothing written", func(t *testing.T) {
		zeroBackoff(t)
		f := newFakeSource()
		f.statsFail = []error{bad502("the dashboard"), bad502("the dashboard"), bad502("the dashboard"), bad502("the dashboard")}
		var buf bytes.Buffer
		err := runNonInteractive(&buf, f, reportRequest{json: true})
		var fe *github.FetchError
		if !errors.As(err, &fe) || fe.Reason != github.ReasonServer {
			t.Fatalf("err = %v, want the 502 once the attempts run out", err)
		}
		if f.statsCalls != github.TransientAttempts {
			t.Errorf("FetchStats calls = %d, want %d", f.statsCalls, github.TransientAttempts)
		}
		if buf.Len() != 0 {
			t.Errorf("wrote output alongside the error:\n%s", buf.String())
		}
	})

	t.Run("a refused token is tried once", func(t *testing.T) {
		zeroBackoff(t)
		f := newFakeSource()
		refused := &github.FetchError{Reason: github.ReasonAuth, Err: errors.New("bad credentials")}
		f.statsFail = []error{refused}
		if err := runNonInteractive(&bytes.Buffer{}, f, reportRequest{json: true}); err != refused {
			t.Fatalf("err = %v, want the auth error itself", err)
		}
		if f.statsCalls != 1 {
			t.Errorf("FetchStats calls = %d, want 1 — retrying a refused token is pointless", f.statsCalls)
		}
	})

	t.Run("--activity rides out a 502 too", func(t *testing.T) {
		zeroBackoff(t)
		f := newFakeSource()
		f.eventsFail = []error{bad502("the events feed")}
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true, activity: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.eventCalls != 2 || f.eventLogin != "gfazioli" {
			t.Errorf("event calls = %d for %q, want 2 for the resolved login", f.eventCalls, f.eventLogin)
		}
		if !strings.Contains(buf.String(), "21190576879") {
			t.Errorf("the feed fetched on the retry did not reach the document:\n%s", buf.String())
		}
	})

	t.Run("--inbox rides out a 502 too", func(t *testing.T) {
		zeroBackoff(t)
		f := newFakeSource()
		f.inboxFail = []error{bad502("the notifications inbox")}
		var buf bytes.Buffer
		if err := runNonInteractive(&buf, f, reportRequest{json: true, inbox: true}); err != nil {
			t.Fatalf("runNonInteractive: %v", err)
		}
		if f.inboxCalls != 2 {
			t.Errorf("inbox calls = %d, want 2", f.inboxCalls)
		}
		var doc struct {
			Inbox []struct {
				ID string `json:"id"`
			} `json:"inbox"`
		}
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, buf.String())
		}
		if len(doc.Inbox) != 2 || doc.Inbox[0].ID != "1" {
			t.Errorf("the inbox fetched on the retry did not reach the document: %+v", doc.Inbox)
		}
	})
}

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

func TestParseArgsRejectsReportFlagsItCannotHonour(t *testing.T) {
	cases := []struct {
		name string
		args string
		want int
		why  string // the refusal's reason, when want is 2
	}{
		{"--activity alone is refused", "--activity", 2, "--activity needs --plain or --json"},
		{"--activity --json is accepted", "--activity,--json", 0, ""},
		{"--activity --plain is accepted", "--activity,--plain", 0, ""},
		{"--inbox alone is refused", "--inbox", 2, "--inbox needs --plain or --json"},
		{"--inbox --json is accepted", "--inbox,--json", 0, ""},
		{"--inbox beside a username is refused", "torvalds,--json,--inbox", 2, "cannot be combined with a username"},
		{"--activity beside a username is accepted", "torvalds,--json,--activity", 0, ""},
		{"--scan alone is accepted", "--scan", 0, ""},
		{"--scan --json is accepted", "--scan,--json", 0, ""},
		{"--scan beside a username is refused", "torvalds,--scan", 2, "--scan sweeps your own repositories"},
		{"--scan with --activity is refused", "--scan,--json,--activity", 2, "--scan prints its own report"},
		{"--scan with --inbox is refused", "--scan,--inbox,--plain", 2, "--scan prints its own report"},
		{"--scan with --theme list is refused", "--scan,--theme,list", 2, "--scan cannot be combined with --theme list"},
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
			cmd := exec.Command(os.Args[0], "-test.run=TestParseArgsRejectsReportFlagsItCannotHonour")
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
			if c.want == 2 && !strings.Contains(string(out), c.why) {
				t.Errorf("the refusal did not say why:\n%s", out)
			}
		})
	}
}
