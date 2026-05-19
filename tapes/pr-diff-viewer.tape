# Smoke + asset: PR diff viewer (v0.12.0 deepest level).
# Walks through PR detail → files-changed list → unified diff
# viewer with chroma syntax highlighting. Captures both the
# files list and the diff body. Feeds:
#   docs/screenshots/drill-in/screenshot-files-changed.png
#   docs/screenshots/drill-in/screenshot-file-diff.png

Output out/pr-diff-viewer.gif

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

# Enter the PR detail, then f for the files list.
Enter
Sleep 3s

Type "f"
Sleep 2s

Screenshot out/files-changed.png
Sleep 1s

# Enter on the first file row → chroma-highlighted diff.
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
