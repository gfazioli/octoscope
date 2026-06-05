# Smoke + asset: dashboard hero shot.
# Captures the Overview tab on first paint — profile card, social,
# activity, operational, network sections + languages bar.
#
# Used to regenerate docs/screenshots/screenshot.png when the
# landing page falls out of sync with the current UI.

Output out/overview.gif

# ~148 columns: the footer goes single-line (hotkeys left, status
# right) only when the terminal fits keys + status + spacer —
# renderFooterBar needs ~147 cols with the current chips. Below
# that it stacks onto two lines, which reads cluttered in a hero
# shot. Not much wider than this, or the layout stops filling the
# frame.
#
# Geometry is @2x (FontSize 36 ≈ logical 18) so the landing hero
# stays crisp on retina displays — the page scales it down.
# NOTE: font metrics don't scale perfectly linearly — at 36pt the
# cell is slightly wider than 2× the 18pt cell, so the width
# carries extra margin to stay safely above the 147-col footer
# threshold.
Set FontSize 36
Set Width 3400
# Tall enough that the full Overview (banner → Network → footer)
# fits without scrolling — this is the landing hero, it must show
# the whole dashboard. If the footer ever shows "↑/↓ scroll",
# the height is too small for the current UI: bump it.
Set Height 3000
Set Padding 20
# Pure-black background to match the landing page and the
# octoscope theme's own identity (the TUI lets the terminal
# background show through).
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

# --public-only keeps the captured asset screenshot-safe (no
# private repo names) and suppresses the v0.16.0 sponsor splash,
# which would otherwise cover the dashboard on first paint
# regardless of the local config.
Type "octoscope --public-only"
Enter

# Let the first GraphQL refresh complete so the dashboard is
# fully populated before we capture the still. The fetch is up
# to five parallel branches (v0.15+) and may absorb a transient
# 502 retry (v0.17+, ~800ms backoff per attempt), so give it
# generous headroom.
Sleep 14s

Screenshot out/overview.png

# Tail of the recording so the GIF has a moment of breathing
# before it loops.
Sleep 1s

Type "q"
