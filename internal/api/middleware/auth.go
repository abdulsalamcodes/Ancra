// Package middleware contains HTTP middleware for the Ancra API.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// cachedKey holds a recently-validated key to avoid a DB hit on every request.
type cachedKey struct {
	key       *store.APIKey
	expiresAt time.Time
}

var (
	keyCache sync.Map // map[string]*cachedKey  (hash → entry)
	cacheTTL = 60 * time.Second
)

// APIKeyAuth returns middleware that validates the Authorization: Bearer header
// against the api_keys table (with a 60-second in-process cache).
// If staticKey is non-empty it is also accepted as a fallback legacy key.
func APIKeyAuth(keys store.APIKeyStore, staticKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				http.Error(w, `{"error":"missing or malformed Authorization header"}`, http.StatusUnauthorized)
				return
			}

			// Fast path: legacy static key.
			if staticKey != "" && raw == staticKey {
				next.ServeHTTP(w, r)
				return
			}

			hash := hashKey(raw)

			// Cache lookup.
			if entry, ok := keyCache.Load(hash); ok {
				ck := entry.(*cachedKey)
				if time.Now().Before(ck.expiresAt) {
					go func() { _ = keys.TouchLastUsed(context.Background(), ck.key.ID) }()
					next.ServeHTTP(w, r)
					return
				}
				keyCache.Delete(hash)
			}

			// DB lookup.
			k, err := keys.GetByHash(r.Context(), hash)
			if err != nil {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
				return
			}

			keyCache.Store(hash, &cachedKey{key: k, expiresAt: time.Now().Add(cacheTTL)})
			go func() { _ = keys.TouchLastUsed(context.Background(), k.ID) }()
			next.ServeHTTP(w, r)
		})
	}
}

// AdminAuth returns middleware that validates the Admin-Secret header.
// If secret is empty the admin routes are disabled entirely.
func AdminAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				http.Error(w, `{"error":"admin routes disabled: ADMIN_SECRET not configured"}`, http.StatusServiceUnavailable)
				return
			}
			if r.Header.Get("Admin-Secret") != secret {
				http.Error(w, `{"error":"invalid admin secret"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// InvalidateKey removes a key hash from the in-process cache.
// Call this immediately after revoking a key to prevent the TTL lag.
func InvalidateKey(hash string) {
	keyCache.Delete(hash)
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
