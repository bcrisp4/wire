# Wire — Architecture

Wire is a single Go binary that embeds a SvelteKit SPA and uses SQLite as
its only datastore. The binary owns three concerns inside one process:

```
┌──────────────────────────────────────────────────┐
│                   Go Binary                       │
│                                                   │
│  ┌─────────────┐  ┌──────────────────────────┐   │
│  │  HTTP Server │  │  Background Scheduler     │   │
│  │  (net/http)  │  │  (Honker cron + queues)   │   │
│  │             │  │                           │   │
│  │  /api/v1/*  │  │  • Feed polling (adaptive)│   │
│  │  /*  (SPA)  │  │  • Content extraction     │   │
│  └──────┬──────┘  └────────────┬──────────────┘   │
│         │                      │                   │
│         └──────────┬───────────┘                   │
│                    │                               │
│         ┌──────────▼──────────┐                   │
│         │    SQLite (WAL)     │                   │
│         │  + FTS5  + Honker   │                   │
│         └─────────────────────┘                   │
│                                                   │
│  ┌─────────────────────────────────────────────┐ │
│  │  Embedded Svelte SPA (go:embed)             │ │
│  │  Service Worker → IndexedDB (Phase 1)       │ │
│  └─────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

## Single process, three concerns

1. **HTTP server** (stdlib `net/http`) serves the REST API under `/api/v1/`
   and the embedded Svelte SPA for all other routes.
2. **Background scheduler** uses Honker's cron primitive to trigger feed polls
   at adaptive intervals. Each poll is a Honker queue job. Feed fetch, parse,
   content extraction, and entry storage happen within a transactional
   boundary. *(Phase 1; Phase 0 only registers a canary heartbeat.)*
3. **SQLite** is the single data store. WAL mode for concurrent reads. FTS5
   for full-text search. Honker's tables (`_honker_*`) coexist in the same
   database file. Honker owns the `*sql.DB`; the application reads/writes via
   `db.Raw()`.

## Phase 0 vs Phase 1

This repository is currently at **Phase 0** (foundation). The skeleton, schema,
queue/scheduler abstraction, and HTTP/SPA serving are in place, but no real
feed polling or content extraction has been wired up yet. Phase 1 is a
follow-up batch that fans out per-resource workers (feeds API, entries API,
search, OPML, river view, reader view, etc.) against this foundation.

See [`design.md`](../design.md) for the full design spec, and the research
notes in this directory for backend choices.

## Package layout

```
cmd/wire/                  — CLI entry; subcommand dispatch (serve, migrate)
internal/api/              — HTTP server, middleware, /api/v1/health, SPA handler
internal/config/           — env-var loader with validation
internal/jobs/             — Queue + Scheduler interfaces; Honker prod backend; MemoryQueue test stub
internal/logger/           — slog wrapper (json|text)
internal/model/            — domain types (User, Category, Feed, Entry, Icon, Tombstone, Enclosure)
internal/store/            — SQLite open + migration runner; per-resource Repo interfaces
internal/store/schema/     — embedded *.sql migration files
internal/web/              — //go:embed dist of the SvelteKit SPA
web/                       — SvelteKit project; adapter-static writes to internal/web/dist
scripts/                   — operational scripts (build-honker-extension.sh)
docs/                      — this document plus research notes
```

## Build and runtime requirements

- **Go 1.22+** with **CGo enabled** — required by `mattn/go-sqlite3`.
- Build tags `sqlite_fts5` (FTS5 indexing) and `sqlite_load_extension`
  (runtime extension loading for Honker).
- **Honker SQLite extension** (`libhonker_ext.{so,dylib}`) must be available
  at the path in `WIRE_HONKER_EXTENSION_PATH`. Build it once with
  `make extension`; the Dockerfile builds it inline.
- **Node 20 / npm** to build the SPA (only at build time; not needed at runtime).
- **Rust toolchain** to build the Honker extension (only at build time).

## Configuration

All runtime configuration is via environment variables; the binary reads them
on startup.

| Variable | Default | Description |
|----------|---------|-------------|
| `WIRE_DB_PATH` | `./wire.db` | SQLite file path |
| `WIRE_LISTEN` | `:8080` | HTTP listen address |
| `WIRE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `WIRE_LOG_FORMAT` | `json` | `json` \| `text` |
| `WIRE_HONKER_EXTENSION_PATH` | `./build/libhonker_ext.so` | Honker SQLite extension cdylib path |
