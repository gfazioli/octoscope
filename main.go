// Package main is the octoscope entry point.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/ui"
)

const version = "0.9.1"

// cliOverrides tracks settings the user passed on the command line.
// Pointers carry "was set" semantics: a nil field means "no CLI
// override for this key, fall back to the config file (or the
// built-in default if the file omits it too)".
type cliOverrides struct {
	refresh    *time.Duration
	compact    *bool
	publicOnly *bool
	theme      *string
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

	// Validate theme name before booting the model so a typo surfaces
	// as a clear startup error instead of a silent fallback.
	if !ui.IsValidTheme(cfg.Theme) {
		fmt.Fprintf(os.Stderr,
			"octoscope: unknown theme %q (valid: %s)\n",
			cfg.Theme, strings.Join(ui.ThemeNames(), ", "))
		os.Exit(2)
	}

	client, err := github.New(userLogin, github.Options{
		PublicOnly: cfg.PublicOnly,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}

	model := ui.NewModel(client, version, ui.Options{
		Interval:    cfg.RefreshInterval,
		Compact:     cfg.Compact,
		ConfigPath:  configPath,
		Theme:       cfg.Theme,
		AccentColor: cfg.AccentColor,
	})
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}
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
			cli.refresh = &d
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
	return userLogin, configPath, cli, true
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
    --config PATH            Read config from PATH instead of the default
                             ~/.config/octoscope/config.toml
    --theme NAME             Visual theme. Built-in: octoscope (default),
                             high-contrast, terminal, monochrome,
                             stranger-things, phosphor, amber
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

Key bindings (while running):
    r         refresh now
    1-5       jump to tab (Overview, Repos, PRs, Issues, Activity)
    tab       next tab   (shift+tab for previous)
    s         cycle sort column (Repos / PRs / Issues)
    /         filter list by substring
    enter     open the selected repo / PR / issue in your browser
    ,         open the in-app settings panel
    q         quit
    ctrl+c    quit`)
}
