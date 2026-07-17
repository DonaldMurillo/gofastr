package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
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
//
// Casing: row is whatever selectPreImage produced, which runs the SELECT
// results through the handler's own scanRow/convertKey pipeline — the same
// key-casing step that shapes every CRUD response. Callers of this function
// (doUpdate, doDelete) must pass a row that already carries the handler's
// configured JSONCase; do not pass a raw snake_case DB row here.
func WithAuditPreImage(ctx context.Context, row map[string]any) context.Context {
	if row == nil {
		return ctx
	}
	return context.WithValue(ctx, auditPreImageKey{}, row)
}

// AuditPreImageFromContext returns the pre-image set by doUpdate or
// doDelete, or nil when no snapshot was captured (legacy callers / async
// hooks).
//
// # Casing contract
//
// The returned map is keyed by the handler's configured JSONCase —
// camelCase by default (e.g. "statusId"), NOT the underlying snake_case
// DB column name ("status_id"). This is the same casing every CRUD
// response uses, but it differs from the BeforeCreate/BeforeUpdate hook
// body, which the framework unconverts back to snake_case before hooks
// run. A hook that does `pre["status_id"]` against a default (camelCase)
// handler silently gets nothing back — no panic, no error, just a missing
// key — because the actual key is "statusId". Casing-identical keys
// (e.g. "version", "key") happen to work either way, which is what makes
// this easy to miss in review.
//
// Two ways to avoid the mismatch:
//   - AuditPreImageAs[T] decodes into a struct with camelCase json tags,
//     matching the same shape typed hooks (framework.OnAfterUpdate etc.)
//     already receive.
//   - AuditPreImageSnakeFromContext returns the row re-keyed to snake_case
//     DB column names, for callers that want map access without a struct.
func AuditPreImageFromContext(ctx context.Context) map[string]any {
	row, _ := ctx.Value(auditPreImageKey{}).(map[string]any)
	return row
}

// AuditPreImageAs decodes the pre-update/pre-delete snapshot into T.
//
// It reuses the exact casing translation typed hooks and typed queries
// already use (see UnmarshalEntity / unmarshalRowToStruct): the row's keys
// are normalized to camelCase before json.Unmarshal, so T's fields should
// carry ordinary camelCase `json:"..."` tags — the same tags a generated
// entity struct already has. This makes the pre-image and a typed hook's
// payload "speak the same language" regardless of the handler's configured
// JSONCase.
//
// Returns the zero value and false when no pre-image was captured for this
// context, or when decoding into T fails (shape mismatch). Use
// AuditPreImageFromContext directly if you need to distinguish those cases
// or need the raw map.
func AuditPreImageAs[T any](ctx context.Context) (T, bool) {
	var out T
	row := AuditPreImageFromContext(ctx)
	if row == nil {
		return out, false
	}
	if err := UnmarshalEntity(row, &out); err != nil {
		return out, false
	}
	return out, true
}

// AuditPreImageSnakeFromContext returns the pre-image re-keyed to
// snake_case DB column names, regardless of the handler's configured
// JSONCase. It applies the same casing.MapToSnake translation
// CrudHandler.unconvertMapKeys uses to turn a camelCase request body back
// into DB columns — no second translator, just the shared helper.
//
// Prefer AuditPreImageAs[T] when a typed shape is available; this exists
// for hooks that want plain map[string]any access keyed the way every
// other hook-adjacent surface (columns, typed-query API, blueprint fields)
// already speaks.
//
// Returns nil when no pre-image was captured for this context.
func AuditPreImageSnakeFromContext(ctx context.Context) map[string]any {
	row := AuditPreImageFromContext(ctx)
	if row == nil {
		return nil
	}
	return casing.MapToSnake(row)
}
