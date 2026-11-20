package handler

import "context"

type userKey struct{}
type tenantKey struct{}
type tenantIDKey struct{}
type requestIDKey struct{}
type loggerKey struct{}

// SetUser stores user in ctx and drops the principal checks installed for
// the previous user.
func SetUser(ctx context.Context, user any) context.Context {
	if cur, _ := ctx.Value(principalCheckKey{}).(*principalChecks); cur != nil {
		ctx = context.WithValue(ctx, principalCheckKey{}, (*principalChecks)(nil))
	}
	return context.WithValue(ctx, userKey{}, user)
}

// GetUser retrieves the user value from the context.
func GetUser(ctx context.Context) (any, bool) {
	u, ok := ctx.Value(userKey{}).(any)
	return u, ok
}

// SetTenant stores a tenant value in the context.
func SetTenant(ctx context.Context, tenant any) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// GetTenant retrieves the tenant value from the context.
func GetTenant(ctx context.Context) (any, bool) {
	t, ok := ctx.Value(tenantKey{}).(any)
	return t, ok
}

// SetTenantID stores the tenant ID tenant scoping filters on.
func SetTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tenantIDKey{}, id)
}

// GetTenantID returns the tenant ID SetTenantID stored, or "" when none was.
func GetTenantID(ctx context.Context) string {
	id, _ := ctx.Value(tenantIDKey{}).(string)
	return id
}

// SetRequestID stores a request ID in the context.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// SetLogger stores a logger in the context.
func SetLogger(ctx context.Context, logger any) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// GetLogger retrieves the logger from the context.
func GetLogger(ctx context.Context) (any, bool) {
	l, ok := ctx.Value(loggerKey{}).(any)
	return l, ok
}
