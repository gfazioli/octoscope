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
#     list of its own. Those two are the only JavaScript fetch() calls in
#     the guide — not the only external loads: every page also pulls Google
#     Fonts through <link>, three references each. Blocking the network
#     stops both, which is fine, because the fallback font is the same on
#     each side of a comparison. An earlier version of this comment named
#     live.html and settings.html as fetching, because a grep for
#     `api.github.com` matched the string inside their PROSE. The grep gave
#     the line; only its scope answered the question.
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

# This script removes and rewrites a whole directory tree, so it refuses to
# start unless that tree is where it claims to be. A symlinked .shoot-guide
# points rm -rf and every screenshot write somewhere else entirely.
#
# What this is NOT: TOCTOU-safe. Checking a path and then using it are two
# operations, and nothing here holds an open directory handle between them —
# bash has no openat(2). A process that replaces .shoot-guide with a symlink
# in the gap still wins. A review pass raised exactly that and it is correct;
# it is stated here rather than papered over, because a guard that overclaims
# is worse than one whose limits are written down. The threat model this does
# cover is the realistic one for a local development tool: a symlink or a
# stray file already sitting at that path.
if [ -L "$OUT" ]; then
  echo "refusing to run: $OUT is a symlink, and everything below would be written through it" >&2
  exit 1
fi
if [ -e "$OUT" ] && [ ! -d "$OUT" ]; then
  echo "refusing to run: $OUT exists and is not a directory" >&2
  exit 1
