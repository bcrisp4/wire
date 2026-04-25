package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFeedRepo is an in-memory store.FeedRepo for handler tests; it lets us
// avoid spinning up SQLite in API-package tests while still exercising the
// handler-level logic (json shapes, status codes, queue interaction).
type fakeFeedRepo struct {
	mu     sync.Mutex
	next   int64
	feeds  map[int64]*model.Feed
	unread map[int64]int
}

func newFakeFeedRepo() *fakeFeedRepo {
	return &fakeFeedRepo{feeds: map[int64]*model.Feed{}, unread: map[int64]int{}}
}

func (r *fakeFeedRepo) List(_ context.Context, userID int64) ([]model.Feed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Feed, 0)
	for _, f := range r.feeds {
		if f.UserID == userID {
			out = append(out, *f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListWithUnreadCounts mirrors List but attaches a per-feed unread count
// driven by the unread map. Tests that need a non-zero count populate
// fakeFeedRepo.unread[feedID]; unset feeds report 0.
func (r *fakeFeedRepo) ListWithUnreadCounts(ctx context.Context, userID int64) ([]store.FeedWithUnreadCount, error) {
	feeds, err := r.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]store.FeedWithUnreadCount, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, store.FeedWithUnreadCount{Feed: f, UnreadCount: r.unread[f.ID]})
	}
	return out, nil
}

func (r *fakeFeedRepo) Get(_ context.Context, id int64) (*model.Feed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.feeds[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeFeedRepo) Create(_ context.Context, f *model.Feed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.feeds {
		if existing.UserID == f.UserID && existing.FeedURL == f.FeedURL {
			return store.ErrConflict
		}
	}
	r.next++
	f.ID = r.next
	f.CreatedAt = 1
	f.UpdatedAt = 1
	cp := *f
	r.feeds[f.ID] = &cp
	return nil
}

func (r *fakeFeedRepo) Update(_ context.Context, f *model.Feed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.feeds[f.ID]; !ok {
		return store.ErrNotFound
	}
	f.UpdatedAt++
	cp := *f
	r.feeds[f.ID] = &cp
	return nil
}

func (r *fakeFeedRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.feeds[id]; !ok {
		return store.ErrNotFound
	}
	delete(r.feeds, id)
	return nil
}

func (r *fakeFeedRepo) DueForPolling(context.Context, int64, int) ([]model.Feed, error) {
	return nil, nil
}

// fakeStore wraps a fakeFeedRepo to satisfy store.Store. Feeds() returns the
// real fake; Categories() returns nil (registerCategoryRoutes only stores the
// repo in handler closures, so as long as no test hits /api/v1/categories
// endpoints, nil is fine). The rest panic if reached.
type fakeStore struct{ feeds *fakeFeedRepo }

func (s *fakeStore) Users() store.UserRepo           { panic("Users not used") }
func (s *fakeStore) Categories() store.CategoryRepo  { return nil }
func (s *fakeStore) Feeds() store.FeedRepo           { return s.feeds }
func (s *fakeStore) Entries() store.EntryRepo        { return nil }
func (s *fakeStore) Icons() store.IconRepo           { panic("Icons not used") }
func (s *fakeStore) Tombstones() store.TombstoneRepo { panic("Tombstones not used") }
func (s *fakeStore) Enclosures() store.EnclosureRepo { panic("Enclosures not used") }
func (s *fakeStore) Close() error                    { return nil }

func newTestAPIServer(t *testing.T) (*Server, *fakeFeedRepo, *jobs.MemoryQueue) {
	t.Helper()
	repo := newFakeFeedRepo()
	q := jobs.NewMemoryQueue()
	srv, err := NewServer(Options{
		Listen: "127.0.0.1:0",
		Logger: slogDiscard(),
		Store:  &fakeStore{feeds: repo},
		Queue:  q,
	})
	require.NoError(t, err)
	return srv, repo, q
}

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		r = httptest.NewRequest(method, path, bytes.NewReader(buf))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	return w
}

func TestFeeds_ListEmpty(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "GET", "/api/v1/feeds", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	// Must serialise as `[]`, not `null`, even when empty.
	assert.Equal(t, "[]\n", w.Body.String())
}

func TestFeeds_CreateAndList(t *testing.T) {
	srv, _, q := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds",
		map[string]any{"feed_url": "https://news.ycombinator.com/rss"})
	require.Equal(t, http.StatusCreated, w.Code)

	var created map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "https://news.ycombinator.com/rss", created["feed_url"])
	assert.NotZero(t, created["id"])
	assert.Equal(t, float64(3600), created["poll_interval"])
	// etag/last_modified are internal HTTP-cache state — never expose them.
	_, hasEtag := created["etag"]
	_, hasLM := created["last_modified"]
	assert.False(t, hasEtag, "etag must not be exposed")
	assert.False(t, hasLM, "last_modified must not be exposed")

	// POST should have enqueued an immediate poll.
	job, err := q.Claim(context.Background(), "feed.poll", "test")
	require.NoError(t, err)
	require.NotNil(t, job)
	var payload map[string]int64
	require.NoError(t, json.Unmarshal(job.Payload, &payload))
	assert.Equal(t, int64(created["id"].(float64)), payload["feed_id"])

	// GET shows it.
	w = doJSON(t, srv, "GET", "/api/v1/feeds", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 1)
	assert.Equal(t, "https://news.ycombinator.com/rss", list[0]["feed_url"])
}

