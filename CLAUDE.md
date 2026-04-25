# Multi-PR coordination

Most merge-conflict pain comes from structural decisions made before the PRs were opened. Apply this before fanning work out.

## Always base PRs on `main`

Never set `base = some-integration-branch`. The "clean diff" benefit is a false economy: GitHub will merge a PR into its literal base, so an integration-branch base means work lands on an orphan, not on `main`. When the integration branch is later deleted, dependent PRs may be closed outright instead of auto-retargeted.

If a unit truly needs another's code to compile, sequence them. Don't parallelize.

## Land cross-cutting infrastructure first

Before fanning out N parallel PRs that touch the same package, add a foundation commit that owns the shared scaffolding:

- **Test helpers** — one `fakeStore` (or whatever) lives in the foundation. Don't let four agents each invent their own at package scope; they'll collide on the second merge.
- **Package-scope constants** — `defaultUserID`, `writeJSON`, error sentinels. Pick one home up front.
- **Shared interfaces** — declared once, implemented N times.

## Make shared mutation points list-driven

If you can predict that N PRs will each register something in the same function, refactor it to a list before the parallel work starts. Adjacent insertions inside a function body conflict; one-line additions to a list don't.

```go
// Bad: each PR conflicts with the next on adjacent lines.
func NewServer(opts Options) (*Server, error) {
    if opts.Store != nil { registerCategoryRoutes(mux, ...) }
    if opts.Store != nil && opts.Queue != nil { s.registerFeedRoutes(mux) }
    // ... every new unit conflicts here ...
}

// Good: each PR adds one line; insertion order is explicit and reviewable.
var routeRegistrars = []func(*http.ServeMux, Options){
    registerCategoryRoutes,
    registerFeedRoutes,
    // ...
}
```

Same applies to `internal/jobs/queue.go` queue-name constants, scheduled tasks, and worker goroutines in `cmd/wire/serve.go`.

## Keep branches short-lived

`go.mod` and `go.sum` are merge-hostile: conflict markers make `go.mod` fail to parse, so `go mod tidy` can't run until you fix it by hand. Long-lived branches cross every dep change on `main` and pay this tax repeatedly.

- Rebase daily, not at merge time.
- Prefer a steady trickle of small PRs over a single big batch.

## Worktree patterns under parallel-agent workloads

Locked worktrees in `.claude/worktrees/` hold branches that you can't check out elsewhere. Use a detached worktree and push to the remote ref directly:

```bash
git worktree add --detach /tmp/work origin/some-branch
cd /tmp/work
# edits, commits
git push origin HEAD:some-branch
```

`gh pr merge --delete-branch` will print `failed to delete local branch ... used by worktree at ...` when a locked worktree owns the branch. The remote merge and branch deletion succeeded; this is cosmetic. Verify with `gh pr view N --json state,mergedAt`.

## Briefing parallel agents

When N agents will edit the same file, the prompt must:

1. Name the file and where additions go (e.g., "append your constant at the end of the `const` block").
2. Specify delimited markers (`// Unit N: <thing>` … `// Unit N: end`) so rebases stay mechanical when conflicts do happen.
3. Forbid declaring package-scope types or constants that another parallel unit might also declare. If a helper is needed, prefix it with the unit (e.g., `searchFakeStore`, not `fakeStore`).

# Common pitfalls

Recurring bug classes from prior review passes. Linters won't catch most of these — they need file-level judgment. Skim before writing in the relevant area.

## HTTP boundaries

- **Status class matters**: bad input is 4xx, dependency failure is 5xx. Don't return 500 for FK violations or 502 for validation errors. Map driver errors to sentinels in the store layer; map sentinels to status codes in the handler.
- **Strict JSON decode**: handlers that take a body should `decoder.DisallowUnknownFields()` and require `io.EOF` after the first `Decode` (reject trailing data). Both are cheap.
- **Reject non-positive path IDs early**: `int64` IDs `<= 0` should 400 at parse, not round-trip to the store for a 404.

## Store / DB layer

- **Wrap `sql.ErrNoRows`** as `store.ErrNotFound` at the boundary. Callers shouldn't see driver sentinels.
- **`UPDATE`/`DELETE` must check `RowsAffected`**: a query that touched zero rows is a silent no-op — usually wrong. Return `ErrNotFound` when `n == 0` for single-row operations.
- **Map constraint errors via `errors.As`**: `errors.As(err, &sqlite3.Error)` + `ExtendedCode == sqlite3.ErrConstraintUnique` (or `ErrConstraintForeignKey`). Never parse error strings.

## Workers / async

- **Don't swallow `Job.Ack` / `Job.Fail` errors** — a failed Ack leaves the job stuck claimed. Log it or `errors.Join` it into the surrounding error path.
- **Cron-tick payloads vs work payloads**: scheduled ticks come in with a sentinel/empty payload. Distinguish "dispatcher firing" from "real work" before unmarshalling.
- **Sanitizers/transforms must guard empty output**: if a transform returns `""` on parse error and the upstream input was non-empty, you'll silently overwrite real content. Treat empty-out / non-empty-in as an error and skip the update.

## URL handling

- **`u.Hostname()` for keys, not `u.Host`**: `u.Host` includes `:port`. Rate-limit buckets, predefined-rule lookups, etc. will diverge for `example.com` vs `example.com:443`. Use `Hostname()` unless you specifically need the port.
- **SSRF guard on user-controllable URLs**: any URL from a feed entry, user input, or HTTP redirect chain must be checked against private/loopback/link-local/cloud-metadata addresses *after* DNS resolution and *on every redirect*. Pattern: `internal/discover/discover.go` `validateURL`.

## Concurrency / shared state

- **No exported mutable package-scope state**: `var TickPayload json.RawMessage = ...` is a footgun — any caller can mutate it for everyone. Use a function returning a fresh value.
- **Don't alias loop-iteration pointers**: `for _, item := range items { out = append(out, T{Field: &now}) }` aliases one `*int64` across every element. Copy `now` into a fresh local inside the loop body.

## Numeric correctness

- **Overflow when computing caps**: `data[:n+1]` wraps when `n == math.MaxInt64`. Saturate or check explicitly.
- **`strconv.Atoi` failure ≠ "use default"**: `Atoi("99999999999999999999")` returns `ErrRange` and your default-on-error branch will silently miss the cap. Use `ParseInt` and treat `ErrRange` distinctly from `ErrSyntax`.
- **Validate non-positive option inputs at construction**: `WithTimeout(0)` produces an immediately-expired context; `WithMaxBodyBytes(-1)` makes every read fail. Reject or clamp at option-application time.
