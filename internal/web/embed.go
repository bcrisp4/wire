// Package web embeds the built SvelteKit SPA produced by `cd web && npm run build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded SPA filesystem rooted at "dist".
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
