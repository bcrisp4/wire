package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bcrisp4/wire/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCategoriesTestServer builds a Server with a fresh, migrated SQLite db
// and returns an httptest server that delegates to the same chained handler
// stack as production (via NewServer's mux).
func newCategoriesTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, store.Migrate(context.Background(), db))

	srv, err := NewServer(Options{
		Listen: "127.0.0.1:0",
		Logger: slogDiscard(),
		Store:  store.New(db),
	})
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestCategories_ListEmpty(t *testing.T) {
	ts := newCategoriesTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/categories")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Empty(t, got)
}

func TestCategories_CreateAndList(t *testing.T) {
	ts := newCategoriesTestServer(t)

	resp, err := http.Post(ts.URL+"/api/v1/categories", "application/json",
		bytes.NewBufferString(`{"name":"News"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created map[string]any
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &created))
	assert.Equal(t, "News", created["name"])
	assert.NotZero(t, created["id"])

	// List should now contain the created category.
	resp2, err := http.Get(ts.URL + "/api/v1/categories")
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(body2, &got))
	require.Len(t, got, 1)
	assert.Equal(t, "News", got[0]["name"])
}

func TestCategories_CreateRejectsBlankName(t *testing.T) {
	ts := newCategoriesTestServer(t)

	resp, err := http.Post(ts.URL+"/api/v1/categories", "application/json",
		bytes.NewBufferString(`{"name":""}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCategories_CreateRejectsTrailingJSON(t *testing.T) {
	ts := newCategoriesTestServer(t)

	resp, err := http.Post(ts.URL+"/api/v1/categories", "application/json",
		bytes.NewBufferString(`{"name":"News"}{"name":"Tech"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCategories_CreateConflictOnDuplicate(t *testing.T) {
	ts := newCategoriesTestServer(t)

	post := func() *http.Response {
		resp, err := http.Post(ts.URL+"/api/v1/categories", "application/json",
			bytes.NewBufferString(`{"name":"News"}`))
		require.NoError(t, err)
		return resp
	}
	r1 := post()
	r1.Body.Close()
	require.Equal(t, http.StatusCreated, r1.StatusCode)

	r2 := post()
	defer r2.Body.Close()
	assert.Equal(t, http.StatusConflict, r2.StatusCode)
}

func TestCategories_Rename(t *testing.T) {
	ts := newCategoriesTestServer(t)

	id := mustCreateCategory(t, ts, "News")

	req, err := http.NewRequest(http.MethodPut,
		ts.URL+"/api/v1/categories/"+strconv.FormatInt(id, 10),
		bytes.NewBufferString(`{"name":"Tech"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify the rename took effect.
	listResp, err := http.Get(ts.URL + "/api/v1/categories")
	require.NoError(t, err)
	defer listResp.Body.Close()
	var got []map[string]any
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&got))
	require.Len(t, got, 1)
	assert.Equal(t, "Tech", got[0]["name"])
}

func TestCategories_RenameMissingReturns404(t *testing.T) {
	ts := newCategoriesTestServer(t)

	req, _ := http.NewRequest(http.MethodPut,
		ts.URL+"/api/v1/categories/9999",
		bytes.NewBufferString(`{"name":"Whatever"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCategories_RenameConflictOnDuplicate(t *testing.T) {
	ts := newCategoriesTestServer(t)

	mustCreateCategory(t, ts, "News")
	techID := mustCreateCategory(t, ts, "Tech")

	req, _ := http.NewRequest(http.MethodPut,
		ts.URL+"/api/v1/categories/"+strconv.FormatInt(techID, 10),
		bytes.NewBufferString(`{"name":"News"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestCategories_Delete(t *testing.T) {
	ts := newCategoriesTestServer(t)

	id := mustCreateCategory(t, ts, "News")

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/categories/"+strconv.FormatInt(id, 10), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	listResp, err := http.Get(ts.URL + "/api/v1/categories")
	require.NoError(t, err)
	defer listResp.Body.Close()
	var got []map[string]any
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&got))
	assert.Empty(t, got)
}

func TestCategories_DeleteMissingReturns404(t *testing.T) {
	ts := newCategoriesTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/categories/9999", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func mustCreateCategory(t *testing.T, ts *httptest.Server, name string) int64 {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/v1/categories", "application/json",
		bytes.NewBufferString(`{"name":"`+name+`"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.NotZero(t, created.ID)
	return created.ID
}

