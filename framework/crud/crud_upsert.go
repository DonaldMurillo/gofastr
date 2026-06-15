package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// errSoftDeletedResurrection signals an UpsertOne attempt against a row
// the soft-delete contract has marked deleted. Upsert would otherwise
// silently undelete the row — bypassing the compliance / forensic story
// that motivates soft-delete in the first place.
var errSoftDeletedResurrection = errors.New("upsert: target row is soft-deleted; restore explicitly before mutating")

// errUpsertForeignRow signals an UpsertOne whose body PK collides with an
// existing row owned by a different owner / tenant. Without this guard the
// ON CONFLICT DO UPDATE matches purely by primary key and re-stamps
// ownership/tenant from the caller's context — a cross-principal takeover.
var errUpsertForeignRow = errors.New("upsert: target row belongs to a different owner or tenant")

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
		delete(body, ch.Entity.Config.TenantColumn())
	}

	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		// Preflight against the pre-existing row (matched by PK only — the
		// same key the ON CONFLICT target uses). Three properties are
		// enforced here, all FAIL-CLOSED:
		//   1. A row owned by a different owner / tenant must not be taken
		//      over (ON CONFLICT DO UPDATE would re-stamp ownership from the
		//      caller's context and overwrite the victim's data).
		//   2. A soft-deleted row must not be silently resurrected (it would
		//      bypass the audit / retention story).
		// The SELECT is intentionally unscoped by owner/tenant so a foreign
		// row is DETECTED rather than treated as absent (which would fall
		// through to a hijacking ON CONFLICT).
		if err := ch.upsertPreflight(ctx, body); err != nil {
			return err
		}
		ch.InjectTenant(body, ctx)
		ch.InjectOwner(body, ctx)
		// Run the media-URL allow-list (http/https/relative only) before
		// persisting, matching doCreate/doUpdate. Without it the upsert
		// path stores a javascript:/data:/../ value verbatim into an
		// Image/File field — stored XSS once it renders into <img src>.
		if err := ch.validateMediaURLs(body); err != nil {
			return err
		}
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
		// ReadOnly/Hidden non-auto fields are skipped — except the
		// framework-managed owner column, which InjectOwner stamps above.
		var cols []string
		var vals []any
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate != schema.AutoNone {
				cols = append(cols, f.Name)
				vals = append(vals, body[f.Name])
				continue
			}
			if (f.ReadOnly || f.Hidden) && f.Name != ch.Entity.Config.OwnerField {
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
				cols = append(cols, ch.Entity.Config.TenantColumn())
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

		visFields := ch.visibleFields()
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
		if errors.Is(err, sql.ErrNoRows) && len(setParts) == 0 {
			// DO NOTHING fired against an existing row (nothing to update),
			// so RETURNING produced zero rows. The contract is "return the
			// resulting row" — fetch the pre-existing row explicitly by PK.
			//
			// Build the fallback through the query builder and apply the same
			// tenant / owner / soft-delete scopes the normal Get path uses.
			// upsertPreflight already fails closed on a foreign or
			// soft-deleted conflict, so this is defense in depth: it keeps the
			// returned row consistent with what a scoped read would surface
			// (e.g. never a soft-deleted row) even if the preflight contract
			// ever changes.
			selQB := query.Select(visFields...).
				From(ch.Entity.GetTable()).
				Where(ch.PrimaryKey+" = $1", body[ch.PrimaryKey])
			ch.ApplyTenantScope(selQB, req)
			ch.ApplyOwnerScope(selQB, req)
			ch.ApplySoftDeleteFilter(selQB, req)
			selSQL, selArgs := selQB.Build()
			sel := ch.DB.QueryRowContext(ctx, selSQL, selArgs...)
			res, err = scanRow(sel, visFields, ch.convertKey)
		}
		if err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
		result = res

		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterCreate, result); err != nil {
				return fmt.Errorf("after-create hook: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityCreated, result)
	return result, nil
}

// upsertPreflight inspects the pre-existing row (if any) matching the body's
// primary key and fails closed when the row belongs to a different owner /
// tenant, or is soft-deleted. Runs inside the upsert tx, before INSERT ...
// ON CONFLICT. A pure-insert (no existing row) is always allowed.
func (ch *CrudHandler) upsertPreflight(ctx context.Context, body map[string]any) error {
	ownerField := ch.Entity.Config.OwnerField
	checkSoftDelete := ch.Entity.Config.SoftDelete
	checkTenant := ch.Entity.Config.MultiTenant
	if ownerField == "" && !checkSoftDelete && !checkTenant {
		return nil
	}
	pk, ok := body[ch.PrimaryKey]
	if !ok || pk == nil {
		return nil // no PK supplied ⇒ no conflict possible.
	}

	// Build the projection: owner_field, tenant_id, deleted_at as needed.
	cols := make([]string, 0, 3)
	if ownerField != "" {
		cols = append(cols, ownerField)
	}
	if checkTenant {
		cols = append(cols, ch.Entity.Config.TenantColumn())
	}
	if checkSoftDelete {
		cols = append(cols, "deleted_at")
	}
	dest := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
		strings.Join(cols, ", "), ch.Entity.GetTable(), ch.PrimaryKey)
	err := ch.DB.QueryRowContext(ctx, q, pk).Scan(ptrs...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil // no existing row — pure insert path, allow.
	case err != nil:
		return fmt.Errorf("upsert preflight: %w", err)
	}

	idx := 0
	if ownerField != "" {
		got := dest[idx].String
		want := ""
		if id, ok := owner.Get(ctx); ok && id != nil {
			want = fmt.Sprintf("%v", id)
		}
		if got != want {
			return errUpsertForeignRow
		}
		idx++
	}
	if checkTenant {
		got := dest[idx].String
		if got != tenant.GetTenantID(ctx) {
			return errUpsertForeignRow
		}
		idx++
	}
	if checkSoftDelete {
		if dest[idx].Valid && dest[idx].String != "" {
			return errSoftDeletedResurrection
		}
	}
	return nil
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
