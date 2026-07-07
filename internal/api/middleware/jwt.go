package middleware

import (
	"context"
	"net/http"

	"github.com/abdulsalamcodes/ancra/internal/domain/auth"
	"github.com/abdulsalamcodes/ancra/internal/tenant"
)

type contextKey string

const (
	contextKeyUserID contextKey = "user_id"
	contextKeyEmail  contextKey = "email"
	contextKeyRole   contextKey = "role"
)

// JWTAuth returns middleware that validates the Authorization: Bearer header as
// a signed JWT. Valid claims are injected into the request context.
// OrgID is stored via the shared tenant package so both JWT and API-key auth
// paths use the same context key for downstream handlers.
func JWTAuth(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				http.Error(w, `{"error":"missing or malformed Authorization header"}`, http.StatusUnauthorized)
				return
			}

			claims, err := authSvc.ParseToken(raw)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, contextKeyEmail, claims.Email)
			ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
			ctx = tenant.WithOrgID(ctx, claims.OrgID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext returns the authenticated user's ID from the request context.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyUserID).(string)
	return v
}

// RoleFromContext returns the authenticated user's role from the request context.
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyRole).(string)
	return v
}
