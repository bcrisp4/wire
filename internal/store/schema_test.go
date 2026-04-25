package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openMigrated(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wire.db")
	db, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, Migrate(context.Background(), db))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSchema_TablesPresent(t *testing.T) {
	db := openMigrated(t)
	want := []string{
		"users", "categories", "icons", "feeds", "entries",
		"entry_tombstones", "enclosures", "entries_fts",
	}
	for _, name := range want {
		var got string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE name = ?", name).Scan(&got)
		assert.NoError(t, err, "table %s missing", name)
		assert.Equal(t, name, got)
	}
}

func TestSchema_IndexesPresent(t *testing.T) {
	db := openMigrated(t)
	want := []string{
		"idx_feeds_user_category", "idx_feeds_next_poll",
		"idx_entries_user_unread", "idx_entries_user_pub",
		"idx_entries_user_saved", "idx_entries_feed_pub",
	}
	for _, name := range want {
		var got string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name = ?", name).Scan(&got)
		assert.NoError(t, err, "index %s missing", name)
	}
}

func TestSchema_FTS5TriggersFire(t *testing.T) {
	db := openMigrated(t)
	_, err := db.Exec(`INSERT INTO feeds(user_id, title, feed_url) VALUES (1, 'F', 'https://x/')`)
	require.NoError(t, err)
	res, err := db.Exec(`INSERT INTO entries(feed_id, user_id, hash, title, content) VALUES (1, 1, 'h', 'A title', 'Some content')`)
	require.NoError(t, err)
	id, _ := res.LastInsertId()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM entries_fts WHERE rowid = ?`, id).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestSchema_DefaultUserSeeded(t *testing.T) {
	db := openMigrated(t)
	var name string
	require.NoError(t, db.QueryRow(`SELECT username FROM users WHERE id = 1`).Scan(&name))
	assert.Equal(t, "default", name)
}

func TestSchema_FTS5Search(t *testing.T) {
	db := openMigrated(t)
	_, err := db.Exec(`INSERT INTO feeds(user_id, title, feed_url) VALUES (1, 'F', 'https://x/')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO entries(feed_id, user_id, hash, title, content) VALUES
		(1, 1, 'a', 'Quantum computing now', 'Long article about quantum computers'),
		(1, 1, 'b', 'Cooking lentils', 'How to braise lentils')`)
	require.NoError(t, err)

	rows, err := db.Query(`SELECT rowid FROM entries_fts WHERE entries_fts MATCH 'quantum'`)
	require.NoError(t, err)
	defer rows.Close()
	var hits []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		hits = append(hits, id)
	}
	assert.Len(t, hits, 1)
}
