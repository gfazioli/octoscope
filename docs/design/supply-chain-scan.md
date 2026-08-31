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
  one IP. Free and client-side, zero extra API cost. Reported under its own
  `push-burst` label rather than as provenance evidence, because it is
  account-wide rather than a fact about this repo's commits — see Tier A
  below for the recency gate and the corroboration-only weighting.

The unsigned-delta baseline counts only genuine author signatures
(`Signed && !SignedByGitHub`) — a GitHub-signed `main` next to an unsigned
feature branch is normal and must not score.

## Axis 4 — capability escalation

**Shipped in 0.27.0** ([#67](https://github.com/gfazioli/octoscope/issues/67)).

The persistence and exfiltration footprint: what a compromise of this
repository would be able to reach.

**Capability alone is never scored, and that is the design.** octoscope's own
`release.yml` requests `contents: write` and reads two secrets, and it is
entirely correct — it fires on a tag push, so only someone who can already push
tags can reach it. Scoring power by itself would flag a large share of GitHub
and teach everyone to ignore the axis. What scores is power reachable from
**untrusted input**.

- **Workflow permissions and triggers** — parsed from `.github/workflows/**`,
  which the scan already fetches, so this half costs no extra API call. The
  outsider-triggerable events are `pull_request_target`, `workflow_run`,
  `issue_comment` and `issues`: each runs with the base repository's token and
  secrets while acting on input an outsider controls. `pull_request` is
  deliberately not one of them — a fork PR there gets a read-only token and no
  secrets — **on a public repository**, which is the qualifier the code carries
  too, since whether the private-repository fork policies can lift it is the open
  question in #114 rather than a settled fact.

  The test is **who can cause the run**, not whether a fork is involved, which is
  why `issues` is on the list ([#111](https://github.com/gfazioli/octoscope/issues/111)):
  on a public repository anyone can open one, the title and body are theirs, and
  `issue_comment` was already listed on exactly that reasoning — opening an issue
  cannot be less untrusted than commenting on one.

  **Known limitation — configuration-dependent events** ([#114](https://github.com/gfazioli/octoscope/issues/114)).
  `discussion`, `discussion_comment`, `fork` and `watch` are publicly triggerable
  only where the corresponding repository feature is enabled, and the scan has no
  repository-configuration input. Adding them unconditionally would score
  workflows an outsider cannot actually reach, which on this axis is the expensive
  direction: a wrong positive is what teaches everyone to ignore it. The same
  issue carries the open question of whether `pull_request` remains a safe
  exclusion under the fork policies available to private repositories and
  organisations — a claim about GitHub's behaviour that contradicts the assumption
  above and is unverified.
  - outsider trigger **+** secrets or write scopes → scores `wCapEscalation`
  - elevated scopes on a trusted trigger → inventory, weight 0
  - a bare outsider trigger with neither → inventory, weight 0
  - `permissions: write-all` → `wCapWriteAll` on any trigger, because it hands
    over every scope rather than the one needed
  - a workflow that will not parse is reported as **not understood**, never as
    checked-and-clean

  Parsing uses a real YAML parser rather than line matching: flow style
  (`permissions: {contents: write}`) and anchors are valid YAML and trivially
  defeat a scanner that reads lines. On the YAML 1.1 `on`-as-boolean trap:
  measured against `yaml.v3`, decoding into `map[string]any` keeps the key as
  the string `"on"`, and only a typed target such as `map[bool]any` resolves it
  to `true`. The parser looks up both spellings as cheap insurance against a
  change of decode target, not because the boolean form occurs today.

  **Secrets are detected in two halves**, and the split follows from the rule
  above ([#110](https://github.com/gfazioli/octoscope/issues/110), which existed
  because that rule was stated here and not applied to this one field):

  - *Structurally*, for `secrets: inherit` on a reusable-workflow call — it is a
    mapping, so it is read from the decoded job. Read as text it was
    spelling-dependent: `"secrets": inherit`, `'secrets': inherit` and
    `secrets:    inherit` decode to exactly what Actions consumes while
    containing no `secrets: inherit` substring.
  - *By expression*, for references. A reference can sit in a script body, an
    `env:` value or a `with:` input — opaque to YAML, but still **scalars**, so
    the decoded document is walked rather than the bytes. That is what keeps a
    commented-out `# ${{ secrets.TOKEN }}` from counting: YAML discards comments,
    and a workflow whose only mention is disabled reaches nothing.

    Inside a scalar, each `${{ … }}` expression is read with **quote state**,
    which matters in both directions. A literal's contents are data, so
    `contains(msg, 'secrets')` — reacting to the word — does not score. And a
    `}}` *inside* a literal does not end the expression: `format()` escapes
    braces by doubling them, so `format('{{Hello {0}!}}', secrets.TOKEN)` would
    otherwise be cut before the reference.

    What is matched is the context at the **root of a reference**, so
    `secrets.NAME`, `secrets['NAME']`, `toJSON(secrets)` and a bare `secrets`
    all count, while `vars.secrets` — a configuration variable that happens to
    be called secrets — does not, and neither does `mysecrets`.

- **Self-hosted runners** — `GET /repos/{owner}/{repo}/actions/runners`.
  Inventory on their own; they score only when the repository *also* has an
  outsider-triggered workflow, because that combination is outsider-supplied code
  executing on hardware you own.
- **Deploy keys and webhooks** — `GET /repos/{owner}/{repo}/keys` and `/hooks`.
  Write keys and active off-platform hook targets are reported as inventory:
  both are ordinary in healthy repositories, and what would make one suspicious
  is its *appearance*, which the delta axis is the right place to catch.

**Reusable-workflow chains are composed before scoring** ([#106](https://github.com/gfazioli/octoscope/issues/106), `internal/github/chain.go`).
A parser reads one file, which is the right shape for a parser and the wrong
shape for this axis's question. Read separately, a `pull_request_target` caller
that holds nothing of its own is not a finding, and a callee that reads a secret
is not either, because on its own `workflow_call` is not untrusted input.
Together they are a fork-triggered path to a repository secret.

Two properties travel in **opposite directions** along a chain, and getting them
the wrong way round is how a composition becomes a false-positive engine:

- **Reachability accumulates.** Whatever can start the caller can reach
  everything it calls, transitively. The callee's finding names the trigger *and*
  the caller that carried it in, so a reader is never told a `workflow_call` file
  is fork-triggered without being shown how.
- **Power is bounded by the giver.** GitHub's reference: what a callee receives
  *"can be only downgraded (not elevated)"*, and *"if
  `jobs.<job_id>.permissions` is not specified in the calling job, the called
  workflow will have the default permissions for the `GITHUB_TOKEN`"*. So a
  callee declaring `contents: write` whose caller hands over nothing holds
  nothing, and a `${{ secrets.X }}` reference resolves to nothing until a caller
  passes secrets — by `inherit`, or by name. The `permissions: {}` case is the
  one a per-file reading gets wrong in the dangerous direction: it grants nothing
  elevated, yet it *is* a declaration, so the repository default never applies.

Three consequences worth naming, because each removes a claim the pre-#106
report made and could not support:

- A callee-only file **nobody in the tree calls** now says so, rather than
  reading its own silence on permissions as "runs with the repository default" —
  a claim its caller actually makes.
- A callee's inventory line says "reachable only through the workflows that call
  it", not "reachable only from its own triggers", which a file with no triggers
  of its own cannot be.
- A call the scan **cannot** resolve — another repository, or this one addressed
  by its full name and a ref — is disclosed as an unfollowed chain, **always,
  and never gated on reachability**. Gating a *score* on whether an outsider can
  reach the caller is right; gating the *boundary marker* on it is the collapse
  this axis exists to prevent, because a cross-repository call on an unreachable
  chain then renders identically to a chain that terminated safely, and silence
  is the one reading it must never support. Cross-repository resolution stays
  out of scope; saying so out loud is what keeps the obscure spelling from
  evading composition unnoticed.

  The disclosure is **one line per branch, naming the destination
  repositories** rather than one row per caller. The caller is not rendered —
  the findings list shows weight, axis, reason and branch — so per-caller rows
  differing only in who hands off print identically. And measured against real
  repositories, listing every target does not scale: one hands fifteen workflows
  to a single shared repo, which rendered as ~1200 characters of near-identical
  paths. The actionable fact is the repository to go and audit.

Composition runs **per branch and is then unioned by path**: a side branch can
wire the same files together differently, which is exactly the divergence this
scan exists to catch, so flattening before composing would hide it.

A cycle (`A` calls `B` calls `A`) is not valid Actions, but a scan reads whatever
is in the tree — so propagation is a **fixpoint** rather than a recursion, and
terminates by construction instead of depending on a visited set threaded through
correctly.

**This makes the axis ceiling carry more weight, not less.** One attack path can
now emit an escalation finding per file in the chain — three files is 9 against a
threshold of 5. Measured with the clamp disabled, the three-file chain test
reaches **score 10 and "likely compromised" from capability alone**, against 7
for the single-workflow shape that [#109](https://github.com/gfazioli/octoscope/pull/109)
was opened for. The clamp bounds the arithmetic and not the disclosure: every
file in the chain still appears in the report.

- **Default workflow permissions** — `GET /repos/{owner}/{repo}/actions/permissions/workflow`
  ([#107](https://github.com/gfazioli/octoscope/issues/107)). A workflow that
  declares no `permissions:` block runs with the repository's default, which an
  owner or organisation can widen to read/write — so the file can hold write
  access it never mentions, and the parser alone cannot see it. The probe supplies
  the missing half, and the two are joined in the scoring engine, where an
  inherited write is then treated exactly as a declared one: the power is the
  same, only its spelling differs.

  Two details carry the weight here.

  *Declaring anything overrides the default*, so what matters is whether a block
  exists, not whether it grants anything — `permissions: {contents: read}` grants
  nothing elevated and still overrides. That is a distinction the write-grant
  extraction cannot make on its own, since both cases leave it empty, so the
  parser reports it separately. Per-job blocks count the same way, and one
  inheriting job is enough.

  *An unknown default resolves to neither value.* Measured: this endpoint answers
  403 on a repository the token does not own, so it works for the owner-affiliated
  repositories that are the scan's default scope and fails open elsewhere. Where
  it fails, the report says so — and stops saying the workflow "holds no secrets
  or write scopes", because that claim is exactly what the unread setting would
  have decided. The gap is declared **only where some workflow actually inherits
  it**: naming an unreadable setting on a repository whose workflows all declare
  their own permissions is noise about a fact that changes nothing, and a report
  has to stay worth reading to be read.

**The invariant.** No single axis may reach a high tier alone. Bounding each finding is not
enough — a review caught one outsider-triggered secret-bearing workflow (3) plus a
reachable self-hosted runner (3) summing to 6 with no second axis agreeing — so
the axis carries an aggregate ceiling (`maxCapabilityScore`, one below
`tSuspicious`). Findings past the ceiling are still reported, at weight 0: the
arithmetic is clamped, not the disclosure.

These calls need elevated scope, so a 403 must not fail the scan. But *not
failing* is different from *not saying*: *a security report that hides its own
blind spots is worse than one that admits them.* A 403 is not an error worth
interrupting the user for, and it **is** coverage the report has to declare —
"deploy keys not checked: token lacks admin scope" — so nobody reads a clean
verdict as a complete one.

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

**Shipped in 0.27.0** ([#68](https://github.com/gfazioli/octoscope/issues/68)).

Every scan records a per-repo fingerprint — the blob OID of each *scoring*
ignition path, per branch, plus whether each branch tip carried a genuine
author signature — and diffs the next scan against it.

Delta detection is the most future-proof axis of all: it is both name-agnostic
*and* content-agnostic. It just notices that something which auto-executes
changed.

**What scores.** A scoring ignition path that appeared on a branch the baseline
already knew (`wDeltaNewIgnition`), a known one whose content changed
(`wDeltaChangedIgnition`), and a branch tip that used to be signed and no longer
is (`wDeltaSignedRegressed`). Gaining a signature is an improvement and says
nothing.

**What does not.** Weight-0 surfaces are never fingerprinted: `package.json` and
editor task files change constantly for ordinary reasons, and diffing them would
bury real signal under the maintainer's own commits. Nor is a path on a branch
the baseline never saw — that is new work, not an appearance, or every feature
branch would alarm.

**The three questions the issue left open, and how they were answered:**

- **Where it lives.** A sibling JSON file, `scan-baselines.json`, next to
  whichever `config.toml` is in use (so `--config` keeps its state together).
  Deliberately *not* in the TOML: that file is hand-edited and carefully
  round-tripped, and filling it with blob OIDs would wreck it. A malformed
  store degrades to "no baseline" rather than failing the scan — it is
  machine-written, so refusing to scan over a corrupt cache would trade a
  security tool for a bookkeeping problem.
- **First run.** Reported explicitly, at weight 0: *"no previous scan of this
  repository to compare against"*. Silence would be indistinguishable from
  "nothing changed", which is the one reading this axis must never support.
- **The comparison window is stated once, on the scan** (`RepoScan.BaselineWindow`),
  not repeated on each finding. The scan is on demand rather than a cron, so the
  store records when somebody was *looking*, not what happened over time — a delta
  measured over 4 minutes and one measured over 29 days are different claims and
  used to render identically inside the freshness window. One baseline and one
  `Now` give exactly one span, so suffixing it onto every Reason denormalised a
  scalar into prose; it also meant the span could only appear when a finding
  existed, leaving the case the issue opens with — a repository where *nothing
  changed* — as silent as before. Always phrased as a gap, never as "unchanged
  for N days", which would assert a continuous watch the tool does not keep. The
  span is **rounded, never truncated**: truncation can only understate, and a
  narrower stated window claims a tighter bracket around when the change happened
  than the measurement supports. The non-scoring branches keep an inline clause,
  because there it is not a span but the reason that finding carries no weight.
- **A capture time is only used when it is plausibly a measurement.** The
  store is user-editable JSON and a malformed one is swallowed rather than
  failing the scan, so a zero, future or absurd timestamp is reported as an
  unknown gap instead of rendered. Without that, `time.Time.Sub` saturating
  at ±292 years — and the negation of `math.MinInt64` being a no-op — let a
  corrupted entry score at full weight while claiming the tightest window
  the report can express.
- **Staleness.** Past `baselineMaxAge` (30 days) the deltas are still listed,
  with the gap stated and the window named, but stop scoring. A months-old fingerprint diffs into a
  long list of legitimate changes, and scoring that would train the user to
  ignore the axis — worse than saying nothing.

Two further consequences worth stating. The store is keyed by `owner/name`, so a
**rename** reads as a first run: continuity is lost, but no delta is ever
invented, which is the right way round. And the fingerprint records the
**verdict at capture time**, so a baseline taken while the repo was already
flagged makes the report say so — "no change" on top of a compromised baseline
means nothing has improved, not that all is well.

## Architecture — tiers inside the complexity ceiling

GitHub's GraphQL gateway enforces an undocumented per-request complexity
budget, and per-item fan-out across many repositories reliably exceeds it. The
scan is tiered accordingly.

- **Tier A — free, on the existing dashboard fetch.** An always-on push-burst
  banner was built and then **dropped**: timing alone cannot separate a worm
  fan-out from an ordinary batch push (a scripted push to 17 repositories in
  two minutes fired it), and without a recency gate any historical batch
  re-alarmed forever. Both objections are answered by folding the signal into
  the on-demand scan instead of letting it stand alone, which is what
  [#69](https://github.com/gfazioli/octoscope/issues/69) did:
  `DetectPushBurst` now runs inside `FetchRepoScan`, gated on
  `pushBurstRecency` (one hour), and contributes `wPushBurstCorroboration`
  **only to a repo that already scored on Axes 1-3 or on the baseline delta**.
  A burst on a repo with no other finding is still reported — at weight 0, so
  the user sees the context without it moving the verdict.

  **Axis 4 is deliberately excluded from that gate**, and from v0.27.0 — where
  the burst and the capability axis shipped together — the code did not honour
  it: the condition was the whole running score, which includes capability. Capability describes *configuration*, not something that
  happened, which is why the axis carries its own ceiling one below
  `tSuspicious` — so a capability finding (3) plus a burst (3) reached
  Suspicious on configuration and timing alone, handing that ceiling straight
  back. The gate now sums non-capability findings explicitly. Found by a
  reviewer on [#116](https://github.com/gfazioli/octoscope/pull/116), which
  widened how many repositories score on Axis 4 and therefore how many could
  reach it; the invariant had been stated in two code comments and in this
  document while the code disagreed with all three. The repo list comes from the caller's
  existing dashboard fetch, so it remains free; `--public-only` narrows it, on
  the grounds that a screenshot-safe mode must not disclose private push
  activity even as a count.
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

Every gap here is a potential **false negative**, which in a security tool is
the expensive direction to be wrong in. The rule that follows from that: a
partial scan must present itself as partial. A clean verdict means "clean in
what I looked at", and the report has to say what that was.

- **OAuth grant enumeration is not available.** The legacy OAuth
  Authorizations API is gone, so octoscope cannot *list* a user's grants — the
  central remediation step is link-out only.
- **Branch enumeration stops at 100.** Tier B walks
  `refs(refPrefix: "refs/heads/", first: 100)` unpaginated, so a repository
  with more branches than that has some unscanned — and the implant hides on
  side branches by preference (the reference worm backdated commits on `next`).
  Either paginate, or mark the report partial and name the number skipped.
  Tracked as [#85](https://github.com/gfazioli/octoscope/issues/85).
- **Self-hosted runners, deploy keys and webhooks need elevated scope**, so
  they are best-effort — and the report declares what the token could not
  reach, rather than omitting it silently.
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
remediation.

The threat research this design is built on is Ionut-Cristian Florescu's dev.to
series (June 2026), written from the position of a maintainer whose own
repositories were hit:

- [The Bot That Never Was](https://dev.to/icflorescu/the-bot-that-never-was-2mfp)
- [The Bot that Never Was, Part 2 (Miasma worm): how a GitHub token survived and hijacked my repos from an Azure IP](https://dev.to/icflorescu/miasma-worm-part-2-how-a-github-token-survived-a-full-machine-rebuild-and-hijacked-my-repos-from-8aa)
  — the source of this design's central remediation lesson: revoke the
  authorization grant, not just the token
- [If the Shai-Hulud worm reached your GitHub repos, please read this](https://dev.to/icflorescu/if-the-shai-hulud-worm-reached-your-github-repos-please-read-this-1pok)
- [Most repos hit by the Shai-Hulud worm are still infected a week later, and the obvious fix punishes the victims](https://dev.to/icflorescu/most-repos-hit-by-the-shai-hulud-worm-are-still-infected-a-week-later-and-the-obvious-fix-punishes-2m6o)
