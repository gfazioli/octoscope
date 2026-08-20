# Smoke + asset: Inbox tab (#74). Same geometry as the other tab
# stills.
#
# The extra sleep is not padding: the inbox is the one tab that loads
# on demand — nothing reaches /notifications until it is opened — so a
# 2s wait like the other tapes use captures the loader rather than the
# inbox. --public-only also drops notifications from private
# repositories, which is what makes this still safe to publish at all.

Output out/inbox.gif

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

Type "7"
Sleep 8s

Screenshot out/inbox.png

Sleep 1s

Type "q"
