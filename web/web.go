// Package web serves the embedded Ancra web pages.
package web

import (
	"embed"
	"net/http"
)

//go:embed *.html
var static embed.FS

func fileHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := static.ReadFile(name)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data) //nolint:errcheck
	}
}

// LandingHandler serves the public marketing landing page.
func LandingHandler() http.HandlerFunc { return fileHandler("landing.html") }

// AppHandler serves the developer portal (API key management, webhook config).
func AppHandler() http.HandlerFunc { return fileHandler("app.html") }

// DashboardHandler serves the internal admin dashboard.
func DashboardHandler() http.HandlerFunc { return fileHandler("index.html") }
