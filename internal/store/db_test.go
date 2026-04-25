package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_AppliesPragmas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wire.db")
	db, err := Open(path)
	require.NoError(t, err)
	defer db.Close()

	var journal string
	require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journal))
	assert.Equal(t, "wal", journal)

	var fk int
	require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	assert.Equal(t, 1, fk)

	var busy int
	require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&busy))
	assert.GreaterOrEqual(t, busy, 5000)
}

func TestOpen_RejectsBlankPath(t *testing.T) {
	_, err := Open("")
	assert.Error(t, err)
}
