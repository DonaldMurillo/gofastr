// Package softdelete marks rows deleted instead of removing them: WithSoftDelete, and helpers that skip every scope gate.
package softdelete

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// SoftDeleteScope names the deleted_at IS NULL scope.
type SoftDeleteScope struct{}

// WithSoftDelete makes ent soft-deleting, so CRUD deletes by setting deleted_at.
func WithSoftDelete(ent *entity.Entity) *entity.Entity {
	if ent.Config.Scope == nil {
		ent.Config.Scope = &entity.ScopeConfig{}
	}
	ent.Config.Scope.SoftDelete = true
	return ent
}

// SoftDelete sets the row's deleted_at to now (UTC) with no tenant, owner or access check; authorise the caller first.
func SoftDelete(ctx context.Context, db *sql.DB, table string, id string) error {
	return setDeletedAt(ctx, db, table, id, "deleted_at = $2", time.Now().UTC())
}

// Restore clears the row's deleted_at with no tenant, owner or access check; authorise the caller first.
func Restore(ctx context.Context, db *sql.DB, table string, id string) error {
	return setDeletedAt(ctx, db, table, id, "deleted_at = NULL")
}

func setDeletedAt(ctx context.Context, db *sql.DB, table, id, setExpr string, setArgs ...any) error {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("softdelete: %w", err)
	}
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = $1", query.QuoteFolded(safeTable), setExpr)
	_, err = conn(ctx, db).ExecContext(ctx, q, append([]any{id}, setArgs...)...)
	return err
}

func conn(ctx context.Context, db *sql.DB) interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
} {
	if tx, ok := fwdb.TxFromContext(ctx); ok {
		return tx
	}
	return db
}

// ForceDelete removes the row for good with no tenant, owner or access check; authorise the caller first.
func ForceDelete(ctx context.Context, db *sql.DB, table string, id string) error {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("softdelete: force: %w", err)
	}
	qb := query.Delete(safeTable).Where("id = $1", id)
	q, args := qb.Build()
	_, err = conn(ctx, db).ExecContext(ctx, q, args...)
	return err
}

// WithTrashed reports whether the request has ?trashed=true; it authorises nothing.
func WithTrashed(r *http.Request) bool {
	return r.URL.Query().Get("trashed") == "true"
}

// ApplySoftDeleteFilter adds deleted_at IS NULL unless showTrashed.
func ApplySoftDeleteFilter(builder *query.QueryBuilder, showTrashed bool) {
	if !showTrashed {
		builder.Where("deleted_at IS NULL")
	}
}
