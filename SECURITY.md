# Security Policy

## Supported versions

octoscope ships from the latest tagged release. Security fixes land on
the most recent minor and are released as a patch. Please make sure
you're on the latest version before reporting:

```
brew upgrade gfazioli/tap/octoscope
```

## Reporting a vulnerability

**Please do not open a public issue for security reports.**

Use GitHub's private vulnerability reporting instead:

1. Go to the [**Security** tab](https://github.com/gfazioli/octoscope/security) of the repository.
2. Click **Report a vulnerability**.
3. Describe the issue, the affected version, and a reproduction if you have one.

You'll get a response as soon as possible. Once a fix is ready it will
be released and the advisory published with credit (unless you prefer to
stay anonymous).

## Scope

octoscope is a **read-only** terminal client for the public GitHub API.
It never mutates GitHub state and never stores your token — auth is
resolved at runtime from `$GITHUB_TOKEN` or `gh auth token`. Reports of
particular interest:

- Terminal-escape / control-sequence injection through GitHub-sourced
  strings (titles, bodies, branch names, etc.) — these are sanitized at
  the fetch boundary, so a bypass is worth reporting.
- Anything that could leak the user's token or write to disk
  unexpectedly.
- Supply-chain concerns in the released binaries or the Homebrew tap.
