# octoscope

A terminal dashboard for your GitHub account — followers, stars, open issues
and PRs at a glance, auto-refreshed in the background.

![Go](https://img.shields.io/badge/Go-1.23-00ADD8)
![License](https://img.shields.io/badge/license-MIT-green)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)

## What it does

octoscope is a single-binary TUI built with [BubbleTea](https://github.com/charmbracelet/bubbletea).
It pulls a handful of numbers from the GitHub GraphQL API and keeps them
current on screen so you can check pulse on your repos without switching to
a browser.

v0.1.0 (MVP) ships a single-screen dashboard with:

- Followers / Following
- Public repository count
- Total stars across your non-fork repos
- Open issues (across your repos)
- Open pull requests (across your repos)
- Authenticated / unauthenticated badge
- Auto-refresh every 60s + manual refresh with `r`

See [ROADMAP](#roadmap) for what's planned next.

## Install

### Homebrew (planned — v0.2.0)

```bash
brew install gfazioli/tap/octoscope
```

Not published yet. Follow [releases](https://github.com/gfazioli/octoscope/releases)
for the first brew formula.

### From source

```bash
go install github.com/gfazioli/octoscope@latest
```

Requires Go 1.23+.

### Pre-built binary

Download a platform binary from the latest [GitHub Release](https://github.com/gfazioli/octoscope/releases/latest)
once the first release is published.

## Usage

```bash
octoscope
```

Key bindings while running:

| Key | Action |
|-----|--------|
| `r` | Refresh now |
| `q` | Quit |
| `ctrl+c` | Quit |

### Authentication

octoscope reads a GitHub token from, in order:

1. `$GITHUB_TOKEN` environment variable
2. `gh auth token` — if the [GitHub CLI](https://cli.github.com) is installed and logged in
3. No token — falls back to the unauthenticated GitHub rate limit (60 req/h)

You **need** a token if you plan to keep the dashboard open for more than a
few minutes: with a 60s auto-refresh the unauthenticated limit is burned
through in an hour.

## Roadmap

- [x] **v0.1.0 — MVP** — single-panel dashboard, viewer stats only
- [ ] **v0.2.0 — Multi-panel layout + Homebrew** — Lipgloss grid, compact / expanded layouts, first brew formula via goreleaser
- [ ] **v0.3.0 — Tabs & drill-down** — Overview / Repos / PRs / Issues / Activity tabs, per-repo detail view
- [ ] **v0.4.0 — Config file** — `~/.config/octoscope/config.toml` for refresh interval, org inclusion, theme
- [ ] **v0.5.0+** — notifications (mentions / review requests), contribution graph, traffic analytics, JSON/CSV export

## Contributing

Early days; the API and layout will move around. If you hit a bug or have an
idea, an issue is the best way in. Pull requests welcome once v0.1.0 ships.

## License

MIT — see [LICENSE](LICENSE).
