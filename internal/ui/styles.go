// Package ui contains the BubbleTea model, its update/view, and the
// Lipgloss styles that give the TUI its visual identity.
package ui

import "github.com/charmbracelet/lipgloss"

// Colour palette. Values are ANSI 256 codes (numeric strings) or hex
// strings — Lipgloss accepts both and picks the best match per terminal.
var (
	colAccent = lipgloss.Color("#E91E63") // magenta-pink — the "o" in octoscope
	colValue  = lipgloss.Color("#00D9FF") // cyan — the number that pops
	colOK     = lipgloss.Color("#2ECC71") // green — authenticated / success
	colWarn   = lipgloss.Color("#F1C40F") // yellow — unauthenticated / stale
	colError  = lipgloss.Color("#FF5555") // red — fetch failed
	colMuted  = lipgloss.Color("241")     // grey — labels, footers
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colAccent).
			PaddingLeft(1)

	boldStyle = lipgloss.NewStyle().Bold(true)

	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)

	errorStyle = lipgloss.NewStyle().Foreground(colError).Bold(true)

	okStyle   = lipgloss.NewStyle().Foreground(colOK)
	warnStyle = lipgloss.NewStyle().Foreground(colWarn)

	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colValue)

	// boxStyle is the card around each stat. Width is fixed so that a
	// row of boxes aligns cleanly via lipgloss.JoinHorizontal.
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colMuted).
			Padding(0, 2).
			Width(20)

	// sectionTitleStyle precedes each stat block — short bold accent
	// line, no box, keeps visual hierarchy light.
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colAccent).
				MarginTop(1)
)
