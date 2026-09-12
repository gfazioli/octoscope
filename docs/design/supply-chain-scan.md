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

## Axis 1b — dependency install surface

**Shipped in 0.33.0** ([#108](https://github.com/gfazioli/octoscope/issues/108)).

Axis 1 enumerates what auto-executes *in the repository's own source*. It
cannot see a dependency that starts executing on install: that is not a new
file and not a code change, it is a lockfile diff — and a lockfile diff is the
one part of a pull request nobody reads line by line, which branch protection
and required reviews do nothing about, because there is nothing in it a
reviewer would recognise as suspicious.

It needs no scope the scan does not already have. The lockfile is a blob in the
repository's own tree.

### npm only, and that is a measurement

Per format, measured 2026-09-08 against real repositories:

| Format | Declares install-time execution? | Evidence |
|---|---|---|
| `package-lock.json` v2/v3 | **yes** — `"hasInstallScript": true` per entry | `axios/axios`: 684 packages, 2 flagged |
| `package-lock.json` v1 | no — no `packages` map | format |
| `npm-shrinkwrap.json` | yes — same schema | format |
| `pnpm-lock.yaml` v6 | yes — `requiresBuild: true` | `vitejs/vite` @ `v4.5.0`: 95 occurrences |
| `pnpm-lock.yaml` v9 | **no** — `requiresBuild` is gone | `vitejs/vite` @ `main`: 0, and 80 `hasBin` |
| `yarn.lock` (v1 and berry) | no per-entry flag | `facebook/react`, `babel/babel` |
| `go.sum` | not applicable | the Go toolchain runs no install scripts |

pnpm is absent *because* it once had the field and dropped it: building on
`requiresBuild` would ship a rule that decays. `hasBin` is not a substitute — a
bin entry runs when the developer chooses to run it, which is not this threat.
The version is checked rather than merely decoded, so a future schema carrying
a `packages` map is declined with its number named rather than answered from a
field nobody has verified still means what it meant.

### The subset, not the file

The delta records `name@version → integrity` for the dependencies that carry an
install script, and nothing else. Fingerprinting the lockfile's own blob OID
would fire on every dependency bump, which is the noise this axis exists to
avoid.

The bet, and how far it holds — the last 20 lockfile revisions of each
repository, newest first, diffing the install-script subset between consecutive
revisions:

| Repo | Revisions | Subset changed | New | Removed | Bumps | Same version, changed `integrity` |
|---|---|---|---|---|---|---|
| `axios/axios` | 19 | **0** | 0 | 0 | 0 | 0 |
| `npm/cli` | 19 | **0** | 0 | 0 | 0 | 0 |
| `nodejs/undici` | 19 | **2** | 1 | 1 | 2 | 0 |
| **Total** | **57** | **2 (3.5 %)** | 1 | 1 | 2 | **0** |

Roughly thirty times quieter than the file it lives in. `npm/cli` is the
strongest case: a large lockfile that churns constantly, whose install-script
set did not move once in 19 revisions.

The second reading is the one that sets the weights. **Zero republish events in
57 revisions** is a measured base rate of false positives, not evidence the
signal fires — and it is what lets the sharpest case carry real weight instead
of training the reader to skip the axis.

Caveats kept deliberately: three repositories, all popular JavaScript projects,
and the diff is *per commit* while octoscope diffs *per scan*. A scan spans many
commits, so the measured rate is a lower bound on what a scan will see.

### Three cases, because the key is `name@version`

| Case | Meaning | Weight |
|---|---|---|
| a name that carried no install script now does | a dependency began executing code at install | `wDeltaNewInstallScript` = 2 |
| same `name@version`, different recorded content | that version was republished | `wDeltaRepublishedDep` = 4 |
| a known install-script package at a new version | an ordinary bump | 0 — inventory |
| an install-script package disappeared | an improvement | 0 — inventory |

4 lands on `watch` alone and reaches `suspicious` only with corroboration — the
same posture as `wDeltaNewIgnition`. octoscope never looks at the registry, so
the claim is exactly *your dependencies' auto-execute surface changed*, never
*this dependency is malicious*.

A missing `integrity` (git and `link:` dependencies) falls back to `resolved`,
and failing that to a stable sentinel: an absent value must compare equal to
itself across scans, or an unchanged dependency would be reported as
republished. **A republish is only ever scored when both sides are content
hashes** — a `resolved` URL says where a dependency came *from*, so a registry
or mirror change is reported as a move at weight 0 rather than as the heaviest
claim this axis makes, on evidence that cannot carry it.

Where one `name@version` appears at two install locations with *different*
integrity, every distinct value is recorded as a sorted composite rather than
one of them: keeping only the lowest was deterministic and silently dropped a
change confined to any other location.

The composite is a **set**, so it discards which location held which value —
see Honest gaps.

### What names a workspace, and how that was settled

A fetched dependency's name is the segment after the **last**
`node_modules/` in its entry path, so the same package hoisted to two depths
collapses to one name rather than reading as two — deliberate, and pinned by
`TestANestedDuplicateIsTheSamePackage`. A **workspace** entry has no such
segment: its path says where the
package lives in the repository, which is not what it is. Keying on the path
meant a monorepo reorganisation (`packages/cli` → `apps/cli`) reported one
dependency that started running code at install and another that stopped, for
a rename that changed nothing about what executes
([#158](https://github.com/gfazioli/octoscope/issues/158)) — and it arrived as
a burst, which is the shape most likely to teach a reader to skip the axis.

There the entry's declared `name` now wins — only there: a `node_modules/`
entry keeps its location-derived name even when it declares another, which is
the aliased-install case below. The fallback is the **last path segment**
rather than the whole path. That second half is the part measurement
added, and the issue's own proposal did not have it — 2026-09-12, every
`package-lock.json` reachable at the default branch of seven npm-workspace
monorepos:

| Repository | workspace entries | carry `name` | `name` ≠ basename | `name` = basename | with an install script | of those, named |
|---|---|---|---|---|---|---|
| `npm/cli` | 16 | 6 | 6 | 0 | 0 | — |
| `open-telemetry/opentelemetry-js` | 56 | 54 | 54 | 0 | 0 | — |
| `socketio/socket.io` | 13 | 6 | 6 | 0 | 0 | — |
| `mochajs/mocha` | 1 | 1 | 1 | 0 | 0 | — |
| `puppeteer/puppeteer` | 10 | 8 | 8 | 0 | 1 | 0 |
| `microsoft/playwright` | 16 | 7 | 7 | 0 | 6 | 3 |
| `nestjs/nest` | 3 | 3 | 3 | 0 | 0 | — |
| **total** | **115** | **85** | **85** | **0** | **7** | **3** |

Two facts decided the shape:

- **Every `name` npm wrote was one the path did not already say.** All 85
  declared names differ from their directory's basename; not one entry carried
  a redundant one. That is a dated observation of npm's output, not a promise
  it makes — and the fallback is built to hold either way, since a redundant
  `name` equals the basename anyway. So an absent field means the basename
  *is* the name — and
  "read `name`, fall back to the path" would have fixed **3 of the 7** real
  install-script workspaces in the sample, leaving the other four keyed on a
  path that a move still breaks.
- **The link entry is not a duplicate.** A workspace also appears as
  `node_modules/<name>` with `link: true`; all seven such entries carry no
  `hasInstallScript`, so the parse loop skips them and keying by name adds no
  second value for the same key.

**A workspace records the sentinel, never a content hash.** Keying by name
means two nameless workspaces sharing a basename *and* a version land in one
bucket, and two integrity-shaped values in one bucket are exactly what the
delta scores as *same version, different bytes* at weight 4 — the heaviest
claim this axis makes. npm refuses two workspaces with one name, so that
collision only exists in a lockfile written to produce it; rather than rely on
that, the parser ignores `integrity` and `resolved` on a workspace entry
outright. It loses nothing observable: those same 115 entries carried **zero**
of either field, because a workspace is local source and npm has no hash to
record for it.

An **aliased** install (`npm i foo@npm:bar`) is the mirror image — a
`node_modules/` entry that declares a different name — and the install
location is deliberately kept there: 20 aliases among 10,881 fetched entries
in the same sample, none carrying an install script, so reading the alias
target would rewrite 0.18% of keys to fix nothing this axis can observe.

**The change of key needed a migration, and the baseline carries a marker for
it** (`DepsKeyVersion`, [#169](https://github.com/gfazioli/octoscope/issues/169)).
A store written before this keyed workspaces by path, so the first scan after
upgrading would have described the same package under two names and reported
weight-2 *"began executing code at install"* for an upgrade — measured before
the marker existed. The keys cannot be rewritten, since the old one was the
path even for a workspace that declares a name and nothing recorded says which;
so a mismatch reads as **not comparable**, in the words the report already has
for a surface it has not seen before. It costs one scan, and the surface is
rewritten in the current format as that scan goes. Zero means "written before
the marker", which is every baseline that existed when it shipped — including
the narrow case of one written from `main` between #158 and this change, which
was already name-keyed and would have compared fine. It is treated the same
way deliberately: the recorded format is unknown, and guessing right is worth
less than one honest scan. Downgrading to a binary from before #158 is not
supported and undoes the migration, since an older binary drops the field it
does not declare.

What remains: renaming the *directory* of a workspace that has no declared
name moves the key once, until npm rewrites the lockfile — at which point the
name differs from the new basename and npm records it, and the key becomes
stable again. And a workspace that shares a name with a registry dependency
now shares its key, which is correct: npm links it at that same name.

### What is read, and what that costs

- **The default branch only.** A dependency change that matters lands there; on
  side branches this would mostly re-report the open pull requests, once per
  branch, against a baseline recorded from the default branch.
- **Its own fetch budget** — `maxLockfileFetches`, counted separately from the
  Axis-2 blob budget and spent *before* it. Axis 2 is the axis that catches the
  payload, and a lockfile is one file per branch on any JavaScript repository;
  sharing the budget would have starved it. That the Axis-2 budget is
  arithmetically unchanged is a test, not a comment.
- **The budget counts candidates, not successful reads**, which is what makes
  ranking a choice rather than a preference. npm ignores `package-lock.json`
  when `npm-shrinkwrap.json` is present, so an unreadable shrinkwrap leaves the
  question unanswered rather than answered about a file npm never installs
  from.
- **Its own size cap too**, `maxLockfileScanBytes`, since
  [#159](https://github.com/gfazioli/octoscope/issues/159). The read used to
  share Axis 2's `maxBlobScanBytes` (1.5 MiB), which bounds what gets handed
  to entropy and obfuscation analysis — a different question, and the wrong
  budget to spend here. Over the cap the file is **declared unread**, not
  silently skipped, which was always the honest half; what was wrong is where
  the line sat.

  **4 MiB, measured 2026-09-12** across thirteen repositories carrying a root
  `package-lock.json`:

  | Repository | size | | Repository | size |
  |---|---|---|---|---|
  | `WordPress/gutenberg` | **1.81 MiB** | | `mozilla/pdf.js` | 0.47 MiB |
  | `puppeteer/puppeteer` | **1.63 MiB** | | `npm/cli` | 0.42 MiB |
  | `jitsi/jitsi-meet` | 1.41 MiB | | `nodejs/undici` | 0.38 MiB |
  | `open-telemetry/opentelemetry-js` | 0.87 MiB | | `mochajs/mocha` | 0.33 MiB |
  | `nestjs/nest` | 0.78 MiB | | `microsoft/playwright` | 0.33 MiB |
  | `microsoft/vscode` | 0.75 MiB | | `axios/axios` | 0.33 MiB |
  | `socketio/socket.io` | 0.56 MiB | | | |

  Two of thirteen sat above the old cap, and they are the shape this axis is
  worth most on: most dependencies, most install scripts, least chance of
  anybody reading the diff by hand. The axis was weakest exactly where it
  would have paid.

  **The number is an allocation bound, not only a patience one** — enforced
  twice, so that it is a property rather than a hope: on the size the tree
  reports for the blob, which costs no request, and again inside `fetchBlob`
  on the size the blob itself reports, which is where the memory is about to
  be spent. The two come from the same git object and should never disagree;
  if they ever do, the fetch returns an *error* and the file reads as content
  that did not arrive, which the report already knows how to say. The error
  matters: an empty result with no error is read by both callers as a
  successful fetch of an empty file, which turns "we did not read it" into a
  claim about its contents — a parse failure disclosed about bytes nobody
  looked at, and an Axis-2 blob recorded as carrying no obfuscation markers
  on the strength of having none to read. It matters because `parseLockfile` hands
  the whole body to `encoding/json`. Measured against
  this code: the real gutenberg file costs 2.3 MiB of allocation — 1.3× its
  size, because only 12 of its packages carry an install script — while a
  pathological file where *every* entry does costs about 4.4×: 17.7 MiB at
  4 MiB of input, 35 at 8, 71 at 16.

  Fetch and parse **together**, measured end to end through this code on a
  3.94 MiB pathological lockfile (2026-09-12): **44.5 MiB before
  [#167](https://github.com/gfazioli/octoscope/issues/167), 32.8 MiB
  after** — the difference being the base64 field and its newline-stripped
  copy, which the raw body removes. Not the 2.7× the arithmetic suggests:
  `io.ReadAll` grows by doubling and the parse dominates, which is exactly
  why the figure is measured rather than derived. Once per scan, since
  `maxLockfileFetches` is 1; 8 MiB would roughly double it to serve nothing
  any measured repository needs.

  **The cap is enforced on the read itself**, not on a size the response
  reports — raw has no envelope to declare one. The reader takes at most
  the cap plus one byte — the extra byte being how a file *at* the cap is
  told from one over it. That is stronger than what it replaced, and it is
  pinned by a test that counts the bytes actually read rather than the
  outcome. The first version of that test passed with the bound removed,
  because the length check after the read caught the same case — by which
  point the whole body had been allocated, which is the one thing the cap
  exists to prevent.

A lockfile is exempt from Axis 2 entirely. It is hundreds of kilobytes of
base64 integrity hashes, which is precisely the shape that axis scores — without
the exemption every ordinary JavaScript repository would have been flagged for
being ordinary.

### Everything it did not compare says so

Every one of these is weight 0, and every one exists because the alternative is
silence — which is indistinguishable from *nothing here runs code at install*.
A reader told nothing assumes coverage.

- a lockfile seen and not read, with the reason recorded at the moment it was
  known: oversized, unfetchable, or the file npm itself ignores;
- a lockfile read with something to declare: a schema that cannot answer,
  content that would not decode, an ambiguous duplicate;
- a lockfile from a package manager this scan does not read. These are
  deliberately *not* in the Axis-1 catalog — a catalog row is a claim that a
  path auto-executes, and they make no such claim — so the tree walk observes
  them separately. A pnpm or Yarn repository is the likeliest place for this
  axis to look like coverage when it is not;
- an npm project committing no lockfile at all. Common rather than anomalous:
  `eslint/eslint` and `expressjs/express` are both in that state. It needs a
  `package.json` to fire, because *no npm lockfile* on a Go repository is not a
  disclosure, and it stays quiet when a foreign lockfile is present, which has
  just said the same thing more precisely;
- a baseline recorded before this axis existed, a lockfile read now and not
  last time, and a surface recorded before and not measured now — that last one
  because nothing else would notice: a lockfile is weight 0, so its path never
  enters the ignition fingerprint;
- more changes than the report will list. The lockfile is attacker-controlled
  and bounded only by the blob scan cap, which at a minimal entry apiece is
  tens of thousands of packages — enough to bury every other axis under this
  one's output. Past `maxDepFindingsPerPath` the count is stated and the rest
  are not listed.

  **The cap keeps the most serious, not the first alphabetically**, and that is
  load-bearing rather than tidy. Cutting a key-ordered list let the lockfile
  choose which findings survived: twenty-five weight-0 bumps named early in the
  alphabet pushed a republish named late out of the report *and out of the
  score*, since a finding dropped before it is recorded never contributes its
  weight. The ordering is by case severity rather than by weight, because a
  stale baseline weighs every case 0 and the sharpest line is still the one to
  show first.

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

  The disclosure is **one line for the scan, naming where those workflows
  live** — and it names them individually whenever a target is not spelled
  as `owner/repo/.github/workflows/…`, since reducing on slash count alone
  was measured turning a full URL into the destination "https:/". A target
  addressing *this* repository by its full name is kept whole too: that
  spelling is exactly what the disclosure exists to surface. The list is
  capped, because grouping only shrinks the line when destinations repeat.
  The count is of distinct targets, and the noun says so,
  rather than one row per caller. The caller is not rendered — the findings list
  shows weight, axis, reason and branch — so per-caller rows differing only in
  who hands off print identically. And measured against real repositories,
  listing every target does not scale: one hands fifteen workflows to a single
  shared repo, which rendered as ~1200 characters of near-identical paths. The
  actionable fact is the repository to go and audit.

  It carries **no branch label**, because the data behind it does not support
  one: the merge above is a union by path, so attaching a branch would credit
  one branch with another's targets — measured, with the same path carrying
  different content on two branches, each was reported as calling the other's
  target. A destination is usually another repository, but not always: a `uses:`
  pointing inside this repository that the scan did not read — over the blob
  budget, or simply absent — is unresolved too, and keeps its own path. The
  claim is that the chain left *the scan's view*, which is the honest one for
  both.

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

**The fingerprint carries a second record** (0.33.0): the dependency install
surface of the default branch's lockfile, as `name@version → integrity` for the
subset that runs code at install. It sits beside the ignition OIDs rather than
inside them because the two say different things about the same file — a
lockfile's OID moves on every bump, and its install-script subset does not. See
[Axis 1b](#axis-1b--dependency-install-surface). A recorded *empty* set means
the file was read and nothing in it runs code at install; *no entry at all*
means the comparison did not happen, and the two never render alike.

**The three questions the issue left open, and how they were answered:**

- **Where it lives.** A sibling JSON file, `scan-baselines.json`, next to
  whichever `config.toml` is in use (so `--config` keeps its state together).
  Deliberately *not* in the TOML: that file is hand-edited and carefully
  round-tripped, and filling it with blob OIDs would wreck it. A malformed
  store degrades to "no baseline" rather than failing the scan — it is
  machine-written, so refusing to scan over a corrupt cache would trade a
  security tool for a bookkeeping problem.
- **When it is written.** On every scan, from inside the fetch command
  (`fetchRepoScanCmd`) rather than from `Update`, because it touches the disk.
  The write is unconditional *with respect to the verdict* — a compromised repo
  still gets a history, which is why the fingerprint records the verdict at
  capture and a later report can admit the baseline was taken while the repo was
  already flagged. It is not unconditional otherwise: the write is guarded by
  `err == nil && scan != nil && baselinePath != ""`. The first of those is
  narrower than it looks — a *returned* scan error writes nothing and preserves
  the previous baseline, but a blob or lockfile that could not be fetched is
  non-fatal by design, so that scan completes, discloses the unread file, and
  records the baseline like any other. The last means there is no store at all
  when no usable config directory exists. And a failed *write* is swallowed on
  purpose — losing a baseline costs the delta axis one run, which is not worth
  failing a completed scan over, though it does mean a completed scan is not a
  guarantee that the baseline moved. There is no
  confirmation step and nothing but a scan ever writes it, which is the same fact
  the comparison window exists to state: the store records when somebody was
  *looking*.
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
- **Retention is not the same knob as scoring.** The store keeps an unbounded
  per-key history of every distinct blob OID ever observed there, with the date
  it was first seen, so a return to previously-seen content is a lookup rather
  than a new mechanism — the stored value is already a content hash, and a
  bit-identical revert reproduces the exact OID. Bounding retention at one, as
  it was, meant `A → B → A` across three scans read as two ordinary changes.

  It is unbounded rather than a longer fixed window because **a fixed lookback
  is a published dwell time**, and patience is the whole point of the attack
  this axis exists to notice: anything willing to revert itself quietly is
  willing to wait. Thirty days was chosen as a *scoring* boundary — past it most
  of a diff is legitimate churn — and it was quietly deciding retention too.

  The note is a note, never a security claim, and its wording carries the same
  honesty as the comparison window one level up: *observed* here on a date, not
  *was* here. A repository rename produces a new store key and starts a fresh
  history, and the wording must not imply otherwise.

  **Growth was measured rather than assumed**, since the file is machine-written
  and nobody opens it. It grows by *distinct contents*, not by scans: a path that
  oscillates between two versions forever holds two entries, and a path nobody
  touches adds none. At roughly 90 bytes per entry, five workflows carrying five
  versions each is 3 KB, twenty versions each is 10 KB, and a deliberately
  pathological forty workflows at fifty versions is 186 KB. Nothing is compacted,
  and that is the decision rather than an omission.
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

GitHub's GraphQL gateway terminates any request it cannot process within 10
seconds — documented, answered as 502/504 from the proxy, and followed by a
rate-limit penalty for the next hour — and per-item fan-out across many
repositories reliably runs past that clock. It is a timeout, not a complexity
score (measured 2026-09-06; `rateLimit.cost` read 1 on the queries that died).
The scan is tiered accordingly.

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
- **The dependency install surface is npm-only**, and a repository whose only
  lockfile is `pnpm-lock.yaml` or `yarn.lock` gets an explicit weight-0 line
  saying its dependency surface was *not* compared. The measurement behind that
  choice is in [Axis 1b](#axis-1b--dependency-install-surface); if pnpm restores
  a build declaration, one catalog row and one parser branch adds it.
- **`--ignore-scripts` is not detected.** A user who installs with it, or an
  `.npmrc` octoscope does not read, is not exposed the way Axis 1b assumes.
  Honest gap, documented, not detected.
- ~~**A lockfile past the blob scan cap reads as "not compared"**~~ — closed by
  [#159](https://github.com/gfazioli/octoscope/issues/159), and not by raising
  Axis 2's cap: the lockfile read has a ceiling of its own,
  `maxLockfileScanBytes`, measured at 4 MiB against real monorepo lockfiles
  and against what the parse allocates. See *What is read, and what that
  costs*. Past **that** a lockfile still reads as "not compared", which is the
  half that was always right.
- **Two install locations of one `name@version` swapping their contents is
  invisible.** The ambiguous case records the sorted *set* of values, so
  `A: aaa→bbb` while `B: bbb→aaa` reduces to the same composite. Recording the
  location instead would key on the install path, and hoisting rearranges those
  constantly for entirely ordinary reasons — trading a rare miss for routine
  noise is the trade this axis exists to refuse. Pinned by a test, so it stays
  a decision rather than becoming a discovery.
- ~~**A workspace entry is keyed by its install path**~~ — narrowed by
  [#158](https://github.com/gfazioli/octoscope/issues/158): the declared
  `name` wins and the fallback is the last path segment, so **moving** a
  workspace no longer moves the key. What survives is smaller and stated in
  *What names a workspace* under Axis 1b: renaming the *directory* of a
  workspace that declares no name moves the key once, until npm rewrites the
  lockfile and records the name it can no longer derive.
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
