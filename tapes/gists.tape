# Smoke + asset: Gists tab (#76). Boots octoscope and switches to it,
# so the guide has a picture of the tab it describes. Geometry copied
# verbatim from activity/repos/prs/issues.tape — this feeds the same
# guide row and has to stay visually consistent with it.

Output out/gists.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

# --public-only suppresses the sponsor splash (otherwise the first
# keystroke dismisses the splash instead of switching tabs) and keeps
# the still screenshot-safe.
Type "octoscope --public-only"
Enter
# First dashboard fetch is ~14s (six parallel branches + possible
# retry); wait it out so the tab renders content, not the loader.
Sleep 14s

Type "6"
Sleep 2s

Screenshot out/gists.png

Sleep 1s

Type "q"
