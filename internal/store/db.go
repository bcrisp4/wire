// Package store wraps SQLite access with the connection-time pragmas Wire requires.
package store

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// Open returns a *sql.DB pointed at path with WAL, foreign keys, and a 5s busy timeout.
//
// In production, the application uses the *sql.DB owned by Honker (jobs.HonkerBackend.RawDB()).
// This helper exists for tests, ad-hoc CLI tools, and any caller that does not need Honker.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("store: empty database path")
	}
	q := url.Values{}
	q.Set("_journal_mode", "WAL")
	q.Set("_foreign_keys", "ON")
	q.Set("_busy_timeout", "5000")
	q.Set("_synchronous", "NORMAL")
	dsn := fmt.Sprintf("file:%s?%s", path, q.Encode())
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	// SQLite serializes writes regardless of pool size. Cap at one connection so
	// tests don't see SQLITE_BUSY churn on concurrent goroutine writes; under WAL
	// this still gives concurrent readers via the same connection.
	db.SetMaxOpenConns(1)
	return db, nil
}
