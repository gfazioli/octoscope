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
Set Theme "OneHalfDark"

Type "octoscope"
Enter
Sleep 5s

# Jump to Repos (tab index 2 in the 1-5 bar).
Type "2"
Sleep 2s

Screenshot out/repos.png

Sleep 1s

Type "q"
