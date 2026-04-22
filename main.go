// Package main is the octoscope entry point.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/ui"
)

const version = "0.4.0"

func main() {
	userLogin, ok := parseArgs(os.Args[1:])
	if !ok {
		// parseArgs already printed version / help / error and told
		// the caller "done". Exit cleanly.
		return
	}

	client, err := github.New(userLogin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(ui.NewModel(client, version), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "octoscope: %v\n", err)
		os.Exit(1)
	}
}

// parseArgs handles the tiny CLI surface: -v/-h print and ok=false so
// main returns, a single positional argument becomes the username to
// render, and anything else is an error.
//
// Returns (userLogin, shouldContinue). shouldContinue=false means
// "we've handled this invocation, don't start the TUI".
func parseArgs(args []string) (string, bool) {
	var userLogin string
	for _, arg := range args {
		switch {
		case arg == "--version" || arg == "-v":
			fmt.Println("octoscope", version)
			return "", false
		case arg == "--help" || arg == "-h":
			printHelp()
			return "", false
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
	return userLogin, true
}

func printHelp() {
	fmt.Println(`octoscope — a terminal dashboard for your GitHub account

Usage:
    octoscope                Show the authenticated user's dashboard
    octoscope <username>     Show the public dashboard for any GitHub user
    octoscope -v, --version  Print version
    octoscope -h, --help     Print this help

Authentication:
    octoscope reads the $GITHUB_TOKEN environment variable first, and
    falls back to 'gh auth token' if the GitHub CLI is installed and
    authenticated. Without either, calls go unauthenticated (60 req/h)
    and a username must be passed on the command line.

Examples:
    octoscope                # your dashboard (token required)
    octoscope torvalds       # any public profile (token optional)
    octoscope gfazioli       # the author's profile

Key bindings (while running):
    r       refresh now
    q       quit
    ctrl+c  quit`)
}
