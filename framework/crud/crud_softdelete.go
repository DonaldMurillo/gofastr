package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Sentinels the two soft-delete operations answer with. In-process callers
// map them to responses the way writeCRUDError maps errNotFound to 404:
// ErrNoSoftDelete is a programming error (calling restore/purge on an
// entity that never declared Scope.SoftDelete), ErrNotSoftDeleted is a
// caller error (restoring a live row, purging a row that must be deleted
// first).
var (
	ErrNoSoftDelete   = errors.New("crud: entity does not use soft delete")
	ErrNotSoftDeleted = errors.New("crud: record is not soft-deleted")
)

// ErrForbidden is wrapped around every RestoreOne/PurgeOne access
// refusal, so an in-process caller can tell a permission denial from a
// not-found (the two share a 40x shape but mean different things to an
// admin screen). Not seen by writeCRUDError: both operations are
// in-process only, no HTTP route maps their errors.
var ErrForbidden = errors.New("crud: forbidden")

type auditOperationKey struct{}

// auditOperation is the ctx-carried override RestoreOne and PurgeOne
// stamp: the operation the audit row should name, keyed to the ONE
// entity and record the operation is about. The key is what keeps the
// override from leaking into nested writes: a hook on entity A that
// updates a row of entity B during A's restore must leave B's audit row
// saying "update", so only an (entity, id) match answers the override.
type auditOperation struct {
	entity string
	id     string
	op     string
}

// withAuditOperation overrides the operation name audit hooks record for
// the writes whose ctx carries it. Unexported so only this package names
// an operation: app code cannot forge a "purge" row in the trail.
// RestoreOne sets "restore" and PurgeOne "purge" for the one record they
// act on, before running the ordinary update/delete hook chains, so an
// audit row (framework.WithAuditLog) says what actually happened instead
// of "update"/"delete" — for THAT record, and for nothing a hook writes
// on the way through. The value flows only on ctx, never a request body,
// and is sanitized by the audit writer.
func withAuditOperation(ctx context.Context, entity, id, op string) context.Context {
	if op == "" {
		return ctx
	}
	return context.WithValue(ctx, auditOperationKey{}, auditOperation{entity: entity, id: id, op: op})
}

// withoutAuditOperation drops any override ctx carries. inTx calls it on
// entry, so the override set inside one write never reaches a write a
// hook starts from it.
func withoutAuditOperation(ctx context.Context) context.Context {
	if _, ok := ctx.Value(auditOperationKey{}).(auditOperation); !ok {
		return ctx
	}
	return context.WithValue(ctx, auditOperationKey{}, auditOperation{})
}

// AuditOperationFor returns the operation override stamped for exactly
// this entity and record id, or "" when none matches. Audit hooks
// consult it for the op column; empty means "use the hook's own
// operation". The entity and id match is load-bearing: a hook chain may
// write other entities' rows inside one restore or purge, and those
// rows' audit entries must name their own operation, not the outer one.
func AuditOperationFor(ctx context.Context, entity, id string) string {
	o, ok := ctx.Value(auditOperationKey{}).(auditOperation)
	if !ok || o.entity != entity || o.id != id {
		return ""
	}
	return o.op
}

// RestoreOne clears the soft-delete marker on one record: the admin's
// Deleted view's Restore action, in-process only. It behaves like an
// update: the entity's update access permission is asked through the same
// access.CanResource call the update route makes — so a resource-aware
// Decider on ctx can deny the one record — and the BeforeUpdate and
// AfterUpdate hooks, the audit row (operation "restore") and the
// entity.updated event all fire as they do for UpdateOne. A permission
// denial is wrapped in ErrForbidden; WithServerWrites skips the
// permission question, matching every other trusted-write escape hatch;
// owner and tenant context are still required.
//
// Visibility answers the read question first: a row the caller cannot see
// under owner and tenant scoping answers errNotFound, the same error a
// read of that id gives, so another owner's id discloses nothing. A row
// that is visible but not soft-deleted answers ErrNotSoftDeleted. An
// entity without Scope.SoftDelete answers ErrNoSoftDelete before any SQL.
func (ch *CrudHandler) RestoreOne(ctx context.Context, id string) error {
	if !ch.Entity.Config.Scope.SoftDelete {
		return ErrNoSoftDelete
	}
	if err := ch.requireOwnerContext(ctx); err != nil {
		return err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return err
	}
	if !serverWrites(ctx) && !ch.itemPermitted(ctx, opUpdate, id) {
		return fmt.Errorf("%w: missing permission %s", ErrForbidden, ch.permissionForOp(opUpdate))
	}
	req := syntheticRequest(ctx, http.MethodPatch, "/")
	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doRestore(ctx, req, id)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return err
	}
	ch.EmitEvent(ctx, event.EntityUpdated, result)
	return nil
}

