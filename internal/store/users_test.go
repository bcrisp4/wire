package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(context.Background(), db))
	return New(db)
}

func TestUserRepo_GetDefaultUser(t *testing.T) {
	s := newTestStore(t)
	u, err := s.Users().Get(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, int64(1), u.ID)
	assert.Equal(t, "default", u.Username)
	// Schema defaults seeded by 0001_initial.sql
	assert.Equal(t, "system", u.Theme)
	assert.Equal(t, "serif", u.Font)
	assert.Equal(t, 50, u.EntriesPerPage)
	assert.Equal(t, "published_at", u.DefaultSort)
	assert.Equal(t, "desc", u.DefaultOrder)
	assert.Greater(t, u.CreatedAt, int64(0))
}

func TestUserRepo_GetMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Users().Get(context.Background(), 9999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestUserRepo_UpdateRoundTripsPreferences(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Users().Get(ctx, 1)
	require.NoError(t, err)
	originalUsername := u.Username
	originalCreatedAt := u.CreatedAt

	u.Theme = "dark"
	u.Font = "sans"
	u.EntriesPerPage = 25
	u.DefaultSort = "created_at"
	u.DefaultOrder = "asc"
	// Attempt to mutate immutable fields; the implementation must ignore these.
	u.Username = "ignored"
	u.CreatedAt = 0

	require.NoError(t, s.Users().Update(ctx, u))

	got, err := s.Users().Get(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "dark", got.Theme)
	assert.Equal(t, "sans", got.Font)
	assert.Equal(t, 25, got.EntriesPerPage)
	assert.Equal(t, "created_at", got.DefaultSort)
	assert.Equal(t, "asc", got.DefaultOrder)
	// Immutable fields preserved.
	assert.Equal(t, originalUsername, got.Username)
	assert.Equal(t, originalCreatedAt, got.CreatedAt)
}

func TestUserRepo_UpdateMissingUserReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Users().Update(context.Background(), &model.User{ID: 9999})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestUserRepo_UpdateNilUserReturnsError(t *testing.T) {
	s := newTestStore(t)
	err := s.Users().Update(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil user")
}
