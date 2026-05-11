package framework

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/framework/entity"
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
	q := "UPDATE " + table + " SET deleted_at = NOW() WHERE id = $1"
	_, err := db.ExecContext(ctx, q, id)
	return err
}

// Restore clears the deleted_at field, making a soft-deleted record visible again.
func Restore(ctx context.Context, db *sql.DB, table string, id string) error {
	q := "UPDATE " + table + " SET deleted_at = NULL WHERE id = $1"
	_, err := db.ExecContext(ctx, q, id)
	return err
}

// ForceDelete permanently removes a record from the database.
// This bypasses soft delete and performs a real DELETE.
func ForceDelete(ctx context.Context, db *sql.DB, table string, id string) error {
	qb := query.Delete(table).Where("id = $1", id)
	q, args := qb.Build()
	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// WithTrashed checks whether the request asks to include soft-deleted records.
// Returns true when the query parameter ?trashed=true is present.
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
