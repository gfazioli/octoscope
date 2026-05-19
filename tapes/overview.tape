# Smoke + asset: dashboard hero shot.
# Captures the Overview tab on first paint — profile card, social,
# activity, operational, network sections + languages bar.
#
# Used to regenerate docs/screenshots/screenshot.png when the
# landing page falls out of sync with the current UI.

Output out/overview.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
Set Theme "OneHalfDark"

Type "octoscope"
Enter

# Let the first GraphQL refresh complete (~2-3s on a typical
# account) so the dashboard is fully populated before we
# capture the still.
Sleep 6s

Screenshot out/overview.png

# Tail of the recording so the GIF has a moment of breathing
# before it loops.
Sleep 1s

Type "q"
