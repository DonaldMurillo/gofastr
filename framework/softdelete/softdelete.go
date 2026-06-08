package softdelete

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// SoftDeleteScope is a query scope that filters out soft-deleted records.
// When applied to a QueryBuilder, it adds WHERE deleted_at IS NULL.
type SoftDeleteScope struct{}

// WithSoftDelete configures an entity for soft delete support.
// Sets the SoftDelete flag so the framework knows to use UPDATE instead of DELETE.
func WithSoftDelete(ent *entity.Entity) *entity.Entity {
	ent.Config.SoftDelete = true
	return ent
}

// SoftDelete marks a record as deleted by setting deleted_at to NOW().
// The record remains in the database but will be excluded from normal queries.
func SoftDelete(ctx context.Context, db *sql.DB, table string, id string) error {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("softdelete: %w", err)
	}
	q := fmt.Sprintf("UPDATE %s SET deleted_at = NOW() WHERE id = $1", query.QuoteIdent(safeTable))
	_, err = db.ExecContext(ctx, q, id)
	return err
}

// Restore clears the deleted_at field, making a soft-deleted record visible again.
//
// SECURITY — UNSCOPED OPERATION: this function issues UPDATE … WHERE id = $1
// with NO tenant, owner, or access-control filter. Any id supplied will be
// restored regardless of which tenant or user owns it. Call this only after
// you have independently verified that the caller is authorised to restore that
// specific record (e.g. behind an admin gate, or after an explicit ownership
// check). Using this helper in a user-facing endpoint without such a check
// creates a cross-tenant / IDOR vulnerability.
func Restore(ctx context.Context, db *sql.DB, table string, id string) error {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("softdelete: restore: %w", err)
	}
	q := fmt.Sprintf("UPDATE %s SET deleted_at = NULL WHERE id = $1", query.QuoteIdent(safeTable))
	_, err = db.ExecContext(ctx, q, id)
	return err
}

// ForceDelete permanently removes a record from the database.
// This bypasses soft delete and performs a real DELETE.
//
// SECURITY — UNSCOPED OPERATION: this function issues DELETE … WHERE id = $1
// with NO tenant, owner, or access-control filter. Any id supplied will be
// permanently deleted regardless of which tenant or user owns it. Call this
// only after you have independently verified that the caller is authorised to
// permanently delete that specific record (e.g. behind an admin gate, or after
// an explicit ownership check). Using this helper in a user-facing endpoint
// without such a check creates a cross-tenant / IDOR vulnerability and
// irreversible data loss.
func ForceDelete(ctx context.Context, db *sql.DB, table string, id string) error {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("softdelete: force: %w", err)
	}
	qb := query.Delete(safeTable).Where("id = $1", id)
	q, args := qb.Build()
	_, err = db.ExecContext(ctx, q, args...)
	return err
}

// WithTrashed checks whether the request asks to include soft-deleted records.
// Returns true when the query parameter ?trashed=true is present.
//
// SECURITY — CALLER MUST AUTHORISE: this function only parses the request
// parameter; it performs no access-control check of its own. If you pass its
// result to ApplySoftDeleteFilter (or build your own query that omits the
// deleted_at IS NULL clause), you must first confirm that the caller has
// permission to view deleted records (e.g. admin-only). Exposing trashed
// records to unprivileged users leaks data that the application logically
// treats as deleted.
func WithTrashed(r *http.Request) bool {
	return r.URL.Query().Get("trashed") == "true"
}

// ApplySoftDeleteFilter adds a WHERE deleted_at IS NULL clause to the query
// unless showTrashed is true. Call this when building list/get queries for
// entities that have soft delete enabled.
func ApplySoftDeleteFilter(builder *query.QueryBuilder, showTrashed bool) {
	if !showTrashed {
		builder.Where("deleted_at IS NULL")
	}
}
