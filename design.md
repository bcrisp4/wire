# Wire — Design Spec

**Working name:** Wire
**Date:** 2026-04-25
**Status:** Draft
**Go module:** `github.com/bcrisp4/wire`

A self-hosted feed reader built for speed, simplicity, and offline reading.
Inspired by Miniflux but opinionated differently: SQLite instead of PostgreSQL,
a Svelte SPA for rich offline support, and a focus on being a single
self-contained binary with zero external infrastructure.

---

## 1. Goals & Constraints

### Goals

- Fast, content-focused reading experience — mobile-first, works on iPhone home screen as a PWA
- Genuine offline support — read cached articles with no connectivity, sync state on reconnect
- Self-contained deployment — single binary, single SQLite file, single container volume
- Privacy by default — no tracking, no external requests beyond feed fetching
- Easy data portability — OPML import/export, REST API, no lock-in
- Built with TDD methodology

### Hard constraints

- SQLite for storage (no PostgreSQL, no external database)
- Runs in a Linux container image (as small as possible)
- Light/dark mode + system theme following
- Serif/sans-serif font toggle for reading
- REST API

### Non-goals (for now)

- Multi-user / authentication (schema is multi-user ready, but no auth in v1)
- Third-party integrations (Pinboard, Telegram, etc.)
- Fever / Google Reader API compatibility
- i18n / multiple languages

---

## 2. Architecture

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
│  │  Service Worker → IndexedDB (offline cache) │ │
│  └─────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

**Single process, three concerns:**

1. **HTTP server** (stdlib `net/http`) serves the REST API under `/api/v1/`
   and the embedded Svelte SPA for all other routes.

2. **Background scheduler** uses Honker's cron primitive to trigger feed polls
   at adaptive intervals. Each poll is a Honker queue job. Feed fetch, parse,
   content extraction, and entry storage happen within a transactional boundary.

3. **SQLite** is the single data store. WAL mode for concurrent reads. FTS5 for
   full-text search. Honker's tables coexist in the same database file.

**The Svelte SPA** is compiled at build time (SvelteKit + Vite) and embedded
via `go:embed`. A service worker caches articles to IndexedDB for offline
reading. State changes made offline are queued and synced on reconnect.

---

## 3. Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Backend language | Go | Single binary, fast, known, good SQLite ecosystem |
| Frontend framework | Svelte (SvelteKit) | Small bundles (no runtime), good PWA/service worker support, compiles to vanilla JS |
| Database | SQLite (WAL mode) | Zero external infra, single file, fast for read-heavy workloads |
| Full-text search | FTS5 (SQLite extension) | BM25 ranking, prefix queries, contentless mode |
| Background jobs | Honker (SQLite extension) | Transactional job queues, cron scheduling, sub-2ms wake latency |
| Content extraction | Go Readability port (from Miniflux) | Server-side article extraction for full-content feeds |
| Feed parsing | Go library (TBD — gofeed or similar) | RSS 1.0/2.0, Atom, JSON Feed support |
| Container base | Alpine (musl libc) | Small image, provides libc for CGo |

### CGo dependency

SQLite and Honker both require CGo (`CGO_ENABLED=1`). This means the binary
isn't fully static and needs libc at runtime. Alpine with musl is the pragmatic
choice for the container image.

---

## 4. Data Model

### Pragmas (set at connection time)

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
```

### Users

Single default user now, multi-user ready. Preferences stored as columns
(Miniflux pattern — avoids joins on every request).

```sql
CREATE TABLE users (
    id               INTEGER PRIMARY KEY,
    username         TEXT    NOT NULL UNIQUE,
    theme            TEXT    NOT NULL DEFAULT 'system',
    font             TEXT    NOT NULL DEFAULT 'serif',
    entries_per_page INTEGER NOT NULL DEFAULT 50,
    default_sort     TEXT    NOT NULL DEFAULT 'published_at',
    default_order    TEXT    NOT NULL DEFAULT 'desc',
    created_at       INTEGER NOT NULL DEFAULT (unixepoch())
);
```

### Categories

```sql
CREATE TABLE categories (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE(user_id, name)
);
```

No separate index needed — `UNIQUE(user_id, name)` covers `WHERE user_id = ?`.

### Icons

Deduplicated favicons. Multiple feeds from the same site share one icon row.

```sql
CREATE TABLE icons (
    id        INTEGER PRIMARY KEY,
    hash      TEXT    NOT NULL UNIQUE,
    mime_type TEXT    NOT NULL,
    content   BLOB   NOT NULL
);
```

### Feeds

```sql
CREATE TABLE feeds (
    id                   INTEGER PRIMARY KEY,
    user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id          INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    icon_id              INTEGER REFERENCES icons(id) ON DELETE SET NULL,
    title                TEXT    NOT NULL,
    feed_url             TEXT    NOT NULL,
    site_url             TEXT,
    description          TEXT,
    -- Polling state
    etag                 TEXT,
    last_modified        TEXT,
    last_polled_at       INTEGER,
    next_poll_at         INTEGER,
    poll_interval        INTEGER NOT NULL DEFAULT 3600,
    error_count          INTEGER NOT NULL DEFAULT 0,
    last_error           TEXT,
    -- Adaptive polling
    weekly_entry_count   INTEGER NOT NULL DEFAULT 0,
    -- Content extraction
    crawler              INTEGER NOT NULL DEFAULT 0,
    scraper_rules        TEXT,
    -- Behaviour flags
    disabled             INTEGER NOT NULL DEFAULT 0,
    ignore_entry_updates INTEGER NOT NULL DEFAULT 0,
    --
    created_at           INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at           INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE(user_id, feed_url)
);
CREATE INDEX idx_feeds_user_category ON feeds(user_id, category_id);
CREATE INDEX idx_feeds_next_poll ON feeds(next_poll_at)
    WHERE disabled = 0 AND error_count < 10;
