# Asset: Repos drill-in detail view, star history in cumulative mode.
# Boots octoscope, filters the Repos list down to "octoscope", opens
# its drill-in, switches the star sparkline to the cumulative curve
# (v0.18.0) and captures the still.
#
# Regenerates docs/screenshots/drill-in/screenshot-repo-detail.png —
# the carousel slide on the landing. Geometry matches the sibling
# slides (2030×1984 @2x, ~92 cols) so the carousel stays uniform.

Output out/repo-detail.gif

# --public-only keeps the asset screenshot-safe and suppresses the
# sponsor splash (see overview.tape for the full rationale).
Set FontSize 36
Set Width 2030
Set Height 1984
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

Type "octoscope --public-only"
Enter

# First dashboard fetch (five parallel branches + possible transient
# retry — same headroom as overview.tape).
Sleep 14s

# Repos tab, filter the list down to the octoscope row, open it.
Type "2"
Sleep 1s
Type "/"
Type "octoscope"
Enter
Sleep 1s
Enter

# Drill-in fetch (single targeted query + stargazers).
Sleep 8s

# v0.18.0: switch the star-history sparkline to the cumulative
# growth curve — the heading reads "(12mo · cumulative)" so the
# still self-documents the feature.
Type "v"
Sleep 2s

Screenshot out/repo-detail.png

Sleep 1s

Type "q"
