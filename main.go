// Package main is the octoscope entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/report"
	"github.com/gfazioli/octoscope/internal/ui"
)

const version = "0.23.0"

// cliOverrides tracks settings the user passed on the command line.
// Pointers carry "was set" semantics: a nil field means "no CLI
// override for this key, fall back to the config file (or the
// built-in default if the file omits it too)".
type cliOverrides struct {
	refresh    *time.Duration
	compact    *bool
	publicOnly *bool
	theme      *string
	noSponsor  *bool
	noColor    *bool

	// plain / json select a non-interactive run: fetch once, print a
	// static summary, exit — no TUI. They are run modes rather than
	// config overrides (no config-file fallback), so plain bools suffice.
	// Mutually exclusive; parseArgs rejects both at once.
	plain bool
	json  bool
}

func main() {
	userLogin, configPath, cli, ok := parseArgs(os.Args[1:])
	if !ok {
		// parseArgs already printed version / help / error and told
		// the caller "done". Exit cleanly.
		return
	}

	if configPath == "" {
		configPath = config.DefaultPath()
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}

	// CLI flags override file values. Defaults() fills any gap.
	if cli.refresh != nil {
		cfg.RefreshInterval = *cli.refresh
	}
	if cli.compact != nil {
		cfg.Compact = *cli.compact
	}
	if cli.publicOnly != nil {
		cfg.PublicOnly = *cli.publicOnly
	}
	if cli.theme != nil {
		cfg.Theme = *cli.theme
	}
	if cli.noSponsor != nil {
		// --no-sponsor is one-directional: present means "force off".
		cfg.ShowSponsor = false
	}

	// Validate theme name before booting the model so a typo surfaces
	// as a clear startup error instead of a silent fallback. Done before
	// the NO_COLOR override so a "--theme bogus" still errors even when
	// NO_COLOR would otherwise mask it with the monochrome fallback.
	if !ui.IsValidTheme(cfg.Theme) {
		fmt.Fprintf(os.Stderr,
			"octoscope: unknown theme %q (valid: %s)\n",
			cfg.Theme, strings.Join(ui.ThemeNames(), ", "))
		os.Exit(2)
	}

	// Same treatment for the view-preference keys (#35): a typo'd
	// default_sort / default_work_filter / default_star_history
	// surfaces at startup instead of silently falling back. Unlike
	// the theme, an empty value is fine — it means "built-in default".
	validateViewPrefKey("default_sort", cfg.DefaultSort, ui.IsValidSortKey, ui.SortKeys)
	validateViewPrefKey("default_work_filter", cfg.DefaultWorkFilter, ui.IsValidWorkFilterKey, ui.WorkFilterKeys)
	validateViewPrefKey("default_star_history", cfg.DefaultStarHistory, ui.IsValidStarHistoryKey, ui.StarHistoryKeys)

	// NO_COLOR (the de-facto env convention, https://no-color.org) and
	// the --no-color flag force the zero-chroma monochrome palette,
	// overriding any configured / --theme value and dropping the accent
	// override (a hex accent would re-introduce colour). It's an
	// environment directive for this run only — ui.Model keeps the
	// file's theme / accent_color keys untouched on persist (see
	// Options.NoColor), so the user's real theme survives unsetting it.
	noColor := noColorActive(cli.noColor != nil, os.Getenv("NO_COLOR"))
	if noColor {
		cfg.Theme = "monochrome"
		cfg.AccentColor = ""
	}

	client, err := github.New(userLogin, github.Options{
		PublicOnly: cfg.PublicOnly,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}
	client.SetWatchRepos(cfg.WatchRepos)

	// Non-interactive modes fetch once and print, never entering the TUI
	// / alt-screen. Placed after client setup so watched repos and review
	// requests are part of the same fetch the dashboard would run.
	if cli.plain || cli.json {
		if err := runNonInteractive(client, cli.json); err != nil {
			fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
			os.Exit(1)
		}
		return
	}

	model := ui.NewModel(client, version, ui.Options{
		Interval:           cfg.RefreshInterval,
		Compact:            cfg.Compact,
		ConfigPath:         configPath,
		Theme:              cfg.Theme,
		AccentColor:        cfg.AccentColor,
		DefaultSort:        cfg.DefaultSort,
		DefaultWorkFilter:  cfg.DefaultWorkFilter,
		DefaultStarHistory: cfg.DefaultStarHistory,
		PinnedRepos:        cfg.PinnedRepos,
		PinnedIssues:       cfg.PinnedIssues,
		ShowSponsor:        cfg.ShowSponsor,
		CheckForUpdates:    cfg.CheckForUpdates,
		NoColor:            noColor,
	})
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}
}

