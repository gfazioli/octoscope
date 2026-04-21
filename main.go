// Package main is the octoscope entry point.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/ui"
)

const version = "0.2.0-dev"

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println("octoscope", version)
			return
		}
		if arg == "--help" || arg == "-h" {
			printHelp()
			return
		}
	}

	client, err := github.New()
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

func printHelp() {
	fmt.Println(`octoscope — a terminal dashboard for your GitHub account

Usage:
    octoscope            Start the dashboard
    octoscope -v         Print version
    octoscope -h         Print this help

Authentication:
    octoscope reads the $GITHUB_TOKEN environment variable first, and
    falls back to 'gh auth token' if the GitHub CLI is installed and
    authenticated. Without either, calls go unauthenticated (60 req/h).

Key bindings (while running):
    r       refresh now
    q       quit
    ctrl+c  quit`)
}
