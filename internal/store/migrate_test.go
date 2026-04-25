package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate_AppliesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wire.db")
	db, err := Open(path)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, Migrate(context.Background(), db))
	require.NoError(t, Migrate(context.Background(), db)) // idempotent

	var v int
	require.NoError(t, db.QueryRow("SELECT MAX(version) FROM _migrations").Scan(&v))
	assert.GreaterOrEqual(t, v, 1)
}

func TestMigrate_RecordsAppliedVersions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "wire.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, Migrate(context.Background(), db))

	rows, err := db.Query("SELECT version, name FROM _migrations ORDER BY version")
	require.NoError(t, err)
	defer rows.Close()

	type rec struct {
		v int
		n string
	}
	var got []rec
	for rows.Next() {
		var r rec
		require.NoError(t, rows.Scan(&r.v, &r.n))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	assert.NotEmpty(t, got)
	assert.Equal(t, 1, got[0].v)
}
