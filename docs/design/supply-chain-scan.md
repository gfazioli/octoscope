# Supply-chain integrity scan — design

How octoscope's integrity scan works and why it is built the way it is. The
on-demand per-repo scan shipped in **v0.20.0**; the axes and tiers still open
are tracked as issues and linked below.

Referenced from `internal/github/scan.go` and `internal/ui/model.go`.

## Threat model

A stolen token — in the reference case a `gh` CLI OAuth grant that survived a
machine rebuild because the *token* was rotated but the underlying
*authorization* was never revoked — is used over the GitHub **API**, not git,
to push an implant into the repositories the victim owns.

The implant is a file that **runs by itself** when a developer or an AI agent
opens the repository (editor session hooks, folder-open tasks) or runs the
project (`npm install` / `npm test` lifecycle scripts). On execution it
harvests every credential it can reach — GitHub PATs, npm tokens, cloud
metadata credentials — and re-pushes itself to more repositories. A worm.

Two facts make this octoscope's job rather than a package manager's:

1. The payload lives in the **GitHub source**, not the registry, so a
   lockfile or registry audit cannot see it.
2. It lands in the repositories **you own**, which is exactly octoscope's
   default scope (`ownerAffiliations: OWNER`).

## The generalization principle

The reference indicator — a 4.3 MB obfuscated `.github/setup.js` committed as
a forged unsigned `github-actions` bot — is a *single instance* of a class.
Matching that filename would catch yesterday's worm and nothing else.

The invariant an attacker **cannot** drop without breaking the attack is: *a
file that auto-executes on repo-open or build, carrying a payload that doesn't
look like the project, arriving through a commit whose provenance is
anomalous.*

So the engine matches that invariant across four filename-agnostic axes, and
treats specific names as a data-driven seed list that is cheap to extend.

## Axis 1 — auto-execution surface inventory

The attack only works because some file runs without the developer choosing to
run it. That surface is finite and enumerable, maintained as a catalog of path
globs grouped by trigger class, so adding a future location is a one-line edit
rather than new logic:

- **AI agent / editor session hooks** — `.claude/settings.json`
  (`hooks.SessionStart`, `PreToolUse`, …), `.gemini/settings.json`,
  `.cursor/rules/*.mdc` (`alwaysApply: true`), `.continue/**`, `.aider*`,
  `.windsurfrules`, MCP server configs (`.mcp.json`, `.vscode/mcp.json`)
- **Editor auto-run** — `.vscode/tasks.json` (`runOptions.runOn: folderOpen`),
  `.vscode/settings.json`, `.vscode/launch.json`,
  `.devcontainer/devcontainer.json` (`postCreateCommand` / `onCreateCommand` /
  `postStartCommand`), `.idea/**` run configurations
- **Package lifecycle hooks** — `package.json` scripts (`preinstall`,
  `install`, `postinstall`, `prepare`, `prepublishOnly`, `test`),
  `pyproject.toml` and `setup.py`, `Cargo.toml` and `build.rs`,
  `composer.json` scripts, the `Makefile` default target, Gradle init scripts
- **VCS hooks** — committed `.husky/**`, a `core.hooksPath` in a tracked
  `.gitconfig`, `.gitattributes` clean/smudge filters
- **CI** — `.github/workflows/**`; see Axis 4 for the dangerous
  permission/trigger combinations

Presence alone is **low signal**: legitimate repositories have
`.vscode/tasks.json` and `postinstall`. It only matters in combination.

One lesson from running the scan against real repositories: AI-agent
*instruction* files (`copilot-instructions.md`, `.cursor/rules`,
`.windsurfrules`, `AGENTS.md`) are weight-0 inventory. Only code-executing
hooks carry weight.

## Axis 2 — blob anomaly

Whatever it is called, a payload has physical tells, all readable cheaply
through GraphQL `Blob` fields without pulling the whole file
(`object(expression: "<ref>:<path>") { ... on Blob { byteSize isBinary text } }`):

- **Abnormal size for the type** — a multi-megabyte `.js`, `.json` or dotfile
  config (`byteSize`, free)
