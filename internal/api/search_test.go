package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is a test double that lets tests stub the EntryRepo. Only Entries()
// is wired up; the other accessors return nil so anything else accidentally
// invoked panics loudly.
type fakeStore struct {
	store.Store
	entries store.EntryRepo
}

func (f *fakeStore) Entries() store.EntryRepo { return f.entries }

type fakeEntryRepo struct {
	store.EntryRepo
	lastUserID int64
	lastQuery  string
	lastLimit  int
	lastOffset int
	results    []model.Entry
	err        error
}

func (f *fakeEntryRepo) Search(_ context.Context, userID int64, query string, limit, offset int) ([]model.Entry, error) {
	f.lastUserID = userID
	f.lastQuery = query
	f.lastLimit = limit
	f.lastOffset = offset
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func newSearchTestServer(repo store.EntryRepo) http.Handler {
	st := &fakeStore{entries: repo}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/search", searchHandler(st, nil))
	return mux
}

func TestSearch_RejectsEmptyQuery(t *testing.T) {
	repo := &fakeEntryRepo{}
	srv := newSearchTestServer(repo)

	for _, q := range []string{"", "%20%20%20"} {
		r := httptest.NewRequest("GET", "/api/v1/search?q="+q, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		assert.Equalf(t, http.StatusBadRequest, w.Code, "q=%q", q)
	}
}

func TestSearch_ReturnsEntries(t *testing.T) {
	titleA := "Linux kernel news"
	titleB := "More Linux"
	repo := &fakeEntryRepo{
		results: []model.Entry{
			{ID: 7, FeedID: 1, UserID: 1, Hash: "h1", Title: titleA},
			{ID: 8, FeedID: 1, UserID: 1, Hash: "h2", Title: titleB},
		},
	}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=linux", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	body, _ := io.ReadAll(w.Body)

	var got struct {
		Entries []model.Entry `json:"entries"`
		Limit   int           `json:"limit"`
		Offset  int           `json:"offset"`
		Query   string        `json:"query"`
		HasMore bool          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "linux", got.Query)
	assert.Equal(t, 50, got.Limit)
	assert.Equal(t, 0, got.Offset)
	assert.False(t, got.HasMore)
	require.Len(t, got.Entries, 2)
	assert.Equal(t, int64(7), got.Entries[0].ID)
	assert.Equal(t, titleA, got.Entries[0].Title)

	// Repo received the right inputs.
	assert.Equal(t, int64(1), repo.lastUserID)
	assert.Equal(t, "linux", repo.lastQuery)
	assert.Equal(t, 50, repo.lastLimit)
	assert.Equal(t, 0, repo.lastOffset)
}

func TestSearch_TrimsQueryWhitespace(t *testing.T) {
	repo := &fakeEntryRepo{}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=%20%20rust%20%20", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "rust", repo.lastQuery)
}

func TestSearch_ClampsLimit(t *testing.T) {
	cases := []struct {
		name     string
		urlLimit string
		want     int
	}{
		{"defaults_to_50_when_missing", "", 50},
		{"defaults_to_50_when_zero", "0", 50},
		{"defaults_to_50_when_negative", "-3", 50},
		{"defaults_to_50_when_garbage", "abc", 50},
		{"caps_at_100", "500", 100},
		{"passes_through_in_range", "25", 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeEntryRepo{}
			srv := newSearchTestServer(repo)
			url := "/api/v1/search?q=go"
			if tc.urlLimit != "" {
				url += "&limit=" + tc.urlLimit
			}
			r := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.want, repo.lastLimit)
		})
	}
}

func TestSearch_OffsetParsing(t *testing.T) {
	cases := []struct {
		name      string
		urlOffset string
		want      int
	}{
		{"defaults_to_0_when_missing", "", 0},
		{"defaults_to_0_when_negative", "-5", 0},
		{"defaults_to_0_when_garbage", "xyz", 0},
		{"passes_through", "75", 75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeEntryRepo{}
			srv := newSearchTestServer(repo)
			url := "/api/v1/search?q=go"
			if tc.urlOffset != "" {
				url += "&offset=" + tc.urlOffset
			}
			r := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.want, repo.lastOffset)
		})
	}
}

func TestSearch_HasMoreWhenLimitFull(t *testing.T) {
	// Repo returned exactly `limit` rows ⇒ likely more available ⇒ has_more=true.
	results := make([]model.Entry, 5)
	for i := range results {
		results[i] = model.Entry{ID: int64(i + 1), Title: "x"}
	}
	repo := &fakeEntryRepo{results: results}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=go&limit=5", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var got struct {
		HasMore bool `json:"has_more"`
		Limit   int  `json:"limit"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 5, got.Limit)
	assert.True(t, got.HasMore)
}

func TestSearch_HasMoreFalseWhenPartial(t *testing.T) {
	repo := &fakeEntryRepo{results: []model.Entry{{ID: 1, Title: "x"}}}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=go&limit=5", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var got struct {
		HasMore bool `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.False(t, got.HasMore)
}

func TestSearch_RepoError500(t *testing.T) {
	repo := &fakeEntryRepo{err: errors.New("boom")}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=go", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSearch_NilEntriesEncodesAsEmptyArray(t *testing.T) {
	repo := &fakeEntryRepo{results: nil}
	srv := newSearchTestServer(repo)

	r := httptest.NewRequest("GET", "/api/v1/search?q=zzz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// Important: front-end clients should see [] not null.
	assert.True(t, strings.Contains(body, `"entries":[]`), "body: %s", body)
}

func TestSearch_RegisteredOnServerMux(t *testing.T) {
	repo := &fakeEntryRepo{results: []model.Entry{{ID: 42, Title: "hit"}}}
	st := &fakeStore{entries: repo}

	addr := runTestServer(t, Options{Store: st})
	resp, err := http.Get("http://" + addr + "/api/v1/search?q=go")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
}
