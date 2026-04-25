package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStoreCategories(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(context.Background(), db))
	return New(db)
}

func TestCategoryRepo_CreateRoundTrip(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	c := &model.Category{UserID: 1, Name: "News"}
	require.NoError(t, s.Categories().Create(ctx, c))
	assert.Greater(t, c.ID, int64(0), "Create should populate Category.ID")

	got, err := s.Categories().List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, c.ID, got[0].ID)
	assert.Equal(t, "News", got[0].Name)
	assert.Equal(t, int64(1), got[0].UserID)
}

func TestCategoryRepo_ListSortedByName(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	for _, name := range []string{"Tech", "Art", "News"} {
		require.NoError(t, s.Categories().Create(ctx, &model.Category{UserID: 1, Name: name}))
	}

	got, err := s.Categories().List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "Art", got[0].Name)
	assert.Equal(t, "News", got[1].Name)
	assert.Equal(t, "Tech", got[2].Name)
}

func TestCategoryRepo_ListIsScopedByUser(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	// user 1 (default user from seed migration)
	require.NoError(t, s.Categories().Create(ctx, &model.Category{UserID: 1, Name: "News"}))

	// no rows for user 2 (which has no row in users); List should return empty.
	got, err := s.Categories().List(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCategoryRepo_DuplicateNamePerUserConflicts(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	require.NoError(t, s.Categories().Create(ctx, &model.Category{UserID: 1, Name: "News"}))
	err := s.Categories().Create(ctx, &model.Category{UserID: 1, Name: "News"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConflict, "duplicate (user_id, name) must surface ErrConflict")
}

func TestCategoryRepo_Rename(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	c := &model.Category{UserID: 1, Name: "News"}
	require.NoError(t, s.Categories().Create(ctx, c))

	require.NoError(t, s.Categories().Rename(ctx, c.ID, "Tech"))

	got, err := s.Categories().List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Tech", got[0].Name)
}

func TestCategoryRepo_RenameToDuplicateConflicts(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	require.NoError(t, s.Categories().Create(ctx, &model.Category{UserID: 1, Name: "News"}))
	tech := &model.Category{UserID: 1, Name: "Tech"}
	require.NoError(t, s.Categories().Create(ctx, tech))

	err := s.Categories().Rename(ctx, tech.ID, "News")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestCategoryRepo_RenameMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStoreCategories(t)
	err := s.Categories().Rename(context.Background(), 9999, "Whatever")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestCategoryRepo_RenameToSameNameSucceeds locks in SQLite's behavior that
// UPDATE ... SET name = current_name WHERE id = X reports RowsAffected == 1
// (i.e. a no-op rename is not mistaken for a missing row). If the driver ever
// changes this, the Rename impl will need to disambiguate via a SELECT.
func TestCategoryRepo_RenameToSameNameSucceeds(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	c := &model.Category{UserID: 1, Name: "News"}
	require.NoError(t, s.Categories().Create(ctx, c))
	assert.NoError(t, s.Categories().Rename(ctx, c.ID, "News"))
}

func TestCategoryRepo_Delete(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	c := &model.Category{UserID: 1, Name: "News"}
	require.NoError(t, s.Categories().Create(ctx, c))

	require.NoError(t, s.Categories().Delete(ctx, c.ID))

	got, err := s.Categories().List(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCategoryRepo_DeleteMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStoreCategories(t)
	err := s.Categories().Delete(context.Background(), 9999)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCategoryRepo_ListWithUnreadCounts(t *testing.T) {
	s := newTestStoreCategories(t)
	ctx := context.Background()

	// News (with feeds + entries), Tech (one feed, no entries), Empty (no feeds).
	news := &model.Category{UserID: 1, Name: "News"}
	tech := &model.Category{UserID: 1, Name: "Tech"}
	empty := &model.Category{UserID: 1, Name: "Empty"}
	require.NoError(t, s.Categories().Create(ctx, news))
	require.NoError(t, s.Categories().Create(ctx, tech))
	require.NoError(t, s.Categories().Create(ctx, empty))

	mkFeed := func(catID int64, url string) int64 {
		f := &model.Feed{UserID: 1, CategoryID: &catID, Title: url, FeedURL: url, PollInterval: 3600}
		require.NoError(t, s.Feeds().Create(ctx, f))
		return f.ID
	}
	a := mkFeed(news.ID, "https://a.example/rss")
	b := mkFeed(news.ID, "https://b.example/rss")
	mkFeed(tech.ID, "https://t.example/rss") // feed with no entries

	// 3 unread + 1 read across the News category; nothing for Tech or Empty.
	// We're inside the store package, so cast to the concrete repo to reach the *sql.DB.
	cr := s.Categories().(*categoryRepo)
	_, err := cr.db.ExecContext(ctx,
		`INSERT INTO entries(feed_id, user_id, hash, title, read) VALUES
		 (?, 1, 'a1', 'x', 0), (?, 1, 'a2', 'x', 0), (?, 1, 'a3', 'x', 1),
		 (?, 1, 'b1', 'x', 0)`,
		a, a, a, b)
	require.NoError(t, err)

	got, err := s.Categories().ListWithUnreadCounts(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 3, "all categories must appear, even those with no feeds")

	byName := make(map[string]int, len(got))
	for _, c := range got {
		byName[c.Name] = c.UnreadCount
	}
	assert.Equal(t, 3, byName["News"])
	assert.Equal(t, 0, byName["Tech"])
	assert.Equal(t, 0, byName["Empty"])
}