// runNonInteractive performs a single dashboard fetch and prints it to
// stdout — as JSON when asJSON is true, otherwise a plain-text summary —
// then returns. It honours the client's public-only filter (applied here
// the same way the TUI applies it at render time) and never starts the
// BubbleTea program. The fetch shares the TUI's 30s timeout.
func runNonInteractive(client *github.Client, asJSON bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := client.FetchStats(ctx)
	if err != nil {
		return err
	}
	publicOnly := client.PublicOnly()
	if publicOnly {
		stats = stats.Public()
	}

	rep := report.FromStats(stats, version, time.Now().UTC(), publicOnly)
	if asJSON {
		return report.RenderJSON(os.Stdout, rep)
	}
	return report.RenderPlain(os.Stdout, rep)
}

// parseArgs walks the CLI tokens once, consuming next-arg values for
// two-arg flags (--refresh DURATION, --config PATH).
//
// Returns (userLogin, configPath, cli, shouldContinue). shouldContinue
// is false when --version / --help has already produced output.
func parseArgs(args []string) (string, string, cliOverrides, bool) {
	var (
		userLogin  string
		configPath string
		cli        cliOverrides
	)

	// nextValue consumes args[i+1] as the value for a two-arg flag.
	// Mutates i via pointer so the caller's loop advances past the
	// consumed token.
	nextValue := func(i *int, flag string) string {
		*i++
		if *i >= len(args) {
			fmt.Fprintf(os.Stderr,
				"octoscope: %s needs a value\nRun with --help for usage.\n", flag)
			os.Exit(2)
		}
		return args[*i]
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--version" || arg == "-v":
			fmt.Println("octoscope", version)
			return "", "", cli, false
		case arg == "--help" || arg == "-h":
			printHelp()
			return "", "", cli, false
		case arg == "--public-only":
			t := true
			cli.publicOnly = &t
		case arg == "--compact":
			t := true
			cli.compact = &t
		case arg == "--no-sponsor":
			t := true
			cli.noSponsor = &t
		case arg == "--no-color":
			t := true
			cli.noColor = &t
		case arg == "--plain":
			cli.plain = true
		case arg == "--json":
			cli.json = true
		case arg == "--refresh":
			raw := nextValue(&i, "--refresh")
			d, err := time.ParseDuration(raw)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"octoscope: invalid --refresh %q: %v\n"+
						"Use Go duration syntax: 30s, 1m, 5m, 1h.\n",
					raw, err)
				os.Exit(2)
			}
			// Floor it through the shared normaliser so --refresh 0s /
			// -5m / 1ns can't busy-loop the fetch.
			nd := config.NormalizeInterval(d)
			cli.refresh = &nd
		case arg == "--config":
			configPath = nextValue(&i, "--config")
		case arg == "--theme":
			t := nextValue(&i, "--theme")
			cli.theme = &t
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr,
				"octoscope: unknown flag: %s\nRun with --help for usage.\n", arg)
			os.Exit(2)
		case userLogin == "":
			userLogin = arg
		default:
			fmt.Fprintf(os.Stderr,
				"octoscope: unexpected extra argument: %s\n"+
					"Only one username can be passed at a time.\n", arg)
			os.Exit(2)
		}
	}
	if cli.plain && cli.json {
		fmt.Fprintln(os.Stderr,
			"octoscope: --plain and --json are mutually exclusive")
		os.Exit(2)
	}
	return userLogin, configPath, cli, true
}