```

Design notes:
- `category_id ON DELETE SET NULL` — deleting a category doesn't delete feeds
- `crawler` defaults to 0 (off) — user enables per-feed for partial-content feeds
- `scraper_rules` — optional custom CSS selectors, falls back to Readability
- Partial index on `next_poll_at` excludes disabled and broken feeds from the poller's scan

### Entries

```sql
CREATE TABLE entries (
    id            INTEGER PRIMARY KEY,
    feed_id       INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hash          TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    url           TEXT,
    comments_url  TEXT,
    author        TEXT,
    summary       TEXT,
    content       TEXT,
    published_at  INTEGER,
    reading_time  INTEGER NOT NULL DEFAULT 0,
    read          INTEGER NOT NULL DEFAULT 0,
    read_at       INTEGER,
    saved         INTEGER NOT NULL DEFAULT 0,
    saved_at      INTEGER,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    changed_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE(feed_id, hash)
);
CREATE INDEX idx_entries_user_unread ON entries(user_id, read, published_at DESC);
CREATE INDEX idx_entries_user_pub    ON entries(user_id, published_at DESC);
CREATE INDEX idx_entries_user_saved  ON entries(user_id, saved, saved_at DESC)
    WHERE saved = 1;
CREATE INDEX idx_entries_feed_pub    ON entries(feed_id, published_at DESC);
```

Design notes:
- `hash` — computed dedup key (more robust than feed-provided GUIDs)
- `user_id` — denormalised from feeds for query efficiency on the river view
- `comments_url` — separate discussion URL for HN/Reddit-style feeds
- `reading_time` — computed at ingest time (words / reading speed)
- `changed_at` — tracks when content was last updated (distinct from created_at)
- `read` and `saved` are independent booleans (a saved entry can be read or unread)
- Partial index on saved entries — only indexes the small subset that are saved

Index coverage:

| Query | Index |
|-------|-------|
| River (unread, chronological) | `idx_entries_user_unread` |
| All entries (chronological) | `idx_entries_user_pub` |
| Saved entries | `idx_entries_user_saved` (partial) |
| Per-feed entries | `idx_entries_feed_pub` |
| Duplicate detection (polling) | `UNIQUE(feed_id, hash)` |

### Entry Tombstones

Prevents re-importing entries after deletion (Miniflux pattern).

```sql
CREATE TABLE entry_tombstones (
    feed_id    INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    hash       TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (feed_id, hash)
);
```

### Enclosures

Media attachments (podcast audio, video, images).

```sql
CREATE TABLE enclosures (
    id        INTEGER PRIMARY KEY,
    entry_id  INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    url       TEXT    NOT NULL,
    mime_type TEXT    NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    UNIQUE(entry_id, url)
);
```

### Full-Text Search

Contentless FTS5 — the index references the entries table rather than
duplicating content. Requires sync triggers.

```sql
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, content,
    content=entries,
    content_rowid=id,
    tokenize='unicode61'
);

CREATE TRIGGER entries_fts_insert AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts(rowid, title, content)
    VALUES (new.id, new.title, new.content);
END;

CREATE TRIGGER entries_fts_delete AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, content)
    VALUES ('delete', old.id, old.title, old.content);
END;

CREATE TRIGGER entries_fts_update AFTER UPDATE OF title, content ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, content)
    VALUES ('delete', old.id, old.title, old.content);
    INSERT INTO entries_fts(rowid, title, content)
    VALUES (new.id, new.title, new.content);
