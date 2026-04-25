# Honker — Pre-Implementation Research

> Captured during Phase 0. The Phase 0 decision is to **adopt Honker** as the
> production queue/scheduler backend behind `internal/jobs.Queue` and
> `internal/jobs.Scheduler` interfaces, with a `MemoryQueue` test stub.

## Summary

Honker is a SQLite-native task runtime (queues, streams, pub/sub, scheduler)
implemented as a Rust SQLite extension (`libhonker_ext.so`) with a
mattn/go-sqlite3-based Go binding (`github.com/russellromney/honker-go`).

- **Queue API:** `db.Queue(name, opts)` returns a `*Queue` with
  `Enqueue(payload, opts)`, `ClaimOne(workerID)`, `ClaimBatch`, `ClaimWaker`,
  `AckBatch`, `SweepExpired`. Per-job methods: `Job.Ack()`, `Fail(errMsg)`,
  `Retry(delaySec, errMsg)`, `Heartbeat(extendSec)`.
- **Scheduler API:** `db.Scheduler().Add(ScheduledTask)`,
  `Run(ctx, owner)` (leader-elected), `Tick`, `Soonest`, `Remove`.
- **Database initialisation:** `honker.Open(path, extensionPath)` opens a
  `*honker.Database` that owns the underlying `*sql.DB`. Application SQL goes
  through `db.Raw()`. Honker tables (`_honker_*`) coexist with app tables in
  the same SQLite file.

## Findings — non-obvious

1. **Sub-2ms wake latency is real.** `wal_watcher_unix.go` polls
   `PRAGMA data_version` every 1ms with a 100ms `stat()` dead-man switch.
   `ClaimWaker.Next` blocks on a channel fed by the watcher. (Not used in
   Phase 0; Phase 1 worker loops will use it.)

2. **Per-host concurrency is NOT built-in.** The design spec assumed it was.
   The closest built-in is `db.TryRateLimit(name, limit, perSec)` (fixed
   window). Phase 1 must DIY per-host limiting on top.

3. **Exponential backoff is DIY.** `Job.Retry(delaySec, errMsg)` takes a
   literal delay in seconds, not a strategy. Phase 1 will compute the delay
   based on `Job.Attempts`.

4. **CGo + Rust extension required.** Honker's loadable extension is a Rust
   cdylib with the package name `honker-extension` and `[lib] name = "honker_ext"`,
   so the artifact is `libhonker_ext.{so,dylib}`. Cross-compilation requires
   both the CGo toolchain and a Rust cross-build per target.

5. **`honker-go` is alpha and license-incomplete.** The repo currently ships
   without a top-level `LICENSE` file; the parent `russellromney/honker` repo
   is Apache-2.0. pkg.go.dev refuses to render the binding's docs. We accept
   this risk for Phase 0 and track upstream; if it deteriorates we can swap
   the backend behind the `Queue`/`Scheduler` interfaces.

6. **macOS dev caveat.** Honker open issue #4 reports a SQLite-locking CI
   hang on macOS. Phase 0 integration tests skip on missing extension paths,
   and Phase 1 unit tests use `MemoryQueue` so macOS dev is unblocked even
   if Honker integration is flaky locally.

## Build pipeline

`scripts/build-honker-extension.sh` clones `russellromney/honker`, runs
`cargo build -p honker-extension --release`, and copies
`target/release/libhonker_ext.{so,dylib}` to `./build/`. The Dockerfile does
the same in a Rust build stage and copies the cdylib into the Alpine runtime
image.

## Phase 1 deltas (planned)

- Wrap `db.TryRateLimit` to give per-host rate limiting on `feed.poll` jobs.
- Implement exponential backoff in the worker loop using `Job.Attempts`.
- Add the `feed.poll` and `entry.extract` cron and queue handlers.
- Replace the Phase 0 canary `wire.heartbeat` cron with the real polling cron.

## References

- Repo: <https://github.com/russellromney/honker>
- Go binding: <https://github.com/russellromney/honker-go>
- pkg.go.dev: <https://pkg.go.dev/github.com/russellromney/honker-go>
