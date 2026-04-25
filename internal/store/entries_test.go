package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedFeed inserts a feed and returns its id.
func seedFeed(t *testing.T, db *sql.DB, userID int64, title, url string, categoryID *int64) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO feeds(user_id, category_id, title, feed_url, poll_interval) VALUES (?, ?, ?, ?, 3600)`,
		userID, categoryID, title, url,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func seedCategory(t *testing.T, db *sql.DB, userID int64, name string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO categories(user_id, name) VALUES (?, ?)`, userID, name)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestEntries_InsertAndGet_RoundTrip(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	body := "Full body content"
	pub := int64(1714000000)
	e := &model.Entry{
		FeedID:      feedID,
		UserID:      1,
		Hash:        "h1",
		Title:       "Hello",
		Content:     &body,
		PublishedAt: &pub,
	}
	require.NoError(t, repo.Insert(context.Background(), e))
	assert.Greater(t, e.ID, int64(0))

	got, err := repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hello", got.Title)
	require.NotNil(t, got.Content)
	assert.Equal(t, "Full body content", *got.Content)
	require.NotNil(t, got.PublishedAt)
	assert.Equal(t, pub, *got.PublishedAt)
}

func TestEntries_Insert_DuplicateHashReturnsErrDuplicate(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	e := &model.Entry{FeedID: feedID, UserID: 1, Hash: "dup", Title: "A"}
	require.NoError(t, repo.Insert(context.Background(), e))

	dup := &model.Entry{FeedID: feedID, UserID: 1, Hash: "dup", Title: "A2"}
	err := repo.Insert(context.Background(), dup)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateEntry))
}

func TestEntries_List_FiltersByStatus(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	insert := func(hash, title string, read bool) int64 {
		e := &model.Entry{FeedID: feedID, UserID: 1, Hash: hash, Title: title, Read: read}
		require.NoError(t, repo.Insert(context.Background(), e))
		return e.ID
	}
	insert("u1", "Unread1", false)
	insert("u2", "Unread2", false)
	rid := insert("r1", "ReadOne", true)
	_ = rid

	unread, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	assert.Len(t, unread, 2)
	for _, e := range unread {
		assert.False(t, e.Read)
		assert.Nil(t, e.Content, "list must omit content")
	}

	read, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "read"})
	require.NoError(t, err)
	assert.Len(t, read, 1)
	assert.True(t, read[0].Read)

	all, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "all"})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestEntries_List_FiltersBySaved(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	for i, saved := range []bool{true, false, true} {
		e := &model.Entry{FeedID: feedID, UserID: 1, Hash: string(rune('a' + i)), Title: "t", Saved: saved}
		require.NoError(t, repo.Insert(context.Background(), e))
	}
	yes := true
	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "all", Saved: &yes})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	for _, e := range got {
		assert.True(t, e.Saved)
	}
	no := false
	got, err = repo.List(context.Background(), EntryQuery{UserID: 1, Status: "all", Saved: &no})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestEntries_List_FiltersByFeedID(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	f1 := seedFeed(t, db, 1, "F1", "https://example.com/1", nil)
	f2 := seedFeed(t, db, 1, "F2", "https://example.com/2", nil)

	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "a", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "b", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f2, UserID: 1, Hash: "c", Title: "t"}))

	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "all", FeedID: &f1})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestEntries_List_FiltersByCategoryID(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	cat := seedCategory(t, db, 1, "Tech")
	f1 := seedFeed(t, db, 1, "F1", "https://example.com/1", &cat)
	f2 := seedFeed(t, db, 1, "F2", "https://example.com/2", nil)

	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "a", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f2, UserID: 1, Hash: "b", Title: "t"}))

	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "all", CategoryID: &cat})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, f1, got[0].FeedID)
}

