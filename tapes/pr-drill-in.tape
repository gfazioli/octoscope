# Smoke + asset: PR drill-in detail (v0.11.0 surface).
# Boots octoscope, switches to the PRs tab, presses enter on the
# first row to open the drill-in. Captures the rich PR detail
# (state chip, glamour-rendered description, reviewers, checks,
# files-changed line, timeline). Feeds the
# docs/screenshots/drill-in/screenshot-pr-detail.png frame.

Output out/pr-drill-in.gif

Set FontSize 14
Set Width 1400
Set Height 900
Set Padding 10
Set Theme "OneHalfDark"

Type "octoscope"
Enter
Sleep 5s

Type "3"
Sleep 2s

# Enter on the first PR row → drill-in.
Enter
Sleep 3s

Screenshot out/pr-detail.png

Sleep 1s

# Back out: esc closes the drill-in.
Escape
Sleep 500ms
Type "q"
