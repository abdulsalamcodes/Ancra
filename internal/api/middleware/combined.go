package middleware

import (
	"net/http"
	"strings"

	"github.com/abdulsalamcodes/ancra/internal/domain/auth"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// JWTOrAPIKeyAuth accepts either a JWT session token or an API key bearer.
//
// All JWTs are base64url-encoded JSON; the header always encodes as "eyJ"
// (base64url of '{"'). API keys use the "ancra_" prefix. The prefix
// determines which validator runs — no response-recorder tricks needed.
//
// This lets a single route registration serve both the dashboard (JWT) and
// programmatic API clients (API key), keeping the router DRY.
func JWTOrAPIKeyAuth(authSvc *auth.Service, keys store.APIKeyStore, staticKey, staticKeyOrgID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		jwtHandler    := JWTAuth(authSvc)(next)
		apiKeyHandler := APIKeyAuth(keys, staticKey, staticKeyOrgID)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isJWTToken(extractBearer(r)) {
				jwtHandler.ServeHTTP(w, r)
			} else {
				apiKeyHandler.ServeHTTP(w, r)
			}
		})
	}
}

func isJWTToken(token string) bool {
	return strings.HasPrefix(token, "eyJ")
}
