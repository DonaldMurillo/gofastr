package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// doCreate runs the BeforeCreate → INSERT → AfterCreate chain for a single
// record. It is the per-item primitive shared by Create and BatchCreate.
//
// Pre-conditions: ch is a tx-bound copy (its DB is *sql.Tx) and ctx is the
// tx-derived context. body is the snake_cased payload; this method mutates
// it in-place when injecting tenant_id and auto-generated values.
func (ch *CrudHandler) doCreate(ctx context.Context, r *http.Request, body map[string]any) (map[string]any, error) {
	ch.InjectTenant(body, ctx)
	ch.InjectOwner(body, ctx)
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate != schema.AutoNone {
			body[f.Name] = generateFieldValue(f.AutoGenerate)
		}
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeCreate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	if err := ch.validateMediaURLs(body); err != nil {
		return nil, err
	}

	vr := schema.ValidateAll(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}

	var cols []string
	var vals []any
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate != schema.AutoNone {
			cols = append(cols, f.Name)
			vals = append(vals, body[f.Name])
			continue
		}
		// The owner column is framework-managed: InjectOwner stamps it
		// above, so it is always persisted even when hidden from the
		// UI/API surface. The tenant column is ALWAYS skipped here: it
		// is appended separately below from the context-derived tenant
		// id, so letting the field loop persist it from the body would
		// double-add the column. Every OTHER ReadOnly/Hidden field is
		// client-unsettable and skipped unless the caller opted in to
		// server writes via WithServerWrites(ctx).
		if (f.ReadOnly || f.Hidden) && f.Name != ch.Entity.Config.OwnerField {
			isTenantCol := ch.Entity.Config.MultiTenant && f.Name == ch.Entity.Config.TenantColumn()
			if isTenantCol || !serverWrites(ctx) {
				continue
			}
		}
		val, ok := body[f.Name]
		if !ok {
			if f.Default != nil {
				val = f.Default
			} else {
				continue
			}
		}
		cols = append(cols, f.Name)
		vals = append(vals, val)
	}

	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(ctx)
		if tenantID == "" {
			// Refuse to write an orphan row. Without a tenant in the
			// context, the new row's tenant_id would default to empty
			// and become readable by anyone passing the matching empty
			// X-Tenant-ID through the filter middleware.
			return nil, &tenantMissingError{}
		}
		cols = append(cols, ch.Entity.Config.TenantColumn())
		vals = append(vals, tenantID)
	}

	visFields := ch.visibleFields()
	ib := query.Insert(ch.Entity.GetTable()).
		Columns(cols...).
		Values(vals...).
		Returning(visFields...)

	sqlStr, args := ib.Build()
	row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

	result, err := scanRow(row, visFields, ch.convertKey)
	if err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterCreate, result); err != nil {
			return nil, fmt.Errorf("after-create hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityCreated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

// doUpdate runs the BeforeUpdate → UPDATE → AfterUpdate chain for a single
// record by id. Same pre-conditions as doCreate.
func (ch *CrudHandler) doUpdate(ctx context.Context, r *http.Request, id string, body map[string]any) (map[string]any, error) {
	// Snapshot the pre-change row inside the same transaction so the audit
	// hook can diff old vs new. Best-effort — a SELECT failure here must
	// not block the update itself (the audit log already tolerates a
	// missing pre-image and just emits {"old": null, "new": ...}).
	if pre, err := ch.selectPreImage(ctx, r, id); err == nil && pre != nil {
		ctx = WithAuditPreImage(ctx, pre)
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeUpdate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	if err := ch.validateMediaURLs(body); err != nil {
		return nil, err
	}

	// Partial validation — only check fields the caller actually sent.
	// Missing fields aren't treated as "required" violations because the
	// existing row already satisfies them; the UPDATE only touches the
	// columns present in the body.
	vr := schema.ValidatePartial(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}

	ub := query.Update(ch.Entity.GetTable())
	anySet := false
	ownerField := ch.Entity.Config.OwnerField
	for _, f := range ch.Entity.GetFields() {
		if f.Name == ch.PrimaryKey || f.AutoGenerate != schema.AutoNone {
			continue
		}
		// ReadOnly/Hidden fields are client-unsettable and skipped unless
		// the caller opted in via WithServerWrites(ctx).
		if (f.ReadOnly || f.Hidden) && !serverWrites(ctx) {
			continue
		}
		// Refuse to let a client reassign ownership through an update body.
		// The owner scope already pins the WHERE to the caller's id, but
		// permitting `user_id` in the SET clause would still allow a
		// legitimate-owner update to hand the row off to another user
		// ("transfer-by-tamper"). Always skip the owner field — the
		// framework manages it.
		if ownerField != "" && f.Name == ownerField {
			continue
		}
		// Same hazard for tenant_id when MultiTenant is on.
		if ch.Entity.Config.MultiTenant && f.Name == ch.Entity.Config.TenantColumn() {
			continue
		}
		val, ok := body[f.Name]
		if !ok {
			continue
		}
		ub.Set(f.Name, val)
		anySet = true
	}
	if !anySet {
		return nil, errNoFieldsToUpdate
	}
	// Restamp updated_at to now(). The field loop above skips every
	// AutoGenerate field (so a client can't forge the timestamp), which
	// would otherwise leave updated_at frozen at its creation value. Only
	// stamp when a real update is happening (anySet) and the entity actually
	// declares an auto-timestamp updated_at column.
	if col := autoUpdatedAtColumn(ch.Entity); col != "" {
		ub.Set(col, generateFieldValue(schema.AutoTimestamp))
	}

	ub.Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	// A soft-deleted row is logically gone: the read paths hide it
	// (ApplySoftDeleteFilter on Get/List/cursor/pre-image) and so must the
	// write path — otherwise an owner could mutate / resurrect a record the
	// system considers deleted, which the upsert path already refuses
	// (errSoftDeletedResurrection). Match-nothing ⇒ scanRow gets ErrNoRows
	// ⇒ errNotFound, same 404 a deleted row gives on Get.
	if ch.Entity.Config.SoftDelete {
		ub.Where("deleted_at IS NULL")
	}
	visFields := ch.visibleFields()
	ub.Returning(visFields...)

	sqlStr, args := ub.Build()
	row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

	result, err := scanRow(row, visFields, ch.convertKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("update: %w", err)
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

// autoUpdatedAtColumn returns the name of the entity's auto-timestamp
// "updated_at" column, or "" when the entity has no such field. Used to
// restamp updated_at on every UPDATE / bulk update.
func autoUpdatedAtColumn(ent *entity.Entity) string {
	for _, f := range ent.GetFields() {
		if f.Name == "updated_at" && f.AutoGenerate == schema.AutoTimestamp {
			return f.Name
		}
	}
	return ""
}

// doDelete runs the BeforeDelete → DELETE/UPDATE → AfterDelete chain for a
// single record by id. Same pre-conditions as doCreate.
func (ch *CrudHandler) doDelete(ctx context.Context, r *http.Request, id string) error {
	// Snapshot the row before deletion so the audit hook can record what
	// went away. Without this the audit row only carries a record_id —
	// useful for "who did it" but useless for "what was lost".
	if pre, err := ch.selectPreImage(ctx, r, id); err == nil && pre != nil {
		ctx = WithAuditPreImage(ctx, pre)
	}

	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeDelete, id); err != nil {
			return &beforeHookError{err: err}
		}
	}

	var affected int64
	if ch.Entity.Config.SoftDelete {
		ub := query.Update(ch.Entity.GetTable()).
			Set("deleted_at", time.Now().UTC()).
			Where(ch.PrimaryKey+" = $1", id)
		ch.ApplyTenantScopeUpdate(ub, r)
		ch.ApplyOwnerScopeUpdate(ub, r)
		// Don't re-soft-delete an already-deleted row: without this the
		// UPDATE matches the trashed row, bumps deleted_at, and reports
		// success — making a re-delete of a logically-gone record look
		// like a fresh delete. Filtering to live rows makes affected==0,
		// which maps to errNotFound below.
		ub.Where("deleted_at IS NULL")
		sqlStr, args := ub.Build()
		res, err := ch.DB.ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return fmt.Errorf("soft delete: %w", err)
		}
		affected, _ = res.RowsAffected()
	} else {
		db := query.Delete(ch.Entity.GetTable()).
			Where(ch.PrimaryKey+" = $1", id)
		ch.ApplyTenantScopeDelete(db, r)
		ch.ApplyOwnerScopeDelete(db, r)
		sqlStr, args := db.Build()
		res, err := ch.DB.ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		affected, _ = res.RowsAffected()
	}
	if affected == 0 {
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

// selectPreImage SELECTs the row matching `id` (with the same tenant /
// owner / soft-delete scopes that the mutating statement will use) so
// audit hooks can capture an old-state snapshot. Returns (nil, nil) when
// the row doesn't exist or the SELECT fails — callers treat that as
// "no snapshot available" rather than aborting the surrounding mutation.
func (ch *CrudHandler) selectPreImage(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	cols := ch.visibleFields()
	qb := query.Select(cols...).
		From(ch.Entity.GetTable()).
		Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	sqlStr, args := qb.Build()
	row := ch.DB.QueryRowContext(ctx, sqlStr, args...)
	result, err := scanRow(row, cols, ch.convertKey)
	if err != nil {
		return nil, err
	}
	return result, nil
}