// PurgeOne hard-deletes a record that is ALREADY soft-deleted: the
// admin's Delete permanently action. The soft delete is never skipped: a
// live row answers ErrNotSoftDeleted, so a caller cannot turn purge into
// a hard delete that bypasses the retention window soft delete exists
// for. Otherwise it mirrors the delete path: the delete access permission
// and the Decider are asked about the one record, BeforeDelete and
// AfterDelete hooks run, the audit row carries operation "purge", and the
// entity.deleted event fires. Owner and tenant scoping answer visibility
// exactly as RestoreOne's. In-process only; no HTTP route mounts it.
func (ch *CrudHandler) PurgeOne(ctx context.Context, id string) error {
	if !ch.Entity.Config.Scope.SoftDelete {
		return ErrNoSoftDelete
	}
	if err := ch.requireOwnerContext(ctx); err != nil {
		return err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return err
	}
	if !serverWrites(ctx) && !ch.itemPermitted(ctx, opDelete, id) {
		return fmt.Errorf("%w: missing permission %s", ErrForbidden, ch.permissionForOp(opDelete))
	}
	req := syntheticRequest(ctx, http.MethodDelete, "/")
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		return ch.doPurge(ctx, req, id)
	})
	if err != nil {
		return err
	}
	ch.EmitEvent(ctx, event.EntityDeleted, map[string]any{ch.convertKey(ch.PrimaryKey): id})
	return nil
}

// selectTrashedRow reads one row by id under tenant and owner-WRITE
// scoping, soft-deleted or not, so its caller can tell the three cases
// apart: absent under scope (sql.ErrNoRows → errNotFound), present and
// live (ErrNotSoftDeleted), present and soft-deleted (the restore/purge
// target). The write scope, not the read scope, decides visibility: a
// CrossOwnerRead grant may read a foreign row but must not revive or
// destroy it, so the lift never applies here, exactly as it never applies
// to ApplyOwnerScopeUpdate. The row doubles as the audit pre-image.
func (ch *CrudHandler) selectTrashedRow(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	cols := withDeletedAtColumn(ch.visibleFields())
	qb := query.Select(cols...).
		From(ch.Entity.GetTable()).
		Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	sqlStr, args := qb.Build()
	return ch.scanOne(ch.DB.QueryRowContext(ctx, sqlStr, args...), cols)
}

// withDeletedAtColumn appends the soft-delete marker to a projection
// unless the entity declared a deleted_at field of its own, in which
// case it is already there.
func withDeletedAtColumn(cols []string) []string {
	for _, c := range cols {
		if c == "deleted_at" {
			return cols
		}
	}
	return append(append([]string(nil), cols...), "deleted_at")
}

// doRestore runs the restore inside the caller's transaction: the scoped
// trashed-row read, the update hooks with an empty body (no data column
// changes; a hook can still veto), one conditional UPDATE pinned to
// soft-deleted rows, then the AfterUpdate hooks, the audit row (op
// "restore" via withAuditOperation) and the staged entity.updated event.
func (ch *CrudHandler) doRestore(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	pre, err := ch.selectTrashedRow(ctx, r, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("restore %s: %w", ch.Entity.GetName(), err)
	}
	if pre[ch.convertKey("deleted_at")] == nil {
		return nil, ErrNotSoftDeleted
	}
	ctx = WithAuditPreImage(ctx, pre)
	ctx = withAuditOperation(ctx, ch.Entity.GetName(), id, "restore")

	if ch.Hooks != nil {
		// Empty body: restore changes no data column. Hooks that need to
		// veto restores (a retention policy, say) do it on the id.
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeUpdate, map[string]any{}); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	ub := query.Update(ch.Entity.GetTable()).
		Set("deleted_at", nil).
		Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	// Pin to soft-deleted rows: a concurrent restore or a hard delete
	// between the read and this statement must not turn into success.
	ub.Where("deleted_at IS NOT NULL")
	if col := autoUpdatedAtColumn(ch.Entity); col != "" {
		ub.Set(col, generateFieldValue(schema.AutoTimestamp))
	}
	ub.Returning(ch.visibleFields()...)
	sqlStr, args := ub.Build()
	result, err := ch.scanOne(ch.DB.QueryRowContext(ctx, sqlStr, args...), ch.visibleFields())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("restore: %w", err)
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterUpdate, result); err != nil {
			return nil, fmt.Errorf("after-update hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityUpdated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

// doPurge runs the hard delete inside the caller's transaction: the same
// scoped trashed-row read (live rows refused), the delete hooks, one
// DELETE pinned to soft-deleted rows, the AfterDelete hooks, the audit
// row (op "purge") and the staged entity.deleted event.
func (ch *CrudHandler) doPurge(ctx context.Context, r *http.Request, id string) error {
	pre, err := ch.selectTrashedRow(ctx, r, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return fmt.Errorf("purge %s: %w", ch.Entity.GetName(), err)
	}
	if pre[ch.convertKey("deleted_at")] == nil {
		return ErrNotSoftDeleted
	}
	ctx = WithAuditPreImage(ctx, pre)
	ctx = withAuditOperation(ctx, ch.Entity.GetName(), id, "purge")

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeDelete, id); err != nil {
			return &beforeHookError{err: err}
		}
	}

	db := query.Delete(ch.Entity.GetTable()).
		Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScopeDelete(db, r)
	ch.ApplyOwnerScopeDelete(db, r)
	// Purge never skips the soft delete: the DELETE only matches rows the
	// soft-delete marker already covers.
	db.Where("deleted_at IS NOT NULL")
	sqlStr, args := db.Build()
	res, err := ch.DB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return errNotFound
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterDelete, id); err != nil {
			return fmt.Errorf("after-delete hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityDeleted, map[string]any{ch.convertKey(ch.PrimaryKey): id}); err != nil {
		return fmt.Errorf("stage event: %w", err)
	}
	return nil
}