END;
```

### Honker Tables

Honker creates and manages its own tables (`_honker_live`, `_honker_dead`,
etc.) when initialised. They coexist in the same SQLite database file.

---

## 5. Background Scheduler & Feed Polling

```
Honker Cron (every 60s)
    │
    ▼
Poll Dispatcher
    │ SELECT feeds due for checking
    │ (idx_feeds_next_poll, partial index)
    │
    ▼
Honker Queue: "feed.poll"
    │ One job per feed, with per-host concurrency limits
    │
    ├─▶ Fetch feed (HTTP GET with ETag/Last-Modified)
    │     304 Not Modified? → update next_poll_at, done
    │
    ├─▶ Parse feed (RSS/Atom/JSON Feed)
    │     Check each entry hash against entries + tombstones
    │     Insert new entries (summary from feed)
    │
    ├─▶ For feeds with crawler=1, enqueue content extraction:
    │     Honker Queue: "entry.extract"
    │     One job per new entry
    │
    └─▶ Update feed state:
          next_poll_at (adaptive formula)
          weekly_entry_count
          etag, last_modified
          error_count (reset on success)
```

All steps within a single transaction. Feed fetch → parse → insert entries →
enqueue extraction jobs → update feed state. Commits atomically.

### Adaptive polling

Formula (from Miniflux):

```
interval = (7 days / weekly_entry_count) / factor
clamped to [15 minutes, 24 hours]
```

Layered with HTTP cache signals:
- ETag / If-None-Match — skip parsing if content unchanged (304)
- Last-Modified / If-Modified-Since — date-based variant
- Cache-Control max-age — server-suggested recheck interval
- Retry-After (429/503) — server-mandated backoff, always respected

### Error handling

- **Transient errors** (timeout, DNS, 5xx): increment `error_count`,
  exponential backoff on `next_poll_at`
- **Persistent errors** (10+ consecutive failures): stop polling. Feed stays
  in DB, can be manually retried via the API or UI.
- **Content extraction failures**: logged but don't block the entry from being
  saved with its summary.

### Politeness

- Per-host concurrency limits via Honker queue configuration
- Conditional HTTP requests (ETag, Last-Modified) to minimise bandwidth
- Respect Retry-After headers

---

## 6. REST API

**Base path:** `/api/v1`

### Feeds

| Method | Path | Description |
|--------|------|-------------|
| GET | `/feeds` | List all feeds (with unread counts) |
| POST | `/feeds` | Subscribe to a new feed |
| GET | `/feeds/:id` | Get feed details |
| PUT | `/feeds/:id` | Update feed (title, category, crawler, etc.) |
| DELETE | `/feeds/:id` | Unsubscribe (deletes entries + tombstones) |
| POST | `/feeds/:id/refresh` | Trigger immediate poll |

### Categories

| Method | Path | Description |
|--------|------|-------------|
| GET | `/categories` | List categories (with unread counts) |
| POST | `/categories` | Create category |
| PUT | `/categories/:id` | Rename category |
| DELETE | `/categories/:id` | Delete (feeds become uncategorised) |

### Entries

| Method | Path | Description |
|--------|------|-------------|
| GET | `/entries` | List entries (filterable, paginated) |
| GET | `/entries/:id` | Get single entry with full content |
| PUT | `/entries/:id` | Update entry state (read, saved) |
| PUT | `/entries/read` | Bulk mark as read (by feed, category, or all) |

Entry listing query params:

| Param | Values | Default |
|-------|--------|---------|
| `status` | `unread`, `read`, `all` | `unread` |
| `saved` | `true`, `false` | (not filtered) |
| `feed_id` | integer | (not filtered) |
| `category_id` | integer | (not filtered) |
| `sort` | `published_at`, `created_at` | `published_at` |
| `order` | `asc`, `desc` | `desc` |
| `limit` | integer | 50 |
| `offset` | integer | 0 |

Entry list responses exclude `content` (full article body) to keep payloads
small. Full content is fetched via `GET /entries/:id`.

### Search

| Method | Path | Description |
|--------|------|-------------|
| GET | `/search?q=...` | Full-text search across entries |

### OPML

| Method | Path | Description |
|--------|------|-------------|
| POST | `/opml/import` | Import feeds from OPML file |
| GET | `/opml/export` | Export all feeds as OPML |

### Discovery

| Method | Path | Description |
|--------|------|-------------|
| POST | `/feeds/discover` | Find feed URLs from a website URL |

---

## 7. Frontend (Svelte SPA)

### Views

| View | Route | Description |
|------|-------|-------------|
| River | `/` | Chronological unread entries (default home) |
| All entries | `/all` | Includes read entries |
| Saved | `/saved` | Saved/starred entries |
| Category | `/category/:id` | Entries for a category |
| Feed | `/feed/:id` | Entries for a single feed |
| Article | `/entry/:id` | Reader view + "view original" button |
| Search | `/search?q=...` | Search results |
| Settings | `/settings` | Theme, font, feed management, OPML |
| Add feed | `/feeds/add` | URL → autodiscovery → subscribe |

### Reading experience

- Clean reader view with serif typography (configurable)
- "View original" button opens source URL in new tab
- Reading time displayed per article
- Feed icon + source name in article metadata

### Interactions

- **Mark as read**: explicit button/tap only (no auto-mark on scroll)
- **Swipe gestures (mobile)**: swipe right → mark read/unread, swipe left → save/unsave
- **Keyboard shortcuts (desktop)**: `j`/`k` navigate, `m` toggle read, `s` save, `v` view original, `o` open article
- **Bulk actions**: mark all read for feed/category/everything
- **Pull-to-refresh**: triggers feed refresh on mobile
- **Infinite scroll**: paginated via API offset/limit

### Theming

- Light / dark / follow system (`prefers-color-scheme`)
- Serif / sans-serif toggle for reading
- CSS custom properties — theme switching is instant, no reload
- Warm, content-focused palette

### Offline

- Service worker caches the app shell on first load (PWA opens instantly)
- API responses cached to IndexedDB (articles with full content)
- Configurable: proactively cache the N most recent entries (default 200)
- Offline reads from IndexedDB; state changes queued and synced on reconnect

### PWA

- Web app manifest with icon for iOS home screen
- `display: standalone` — looks like a native app
- Status bar styling matches the active theme

---

## 8. Deployment

### Container build

```
Multi-stage Dockerfile:

