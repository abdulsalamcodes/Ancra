package middleware

import (
	"net/http"
	"sync"
)

// idempotencyStore is a simple in-memory set of seen Idempotency-Key values.
// In production this should be backed by Redis or Postgres for multi-instance
// deployments. For now it provides correctness within a single process.
type idempotencyStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

var globalIdempotency = &idempotencyStore{seen: make(map[string]struct{})}

// Idempotency rejects duplicate requests that carry a previously-seen
// Idempotency-Key header. Only state-mutating methods (POST, PUT, PATCH,
// DELETE) are checked; GET and HEAD pass through unchecked.
func Idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			// No key supplied — allow through (key is optional for non-critical paths).
			next.ServeHTTP(w, r)
			return
		}

		globalIdempotency.mu.Lock()
		_, duplicate := globalIdempotency.seen[key]
		if !duplicate {
			globalIdempotency.seen[key] = struct{}{}
		}
		globalIdempotency.mu.Unlock()

		if duplicate {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"duplicate request — this Idempotency-Key has already been processed"}`)) //nolint:errcheck
			return
		}

		next.ServeHTTP(w, r)
	})
}
