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

// AuthHandler serves the signup / login page.
func AuthHandler() http.HandlerFunc { return fileHandler("auth.html") }

// DashboardHandler serves the JWT-gated developer dashboard.
func DashboardHandler() http.HandlerFunc { return fileHandler("dashboard.html") }

// AdminHandler serves the operator admin dashboard.
func AdminHandler() http.HandlerFunc { return fileHandler("admin.html") }
