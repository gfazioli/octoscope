# Smoke + asset: in-app settings panel.
# Captures the panel opened with `,` so the modal's focus border,
# row layout, and the theme cycler are visible. Feeds a future
# docs/screenshots/screenshot-settings.png if/when we wire it
# into the landing.

Output out/settings.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
Set Theme "OneHalfDark"

Type "octoscope"
Enter
Sleep 5s

# Comma opens the in-app settings panel.
Type ","
Sleep 2s

Screenshot out/settings.png

Sleep 1s

# Cancel without changing anything.
Escape
Sleep 500ms
Type "q"