- **High entropy, minification or encoding** — Shannon entropy over a sampled
  prefix, very long lines, low whitespace ratio, long base64 or hex runs
- **Obfuscation markers** — `eval(`, `new Function(`, `atob(`,
  `Buffer.from(…, 'base64')`, dense `\x` / `\u` escapes, a ROT/Caesar
  bootstrap, or an explicit reference to an alternate runtime (`bun`) used to
  dodge Node-based monitors — all seen in the reference payload
- **Indirection** — an ignition file whose command points at another file that
  itself trips this axis

This is the axis that catches **future** variants: rename the dropper all you
like, an oversized obfuscated blob wired into an ignition point is
intrinsically suspect.

## Axis 3 — provenance anomaly

The sharpest lesson of the reference case: a commit forged under the
maintainer's own name defeats author-based detection. Detect by signature and
shape instead.

- **Unsigned tip against a signed history** — `signature { isValid state }` on
  the branch-tip commit; flag `UNSIGNED` / `INVALID` when recent history is
  normally `VALID`. A signature-state *delta*, not an absolute.
- **Spoofed identity** — a `committer` or `author` of `github-actions` on an
  account that never uses Actions, or the maintainer's own name, paired with
  `signature.state != VALID`
- **Backdated tip** — a branch tip whose `committedDate` is far older than its
  siblings or than the branch's prior tip; the reference worm backdated
  stealth commits on side branches such as `next`
- **Side-branch divergence** — a non-default branch whose tip introduces
  Axis-1/2 findings the default branch does not have
- **Push burst** — many owned repositories with `PushedAt` clustered in a
  tight window; the reference attack hit five repositories in 49 seconds from
  one IP. Free and client-side, zero extra API cost.

The unsigned-delta baseline counts only genuine author signatures
(`Signed && !SignedByGitHub`) — a GitHub-signed `main` next to an unsigned
feature branch is normal and must not score.

## Axis 4 — capability escalation