func TestFeeds_CreateRequiresFeedURL(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds", map[string]any{"feed_url": ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFeeds_CreateRejectsUnknownField(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	r := httptest.NewRequest("POST", "/api/v1/feeds",
		bytes.NewBufferString(`{"feed_url":"https://x/rss","bogus":1}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFeeds_CreateRejectsTrailingJSON(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	r := httptest.NewRequest("POST", "/api/v1/feeds",
		bytes.NewBufferString(`{"feed_url":"https://x/rss"}{"feed_url":"https://y/rss"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFeeds_CreateRejectsDuplicate(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	body := map[string]any{"feed_url": "https://example.com/rss"}
	w := doJSON(t, srv, "POST", "/api/v1/feeds", body)
	require.Equal(t, http.StatusCreated, w.Code)
	w = doJSON(t, srv, "POST", "/api/v1/feeds", body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestFeeds_GetUnknownReturns404(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "GET", "/api/v1/feeds/999", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFeeds_GetWrongUserReturns404(t *testing.T) {
	srv, repo, _ := newTestAPIServer(t)
	// Insert a feed owned by user 2; default handler user is 1, so it must look like 404.
	require.NoError(t, repo.Create(context.Background(), &model.Feed{
		UserID: 2, Title: "x", FeedURL: "https://other/rss", PollInterval: 3600,
	}))
	w := doJSON(t, srv, "GET", "/api/v1/feeds/1", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFeeds_Update(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds",
		map[string]any{"feed_url": "https://example.com/rss"})
	require.Equal(t, http.StatusCreated, w.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	w = doJSON(t, srv, "PUT", "/api/v1/feeds/1",
		map[string]any{"title": "Renamed", "disabled": true, "crawler": true})
	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "Renamed", got["title"])
	assert.Equal(t, true, got["disabled"])
	assert.Equal(t, true, got["crawler"])
}

func TestFeeds_Delete(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds",
		map[string]any{"feed_url": "https://example.com/rss"})
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(t, srv, "DELETE", "/api/v1/feeds/1", nil)
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = doJSON(t, srv, "GET", "/api/v1/feeds/1", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFeeds_RefreshEnqueuesJob(t *testing.T) {
	srv, _, q := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds",
		map[string]any{"feed_url": "https://example.com/rss"})
	require.Equal(t, http.StatusCreated, w.Code)

	// Drain the create-time poll so we can assert the refresh enqueues exactly one new job.
	_, err := q.Claim(context.Background(), "feed.poll", "test")
	require.NoError(t, err)

	w = doJSON(t, srv, "POST", "/api/v1/feeds/1/refresh", nil)
	require.Equal(t, http.StatusAccepted, w.Code)

	job, err := q.Claim(context.Background(), "feed.poll", "test")
	require.NoError(t, err)
	require.NotNil(t, job)
	var payload map[string]int64
	require.NoError(t, json.Unmarshal(job.Payload, &payload))
	assert.Equal(t, int64(1), payload["feed_id"])
}

// TestFeeds_ListIncludesUnreadCount asserts that GET /api/v1/feeds includes
// the unread_count field on every entry, populated from the store-side
// aggregate. The fakeFeedRepo's unread map mirrors the LEFT JOIN aggregate
// the production repo runs against SQLite.
func TestFeeds_ListIncludesUnreadCount(t *testing.T) {
	srv, repo, _ := newTestAPIServer(t)
	require.NoError(t, repo.Create(context.Background(), &model.Feed{
		UserID: defaultUserID, Title: "A", FeedURL: "https://a/rss", PollInterval: 3600,
	}))
	require.NoError(t, repo.Create(context.Background(), &model.Feed{
		UserID: defaultUserID, Title: "B", FeedURL: "https://b/rss", PollInterval: 3600,
	}))
	repo.unread[1] = 5
	// repo.unread[2] left unset -> handler must still surface 0, not omit the field.

	w := doJSON(t, srv, "GET", "/api/v1/feeds", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	require.Contains(t, list[0], "unread_count")
	require.Contains(t, list[1], "unread_count")
	assert.Equal(t, float64(5), list[0]["unread_count"])
	assert.Equal(t, float64(0), list[1]["unread_count"])
}

func TestFeeds_RefreshUnknownReturns404(t *testing.T) {
	srv, _, _ := newTestAPIServer(t)
	w := doJSON(t, srv, "POST", "/api/v1/feeds/999/refresh", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
