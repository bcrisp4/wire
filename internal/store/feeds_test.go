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

func newTestFeedRepo(t *testing.T) *feedRepo {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(context.Background(), db))
	return &feedRepo{db: db}
}

func TestFeedRepo_CreateGetRoundTrip(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()

	site := "https://example.com"
	desc := "Example feed"
	f := &model.Feed{
		UserID:       1,
		Title:        "Example",
		FeedURL:      "https://example.com/rss",
		SiteURL:      &site,
		Description:  &desc,
		PollInterval: 3600,
	}
	require.NoError(t, r.Create(ctx, f))
	assert.Greater(t, f.ID, int64(0))
	assert.Greater(t, f.CreatedAt, int64(0))
	assert.Greater(t, f.UpdatedAt, int64(0))

	got, err := r.Get(ctx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, f.ID, got.ID)
	assert.Equal(t, "Example", got.Title)
	assert.Equal(t, "https://example.com/rss", got.FeedURL)
	require.NotNil(t, got.SiteURL)
	assert.Equal(t, site, *got.SiteURL)
	require.NotNil(t, got.Description)
	assert.Equal(t, desc, *got.Description)
	assert.Equal(t, 3600, got.PollInterval)
	assert.False(t, got.Disabled)
	assert.False(t, got.Crawler)
	assert.Equal(t, 0, got.ErrorCount)
}

func TestFeedRepo_GetMissingReturnsErrNotFound(t *testing.T) {
	r := newTestFeedRepo(t)
	_, err := r.Get(context.Background(), 9999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestFeedRepo_UpdateChangesUpdatedAt(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()

	f := &model.Feed{UserID: 1, Title: "Old", FeedURL: "https://example.com/rss", PollInterval: 3600}
	require.NoError(t, r.Create(ctx, f))
	originalUpdatedAt := f.UpdatedAt
	originalCreatedAt := f.CreatedAt

	// Force updated_at to an earlier value so the subsequent unixepoch() bump is observable.
	_, err := r.db.ExecContext(ctx, `UPDATE feeds SET updated_at = ? WHERE id = ?`, originalUpdatedAt-10, f.ID)
	require.NoError(t, err)

	f.Title = "New"
	require.NoError(t, r.Update(ctx, f))

	got, err := r.Get(ctx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, "New", got.Title)
	assert.Greater(t, got.UpdatedAt, originalUpdatedAt-10)
	assert.Equal(t, originalCreatedAt, got.CreatedAt)
}

func TestFeedRepo_UpdateMissingReturnsErrNotFound(t *testing.T) {
	r := newTestFeedRepo(t)
	err := r.Update(context.Background(), &model.Feed{ID: 9999, UserID: 1, Title: "x", FeedURL: "y"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestFeedRepo_DeleteRemoves(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()
	f := &model.Feed{UserID: 1, Title: "T", FeedURL: "https://example.com/rss", PollInterval: 3600}
	require.NoError(t, r.Create(ctx, f))
	require.NoError(t, r.Delete(ctx, f.ID))
	_, err := r.Get(ctx, f.ID)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestFeedRepo_DeleteMissingReturnsErrNotFound(t *testing.T) {
	r := newTestFeedRepo(t)
	err := r.Delete(context.Background(), 9999)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestFeedRepo_ListFiltersByUserID(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()

	// Create a second user so we can verify filtering.
	_, err := r.db.ExecContext(ctx, `INSERT INTO users(id, username) VALUES (2, 'other')`)
	require.NoError(t, err)

	f1 := &model.Feed{UserID: 1, Title: "A", FeedURL: "https://a.example/rss", PollInterval: 3600}
	f2 := &model.Feed{UserID: 1, Title: "B", FeedURL: "https://b.example/rss", PollInterval: 3600}
	f3 := &model.Feed{UserID: 2, Title: "C", FeedURL: "https://c.example/rss", PollInterval: 3600}
	require.NoError(t, r.Create(ctx, f1))
	require.NoError(t, r.Create(ctx, f2))
	require.NoError(t, r.Create(ctx, f3))

	got, err := r.List(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	titles := []string{got[0].Title, got[1].Title}
	assert.ElementsMatch(t, []string{"A", "B"}, titles)

	gotOther, err := r.List(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, gotOther, 1)
	assert.Equal(t, "C", gotOther[0].Title)

	empty, err := r.List(ctx, 999)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestFeedRepo_DueForPollingSkipsDisabledAndErrored(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()

	now := int64(1_000_000)
	due1 := now - 10
	due2 := now - 5
	notDue := now + 100

	// Insert directly so we can set next_poll_at, disabled, error_count precisely.
	mk := func(url string, nextPoll int64, disabled int, errCount int) {
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO feeds(user_id, title, feed_url, poll_interval, next_poll_at, disabled, error_count)
			VALUES (1, ?, ?, 3600, ?, ?, ?)`, url, url, nextPoll, disabled, errCount)
		require.NoError(t, err)
	}
	mk("a", due1, 0, 0)        // due, healthy
	mk("b", due2, 0, 5)        // due, some errors but < 10
	mk("c", notDue, 0, 0)      // not yet due
	mk("d", due1, 1, 0)        // due but disabled
	mk("e", due1, 0, 10)       // due but error_count >= 10
	mk("f", due1, 0, 100)      // due but very errored

	got, err := r.DueForPolling(ctx, now, 10)
	require.NoError(t, err)
	urls := make([]string, 0, len(got))
	for _, f := range got {
		urls = append(urls, f.FeedURL)
	}
	assert.ElementsMatch(t, []string{"a", "b"}, urls)
}

func TestFeedRepo_DueForPollingRespectsLimit(t *testing.T) {
	r := newTestFeedRepo(t)
	ctx := context.Background()
	now := int64(1_000_000)
	for i := 0; i < 5; i++ {
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO feeds(user_id, title, feed_url, poll_interval, next_poll_at)
			VALUES (1, ?, ?, 3600, ?)`,
			"t", "https://x/"+string(rune('a'+i))+".rss", now-int64(i+1))
		require.NoError(t, err)
	}
	got, err := r.DueForPolling(ctx, now, 3)
	require.NoError(t, err)
	assert.Len(t, got, 3)
}
