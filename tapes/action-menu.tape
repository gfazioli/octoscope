# Smoke + asset: action menu on a list row.
# Goes to the Repos tab, lands on the first row, opens the
# action menu with space. Captures the modal centred over the
# table. Feeds docs/screenshots/drill-in/screenshot-action-menu.png.

Output out/action-menu.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
Set Theme "OneHalfDark"

Type "octoscope"
Enter
Sleep 5s

Type "2"
Sleep 2s

# Space opens the action menu modal (Open in GitHub / View details
# / Pin / Copy URL on Repos).
Space
Sleep 2s

Screenshot out/action-menu.png

Sleep 1s

# Close the menu (esc) and quit cleanly.
Escape
Sleep 1s
Type "q"