fi
mkdir -p "$OUT"
# And after creating it, assert the PHYSICAL path is still inside the
# repository. This does not close the race, but it does mean the window has
# to be won and then survive a check, rather than being enough on its own.
out_real="$(cd "$OUT" && pwd -P)"
repo_real="$(cd "$REPO" && pwd -P)"
case "$out_real" in
  "$repo_real"/*) ;;
  *) echo "refusing to run: $OUT resolves to $out_real, outside $repo_real" >&2; exit 1 ;;
esac

shoot() { # shoot <file-url-path> <png>
  "$CHROME" --headless=new --hide-scrollbars \
    --window-size="$WIDTH,$HEIGHT" --virtual-time-budget=4000 \
    --host-resolver-rules="MAP * ~NOTFOUND" \
    --screenshot="$2" "file://$1" >/dev/null 2>&1
}

# A screenshot harness that cannot tell two identical renders apart is not a
# harness. This proves trap 2 is actually closed on this machine.
if [ "${1:-}" = "--selftest" ]; then
  # Both pages, because they exercise different render paths: settings.html
  # only inherits docs.js's version-pill fetch, while releases.html has a
  # fetch of its own whose FAILURE path injects content. A self-test on the
  # quiet page alone cannot speak for the noisy one.
  rc=0
  for page in settings releases; do
    for i in 1 2 3; do shoot "$REPO/docs/guide/$page.html" "$OUT/selftest-$page-$i.png"; done
    if cmp -s "$OUT/selftest-$page-1.png" "$OUT/selftest-$page-2.png" &&
       cmp -s "$OUT/selftest-$page-2.png" "$OUT/selftest-$page-3.png"; then
      echo "selftest: three renders of $page.html are byte-identical"
    else
      echo "selftest: $page.html renders differ between runs; a comparison would be noise" >&2
      rc=1
    fi
  done
  # Deliberately not the word "deterministic". Three identical renders show
  # the common case is stable, which is what blocking the network buys; they
  # do not rule out the rare anti-aliasing jitter measured at roughly one
  # page in a dozen runs, which is why a CHANGED verdict is re-rendered
  # before it is believed.
  [ "$rc" -eq 0 ] && echo "selftest: stable across three renders of each page"
  exit "$rc"
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
# The ref itself is verified FIRST, because `rev-parse "$ref:docs"` fails
# identically for a ref that has no docs/ and for a ref that does not exist.
# Without this, `shoot-guide.sh ref-that-is-a-typo` printed a complete,
# plausible report — every page ADDED — and exited 0. A review pass found
# it. An empty result produced in the wrong place is the failure that looks
# exactly like an answer.
if ! git -C "$REPO" rev-parse --verify --quiet "$ref^{commit}" >/dev/null; then
  echo "refusing to run: '$ref' is not a ref in this repository" >&2
  exit 1
fi
# A ref from before docs/ existed makes `git archive` fail on the pathspec,
# which under `pipefail` used to abort the run. An empty baseline is the
# correct answer there, not an error: every current page is then ADDED.
if git -C "$REPO" rev-parse --verify --quiet "$ref:docs" >/dev/null; then
  git -C "$REPO" archive "$ref" docs | tar -x -C "$base/tree"
else
  echo "note: $ref has no docs/ — every current page counts as added"
fi
if [ -d "$base/tree/docs/guide" ]; then
  for f in "$base/tree"/docs/guide/*.html; do
    [ -e "$f" ] || continue
    shoot "$f" "$base/png/$(basename "$f" .html).png"
  done
fi

# The UNION of both sides, not just the working tree: iterating only the
# pages that exist now means a page DELETED since the ref is never mentioned,
# and the script cheerfully reports "0 pages changed" for a change that
# removed one.
# Read line by line rather than word by word: `for n in $pages` splits on
# IFS, so a page named "release notes.html" would be reported as two pages
# called `release` and `notes`. No guide file has a space today; the loop
# was wrong anyway.
#
# And each find is guarded by a -d test rather than by 2>/dev/null: under
# `pipefail` a find over a missing directory fails the whole pipeline, so
# comparing against a ref from before docs/guide/ existed aborted the script
# instead of reporting every current page as ADDED.
changed=0
total=0
list_pages() {
  [ -d "$REPO/docs/guide" ] && find "$REPO/docs/guide" -maxdepth 1 -name '*.html'
  [ -d "$base/tree/docs/guide" ] && find "$base/tree/docs/guide" -maxdepth 1 -name '*.html'
  return 0
}
echo "docs/guide, working tree vs $ref:"
while IFS= read -r n; do
  [ -n "$n" ] || continue
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
    # Re-render BOTH sides before believing it. Measured over repeated runs
    # of this script, a page occasionally differs by a handful of
    # anti-aliased pixels with nothing changed in the source — roughly one
    # run in a dozen, on a page nobody edited. A byte comparison cannot tell
    # that from a real change, so the discriminator is repetition.
    #
    # Both sides, not just the working tree: the first version of this check
    # re-rendered only the current page and compared it against the ORIGINAL
    # baseline image, so a run where the BASELINE was the jittered one still
    # reported CHANGED. Measured — the false positive survived the re-check
    # at the same rate it had before it.
    #
    # This REDUCES the false-positive rate; it does not remove it. One extra
    # pair can jitter too, and a second comparison cannot prove a negative.
    # Measured: eight consecutive runs against an unchanged tree, against a
    # false positive that used to appear within five. A CHANGED verdict on a
    # page you did not touch is still worth re-running before believing.
    recheck=""
    if [ -e "$REPO/docs/guide/$n.html" ] && [ -e "$base/tree/docs/guide/$n.html" ]; then
      shoot "$REPO/docs/guide/$n.html" "$OUT/recheck-now-$n.png"
      shoot "$base/tree/docs/guide/$n.html" "$OUT/recheck-was-$n.png"
      cmp -s "$OUT/recheck-was-$n.png" "$OUT/recheck-now-$n.png" && recheck=match
    fi
    if [ "$recheck" = match ]; then
      printf '  %-16s unchanged (first pair differed, a fresh pair matched — rendering jitter)\n' "$n"
    else
      printf '  %-16s CHANGED\n' "$n"
      changed=$((changed + 1))
    fi
  fi
done < <(list_pages | sed -e 's|.*/||' -e 's|\.html$||' | sort -u)
echo "$changed of $total pages changed"