// validateViewPrefKey exits with a usage error when a view-preference
// config key holds a value the UI layer doesn't recognise. Empty means
// "not set, keep the built-in default" and always passes.
func validateViewPrefKey(name, value string, valid func(string) bool, keys func() []string) {
	if value == "" || valid(value) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"octoscope: unknown %s %q (valid: %s)\n",
		name, value, strings.Join(keys(), ", "))
	os.Exit(2)
}

// noColorActive resolves whether colour output should be suppressed for
// this run. flagSet is true when --no-color was passed; env is the raw
// value of the NO_COLOR environment variable. Following the no-color.org
// convention, NO_COLOR counts only when present and non-empty (its value
// is irrelevant — "0" still disables colour), so an explicit NO_COLOR=""
// does NOT trigger it. The --no-color flag triggers it regardless.
func noColorActive(flagSet bool, env string) bool {
	return flagSet || env != ""
}

func printHelp() {
	fmt.Println(`octoscope — a terminal dashboard for your GitHub account

Usage:
    octoscope                Show the authenticated user's dashboard
    octoscope <username>     Show the public dashboard for any GitHub user

Flags:
    --refresh DURATION       Auto-refresh interval, Go duration syntax
                             (e.g. 30s, 1m, 5m, 1h). Default: 1m.
    --compact                Use the dense card layout in the Overview tab
                             (smaller cards, abbreviated labels)
    --public-only            Hide private repos/PRs/issues from the lists
                             (safe for screenshots and demos; global
                             counters like PRs Authored stay complete)
    --no-sponsor             Skip the sponsor splash for this run (set
                             show_sponsor = false in the config to opt
                             out permanently)
    --config PATH            Read config from PATH instead of the default
                             ~/.config/octoscope/config.toml
    --theme NAME             Visual theme. Built-in: octoscope (default),
                             high-contrast, terminal, monochrome,
                             stranger-things, phosphor, amber
    --no-color               Force the zero-chroma monochrome theme,
                             overriding --theme / config. Also honoured
                             via the NO_COLOR environment variable
                             (https://no-color.org). Does not alter the
                             theme saved in your config file.
    --plain                  Print a static, human-readable summary and
                             exit (no TUI). For quick checks and shells.
    --json                   Print the dashboard as JSON and exit (no
                             TUI). Stable, documented schema — pipe it
                             into jq, cron jobs, status-lines. Mutually
                             exclusive with --plain.
    -v, --version            Print version
    -h, --help               Print this help

Configuration:
    octoscope reads ~/.config/octoscope/config.toml (or
    $XDG_CONFIG_HOME/octoscope/config.toml when set) on startup. All
    keys are optional; CLI flags override file values. See the README
    for the full key list and an example file.

Authentication:
    octoscope reads the $GITHUB_TOKEN environment variable first, and
    falls back to 'gh auth token' if the GitHub CLI is installed and
    authenticated. Without either, calls go unauthenticated (60 req/h)
    and a username must be passed on the command line.

Examples:
    octoscope                       # your dashboard (token required)
    octoscope torvalds              # any public profile (token optional)
    octoscope --refresh 30s         # auto-refresh every 30 seconds
    octoscope --compact             # dense layout for narrow terminals
    octoscope --public-only         # screenshot-safe (hides private items)
    octoscope --no-color            # force monochrome (or set NO_COLOR=1)
    octoscope --plain               # static text summary, no TUI
    octoscope --json | jq .social   # machine-readable, pipe into jq

Key bindings (while running):
    r         refresh now
    1-6       jump to tab (Overview, Repos, PRs, Issues, Activity, What's new)
    tab       next tab   (shift+tab for previous)
    s         cycle sort column (Repos / PRs / Issues)
    /         filter list by substring
    enter     open the drill-in detail for the selected row
    o         open the selected repo / PR / issue in your browser
    ,         open the in-app settings panel
    ?         keyboard-shortcut overlay
    q         quit
    ctrl+c    quit`)
}
