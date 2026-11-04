# internal/github — fetching from GitHub and other APIs

Detail for the root rules on probing, unions, URL fields and the 10-second
clock. Measurements and incidents behind them:
`contributor-notes/github-area-history.md`.

## Verify before writing Go

- Probe with `gh api graphql` first: introspection
  (`{__type(name:"PullRequest"){fields{name}}}`) shows a field exists; a
  live query on a real object proves the token reads it without a preview
  header and shows the empty case (`null` vs error) the extractor must
  handle. A changelog post is not evidence of API support either way.
- Third-party APIs: vendor docs are a hypothesis, a fetched live payload is
  the evidence (never grep a doc page's example JSON). An unrecognised
  status value falls toward the warning, not "fine". Non-GitHub hosts use
  a credential-free package-level function, never a `Client` method
  (`Client.rest` shares the oauth2 transport), plus a test that fails if an
  `Authorization` header reaches them.
- Smoke tests for new fetch paths: `//go:build smoke`, run with
  `GITHUB_TOKEN=$(gh auth token) go test -tags smoke -v -run TestVxxx ./internal/...`,
  delete before committing. Assert, don't just log. Copy the constructor,
  which returns two values (empty login = authenticated viewer):
  ```go
  c, err := New("", Options{})
  if err != nil {
      t.Fatalf("New: %v", err)
  }
  ```

## Unions and URL fields

- Discriminate every union on `__typename`, never on which field looks
  populated: `shurcooL/githubv4` fills shared field names across fragments.
  References: `issue_detail.go` timeline, `review_requests.go`, rollup
  contexts in `detail.go` / `pr_detail.go`. Check an existing extractor
  follows this before mirroring it.
- `githubv4.URI` fails the whole response's decode on one bad URL; use
  `githubv4.String` and let `Sanitize` and the UI gate clean it.

## The 10-second clock

GitHub's gateway kills any request not done in 10 s (502/504) and charges
the primary rate limit for the next hour; `rateLimit.cost` reads 1 either
way and is not the dial. `repoFields` already spends ~6.5 s on a 91-repo
account, leaving ~3.5 s for additions.

1. `FetchStats` (`client.go`) runs parallel branches via goroutines +
   `sync.WaitGroup` (currently eight); latency tracks the slowest branch.
   Gists, commit counts and stack placements are best-effort: their errors
   are dropped so a decoration never fails the dashboard.
2. Unbounded per-item fan-out (N repos × sub-query, e.g. `history`
   or `statusCheckRollup` inline on `repoFields`) is forbidden. Use a
   drill-in query per selected item, or bounded fan-out over config-listed
   items behind a semaphore (`watchedRepoConcurrency`).
3. When all results are needed (`FetchPRDetail`), use a
   `context.WithCancel` child and `sync.Once` for the first error so a
   sibling's cancellation echo cannot mask Auth/RateLimit/5xx.
   Best-effort branches degrade instead of aborting (`FetchRepoDetail`'s
   star-history walk).
4. A new field: run the full query five times on the busiest account and
   read the spread against 10 s; one green run near the clock proves
   nothing. Past ~7 s on the list fetch it gets its own branch.
5. Per-repo data goes in the detail view first; add a list column only if
   still needed.
6. Transient 5xx and HTTP/2 transport errors classify as `ReasonServer` in
   `classifyErr`; fetch paths wrap in `RetryTransient` (`retry.go`), which
   retries only that. Teach new transport error strings to `classifyErr`.
   A query that times out on its own weight is not transient: fix it,
   don't lean on the retry.

## Hermetic fetch tests

GraphQL paths reuse `newTestGQLClient` (`watched_repo_fetch_test.go`), which
points a `githubv4.Client` at `httptest` through `rewriteHost`; REST paths
point `Client.rest` the same way and dispatch on path (`capability_test.go`).
