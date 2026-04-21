package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
)

// Model is the top-level BubbleTea state. For the v0.1.0 MVP it's a
// single-screen dashboard; later phases add tabs / panels by nesting
// sub-models here rather than replacing this one.
type Model struct {
	client *github.Client
	stats  *github.Stats
	err    error

	loading   bool
	lastFetch time.Time

	width, height int

	interval time.Duration

	// version string shown in the banner and footer. Set by the
	// caller — keeps the UI package ignorant of the main package's
	// build-time constant.
	version string
}

// fetchMsg carries the outcome of a FetchStats call back to the
// model's Update loop.
type fetchMsg struct {
	stats *github.Stats
	err   error
	at    time.Time
}

// tickMsg fires at `interval` and schedules the next auto-refresh.
type tickMsg time.Time

// NewModel returns a Model ready for tea.NewProgram. The first fetch
// is kicked off as an Init command so the UI renders a loading state
// immediately rather than waiting for the network.
func NewModel(client *github.Client, version string) Model {
	return Model{
		client:   client,
		loading:  true,
		interval: 60 * time.Second,
		version:  version,
	}
}

// Init starts the first fetch and schedules the periodic tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(m.client), tickCmd(m.interval))
}

// Update routes keyboard, resize, and network messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, fetchCmd(m.client)
			}
		}

	case fetchMsg:
		m.loading = false
		m.stats = msg.stats
		m.err = msg.err
		m.lastFetch = msg.at

	case tickMsg:
		// Every `interval`, re-fetch and reschedule. Kicked off as a
		// batch so the next tick is enqueued even if fetchCmd takes a
		// while.
		return m, tea.Batch(fetchCmd(m.client), tickCmd(m.interval))
	}

	return m, nil
}

// fetchCmd runs FetchStats with a 10s timeout and packs the result in
// a fetchMsg. Returning a command rather than calling directly keeps
// the network off BubbleTea's synchronous update loop.
func fetchCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stats, err := client.FetchStats(ctx)
		return fetchMsg{stats: stats, err: err, at: time.Now()}
	}
}

// tickCmd is just tea.Tick with a tickMsg envelope, for readability.
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
