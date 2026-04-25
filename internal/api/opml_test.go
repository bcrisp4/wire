package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/opml"
	"github.com/bcrisp4/wire/internal/store"
)

// fakeStore implements store.Store with the only repos OPML handlers need.
// Other repos return nil and are unused by the OPML endpoints.
type fakeStore struct {
	mu         sync.Mutex
	cats       []model.Category
	feeds      []model.Feed
	nextCatID  int64
	nextFeedID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{nextCatID: 1, nextFeedID: 1}
}

func (s *fakeStore) Users() store.UserRepo           { return nil }
func (s *fakeStore) Categories() store.CategoryRepo  { return &fakeCats{s: s} }
func (s *fakeStore) Feeds() store.FeedRepo           { return &fakeFeeds{s: s} }
func (s *fakeStore) Entries() store.EntryRepo        { return nil }
func (s *fakeStore) Icons() store.IconRepo           { return nil }
func (s *fakeStore) Tombstones() store.TombstoneRepo { return nil }
func (s *fakeStore) Enclosures() store.EnclosureRepo { return nil }
func (s *fakeStore) Close() error                    { return nil }

type fakeCats struct{ s *fakeStore }

func (c *fakeCats) List(_ context.Context, userID int64) ([]model.Category, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	out := make([]model.Category, 0, len(c.s.cats))
	for _, cat := range c.s.cats {
		if cat.UserID == userID {
			out = append(out, cat)
		}
	}
	return out, nil
}
func (c *fakeCats) Create(_ context.Context, m *model.Category) error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	for _, e := range c.s.cats {
		if e.UserID == m.UserID && e.Name == m.Name {
			return fmt.Errorf("UNIQUE constraint failed: categories.user_id, categories.name")
		}
	}
	m.ID = c.s.nextCatID
	c.s.nextCatID++
	c.s.cats = append(c.s.cats, *m)
	return nil
}
func (c *fakeCats) Rename(context.Context, int64, string) error { return errors.New("not used") }
func (c *fakeCats) Delete(context.Context, int64) error         { return errors.New("not used") }

type fakeFeeds struct{ s *fakeStore }

func (f *fakeFeeds) List(_ context.Context, userID int64) ([]model.Feed, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	out := make([]model.Feed, 0, len(f.s.feeds))
	for _, fd := range f.s.feeds {
		if fd.UserID == userID {
			out = append(out, fd)
		}
	}
	return out, nil
}
func (f *fakeFeeds) Get(context.Context, int64) (*model.Feed, error) {
	return nil, errors.New("not used")
}
func (f *fakeFeeds) Create(_ context.Context, m *model.Feed) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	for _, e := range f.s.feeds {
		if e.UserID == m.UserID && e.FeedURL == m.FeedURL {
			return fmt.Errorf("UNIQUE constraint failed: feeds.user_id, feeds.feed_url")
		}
	}
	m.ID = f.s.nextFeedID
	f.s.nextFeedID++
	f.s.feeds = append(f.s.feeds, *m)
	return nil
}
func (f *fakeFeeds) Update(context.Context, *model.Feed) error { return errors.New("not used") }
func (f *fakeFeeds) Delete(context.Context, int64) error       { return errors.New("not used") }
func (f *fakeFeeds) DueForPolling(context.Context, int64, int) ([]model.Feed, error) {
	return nil, errors.New("not used")
}

func newOPMLTestHandler(t *testing.T, st store.Store) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	registerOPMLRoutes(mux, st, slogDiscard())
	return mux
}

const sampleOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline title="News">
      <outline type="rss" xmlUrl="https://hn.example/rss" htmlUrl="https://hn.example" title="HN" />
    </outline>
    <outline type="atom" xmlUrl="https://orphan.example/feed" htmlUrl="https://orphan.example" title="Orphan" />
  </body>
</opml>`

func TestOPML_ImportFlatXML(t *testing.T) {
	st := newFakeStore()
	h := newOPMLTestHandler(t, st)

	r := httptest.NewRequest("POST", "/api/v1/opml/import", strings.NewReader(sampleOPML))
	r.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got map[string]int
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 2, got["imported"])
	assert.Equal(t, 1, got["categories_created"])
	assert.Equal(t, 0, got["skipped_duplicates"])

	feeds, _ := st.Feeds().List(context.Background(), 1)
	require.Len(t, feeds, 2)
	cats, _ := st.Categories().List(context.Background(), 1)
	require.Len(t, cats, 1)
	assert.Equal(t, "News", cats[0].Name)

	for _, f := range feeds {
		assert.Equal(t, 3600, f.PollInterval)
		assert.False(t, f.Crawler)
		require.NotNil(t, f.NextPollAt)
	}
}

func TestOPML_ImportSkipsDuplicates(t *testing.T) {
	st := newFakeStore()
	h := newOPMLTestHandler(t, st)

	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/opml/import", strings.NewReader(sampleOPML))
		r.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := post()
	require.Equal(t, http.StatusOK, first.Code)
	second := post()
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())

	var got map[string]int
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &got))
	assert.Equal(t, 0, got["imported"])
	assert.Equal(t, 2, got["skipped_duplicates"])
	assert.Equal(t, 0, got["categories_created"])

	feeds, _ := st.Feeds().List(context.Background(), 1)
	assert.Len(t, feeds, 2)
}

func TestOPML_ImportMultipart(t *testing.T) {
	st := newFakeStore()
	h := newOPMLTestHandler(t, st)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "subs.opml")
	require.NoError(t, err)
	_, err = io.WriteString(fw, sampleOPML)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	r := httptest.NewRequest("POST", "/api/v1/opml/import", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	feeds, _ := st.Feeds().List(context.Background(), 1)
	assert.Len(t, feeds, 2)
}

func TestOPML_ImportRejectsMalformedXML(t *testing.T) {
	st := newFakeStore()
	h := newOPMLTestHandler(t, st)

	r := httptest.NewRequest("POST", "/api/v1/opml/import", strings.NewReader("<opml><body>"))
	r.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOPML_Export(t *testing.T) {
	st := newFakeStore()
	require.NoError(t, st.Categories().Create(context.Background(), &model.Category{UserID: 1, Name: "News"}))
	cats, _ := st.Categories().List(context.Background(), 1)
	catID := cats[0].ID
	siteHN := "https://hn.example"
	siteOrphan := "https://orphan.example"
	require.NoError(t, st.Feeds().Create(context.Background(), &model.Feed{
		UserID: 1, CategoryID: &catID, Title: "HN", FeedURL: "https://hn.example/rss", SiteURL: &siteHN,
	}))
	require.NoError(t, st.Feeds().Create(context.Background(), &model.Feed{
		UserID: 1, Title: "Orphan", FeedURL: "https://orphan.example/feed", SiteURL: &siteOrphan,
	}))

	h := newOPMLTestHandler(t, st)
	r := httptest.NewRequest("GET", "/api/v1/opml/export", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "xml")

	subs, err := opml.Parse(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	require.Len(t, subs, 2)

	byURL := map[string]opml.Subscription{}
	for _, s := range subs {
		byURL[s.FeedURL] = s
	}
	assert.Equal(t, "News", byURL["https://hn.example/rss"].Category)
	assert.Equal(t, "", byURL["https://orphan.example/feed"].Category)
}
