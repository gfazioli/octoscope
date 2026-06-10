# Smoke + asset: Issues tab list.
# Boots octoscope, switches to the Issues tab, lets the list render.
# Feeds the landing's "tabs at a glance" mini-row — keep the geometry +
# pure-black theme identical to repos/prs/activity.tape so the row stays
# visually consistent.

Output out/issues.gif

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
# First dashboard fetch is ~14s (five parallel branches + possible
# retry); wait it out so the tab renders rows, not the loader.
Sleep 14s

# Jump to Issues (tab index 4 in the 1-6 bar).
Type "4"
Sleep 2s

Screenshot out/issues.png

Sleep 1s

Type "q"