Stage 1: Node        → npm ci, npm run build (Svelte → static assets)
Stage 2: Go + CGo    → copy assets into embed dir, go build
Stage 3: Alpine      → copy binary, minimal runtime

Target: < 30MB final image
```

CGo is required (SQLite + Honker). Alpine provides musl libc.

### 12-Factor compliance

| Factor | Implementation |
|--------|---------------|
| Codebase | One repo, one deployable |
| Dependencies | Go modules + npm lockfile |
| Config | Environment variables |
| Backing services | SQLite co-located (not networked) |
| Build/release/run | Dockerfile builds; container image is the release |
| Processes | Single process |
| Port binding | Configurable HTTP port (default 8080) |
| Concurrency | Single process, goroutines internally |
| Disposability | Fast startup, graceful shutdown |
| Dev/prod parity | Same binary, same SQLite |
| Logs | Structured JSON to stdout |
| Admin processes | CLI subcommands |

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WIRE_DB_PATH` | `./wire.db` | SQLite database file path |
| `WIRE_LISTEN` | `:8080` | HTTP listen address |
| `WIRE_LOG_LEVEL` | `info` | debug, info, warn, error |
| `WIRE_LOG_FORMAT` | `json` | json, text |

### Data persistence

Single volume mount for the SQLite database file. Backup = copy the file
(or `VACUUM INTO` for a consistent snapshot).

### Graceful shutdown

On SIGTERM: stop accepting connections → drain in-flight requests → let current
poll job finish (30s timeout) → close database.

---

## 9. Pre-Implementation Research

Before writing code, two areas need deep investigation:

### Honker Go bindings

**Source:** `https://github.com/russellromney/honker.git`

Investigate:
- Go binding API surface and maturity
- How to initialise Honker on an existing SQLite connection
- Cron job scheduling API
- Queue job enqueue/claim/complete API
- Per-queue concurrency configuration (needed for per-host rate limiting)
- Error handling and dead-lettering behaviour
- How Honker tables coexist with application tables
- CGo build requirements and cross-compilation story

### Miniflux Readability port

**Source:** `https://github.com/miniflux/v2.git`

Investigate:
- Location and structure of the Go Readability implementation
- How it integrates with feed processing (when is extraction triggered?)
- Custom CSS selector rules — how are they configured and applied?
- Image handling (lazy loading, srcset, proxying)
- HTML sanitisation pipeline
- What it does with extraction failures
- Whether it can be extracted as a standalone package or needs adaptation

---

## 10. Inspiration

- **Miniflux** — architecture, adaptive polling, Readability extraction,
  tombstone pattern, privacy approach
- **Wiki research** — SQLite concepts, FTS5, Honker, adaptive polling,
  PRAGMA data_version polling, partial index queue design, web content
  extraction (Readability vs Defuddle)
