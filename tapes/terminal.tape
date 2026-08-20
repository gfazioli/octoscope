# Asset: octoscope in the terminal theme (landscape, for the hero carousel).
# Added 2026-08-20: docs/themes/theme-terminal.png is referenced by the
# landing but had no tape, so it could not be regenerated and went stale
# every time the UI changed. Geometry copied verbatim from phosphor.tape —
# the theme stills share the carousel's contract.
Output out/terminal.gif
Set FontSize 32
Set Width 3400
Set Height 2140
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }
Type "octoscope --theme terminal --public-only"
Enter
Sleep 14s
Screenshot out/theme-terminal.png
Sleep 1s
Type "q"
