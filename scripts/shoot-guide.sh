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
#     to fill the sidebar version pill, and releases.html fetches the release
#     list of its own. Those two are the only fetches in the guide — an
#     earlier version of this comment also named live.html and settings.html,
#     because a grep for `api.github.com` matched the string inside their
#     PROSE. The grep gave the line; only its scope answered the question.
#     So two renders of the same file differ depending on network timing —
#     intermittently, which is what makes it fool you. --host-resolver-rules
#     pins every host to nowhere and the render becomes reproducible. Verify
#     with --selftest.
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

# This script removes and rewrites a whole directory tree, so it checks that
# the directory is the one it thinks it is before touching anything. A
# symlinked .shoot-guide would point rm -rf and every screenshot write
# somewhere else entirely, and the rule against arming a destructive
# operation on a path you have not confirmed is exactly what this is.
if [ -L "$OUT" ]; then
  echo "refusing to run: $OUT is a symlink, and everything below would be written through it" >&2
  exit 1
fi
if [ -e "$OUT" ] && [ ! -d "$OUT" ]; then
  echo "refusing to run: $OUT exists and is not a directory" >&2
  exit 1
fi

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

# Cleared, not merely created. A page deleted since the last run leaves its
# PNG behind, and the comparison below then finds two identical stale images
# and calls the page unchanged — which is the very failure the union loop
# exists to catch, reintroduced one directory over. Measured: the first
# version of that loop reported a deliberately deleted page as "unchanged".
if [ -e "$OUT/now" ]; then
  [ -L "$OUT/now" ] && { echo "refusing to run: $OUT/now is a symlink" >&2; exit 1; }
  rm -rf "$OUT/now"
fi
mkdir -p "$OUT/now"
for f in "$REPO"/docs/guide/*.html; do
  shoot "$f" "$OUT/now/$(basename "$f" .html).png"
done
$shots_only && { echo "rendered $(find "$OUT/now" -name '*.png' | wc -l | tr -d ' ') pages into $OUT/now"; exit 0; }

base="$OUT/base"
# NOT `|| true`: a cleanup that half-failed leaves files from the previous
# baseline, and `git archive | tar` then OVERLAYS the requested ref onto
# them — so a page that does not exist at that ref gets rendered and
# compared as though it did.
if [ -e "$base" ]; then
  [ -L "$base" ] && { echo "refusing to run: $base is a symlink" >&2; exit 1; }
  rm -rf "$base"
fi
mkdir -p "$base/tree" "$base/png"
git -C "$REPO" archive "$ref" docs | tar -x -C "$base/tree"
for f in "$base/tree"/docs/guide/*.html; do
  shoot "$f" "$base/png/$(basename "$f" .html).png"
done

# The UNION of both sides, not just the working tree: iterating only the
# pages that exist now means a page DELETED since the ref is never mentioned,
# and the script cheerfully reports "0 pages changed" for a change that
# removed one.
changed=0
total=0
pages="$( { find "$REPO/docs/guide" -maxdepth 1 -name '*.html' 2>/dev/null
              find "$base/tree/docs/guide" -maxdepth 1 -name '*.html' 2>/dev/null; } |
            sed -e 's|.*/||' -e 's|\.html$||' | sort -u )"
echo "docs/guide, working tree vs $ref:"
for n in $pages; do
  total=$((total + 1))
  now="$OUT/now/$n.png"
  was="$base/png/$n.png"
  if [ ! -e "$now" ]; then
    printf '  %-16s REMOVED since %s\n' "$n" "$ref"
    changed=$((changed + 1))
  elif [ ! -e "$was" ]; then
    printf '  %-16s ADDED since %s\n' "$n" "$ref"
    changed=$((changed + 1))
  elif cmp -s "$was" "$now"; then
    printf '  %-16s unchanged\n' "$n"
  else
    printf '  %-16s CHANGED\n' "$n"
    changed=$((changed + 1))
  fi
done
echo "$changed of $total pages changed"
