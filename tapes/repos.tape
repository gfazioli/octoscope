# Smoke + asset: Repos tab list.
# Boots octoscope, switches to the Repos tab, lets the list render
# with CI dots, pinned section, and the language column. Output is
# the recurring screenshot we use on the landing under the
# "tabs at a glance" mini-row.

Output out/repos.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
# Pure-black theme to match the hero + carousel stills (was OneHalfDark,
# which rendered a grey background that stood out on the landing).
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

# --public-only suppresses the sponsor splash (otherwise the first
# keystroke below dismisses the splash instead of switching tabs) and
# keeps private repositories out of the still.
Type "octoscope --public-only"
Enter
# First dashboard fetch is ~14s (five parallel branches + possible
# retry); wait it out so the Repos tab renders rows, not the loader.
Sleep 14s

# Jump to Repos (tab index 2 in the 1-5 bar).
Type "2"
Sleep 2s

Screenshot out/repos.png

Sleep 1s

Type "q"