Not implemented yet — tracked as
[#67](https://github.com/gfazioli/octoscope/issues/67).

- **Workflow permissions and triggers** — `.github/workflows/**` requesting
  `contents: write` or `id-token: write`, using `pull_request_target`, or
  exposing secrets in a fork-triggered context
- **Self-hosted runners** — `GET /repos/{owner}/{repo}/actions/runners`
  (needs admin scope; best-effort, skipped silently on 403)
- **New deploy keys and webhooks** — `GET /repos/{owner}/{repo}/keys` and
  `/hooks`, especially write keys or hooks pointing off-platform

## The engine — weighted, explainable, signal not verdict

Each matched rule contributes a **weight**; the per-repo total maps to a
verdict tier (`clean` · `watch` · `suspicious` · `likely-compromised`). Two
non-negotiables:

1. **Every contribution is shown with its reason.** Not "INFECTED" but
   "oversized obfuscated blob at `.github/setup.js` (4.3 MB) on branch `next`
   · tip commit unsigned, committer `github-actions` · 5 repos pushed within
   49 s". The user audits the *evidence*, not a black-box score.
2. **One axis is never enough.** A lone `.vscode/tasks.json` scores near zero.
   The high tiers require the combination — ignition point **plus** blob
   anomaly **plus** provenance anomaly — which is what keeps the
   false-positive rate survivable on real accounts full of legitimate
   postinstall scripts and editor configs.

## Baseline / delta

Not implemented yet — tracked as
[#68](https://github.com/gfazioli/octoscope/issues/68).

Persist a per-repo fingerprint — the set of ignition-path blob OIDs plus the
HEAD signature state — then flag **deltas** on a later scan: "a new
`.vscode/tasks.json` appeared since the last scan", "HEAD on `main` went
signed → unsigned".

Delta detection is the most future-proof axis of all: it is both name-agnostic
*and* content-agnostic. It just notices that something which auto-executes
changed.

## Architecture — tiers inside the complexity ceiling

GitHub's GraphQL gateway enforces an undocumented per-request complexity
budget, and per-item fan-out across many repositories reliably exceeds it. The
scan is tiered accordingly.

- **Tier A — free, on the existing dashboard fetch.** An always-on push-burst
  banner was built and then **dropped**: timing alone cannot separate a worm
  fan-out from an ordinary batch push (a scripted push to 17 repositories in
  two minutes fired it), and without a recency gate any historical batch
  re-alarmed forever. `DetectPushBurst` is kept, tested and unwired, to fold
  into the scan later — recency-gated and combined with the other axes
  ([#69](https://github.com/gfazioli/octoscope/issues/69)).
- **Tier B — on-demand per-repo scan. Shipped in v0.20.0.** An action on Repos
  rows (`s`). One targeted query per selected repository: enumerate branches
  (`refs(refPrefix: "refs/heads/", first: 100)`), then aliased
  `object(expression: "refs/heads/<branch>:<path>")` probes over the Axis-1
  catalog returning `byteSize` / `isBinary` / sampled `text` (Axes 1 and 2),
  plus each branch tip's `signature` / `committer` / `committedDate`
  (Axis 3). Renders the explainable report. This is the endorsed drill-in
  pattern: one query per *selected* item.
- **Tier C — bounded account-wide sweep.** Not implemented yet, tracked as
  [#66](https://github.com/gfazioli/octoscope/issues/66). A dedicated mode,
  semaphore-capped like the watched-repo fan-out, probing the Axis-1 catalog
  on the **default branch only** per owned repository. Deep all-branch
  scanning stays on-demand. Kept out of the always-on fetch so a normal
  refresh never pays for it.

All attacker-controlled strings — branch names, commit messages, file paths,
sampled blob text — pass through `github.Sanitize` at the extractor boundary.
Non-negotiable here, since the whole point is rendering content from a
*potentially hostile* repository. Severity colours honour `IsMonochromatic()`.

## Fix surface — actionable, still read-only

octoscope flags and **guides**; it never mutates GitHub state. A flagged
repository's report offers:

- **A copy-paste remediation script** (`y` copies it) with the safe steps:
  `git clone --no-checkout` to inspect without executing, a branch scan,
  **reset rather than `revert`** plus a force-push of the clean parent (a
  `revert` leaves the payload retrievable at the old commit), and the
  GitHub-Support garbage-collection request.
- **Deep links** to the right pages. The central lesson is *revoke the
  authorization grant, not just the token*:
  <https://github.com/settings/applications> for OAuth grants,
  <https://github.com/settings/tokens> for PATs, and the repository's
  branch-protection / required-signed-commits settings.

## Honest gaps

- **OAuth grant enumeration is not available.** The legacy OAuth
  Authorizations API is gone, so octoscope cannot *list* a user's grants — the
  central remediation step is link-out only.
- **Self-hosted runners, deploy keys and webhooks need elevated scope**, so
  they are best-effort and skipped silently on 403.
- **The Axis-1 catalog is a moving target** by nature. It ships as a
  maintained data table, and the scan leans on Axes 2–4 — which do not depend
  on the catalog being exhaustive — for variants using an ignition point
  nobody has catalogued yet.

## Seed indicators — data, not logic

From the 2026-06 reference case: `.github/setup.js` (4.3 MB obfuscated
dropper) · `.claude/settings.json` (SessionStart hook) ·
`.gemini/settings.json` · `.cursor/rules/setup.mdc` (`alwaysApply`) ·
`.vscode/tasks.json` (`runOn: folderOpen`) · `package.json` `test` pointing at
the payload · forged unsigned `github-actions` commits with the message
`chore: update dependencies [skip ci]` · backdated maintainer-named commits on
`next` branches.

These are **seed rows** in the Axis-1/2/3 tables, weighted high — never the
detection logic itself.

## Validation

Tier B was validated against real repositories, including the (since cleaned)
victims of the reference worm and unaffected controls; all scored *clean* after
remediation. Origin of the threat research: the icflorescu dev.to series on the
Miasma worm, 2026-06.
