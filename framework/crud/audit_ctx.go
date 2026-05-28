package crud

import (
	"context"
	"net/http"
)

// Context-key types for audit metadata that the CRUD handlers stash so the
// AfterCreate/AfterUpdate/AfterDelete hooks (which only receive ctx) can
// reach the live *http.Request and any pre-image captured before an UPDATE
// or DELETE statement ran. Audit hooks read these via the exported helpers
// below; production callers should not need to set them by hand.
type (
	auditRequestKey  struct{}
	auditPreImageKey struct{}
)

// WithAuditRequest returns ctx with r attached so audit hooks can read
// client-IP / user-agent / headers in their AfterCreate/Update/Delete
// callbacks. CRUD's HTTP entry points call this before any DB work.
func WithAuditRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, auditRequestKey{}, r)
}

// AuditRequestFromContext returns the *http.Request that CRUD attached,
// or nil if none was set (e.g. async hook fired outside a request).
func AuditRequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(auditRequestKey{}).(*http.Request)
	return r
}

// WithAuditPreImage stores the pre-change row snapshot in ctx so the
// AfterUpdate / AfterDelete hooks can diff it against the new state.
// doUpdate / doDelete populate this with a freshly SELECTed copy before
// the mutating statement runs.
func WithAuditPreImage(ctx context.Context, row map[string]any) context.Context {
	if row == nil {
		return ctx
	}
	return context.WithValue(ctx, auditPreImageKey{}, row)
}

// AuditPreImageFromContext returns the pre-image set by doUpdate or
// doDelete, or nil when no snapshot was captured (legacy callers / async
// hooks).
func AuditPreImageFromContext(ctx context.Context) map[string]any {
	row, _ := ctx.Value(auditPreImageKey{}).(map[string]any)
	return row
}
