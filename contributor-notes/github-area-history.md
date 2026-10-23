# internal/github — fetching from GitHub and other APIs

Loaded when working under `internal/github/`. These sections were in the
root `CLAUDE.md` until 2026-10-10 and moved here because they are about
this package's fetch paths. The root keeps a one-line version of each rule
whose trigger lies outside this directory.

#### Verify before writing Go

- **Probe the schema with `gh api graphql` before writing any Go.**
  This is the cheap first rung of the verification ladder and it
  answers most "can octoscope even show X?" questions on its own — no
  build, no test file, no compile round-trip. Two steps:
  ```bash
  # 1. does the field exist at all? (introspection)
  gh api graphql -f query='{__type(name:"PullRequest"){fields{name}}}' \
    --jq '[.data.__type.fields[].name | select(test("stack";"i"))] | .[]'
  # 2. is it actually queryable, and what does "absent" look like?
  gh api graphql -f query='{repository(owner:"gfazioli",name:"octoscope")
    {pullRequest(number:98){stackEntry{position stack{size}}}}}'
  ```
  Step 2 is the one that matters: introspection only proves a field is
  *in the schema*, while a live query proves the **token can read it
  without a preview header** and shows the shape of the empty case
  (`stackEntry: null` rather than an error) — which is exactly what the
  extractor has to handle. Used 2026-07-31 to settle whether GitHub's
  brand-new stacked-pull-requests feature was reachable at all (it is;
  issue #99). A changelog announcement is **not** evidence of API
  support — the stacked-PR post never mentioned the API, and the
  fields were there anyway.
- **The same rung applies to any API, and hardest to a third-party one —
  including its documentation.** GitHub's schema is at least
  introspectable; somebody else's is a prose page that can be stale,
  incomplete, or describing a different endpoint. Probing first changed
  three decisions while adding the service-status check (#119) that
  reading would have got wrong. **Which endpoint**: `summary.json`
  carries status, components *and* unresolved incidents for 113 bytes
  more than `components.json` alone, replacing three requests. **What
  the vocabulary is**: Atlassian's own reference states it verbatim —
  "one of `operational`, `degraded_performance`, `partial_outage`, or
  `major_outage`" — and omits `under_maintenance`, live on 29 of
  Cloudflare's 478 components that day, which is why an unrecognised
  value has to fall toward the warning rather than toward "fine".
  **Whether a populated field is usable**: 15 of 50 real incidents
  carried no component join, and every component inside a *resolved* one
  reads `operational` because that field reflects the state now. So
  **treat vendor docs as a hypothesis and a live payload as the
  evidence**. Two traps on the way: a doc page contains example JSON,
  and grepping one for an indicator value briefly read as a live
  measurement — fetch the endpoint, don't grep the manual; and a
  third-party host must never receive the token, since `Client.rest`
  shares the oauth2 transport, so non-GitHub calls get their own
  credential-free client, a package-level function rather than a
  `Client` method, and a test that fails if an `Authorization` header
  ever reaches the endpoint.
- **Smoke integration tests gated behind a build tag**
  (`//go:build smoke`) are the maintainer-side check for new fetch
  paths: write one, run via
  `GITHUB_TOKEN=$(gh auth token) go test -tags smoke -v -run TestVxxx ./internal/...`,
  delete it before committing. Used for v0.13.0 (CI dot fetch),
  v0.14.0 (star-history + watched-repos) and twice in v0.25.0 (the
  check payload, then the `__typename` switch — the second run is what
  proved the real rollup still decoded). Never lands in git — the unit
  suite stays hermetic.
  - The client constructor is
    **`c, err := New("", Options{})`** (empty login = authenticated
    viewer), not a `NewClient(ctx)`. **It returns two values** — the
    note used to omit that and cost the exact compile round-trip it
    exists to prevent, twice in 0.27.0. Copy the line, don't retype it:
    ```go
    c, err := New("", Options{})
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    ```
  - Assert something, don't just log: a smoke test that only prints is
    green even when the fetch returns nothing. `if d.CIState != "" &&
    len(d.Checks) == 0 { t.Errorf(...) }` is what actually catches a
    broken discriminator.

#### Unions and URL fields

- **Discriminate every union on `__typename`, never on "which field
  looks populated".** `shurcooL/githubv4` resolves shared field names
  across inline fragments, so a node of one type can leave non-zero
  values in another fragment's struct — the heuristic was tried through
  v0.11.0 development and was wrong. It also drops a node whose
  discriminator field is legitimately empty (a `CheckRun` with an empty
  `name`) and silently swallows a union member GitHub adds later.
  Reference implementations: `issue_detail.go` timeline,
  `review_requests.go`, and the rollup contexts in `detail.go` /
  `pr_detail.go`. That last pair only got it right in v0.25.0, because
  the new code copied the older heuristic — when extending an existing
  extractor, check it follows this rule before mirroring it.
- **Query URL fields as `githubv4.String`, not `githubv4.URI`.** `URI`
  unmarshals through `url.Parse`, which errors on a control character —
  and that error aborts the decode of the **entire response**, so one
  malformed URL from one third-party app fails a whole fetch instead of
  costing one row. A string always decodes; `Sanitize` cleans it at the
  boundary and the UI applies its own gate before use (v0.25.0, the
  check `detailsUrl` / `targetUrl` fields).

#### The ceiling is a 10-second clock — what we can and can't query (since v0.10.1)

This section called it a "complexity budget" from v0.10.1 until
2026-09-06, when it was measured. It is a **clock, not a score**.
GitHub terminates any request it cannot process within **10 seconds**
and answers 502 or 504 from the gateway, before the request reaches the
GraphQL backend — documented under *Timeouts* on the GraphQL
rate-limits page. `rateLimit.cost` is **not the dial**: it read 1 for
every query shape in that measurement, the ones that survived and the
ones that died alike. And a timeout is not free — the same page says
extra points are deducted from the primary rate limit for the next
hour, so a query that flirts with the clock taxes the account on every
refresh that loses.

Two numbers to carry, both from the maintainer's 91-repo account:

- the real `repoFields` query already spends **6.4–6.7 s** of the ten.
  Any field added to the list fetch buys from a ~3.5 s budget;
- one `history { totalCount }` per repo on top of it: **8.2–10.9 s,
  three 502s in five runs**. The first run passed, at 9.8 s. A single
  green run of a query near the clock proves nothing — run five.

These patterns hit that clock on a ~74-repo account in early 2026 and
again on 91 repos in September, always as HTTP 502 *from the proxy*:

- A single combined query covering profile + counters + open PR/Issue
  nodes + 52-week contribution calendar + `repositories(first: 100)`
  with full nested fields. **Always 502.** This is what forced the
  v0.10.1 split.
- `defaultBranchRef.target.history.totalCount` requested once per
  repo across `repositories(first: 100)` (i.e. per-item fan-out on
  100 items). **Always 502** in 2026; **3 of 5** on re-measurement
  in September, with the first run passing — see #70. This killed
  the original issue #4 plan (configurable columns + commit-count
  metrics). The same field in a query of its own: 4.4–6.2 s, five
  of five, which is why the fallback is a separate branch.

**Rules of thumb derived from those scars**:

1. **The dashboard fetch is N parallel branches.** Started as two
   parallel queries in v0.10.1 (`profileFields` + `repoFields`),
   currently up to **eight** as of v0.38.0:
   1. `profileFields` — profile, counters, open PR/Issue nodes,
      contribution calendar
   2. `repoFields` — `repositories(first: 100)` with full nested
      fields
   3. `repoCIFields` — CI rollup state + latest release per repo
      (split from repoFields after v0.13.0 inline attempt 502'd)
   4. `watch_repos` fan-out (v0.14.0, gated on `len(watchRefs) > 0`)
      — one `singleRepoQuery` per entry, **bounded** by a
      semaphore (`watchedRepoConcurrency = 10`) so a 200-entry
      config can't burst-flood GitHub
   5. `reviewRequests` search (v0.15.0, gated on
      `authenticated && viewer-mode`) — single search query
   6. `FetchGists` (v0.29.0) — one connection, and the only branch
      that is **best-effort**: its error is deliberately dropped.
      Gists are the one surface GitHub answers with data *and* a
      GraphQL error on a permission edge, which is the shape that
      aborts a decode — sharing a branch with anything mandatory
      would take the dashboard down with it.
   7. `repoCommitFields` (v0.32.0, #70) — the viewer's commits per
      owned repo over the last year, gated on config `commit_counts`
      **and** an authenticated viewer. Best-effort like gists, for a
      measured reason: inline on `repoFields` this field pushed the
      list query past the 10-second clock (three 502s in five runs on
      91 repos); standalone it took 4.4–6.2 s, so it pages at 50 and
      a timeout costs the column for one refresh, never the dashboard.
      `Stats.CommitsLastYearApplied` is how the UI tells counts from
      placeholders.
   8. `fetchStackPlacements` (v0.38.0, #99) — where each listed PR sits
      in its stacked pull request, for the PRs tab's `2/4` marker,
      gated on a token. Best-effort like gists, and a branch rather
      than a field for a measured reason that is not time: inline on
      the profile query it cost nothing (3.29–4.49 s against 3.79–4.37 s
      on 50 open PRs, five runs each), but a GraphQL error on it would
      have failed the dashboard for a decoration.
   All run via goroutines + `sync.WaitGroup`. Wall-clock latency
   stays close to the slowest branch rather than their sum. See
   `internal/github/client.go` `FetchStats` for the canonical
   layout.
2. **Per-item fan-out across many items is forbidden when
   unbounded.** Asking GitHub to walk N repos × M sub-queries in
   a single GraphQL doc (history fan-out, statusCheckRollup inline
   on `repoFields`, etc.) consistently 502s on busy accounts. Two
   safe alternatives:
   - **Drill-in pattern**: one query per *selected* item, on demand.
   - **Bounded fan-out**: one targeted query per *config-listed*
     item (≤ tens), capped by a semaphore. Used for `watch_repos`.
3. **Sibling-cancellation on error — when results are *all
   needed*.** When a fetch combines multiple goroutines whose
   results are all required (`FetchPRDetail` GraphQL + REST),
   wrap the caller's ctx in a `context.WithCancel` child and use
   `sync.Once` to capture the first error. The sibling-
   cancellation echo (`ReasonNetwork` from a cancelled query)
   would otherwise clobber the real failure (Auth / RateLimit /
   5xx). Reference: `FetchPRDetail` v0.12.0 polish.
   - **Best-effort branches degrade, they don't abort.** When a
     parallel branch is decorative / optional it must *not* feed
     the shared error path: swallow its failure and leave its
     result empty so the mandatory branch still renders.
     `FetchRepoDetail` is the reference (since PR #47) — the
     star-history walk hits the restricted `stargazers`
     connection (prone to GitHub tightening + its own transient
     5xx), so it is best-effort, while the detail query stays
     mandatory and still `cancel()`s an in-flight walk.
4. **Adding new fields to a query: measure the wall clock, do not
   estimate complexity.** Run the full query with the field against
   the busiest account available, **five times**, and read the
   spread against 10 s — `rateLimit.cost` will say 1 either way.
   Anything past ~7 s on the list fetch belongs in its own parallel
   branch. `languages(first: 10)` × 100 repos is already inside the
   6.5 s the list fetch spends; `defaultBranchRef.target.statusCheckRollup`
   inline on 100 repos pushed it over. New nested aggregates ride on
   top of what's already there.
5. **If a feature needs per-repo data on the list**, surface it
   on-demand in the detail view first, then evaluate whether a
   list-level column is even necessary. The drill-in already
   answers most of those questions.
6. **Transient 5xx are noise, not always complexity** (v0.17.0).
   A 502 can also hit an *unchanged*, previously-fine query —
   pure gateway flakiness on GitHub's side — and HTTP/2 transport
   failures (`stream error`, `received from peer`, GOAWAY)
   surface the same way. Both classify as `ReasonServer` via
   `classifyErr` (`internal/github/client.go`); the dashboard
   fetch and every request of the `--json` / `--plain` report wrap
   in `github.RetryTransient` (`internal/github/retry.go` — 3
   attempts, short backoff, retries **only** `ReasonServer`). The
   report went without it until #224, so a cron run failed on the
   first 502 the dashboard rode out.
   New fetch paths reuse the same retry helper, and any new
   transport-level error string gets taught to `classifyErr`
   rather than leaking raw text into the error screen. The retry is
   for a *fine* query on a bad moment: a query that times out on its
   own weight is not transient, and each retry of it deducts more
   from the hour-long penalty. Fix the query; do not lean on the
   retry.

The principle "one GraphQL query per refresh" from v0.x.x docs is
**superseded** — current invariant is "as many parallel branches
as the feature shape demands, each one measured against the
10-second clock before adding fields".

#### Hermetic fetch tests

- GraphQL fetch paths can reuse the `newTestGQLClient` harness
  (`internal/github/watched_repo_fetch_test.go`, since v0.20.2): it points
  a `githubv4.Client` at an `httptest` server through the `rewriteHost`
  round-tripper, so a fetch is exercised hermetically against a canned
  JSON response. **REST** paths use the same trick — point `Client.rest`
  at an `httptest` server via `rewriteHost` and dispatch on request path
  (`internal/github/capability_test.go`, since 0.27.0).
