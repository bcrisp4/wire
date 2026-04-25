package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRepo opens a fresh migrated DB and returns a concrete *entryRepo via
// the public store factory once one exists. For now we exercise the package
// helper that wraps it.
func newEntriesAPI(t *testing.T) (http.Handler, store.EntriesAPI, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wire.db")
	db, err := store.Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(context.Background(), db))
	// Seed a feed for entry insertion.
	_, err = db.Exec(`INSERT INTO feeds(user_id, title, feed_url, poll_interval) VALUES (1, 'F1', 'https://example.com/1', 3600)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO feeds(user_id, title, feed_url, poll_interval) VALUES (1, 'F2', 'https://example.com/2', 3600)`)
	require.NoError(t, err)

	repo := store.NewEntryRepo(db)
	mux := http.NewServeMux()
	registerEntryRoutes(mux, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux, repo, func() { _ = db.Close() }
}

func mustInsert(t *testing.T, repo store.EntriesAPI, e *model.Entry) {
	t.Helper()
	require.NoError(t, repo.Insert(context.Background(), e))
}

func TestEntries_GET_ListsUnreadByDefault(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha"})
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "b", Title: "Beta", Read: true})

	r := httptest.NewRequest("GET", "/api/v1/entries", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []model.Entry `json:"entries"`
		Total   int           `json:"total"`
		Limit   int           `json:"limit"`
		Offset  int           `json:"offset"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "Alpha", resp.Entries[0].Title)
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, 50, resp.Limit)
}

func TestEntries_GET_FilterByStatusAll(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha"})
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "b", Title: "Beta", Read: true})

	r := httptest.NewRequest("GET", "/api/v1/entries?status=all", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
}

func TestEntries_GET_FilterByFeedID(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha"})
	mustInsert(t, repo, &model.Entry{FeedID: 2, UserID: 1, Hash: "b", Title: "Beta"})

	r := httptest.NewRequest("GET", "/api/v1/entries?feed_id=2&status=all", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Entries []model.Entry `json:"entries"`
		Total   int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, int64(2), resp.Entries[0].FeedID)
}

func TestEntries_GET_ListOmitsContent(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	body := "huge body"
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha", Content: &body})

	r := httptest.NewRequest("GET", "/api/v1/entries", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Entries []model.Entry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Entries, 1)
	assert.Nil(t, resp.Entries[0].Content)
}

func TestEntries_GETByID_IncludesContent(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	body := "full body"
	e := &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha", Content: &body}
	mustInsert(t, repo, e)

	r := httptest.NewRequest("GET", "/api/v1/entries/"+strconv.FormatInt(e.ID, 10), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var got model.Entry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "Alpha", got.Title)
	require.NotNil(t, got.Content)
	assert.Equal(t, "full body", *got.Content)
}

func TestEntries_GETByID_NotFound(t *testing.T) {
	h, _, cleanup := newEntriesAPI(t)
	defer cleanup()
	r := httptest.NewRequest("GET", "/api/v1/entries/9999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestEntries_PUT_TogglesReadAndSaved(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	e := &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha"}
	mustInsert(t, repo, e)

	body := bytes.NewBufferString(`{"read": true, "saved": true}`)
	r := httptest.NewRequest("PUT", "/api/v1/entries/"+strconv.FormatInt(e.ID, 10), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	got, err := repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.True(t, got.Read)
	assert.True(t, got.Saved)
	require.NotNil(t, got.ReadAt)
	require.NotNil(t, got.SavedAt)
}

func TestEntries_PUT_NullFieldsAreNoop(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	e := &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "Alpha", Read: true}
	mustInsert(t, repo, e)
	yes := true
	require.NoError(t, repo.UpdateState(context.Background(), e.ID, &yes, nil))

	body := bytes.NewBufferString(`{"read": null, "saved": null}`)
	r := httptest.NewRequest("PUT", "/api/v1/entries/"+strconv.FormatInt(e.ID, 10), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	got, err := repo.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.True(t, got.Read, "read must remain true (null = no-op)")
}

func TestEntries_PUT_BulkRead_AllForUser(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "A"})
	mustInsert(t, repo, &model.Entry{FeedID: 2, UserID: 1, Hash: "b", Title: "B"})

	body := bytes.NewBufferString(`{}`)
	r := httptest.NewRequest("PUT", "/api/v1/entries/read", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusNoContent, w.Code, "body: %s", w.Body.String())

	got, err := repo.List(context.Background(), store.EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEntries_PUT_BulkRead_ScopedByFeed(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "A"})
	mustInsert(t, repo, &model.Entry{FeedID: 2, UserID: 1, Hash: "b", Title: "B"})

	body := bytes.NewBufferString(`{"feed_id": 1}`)
	r := httptest.NewRequest("PUT", "/api/v1/entries/read", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusNoContent, w.Code)

	got, err := repo.List(context.Background(), store.EntryQuery{UserID: 1, Status: "unread"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(2), got[0].FeedID)
}

func TestEntries_PUT_BulkRead_RejectsBothScopes(t *testing.T) {
	h, _, cleanup := newEntriesAPI(t)
	defer cleanup()

	body := bytes.NewBufferString(`{"feed_id": 1, "category_id": 2}`)
	r := httptest.NewRequest("PUT", "/api/v1/entries/read", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEntries_PUT_BulkRead_AllowsEmptyBodyWithUnknownContentLength(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	mustInsert(t, repo, &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "A"})

	r := httptest.NewRequest("PUT", "/api/v1/entries/read", http.NoBody)
	r.ContentLength = -1 // simulate chunked transfer
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code, "body: %s", w.Body.String())
}

func TestEntries_PUT_RejectsTrailingJSON(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	e := &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "A"}
	mustInsert(t, repo, e)

	r := httptest.NewRequest("PUT", "/api/v1/entries/"+strconv.FormatInt(e.ID, 10),
		bytes.NewBufferString(`{"read":true}{"saved":false}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEntries_PUT_UnknownIDReturns404(t *testing.T) {
	h, _, cleanup := newEntriesAPI(t)
	defer cleanup()
	r := httptest.NewRequest("PUT", "/api/v1/entries/9999", bytes.NewBufferString(`{"read":true}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestEntries_PUT_RejectsInvalidJSON(t *testing.T) {
	h, repo, cleanup := newEntriesAPI(t)
	defer cleanup()
	e := &model.Entry{FeedID: 1, UserID: 1, Hash: "a", Title: "A"}
	mustInsert(t, repo, e)

	r := httptest.NewRequest("PUT", "/api/v1/entries/"+strconv.FormatInt(e.ID, 10), bytes.NewBufferString(`{not json`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	_, _ = io.ReadAll(w.Body)
}
