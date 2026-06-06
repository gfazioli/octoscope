# Asset: action menu on a Repos row.
# Goes to the Repos tab (pinned octoscope is row 0 under
# --public-only), opens the action menu with space. Captures the
# modal centred over the table.
# Feeds docs/screenshots/drill-in/screenshot-action-menu.png.
#
# CAROUSEL GEOMETRY — shared verbatim by all five drill-in tapes
# (action-menu, repo-detail, pr-drill-in, pr-diff-viewer ×2): the
# landing carousel cross-fades the slides, so the banner, profile
# card, tab bar and footer must land on identical pixel coordinates
# in every capture or the transition visibly jumps. 3400 wide
# (~148 cols) clears the single-line-footer threshold (~147 cols,
# see overview.tape); FontSize 36 keeps glyphs crisp at @2x.

Output out/action-menu.gif

Set FontSize 36
Set Width 3400
Set Height 2340
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

# --public-only keeps the captured asset screenshot-safe and
# suppresses the sponsor splash.
Type "octoscope --public-only"
Enter
Sleep 14s

Type "2"
Sleep 2s

# Space opens the action menu modal (Open in GitHub / View details
# / Unpin / Copy URL — octoscope is pinned, hence Unpin).
Space
Sleep 2s

Screenshot out/action-menu.png

Sleep 1s

Escape
Sleep 1s
Type "q"
