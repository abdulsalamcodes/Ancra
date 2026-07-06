// Package web serves the embedded Ancra admin dashboard.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
var static embed.FS

// Handler returns an http.Handler that serves the dashboard at /.
// API routes registered before this handler take priority.
func Handler() http.Handler {
	sub, _ := fs.Sub(static, ".")
	return http.FileServer(http.FS(sub))
}
