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

	vr := schema.ValidateAll(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &validationError{fields: vr.Errors}
	}

	var cols []string
	var vals []any
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate != schema.AutoNone {
			cols = append(cols, f.Name)
			vals = append(vals, body[f.Name])
			continue
		}
		if f.ReadOnly || f.Hidden {
			continue
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
		cols = append(cols, "tenant_id")
		vals = append(vals, tenantID)
	}

	visFields := ch.VisibleFields()
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
	return result, nil
}

// doUpdate runs the BeforeUpdate → UPDATE → AfterUpdate chain for a single
// record by id. Same pre-conditions as doCreate.
func (ch *CrudHandler) doUpdate(ctx context.Context, r *http.Request, id string, body map[string]any) (map[string]any, error) {
	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeUpdate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	// Partial validation — only check fields the caller actually sent.
	// Missing fields aren't treated as "required" violations because the
	// existing row already satisfies them; the UPDATE only touches the
	// columns present in the body.
	vr := schema.ValidatePartial(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &validationError{fields: vr.Errors}
	}

	ub := query.Update(ch.Entity.GetTable())
	anySet := false
	ownerField := ch.Entity.Config.OwnerField
	for _, f := range ch.Entity.GetFields() {
		if f.Name == ch.PrimaryKey || f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
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
		if ch.Entity.Config.MultiTenant && f.Name == "tenant_id" {
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

	ub.Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	visFields := ch.VisibleFields()
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
	return result, nil
}

// doDelete runs the BeforeDelete → DELETE/UPDATE → AfterDelete chain for a
// single record by id. Same pre-conditions as doCreate.
func (ch *CrudHandler) doDelete(ctx context.Context, r *http.Request, id string) error {
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
	return nil
}
