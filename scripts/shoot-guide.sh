#!/usr/bin/env bash
# Deterministic screenshots of docs/guide/, and a page-by-page diff against a
# git ref. Written because verifying a one-line CSS change took four attempts,
# and three of the four failures looked like answers.
#
#   scripts/shoot-guide.sh                 # diff the working tree against origin/main
#   scripts/shoot-guide.sh HEAD            # ...against another ref
#   scripts/shoot-guide.sh --shots-only    # just render, no comparison
#
# Output goes under .shoot-guide/ (gitignored).
#
# THE THREE TRAPS. Each one produces a confident wrong answer, so they are
# worth more than the script:
#
#  1. --screenshot captures the VIEWPORT, not the page. A window shorter than
#     the page silently compares only the part above the fold; a paragraph
#     edited lower down reads as "unchanged". Hence --window-size height 9000,
#     comfortably past the tallest guide page.
#
#  2. docs.js fetches https://api.github.com/.../releases/latest on EVERY page
#     to fill the sidebar version pill, and releases.html, live.html and
#     settings.html fetch more. So two renders of the same file differ
#     depending on network timing — intermittently, which is what makes it
#     fool you. --host-resolver-rules pins every host to nowhere, and the
#     render becomes reproducible. Verify with --selftest.
#
#  3. Rendering the "before" copy from a temp directory changes how its
#     relative assets resolve, so every page comes back "changed". The old
#     revision is therefore extracted with `git archive`, which preserves the
#     tree shape, and rendered from the same relative position.
#
# And a fourth, about counting rather than rendering: a regex that reads
# `<div class="note">(.*?)</div>` stops at the first NESTED </div>, and a note
# injected by JavaScript is not in the markup at all. releases.html's
# multi-paragraph note is exactly that case — it exists only in its fetch
# failure path, which is why blocking the network is what makes it visible.
set -euo pipefail

CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$REPO/.shoot-guide"
WIDTH=1100
HEIGHT=9000

shoot() { # shoot <file-url-path> <png>
  "$CHROME" --headless=new --hide-scrollbars \
    --window-size="$WIDTH,$HEIGHT" --virtual-time-budget=4000 \
    --host-resolver-rules="MAP * ~NOTFOUND" \
    --screenshot="$2" "file://$1" >/dev/null 2>&1
}

# A screenshot harness that cannot tell two identical renders apart is not a
# harness. This proves trap 2 is actually closed on this machine.
if [ "${1:-}" = "--selftest" ]; then
  mkdir -p "$OUT"
  page="$REPO/docs/guide/settings.html"
  for i in 1 2 3; do shoot "$page" "$OUT/selftest-$i.png"; done
  if cmp -s "$OUT/selftest-1.png" "$OUT/selftest-2.png" &&
     cmp -s "$OUT/selftest-2.png" "$OUT/selftest-3.png"; then
    echo "selftest: three renders of settings.html are byte-identical — deterministic"
    exit 0
  fi
  echo "selftest: renders differ between runs; the comparison below would be noise" >&2
  exit 1
fi

shots_only=false
[ "${1:-}" = "--shots-only" ] && { shots_only=true; shift; }
ref="${1:-origin/main}"

mkdir -p "$OUT/now"
for f in "$REPO"/docs/guide/*.html; do
  shoot "$f" "$OUT/now/$(basename "$f" .html).png"
done
$shots_only && { echo "rendered $(ls "$OUT/now" | wc -l | tr -d ' ') pages into $OUT/now"; exit 0; }

base="$OUT/base"
rm -rf "$base" 2>/dev/null || true
mkdir -p "$base/tree" "$base/png"
git -C "$REPO" archive "$ref" docs | tar -x -C "$base/tree"
for f in "$base/tree"/docs/guide/*.html; do
  shoot "$f" "$base/png/$(basename "$f" .html).png"
done

changed=0
echo "docs/guide, working tree vs $ref:"
for f in "$REPO"/docs/guide/*.html; do
  n="$(basename "$f" .html)"
  if cmp -s "$base/png/$n.png" "$OUT/now/$n.png"; then
    printf '  %-16s unchanged\n' "$n"
  else
    printf '  %-16s CHANGED\n' "$n"
    changed=$((changed + 1))
  fi
done
echo "$changed of $(ls "$REPO"/docs/guide/*.html | wc -l | tr -d ' ') pages changed"
