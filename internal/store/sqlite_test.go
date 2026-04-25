package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface satisfaction. These declarations fail to build if a
// repo struct drifts from its interface, catching shape changes earlier than a
// runtime test would.
var (
	_ Store         = (*sqliteStore)(nil)
	_ UserRepo      = (*userRepo)(nil)
	_ CategoryRepo  = (*categoryRepo)(nil)
	_ FeedRepo      = (*feedRepo)(nil)
	_ EntryRepo     = (*entryRepo)(nil)
	_ IconRepo      = (*iconRepo)(nil)
	_ TombstoneRepo = (*tombstoneRepo)(nil)
	_ EnclosureRepo = (*enclosureRepo)(nil)
)

func TestNew_ReturnsStoreWithAllRepos(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, Migrate(context.Background(), db))

	s := New(db)
	require.NotNil(t, s)
	assert.NotNil(t, s.Users())
	assert.NotNil(t, s.Categories())
	assert.NotNil(t, s.Feeds())
	assert.NotNil(t, s.Entries())
	assert.NotNil(t, s.Icons())
	assert.NotNil(t, s.Tombstones())
	assert.NotNil(t, s.Enclosures())
}

func TestStoreClose_DoesNotClosePassedDB(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, Migrate(context.Background(), db))

	s := New(db)
	require.NoError(t, s.Close())
	// The DB should still be usable since Honker, not Store, owns it.
	require.NoError(t, db.PingContext(context.Background()))
}
