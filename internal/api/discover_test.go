package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bcrisp4/wire/internal/discover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installPermissiveDiscoverGuard relaxes the discover SSRF guard so handler
// tests can reach the httptest server on 127.0.0.1.
func installPermissiveDiscoverGuard(t *testing.T) {
	t.Helper()
	restore := discover.SetValidateURLForTest(func(string) error { return nil })
	t.Cleanup(restore)
}

// TestDiscoverHandler_EmptyBodyReturns400 confirms that a POST without a JSON
// body (or without a url field) is rejected with 400.
func TestDiscoverHandler_EmptyBodyReturns400(t *testing.T) {
	h := discoverHandler(http.DefaultClient)

	r := httptest.NewRequest("POST", "/api/v1/feeds/discover", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDiscoverHandler_MissingURLReturns400 confirms that a JSON body without a
// url field is rejected.
func TestDiscoverHandler_MissingURLReturns400(t *testing.T) {
	h := discoverHandler(http.DefaultClient)

	r := httptest.NewRequest("POST", "/api/v1/feeds/discover", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDiscoverHandler_ValidURLReturnsCandidates uses a mock HTTP server to
// stand in for the target site, then confirms the handler returns the
// discovered candidates as JSON.
func TestDiscoverHandler_ValidURLReturnsCandidates(t *testing.T) {
	installPermissiveDiscoverGuard(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="alternate" type="application/rss+xml" title="My Blog" href="/feed.xml">
</head><body></body></html>`))
	}))
	defer target.Close()

	h := discoverHandler(target.Client())

	body := `{"url":"` + target.URL + `/"}`
	r := httptest.NewRequest("POST", "/api/v1/feeds/discover", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	raw, err := io.ReadAll(w.Body)
	require.NoError(t, err)

	var got struct {
		Candidates []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Candidates, 1)
	assert.Equal(t, target.URL+"/feed.xml", got.Candidates[0].URL)
	assert.Equal(t, "My Blog", got.Candidates[0].Title)
	assert.Equal(t, "rss", got.Candidates[0].Type)
}

// TestDiscoverHandler_ValidationErrorReturns400 confirms that a syntactically
// valid JSON body whose URL fails the discover guard (bad scheme, blocked
// host) is rejected with 400 rather than 502, so clients can distinguish bad
// input from upstream failures. The production guard is in effect here — no
// stub — because the URL never reaches the network.
func TestDiscoverHandler_ValidationErrorReturns400(t *testing.T) {
	h := discoverHandler(http.DefaultClient)

	r := httptest.NewRequest("POST", "/api/v1/feeds/discover",
		strings.NewReader(`{"url":"file:///etc/passwd"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDiscoverHandler_NoCandidatesReturnsEmptyArray ensures the response shape
// is stable (an array, not null) when nothing is found.
func TestDiscoverHandler_NoCandidatesReturnsEmptyArray(t *testing.T) {
	installPermissiveDiscoverGuard(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head></head><body></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	h := discoverHandler(target.Client())

	body := `{"url":"` + target.URL + `/"}`
	r := httptest.NewRequest("POST", "/api/v1/feeds/discover", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"candidates":[]`)
}
