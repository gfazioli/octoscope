# Asset: PR drill-in detail (v0.11.0 surface).
# Boots octoscope, switches to the PRs tab, filters down to the
# mantine-gantt-chart PR (the only deterministic public authored
# PR — filtering also skips the review-requests sticky section),
# opens the drill-in. Captures the rich PR detail (state chip,
# glamour-rendered description, checks, files-changed line,
# timeline). Feeds docs/screenshots/drill-in/screenshot-pr-detail.png.
#
# CAROUSEL GEOMETRY — keep identical across the five drill-in
# tapes; see action-menu.tape for the rationale.

Output out/pr-drill-in.gif

Set FontSize 36
Set Width 3400
Set Height 2340
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

Type "octoscope --public-only"
Enter
Sleep 14s

Type "3"
Sleep 1s

# Filter to the deterministic public PR, then open its drill-in.
Type "/"
Type "gantt"
Enter
Sleep 1s
Enter

# Drill-in fetch: GraphQL detail + REST files in parallel.
Sleep 8s

Screenshot out/pr-detail.png

Sleep 1s

Escape
Sleep 500ms
Type "q"
