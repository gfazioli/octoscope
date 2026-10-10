# .github/ — CI and the release workflow

Loaded when working under `.github/`. These sections were in the root
`CLAUDE.md` until 2026-10-10. The root keeps the govulncheck recipe and
the release traps, one line each.

#### The CI supply-chain gate

- **CI supply-chain gate (since v0.20.2)**: `ci.yml` runs `govulncheck`
  on every push/PR (pinned `@v1.4.0`) and scans the **stdlib too**. A
  fresh Go advisory turns CI red and can hit **either** the stdlib **or**
  a module dependency — the fix differs:
  - **stdlib** → bump the `go` directive in `go.mod` to the patched
    release.
  - **dependency** → `go get <module>@<patched> && go mod tidy` (e.g.
    v0.24.0 bumped `github.com/yuin/goldmark` to v1.7.17 for GO-2026-5320,
    reachable via glamour's markdown renderer). This is common: the
    advisory usually lands on a PR that never touched the flagged code —
    it's a *pre-existing* red, not something that PR introduced.

  Either way the bump *is* the fix, not a suppression. Reproduce and
  verify locally before pushing with the same pin as CI:
  `go run golang.org/x/vuln/cmd/govulncheck@v1.4.0 ./...` (expect
  `No vulnerabilities found`). Workflow actions are pinned to commit SHAs (with a
  `# vX.Y.Z` comment) and kept current by `.github/dependabot.yml`
  (weekly, grouped) — bump via Dependabot's PR, never refloat to a tag.

  **The bump rides the next release cycle — no patch release just to
  re-compile** (decided 2026-07-29 on GO-2026-5970, `x/text` v0.39.0).
  A dedicated patch would change nothing but the shipped binary, and
  the practical exposure is usually already closed upstream of our
  code: every GitHub-sourced string arrives through `encoding/json`,
  which replaces invalid UTF-8 with U+FFFD, so the malformed-input
  class most of these advisories need never reaches the flagged
  symbol. (`github.Sanitize` does *not* help there — it walks bytes
  and copies non-ASCII through untouched.) Precedent both ways:
  GO-2026-5856 and GO-2026-5970 each landed as a standalone
  `fix(deps):` PR and then shipped inside the following cycle. The one
  argument for a patch is wanting the Homebrew binary to pass a
  third-party `govulncheck -mode=binary` scan — raise it, don't assume
  it.

  **Reading a trace before you panic**: `X calls io.WriteString, which
  eventually calls Y` crossing an interface method (`io.Writer.Write`)
  is a conservative call-graph edge over *every* implementer linked
  into the binary — not a demonstrated path from our code to the
  vulnerable symbol. Check what actually feeds the input before
  treating it as reachable.

#### Releases that pass every check and still do not run

The same shape as the version-pill trap in `docs/CLAUDE.md`, learnt the
expensive way in 0.34.0:
**never verify a Homebrew release by whether it installs.** `brew install`
succeeding, `brew info` loading the cask and `brew style` passing are all
checks on the *recipe*; none of them executes what was installed. 0.34.0
passed all three and shipped a macOS binary that could not run at all —
distribution had moved from a formula to a cask, a cask's download carries
`com.apple.quarantine` where a formula's does not, and under quarantine
Gatekeeper refuses an ad-hoc-signed binary: SIGKILL, exit 137, and the file
removed from the Caskroom. `octoscope --version` printed nothing.

So the release check is to **install from the real tap and run the binary**,
asserting the version string and exit 0 — on a machine where the previous
version has been uninstalled first, because `brew install` answers *"the
latest version is already installed"* and exits 0 without staging anything.
That last part is not hypothetical either: it silently turned a set of
verification runs into no-ops while reporting success for every one.

Since 0.34.3 the macOS binaries are signed with a Developer ID certificate
and notarized, through goreleaser's `notarize` block. Three things about
that were measured rather than assumed, and each one is a way to believe
it is working when it is not:

- **Signing without notarizing buys nothing.** A binary carrying a valid
  Developer ID signature, hardened runtime and Apple timestamp is still
  killed under quarantine — `spctl` answers *"rejected / source=Unnotarized
  Developer ID"* and the process dies on SIGKILL exactly as the ad-hoc one
  did. Gatekeeper looks for the notarization ticket; the signature is only
  its prerequisite. Anything that reports "signed" is not reporting on the
  thing that matters.
- **A green release does not mean a notarized one.** goreleaser's notary
  pipe fails on an `Invalid` or `Rejected` verdict, but on a TIMEOUT it
  logs `notarize timeout` and carries on
  ([goreleaser's notary pipe](https://github.com/goreleaser/goreleaser/blob/v2.18.1/internal/pipe/notary/macos.go),
  read at v2.18.1). A slow notary therefore leaves a
  signed-but-unnotarized binary behind a green release job — 0.34.1's
  shape again. The `verify-macos` job in `release.yml` is what catches it:
  it downloads the release's assets (the draft's, on a tag push), asks
  `spctl` for a verdict, re-applies the
  quarantine flag by hand and runs the binaries. Do not delete it as
  redundant with the release job — it tests what the release job cannot see.

  **What a runner can prove is not a constant, so the job measures it
  rather than assuming it.** v0.34.3 went red on a release that was
  perfectly good: its control found that an ad-hoc-signed, quarantined
  binary runs happily on GitHub's macOS image, which made the launch test
  meaningless there — and being fatal about it turned a correct release
  red. The measurement that explains it is worth carrying, because the
  obvious check is the wrong one: `spctl --status` reports **`assessments
  enabled`** on that runner, and a quarantined ad-hoc binary still runs.
  *Assessments enabled is not the same as launches policed*, so the status
  is not the answer and only the behaviour is.

  The job therefore opens by running two controls on a copy of the shipped
  binary re-signed ad-hoc — the exact state 0.34.2 shipped, so the only
  variable is the signature. If `spctl` **refuses** that copy while
  accepting the published one, the notarization verdict is a real gate
  wherever it runs, and that is what gates; if `spctl` ever calls an ad-hoc
  binary notarized, the job fails hard, because a verdict that cannot fail
  would pass every future release. Separately, if the quarantined copy is
  killed, launches are policed and "the shipped binary ran" is evidence;
  if it is not, that line is reported as informational and says so. The
  version assertion stays a hard gate either way — tying the artifact to
  the tag has nothing to do with Gatekeeper.

  The workflow also takes a **manual dispatch** with a tag input, which
  verifies an already-published tag without building or publishing
  anything. Use it after any change to this job: 0.34.3's verification
  shipped having never run once, and its first execution was against a
  real release, which is the worst possible place to discover that a check
  is wrong about its environment.

  **Since #182 it prevents as well as detects.** goreleaser creates the
  release as a draft (`draft: true` in `.goreleaser.yaml`), and the
  `promote` job publishes it and pushes the Homebrew cask only after
  `verify-macos` has passed; `verify-cask` then installs the published
  cask and checks it delivers the bytes `verify-macos` verified (the job
  list at the top of `release.yml`). Before that, an unnotarized binary
  was downloadable, and the cask live, for the minutes between the publish
  and the red X
  ([#182](https://github.com/gfazioli/octoscope/issues/182), closed
  2026-09-15).
- **The ticket cannot be stapled into a bare binary.** `stapler` looks for
  `Contents/CodeResources`, i.e. a bundle, and exits 73 on a plain Mach-O.
  So Gatekeeper resolves the ticket **online** at first run, and the cask's
  `xattr` step stays as the offline belt — installing on Wi-Fi and first
  running offline is a case nobody has measured. Notarization is not there
  to replace the hook; it covers the path the hook never could, a `.tar.gz`
  downloaded straight from the Releases page.
