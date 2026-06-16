# Asset: Repos drill-in supply-chain integrity scan (v0.20.0).
# Boots octoscope, filters the Repos list down to "octoscope", opens the
# action menu (space) and runs the Security scan (s), capturing the
# read-only report — verdict, auto-execution surface inventory and
# per-branch commit-tip provenance.
#
# Regenerates docs/screenshots/drill-in/screenshot-scan.png — a
# standalone framed shot in the "Drill in" section of the landing (not
# a carousel slide).
#
# SHARED DRILL-IN GEOMETRY — keep identical across the drill-in tapes so
# the still sits at the same scale as the carousel slides; see
# action-menu.tape / repo-detail.tape for the rationale. The capture is a
# real terminal: header at the top, single-line footer anchored at the
# bottom, octoscope's own spacing in between.

Output out/scan.gif

Set FontSize 36
Set Width 3400
Set Height 2340
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

Type "octoscope --public-only"
Enter

# First dashboard fetch (five parallel branches + possible transient
# retry — same headroom as overview.tape / repo-detail.tape).
Sleep 14s

# Repos tab, filter the list down to the octoscope row.
Type "2"
Sleep 1s
Type "/"
Type "octoscope"
Enter
Sleep 1s

# Action menu (space) -> Security scan (s).
Space
Sleep 1500ms
Type "s"

# Scan fetch: one GraphQL refs query + a bounded REST tree/blob fan-out.
Sleep 12s

Screenshot out/scan.png

Sleep 1s

Type "q"
