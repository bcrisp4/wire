package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves files from spaFS, falling back to index.html for unknown
// paths so client-side routing works on deep links.
func SPAHandler(spaFS fs.FS) http.Handler { return spaHandler(spaFS) }

func spaHandler(spaFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(spaFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defense-in-depth: even if the mux's /api/v1/* routes are misconfigured,
		// the SPA handler must never satisfy /api/ requests.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean != "" {
			if _, err := fs.Stat(spaFS, clean); err != nil {
				http.ServeFileFS(w, r, spaFS, "index.html")
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
