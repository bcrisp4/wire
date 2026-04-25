package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves files from spaFS, falling back to index.html for unknown paths
// (so client-side routing works on deep links).
func SPAHandler(spaFS fs.FS) http.Handler { return spaHandler(spaFS) }

func spaHandler(spaFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(spaFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject API paths — should already be routed elsewhere by the mux.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(spaFS, clean); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback: serve index.html so the client-side router can take over.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
