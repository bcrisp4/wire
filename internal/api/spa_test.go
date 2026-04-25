package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestSPA_ServesIndex(t *testing.T) {
	mfs := fstest.MapFS{
		"index.html":                     &fstest.MapFile{Data: []byte("<html>Wire</html>")},
		"app.js":                         &fstest.MapFile{Data: []byte("console.log(1)")},
		"_app/immutable/chunks/main.js":  &fstest.MapFile{Data: []byte("// chunk")},
	}
	var spaFS fs.FS = mfs
	h := spaHandler(spaFS)

	cases := map[string]string{
		"/":                                       "Wire",
		"/app.js":                                 "console.log(1)",
		"/_app/immutable/chunks/main.js":          "// chunk",
		"/deep/path/that/does/not/exist":          "Wire", // SPA fallback
		"/saved":                                  "Wire", // SPA fallback for client-routed paths
	}
	for path, wantBody := range cases {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), wantBody)
		})
	}
}

func TestSPA_DoesNotMaskAPI(t *testing.T) {
	h := spaHandler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("hi")}})
	r := httptest.NewRequest("GET", "/api/v1/something", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSPAHandler_PublicWrapper(t *testing.T) {
	// SPAHandler is the exported alias used by cmd/wire.
	h := SPAHandler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}})
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}
