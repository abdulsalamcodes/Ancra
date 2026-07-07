// Package tenant provides request-scoped helpers for multi-tenant data access.
// Both HTTP middleware and domain services import this package so that org
// identity can flow through context without coupling those layers to each other.
package tenant

import "context"

type contextKey struct{}

// WithOrgID returns a child context that carries the given organisation ID.
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, contextKey{}, orgID)
}

// OrgIDFromContext returns the organisation ID stored in ctx, or an empty
// string if none was set.
func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}
