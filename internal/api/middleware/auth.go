// Package middleware contains HTTP middleware for the Ancra API.
package middleware

import (
	"net/http"
	"strings"
)

// APIKeyAuth returns a middleware that validates the Authorization header
// against the configured API key. Expected format: "Bearer <api_key>".
func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, `{"error":"Authorization header must be Bearer <token>"}`, http.StatusUnauthorized)
				return
			}

			if parts[1] != apiKey {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
