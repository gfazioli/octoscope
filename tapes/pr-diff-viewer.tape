# Asset: PR diff viewer (v0.12.0 deepest level).
# Walks through PR detail → files-changed list → unified diff
# viewer with chroma syntax highlighting. Same deterministic PR as
# pr-drill-in.tape (filter "gantt"); the diff capture descends to
# package/src/GanttChart.tsx (row 15 in the alphabetical files
# list) — a real TSX change, far more presentable than the
# one-line .nvmrc diff sitting at row 0. Captures both stills:
#   docs/screenshots/drill-in/screenshot-files-changed.png
#   docs/screenshots/drill-in/screenshot-file-diff.png
#
# CAROUSEL GEOMETRY — keep identical across the five drill-in
# tapes; see action-menu.tape for the rationale.

Output out/pr-diff-viewer.gif

Set FontSize 36
Set Width 3400
Set Height 2340
Set Padding 20
Set Theme { "name": "octoscope-black", "background": "#000000", "foreground": "#e6edf3", "cursor": "#f00050", "black": "#000000", "red": "#ff5555", "green": "#2ecc71", "yellow": "#f1fa8c", "blue": "#6272a4", "magenta": "#f00050", "cyan": "#00f0f0", "white": "#e6edf3", "brightBlack": "#555555", "brightRed": "#ff6e6e", "brightGreen": "#69ff94", "brightYellow": "#ffffa5", "brightBlue": "#d6acff", "brightMagenta": "#ff92df", "brightCyan": "#a4ffff", "brightWhite": "#ffffff" }

Type "octoscope --public-only"
Enter
Sleep 14s

Type "3"
Sleep 1s

# Filter to the deterministic public PR, open the drill-in, then
# `f` for the files list.
Type "/"
Type "gantt"
Enter
Sleep 1s
Enter
Sleep 8s

Type "f"
Sleep 2s

Screenshot out/files-changed.png
Sleep 1s

# Descend to package/src/GanttChart.tsx and open its diff.
Down 15
Sleep 1s
Enter
Sleep 3s

Screenshot out/file-diff.png
Sleep 1s

# Back out three levels: diff → files list → PR detail → PRs tab.
Escape
Sleep 500ms
Escape
Sleep 500ms
Escape
Sleep 500ms
Type "q"
