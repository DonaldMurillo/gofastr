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

// ErrNoSoftDelete answers restore, purge or a Deleted list on an entity
// without Scope.SoftDelete; ErrNotSoftDeleted answers restore or purge of
// a live row.
var (
	ErrNoSoftDelete   = errors.New("crud: entity does not use soft delete")
	ErrNotSoftDeleted = errors.New("crud: record is not soft-deleted")
)

// ErrForbidden is wrapped around every RestoreOne/PurgeOne permission
// refusal.
var ErrForbidden = errors.New("crud: forbidden")

type auditOperationKey struct{}

type auditOperation struct {
	entity string
	id     string
	op     string
}

func withAuditOperation(ctx context.Context, entity, id, op string) context.Context {
	if op == "" {
		return ctx
	}
	return context.WithValue(ctx, auditOperationKey{}, auditOperation{entity: entity, id: id, op: op})
}

func withoutAuditOperation(ctx context.Context) context.Context {
	if _, ok := ctx.Value(auditOperationKey{}).(auditOperation); !ok {
		return ctx
	}
	return context.WithValue(ctx, auditOperationKey{}, auditOperation{})
}

// AuditOperationFor returns the operation ("restore", "purge",
// "state_override", "transition:<key>") the running write stamped for
// exactly this entity and record id, or "" when none matches.
func AuditOperationFor(ctx context.Context, entity, id string) string {
	o, ok := ctx.Value(auditOperationKey{}).(auditOperation)
	if !ok || o.entity != entity || o.id != id {
		return ""
	}
	return o.op
}

// RestoreOne clears the soft-delete marker on one record, in-process
// only. It runs as an update: the update permission (wrapped in
// ErrForbidden when refused; skipped under WithServerWrites), the update
// hooks, an audit row with operation "restore" and entity.updated. A row
// outside the caller's owner or tenant scope answers the not-found a read
// gives; a live row answers ErrNotSoftDeleted; an entity without
// Scope.SoftDelete answers ErrNoSoftDelete.
func (ch *CrudHandler) RestoreOne(ctx context.Context, id string) error {
	ctx = withEventStamps(ctx)
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
		return ch.errForbiddenOp(opUpdate)
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

// PurgeOne hard-deletes a record that is already soft-deleted, in-process
// only; a live row answers ErrNotSoftDeleted. It runs as a delete: the
// delete permission, the delete hooks, an audit row with operation
// "purge", entity.deleted, and the row's stored files. Errors as
// RestoreOne.
func (ch *CrudHandler) PurgeOne(ctx context.Context, id string) error {
	ctx = withEventStamps(ctx)
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
		return ch.errForbiddenOp(opDelete)
	}
	req := syntheticRequest(ctx, http.MethodDelete, "/")
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		return ch.doPurge(ctx, req, id)
	})
	if err != nil {
		return err
	}
	ch.EmitEvent(withAuditOperation(ctx, ch.Entity.GetName(), id, "purge"), event.EntityDeleted, ch.keyPayload(id))
	return nil
}

// selectTrashedRow reads row id, live or trashed, under the tenant and
// owner write scope.
func (ch *CrudHandler) selectTrashedRow(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	cols := withDeletedAtColumn(ch.visibleFields())
	kc, ka := ch.keyWhere(id)
	qb := query.Select(ch.keyedCols(cols)...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	sqlStr, args := qb.Build()
	return ch.scanOwn(ctx, id, cols, sqlStr, args)
}

func withDeletedAtColumn(cols []string) []string {
	for _, c := range cols {
		if c == "deleted_at" {
			return cols
		}
	}
	return append(append([]string(nil), cols...), "deleted_at")
}

func (ch *CrudHandler) doRestore(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	if err := ch.refuseHiddenTarget(ctx, r, id, targetTrashed); err != nil {
		return nil, err
	}
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
		if err := runHooks(ch.Hooks, ctx, hook.BeforeUpdate, map[string]any{}); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	if err := ch.stampRow(ctx, id); err != nil {
		return nil, err
	}
	kc, ka := ch.keyWhere(id)
	ub := query.Update(ch.Entity.GetTable()).
		Set("deleted_at", nil).
		Where(kc, ka...)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	ub.Where("deleted_at IS NOT NULL")
	if col := autoUpdatedAtColumn(ch.Entity); col != "" {
		ub.Set(col, generateFieldValue(schema.AutoTimestamp))
	}
	ub.Returning(ch.keyedCols(ch.visibleFields())...)
	sqlStr, args := ub.Build()
	result, err := ch.writeOwn(ctx, id, ch.visibleFields(), sqlStr, args)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("restore: %w", err)
	}

	if ch.Hooks != nil {
		if err := ch.afterRow(ctx, hook.AfterUpdate, result); err != nil {
			return nil, fmt.Errorf("after-update hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityUpdated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

func (ch *CrudHandler) doPurge(ctx context.Context, r *http.Request, id string) error {
	if err := ch.refuseHiddenTarget(ctx, r, id, targetTrashed); err != nil {
		return err
	}
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
	if err := ch.stampRow(ctx, id); err != nil {
		return err
	}

	if ch.Hooks != nil {
		if err := runHooks(ch.Hooks, ctx, hook.BeforeDelete, id); err != nil {
			return &beforeHookError{err: err}
		}
	}

	idKey, _ := ch.idVals(id)
	objs, err := ch.objectsBefore(ctx, r, id, idKey, nil, true)
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}
	kc, ka := ch.keyWhere(id)
	db := query.Delete(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScopeDelete(db, r)
	ch.ApplyOwnerScopeDelete(db, r)
	db.Where("deleted_at IS NOT NULL")
	affected, err := ch.execOwn(ctx, id, db.Matching, func(returning []string) (string, []any) {
		return db.Returning(returning...).Build()
	})
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}
	if affected == 0 {
		return errNotFound
	}
	if err := ch.releaseObjects(ctx, r, id, idKey, objs, true); err != nil {
		return fmt.Errorf("purge: %w", err)
	}

	if ch.Hooks != nil {
		if err := runHooks(ch.Hooks, ctx, hook.AfterDelete, id); err != nil {
			return fmt.Errorf("after-delete hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityDeleted, ch.keyPayload(id)); err != nil {
		return fmt.Errorf("stage event: %w", err)
	}
	return nil
}

func (ch *CrudHandler) errForbiddenOp(op crudOp) error {
	if perm := ch.permissionForOp(op); perm != permRefused {
		return fmt.Errorf("%w: missing permission %s", ErrForbidden, perm)
	}
	return fmt.Errorf("%w: the entity declares no permission for the operation", ErrForbidden)
}
