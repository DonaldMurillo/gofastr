package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

type (
	auditRequestKey  struct{}
	auditPreImageKey struct{}
	auditRecordKey   struct{}
)

type auditRecord struct{ entity, id string }

// AuditRecordIDFor returns the record id of the row the running
// AfterCreate/AfterUpdate chain was handed, read from the handler's own
// key columns, when that chain runs for entity; false otherwise.
func AuditRecordIDFor(ctx context.Context, entity string) (string, bool) {
	r, ok := ctx.Value(auditRecordKey{}).(auditRecord)
	if !ok || r.entity != entity || r.id == "" {
		return "", false
	}
	return r.id, true
}

// WithAuditRequest returns ctx with r attached for audit hooks to read;
// a nil r returns ctx unchanged.
func WithAuditRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, auditRequestKey{}, r)
}

// AuditRequestFromContext returns the *http.Request WithAuditRequest
// attached, or nil.
func AuditRequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(auditRequestKey{}).(*http.Request)
	return r
}

// WithAuditPreImage stores the pre-change row in ctx for AfterUpdate and
// AfterDelete hooks. row is keyed in the handler's JSONCase, as a
// response is; a nil row returns ctx unchanged.
func WithAuditPreImage(ctx context.Context, row map[string]any) context.Context {
	if row == nil {
		return ctx
	}
	return context.WithValue(ctx, auditPreImageKey{}, row)
}

// AuditPreImageFromContext returns the pre-image an update or delete
// captured, or nil. Its keys are in the handler's JSONCase (camelCase by
// default, "statusId"), not the snake_case column names a Before hook's
// body uses; AuditPreImageAs and AuditPreImageSnakeFromContext give the
// other shapes.
func AuditPreImageFromContext(ctx context.Context) map[string]any {
	row, _ := ctx.Value(auditPreImageKey{}).(map[string]any)
	return row
}

// AuditPreImageAs decodes the pre-image into T through UnmarshalEntity,
// so T carries camelCase json tags whatever the handler's JSONCase. It
// returns false when there is no pre-image or it does not decode into T.
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

// AuditPreImageSnakeFromContext returns the pre-image keyed by snake_case
// column names whatever the handler's JSONCase, or nil when there is none.
func AuditPreImageSnakeFromContext(ctx context.Context) map[string]any {
	row := AuditPreImageFromContext(ctx)
	if row == nil {
		return nil
	}
	return casing.MapToSnake(row)
}