func TestEntries_List_RespectsLimitOffsetAndOrder(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	for i := 0; i < 5; i++ {
		ts := int64(1000 + i)
		e := &model.Entry{FeedID: feedID, UserID: 1, Hash: string(rune('a' + i)), Title: "t", PublishedAt: &ts}
		require.NoError(t, repo.Insert(context.Background(), e))
	}
	got, err := repo.List(context.Background(), EntryQuery{
		UserID: 1, Status: "all", Sort: "published_at", Order: "desc", Limit: 2, Offset: 0,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NotNil(t, got[0].PublishedAt)
	require.NotNil(t, got[1].PublishedAt)
	assert.Equal(t, int64(1004), *got[0].PublishedAt)
	assert.Equal(t, int64(1003), *got[1].PublishedAt)

	got, err = repo.List(context.Background(), EntryQuery{
		UserID: 1, Status: "all", Sort: "published_at", Order: "asc", Limit: 2, Offset: 2,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(1002), *got[0].PublishedAt)
	assert.Equal(t, int64(1003), *got[1].PublishedAt)
}

func TestEntries_UpdateState_TogglesReadAndSavedAt(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)
	e := &model.Entry{FeedID: feedID, UserID: 1, Hash: "h", Title: "t"}
	require.NoError(t, repo.Insert(context.Background(), e))

	yes := true
	require.NoError(t, repo.UpdateState(context.Background(), e.ID, &yes, &yes))
	got, err := repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.True(t, got.Read)
	assert.True(t, got.Saved)
	require.NotNil(t, got.ReadAt)
	require.NotNil(t, got.SavedAt)
	assert.Greater(t, *got.ReadAt, int64(0))
	assert.Greater(t, *got.SavedAt, int64(0))

	no := false
	require.NoError(t, repo.UpdateState(context.Background(), e.ID, &no, &no))
	got, err = repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.False(t, got.Read)
	assert.False(t, got.Saved)
	assert.Nil(t, got.ReadAt)
	assert.Nil(t, got.SavedAt)
}

func TestEntries_Get_UnknownIDReturnsErrNotFound(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	_, err := repo.Get(context.Background(), 9999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestEntries_UpdateState_UnknownIDReturnsErrNotFound(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	yes := true
	err := repo.UpdateState(context.Background(), 9999, &yes, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestEntries_UpdateState_NoOpWhenBothNil(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)
	e := &model.Entry{FeedID: feedID, UserID: 1, Hash: "h", Title: "t"}
	require.NoError(t, repo.Insert(context.Background(), e))
	require.NoError(t, repo.UpdateState(context.Background(), e.ID, nil, nil))
	got, err := repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.False(t, got.Read)
	assert.False(t, got.Saved)
}

func TestEntries_BulkMarkRead_ByFeed(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	f1 := seedFeed(t, db, 1, "F1", "https://example.com/1", nil)
	f2 := seedFeed(t, db, 1, "F2", "https://example.com/2", nil)
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "a", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "b", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f2, UserID: 1, Hash: "c", Title: "t"}))

	require.NoError(t, repo.BulkMarkRead(context.Background(), BulkReadScope{UserID: 1, FeedID: &f1}))

	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, f2, got[0].FeedID)
}

func TestEntries_BulkMarkRead_ByCategory(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	cat := seedCategory(t, db, 1, "Tech")
	f1 := seedFeed(t, db, 1, "F1", "https://example.com/1", &cat)
	f2 := seedFeed(t, db, 1, "F2", "https://example.com/2", nil)
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "a", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f2, UserID: 1, Hash: "b", Title: "t"}))

	require.NoError(t, repo.BulkMarkRead(context.Background(), BulkReadScope{UserID: 1, CategoryID: &cat}))

	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, f2, got[0].FeedID)
}

func TestEntries_BulkMarkRead_AllForUser(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	f1 := seedFeed(t, db, 1, "F1", "https://example.com/1", nil)
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "a", Title: "t"}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{FeedID: f1, UserID: 1, Hash: "b", Title: "t"}))

	require.NoError(t, repo.BulkMarkRead(context.Background(), BulkReadScope{UserID: 1}))
	got, err := repo.List(context.Background(), EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEntries_Search_MatchesTitleAndContent(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)

	body1 := "Long article about quantum computers"
	body2 := "How to braise lentils"
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{
		FeedID: feedID, UserID: 1, Hash: "a", Title: "Quantum computing now", Content: &body1,
	}))
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{
		FeedID: feedID, UserID: 1, Hash: "b", Title: "Cooking lentils", Content: &body2,
	}))

	got, err := repo.Search(context.Background(), 1, "quantum", 10, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Quantum computing now", got[0].Title)
}

func TestEntries_Search_RespectsUserScope(t *testing.T) {
	db := openMigrated(t)
	repo := &entryRepo{db: db}
	feedID := seedFeed(t, db, 1, "F", "https://example.com/feed", nil)
	require.NoError(t, repo.Insert(context.Background(), &model.Entry{
		FeedID: feedID, UserID: 1, Hash: "a", Title: "Quantum",
	}))
	got, err := repo.Search(context.Background(), 999, "quantum", 10, 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}
