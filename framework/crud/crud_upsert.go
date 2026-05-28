package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// errSoftDeletedResurrection signals an UpsertOne attempt against a row
// the soft-delete contract has marked deleted. Upsert would otherwise
// silently undelete the row — bypassing the compliance / forensic story
// that motivates soft-delete in the first place.
var errSoftDeletedResurrection = errors.New("upsert: target row is soft-deleted; restore explicitly before mutating")

// UpsertOne performs an INSERT ... ON CONFLICT DO UPDATE on the entity's
// primary key. If a row with the same PK already exists, every writable
// field in body overwrites the existing row; otherwise a new row is
// inserted. Returns the resulting row (post-insert or post-update).
//
// Both Postgres and SQLite 3.24+ support this exact syntax via the
// EXCLUDED pseudo-table. The framework's BeforeCreate/AfterCreate hooks
// fire (an upsert that turns into an INSERT is semantically a Create);
// no Update hooks fire — if you need to distinguish, route through the
// regular Create/Update endpoints.
func (ch *CrudHandler) UpsertOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	req := syntheticRequest(ctx, http.MethodPost, "/")

	// Refuse anonymous upserts on owner-scoped entities. Without this
	// check, an unauthenticated caller can create orphan rows or
	// forge ownership via a body-supplied owner_id (see
	// upsert_security_test.go).
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	// Refuse upserts on multi-tenant entities when no tenant id is in
	// the context. Mirrors the InjectTenant guard in doCreate so the
	// upsert path can't write an orphan tenant row by omission.
	if ch.Entity.Config.MultiTenant {
		if tenant.GetTenantID(ctx) == "" {
			return nil, &tenantMissingError{}
		}
	}
	// Strip caller-supplied owner_id / tenant_id from the body BEFORE
	// the tx so a body field can never override what the context says.
	// The framework stamps both from context-derived values below.
	if of := ch.Entity.Config.OwnerField; of != "" {
		delete(body, of)
	}
	if ch.Entity.Config.MultiTenant {
		delete(body, "tenant_id")
	}

	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		// Refuse to resurrect a soft-deleted row: ON CONFLICT DO UPDATE
		// silently clears deleted_at without firing the lifecycle hooks,
		// which would smuggle a row past audit / retention contracts.
		if ch.Entity.Config.SoftDelete {
			if pk, ok := body[ch.PrimaryKey]; ok && pk != nil {
				var deletedAt sql.NullString
				q := fmt.Sprintf("SELECT deleted_at FROM %s WHERE %s = $1", ch.Entity.GetTable(), ch.PrimaryKey)
				err := ch.DB.QueryRowContext(ctx, q, pk).Scan(&deletedAt)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					// no existing row — pure insert path, allow.
				case err != nil:
					return fmt.Errorf("upsert preflight: %w", err)
				case deletedAt.Valid && deletedAt.String != "":
					return errSoftDeletedResurrection
				}
			}
		}
		ch.InjectTenant(body, ctx)
		ch.InjectOwner(body, ctx)
		// Auto-generate any field that needs it; on conflict the existing
		// value stays (we exclude pk + auto fields from the update set).
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate != schema.AutoNone {
				if _, present := body[f.Name]; !present {
					body[f.Name] = generateFieldValue(f.AutoGenerate)
				}
			}
		}
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeCreate, body); err != nil {
				return &beforeHookError{err: err}
			}
		}
		vr := schema.ValidateAll(ch.entitySchema(), body)
		if !vr.Valid {
			return &validationError{fields: vr.Errors}
		}

		// Build the column + value lists, same shape Create uses: auto-gen
		// fields are always included (the body has the generated value);
		// ReadOnly/Hidden non-auto fields are skipped.
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
			if tid := tenant.GetTenantID(ctx); tid != "" {
				cols = append(cols, "tenant_id")
				vals = append(vals, tid)
			}
		}

		// Build the SET clause for the UPDATE side. Skip pk + auto-generate
		// fields — those represent identity / immutability.
		setParts := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == ch.PrimaryKey {
				continue
			}
			if isAutoField(ch.Entity, c) {
				continue
			}
			setParts = append(setParts, fmt.Sprintf("%s = EXCLUDED.%s", c, c))
		}

		// Render parameter placeholders $1..$N for VALUES.
		placeholders := make([]string, len(cols))
		for i := range cols {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}

		visFields := ch.VisibleFields()
		var sb strings.Builder
		sb.WriteString("INSERT INTO ")
		sb.WriteString(ch.Entity.GetTable())
		sb.WriteString(" (")
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(") VALUES (")
		sb.WriteString(strings.Join(placeholders, ", "))
		sb.WriteString(") ON CONFLICT (")
		sb.WriteString(ch.PrimaryKey)
		sb.WriteString(") DO ")
		if len(setParts) == 0 {
			// No fields to update — just DO NOTHING; RETURNING still picks
			// the existing row though we have to query it explicitly.
			sb.WriteString("NOTHING")
		} else {
			sb.WriteString("UPDATE SET ")
			sb.WriteString(strings.Join(setParts, ", "))
		}
		sb.WriteString(" RETURNING ")
		sb.WriteString(strings.Join(visFields, ", "))

		row := ch.DB.QueryRowContext(ctx, sb.String(), vals...)
		res, err := scanRow(row, visFields, ch.convertKey)
		if err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
		result = res

		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterCreate, result); err != nil {
				return fmt.Errorf("after-create hook: %w", err)
			}
		}
		_ = req
		return nil
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityCreated, result)
	return result, nil
}

// isAutoField reports whether a field name corresponds to an auto-generated
// column on the entity (UUID, timestamp, increment). Used by UpsertOne to
// avoid clobbering id/created_at when the same row already exists.
func isAutoField(ent *entity.Entity, col string) bool {
	for _, f := range ent.GetFields() {
		if f.Name == col && f.AutoGenerate != schema.AutoNone {
			return true
		}
	}
	return false
}
