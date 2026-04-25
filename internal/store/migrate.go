package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/bcrisp4/wire/internal/store/schema"
)

const migrationsTable = `CREATE TABLE IF NOT EXISTS _migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    applied_at INTEGER NOT NULL DEFAULT (unixepoch())
)`

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies all embedded SQL migrations not yet recorded in _migrations.
// Idempotent: calling Migrate twice in a row is a no-op the second time.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, migrationsTable); err != nil {
		return fmt.Errorf("migrate: bootstrap: %w", err)
	}
	migrations, err := loadMigrations(schema.FS)
	if err != nil {
		return err
	}
	applied, err := loadApplied(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("migrate: %d %s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	seen := map[int]string{}
	var ms []migration
	for _, name := range names {
		// Filename: NNNN_name.sql
		prefix, _, ok := strings.Cut(name, "_")
		if !ok || prefix == "" {
			return nil, fmt.Errorf("malformed migration name %q (want NNNN_name.sql)", name)
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("non-numeric prefix in %q: %w", name, err)
		}
		if dup, ok := seen[v]; ok {
			return nil, fmt.Errorf("duplicate migration version %d: %q and %q", v, dup, name)
		}
		seen[v] = name
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

func loadApplied(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM _migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func apply(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO _migrations(version, name) VALUES (?, ?)", m.version, m.name); err != nil {
		return err
	}
	return tx.Commit()
}
