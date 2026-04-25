-- Wire initial schema. See design.md §4.

-- Users
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

-- Categories
CREATE TABLE categories (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE(user_id, name)
);

-- Icons (deduplicated favicons)
CREATE TABLE icons (
    id        INTEGER PRIMARY KEY,
    hash      TEXT    NOT NULL UNIQUE,
    mime_type TEXT    NOT NULL,
    content   BLOB    NOT NULL
);

-- Feeds
CREATE TABLE feeds (
    id                   INTEGER PRIMARY KEY,
    user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id          INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    icon_id              INTEGER REFERENCES icons(id) ON DELETE SET NULL,
    title                TEXT    NOT NULL,
    feed_url             TEXT    NOT NULL,
    site_url             TEXT,
    description          TEXT,
    etag                 TEXT,
    last_modified        TEXT,
    last_polled_at       INTEGER,
    next_poll_at         INTEGER,
    poll_interval        INTEGER NOT NULL DEFAULT 3600,
    error_count          INTEGER NOT NULL DEFAULT 0,
    last_error           TEXT,
    weekly_entry_count   INTEGER NOT NULL DEFAULT 0,
    crawler              INTEGER NOT NULL DEFAULT 0,
    scraper_rules        TEXT,
    disabled             INTEGER NOT NULL DEFAULT 0,
    ignore_entry_updates INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at           INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE(user_id, feed_url)
);
CREATE INDEX idx_feeds_user_category ON feeds(user_id, category_id);
CREATE INDEX idx_feeds_next_poll ON feeds(next_poll_at)
    WHERE disabled = 0 AND error_count < 10;

-- Entries
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

-- Entry tombstones (prevent re-import after deletion)
CREATE TABLE entry_tombstones (
    feed_id    INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    hash       TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (feed_id, hash)
);

-- Enclosures (media attachments)
CREATE TABLE enclosures (
    id        INTEGER PRIMARY KEY,
    entry_id  INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    url       TEXT    NOT NULL,
    mime_type TEXT    NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    UNIQUE(entry_id, url)
);

-- FTS5 contentless index over entries.title and entries.content
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, content,
    content=entries,
    content_rowid=id,
    tokenize='unicode61'
);

CREATE TRIGGER entries_fts_insert AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

CREATE TRIGGER entries_fts_delete AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
END;

CREATE TRIGGER entries_fts_update AFTER UPDATE OF title, content ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
    INSERT INTO entries_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;
