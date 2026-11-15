package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

var errSoftDeletedResurrection = errors.New("upsert: target row is soft-deleted; restore explicitly before mutating")

var errUpsertForeignRow = errors.New("upsert: target row belongs to a different owner or tenant")

var errUpsertBeforeUpdate = errors.New("upsert: entities with BeforeUpdate hooks require CreateOne or UpdateOne")

// UpsertOne inserts body, or on a primary-key conflict updates the
// existing row with the columns body carries, and returns the resulting
// row. A conflict onto another owner's or tenant's row, a row the caller's
// BeforeGet hooks hide, or a soft-deleted row is refused. BeforeCreate
// hooks run for either arm; the After hooks and event follow a pre-read of
// the key (create when absent, update when present, none when nothing is
// written), so a competing writer between that read and the statement can
// change the arm. An entity with BeforeUpdate hooks is refused: use
// CreateOne or UpdateOne. A Default fills an omitted column on the insert
// arm only; a conflict that writes a column restamps updated_at. Nested
// writes run as CreateOne or UpdateOne would for the arm taken.
func (ch *CrudHandler) UpsertOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	ctx = withEventStamps(ctx)
	if ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.BeforeUpdate)) > 0 {
		return nil, errUpsertBeforeUpdate
	}
	req := syntheticRequest(ctx, http.MethodPost, "/")

	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if ch.Entity.Config.Scope.MultiTenant {
		if tenant.GetTenantID(ctx) == "" {
			return nil, &tenantMissingError{}
		}
	}
	if err := ch.bodyColumns(body); err != nil {
		return nil, err
	}
	if of := ch.Entity.Config.Scope.OwnerField; of != "" {
		delete(body, of)
	}
	if ch.Entity.Config.Scope.MultiTenant {
		delete(body, ch.Entity.Config.TenantColumn())
	}

	var result, out map[string]any
	wrote := false
	eventType := event.EntityCreated
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		if err := ch.checkMediaProvenance(ctx, body,
			ch.currentMediaKeys(ctx, req, "", ch.colKeyVals(body))); err != nil {
			return err
		}
		ch.InjectTenant(body, ctx)
		ch.InjectOwner(body, ctx)
		if err := ch.validateMediaURLs(body); err != nil {
			return err
		}
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate != schema.AutoNone {
				if _, present := body[f.Name]; !present {
					body[f.Name] = generateFieldValue(f.AutoGenerate)
				}
			}
		}
		s := ch.entitySchema()
		keys := ch.keyCols()
		s.Fields = slices.DeleteFunc(slices.Clone(s.Fields), func(f schema.Field) bool {
			return slices.Contains(keys, f.Name)
		})
		var blanked []string
		if ch.Hooks != nil {
			blanked = ch.coerceBeforeHooks(s, body, false)
			if err := runHooks(ch.Hooks, ctx, hook.BeforeCreate, body); err != nil {
				return &beforeHookError{err: err}
			}
			blanked = slices.DeleteFunc(blanked, func(name string) bool {
				_, set := body[name]
				return set
			})
		}
		blanked = append(blanked, dropEmptyFormValues(s, body, false)...)
		if err := ch.coerceNumberColumnValues(body); err != nil {
			return err
		}
		if err := ch.refuseCascadeUpsertKey(body); err != nil {
			return err
		}
		vs := ch.entitySchema()
		if setKeys := ch.cascadeSetKeys(body); len(setKeys) > 0 {
			vs.Fields = slices.Clone(vs.Fields)
			for i := range vs.Fields {
				if setKeys[vs.Fields[i].Name] {
					vs.Fields[i].Required = false
				}
			}
		}
		vr := schema.ValidateAll(vs, body)
		if !vr.Valid {
			return &ValidationError{fields: vr.Errors}
		}
		if err := ch.refuseInexactDecimals(ctx, body); err != nil {
			return err
		}
		if err := ch.refuseBlankKeyParts(body); err != nil {
			return err
		}

		if err := ch.upsertPreflight(ctx, body); err != nil {
			return err
		}
		previous, err := ch.upsertExistingRow(ctx, body)
		if err != nil {
			return err
		}
		existed := previous != nil
		if existed && ch.upsertTargetHidden(ctx, req, body) {
			return errUpsertForeignRow
		}
		objs, err := ch.objectsBefore(ctx, req, "", ch.colKeyVals(body), body, false)
		if err != nil {
			return err
		}
		if existed {
			ctx = WithAuditPreImage(ctx, previous)
			eventType = event.EntityUpdated
			for _, name := range blanked {
				body[name] = nil
			}
		}
		belongsToResults, err := ch.processBelongsToCascadeWrites(ctx, req, body, existed)
		if err != nil {
			return err
		}
		if err := ch.coerceNumberColumnValues(body); err != nil {
			return err
		}
		vr = schema.ValidateAll(ch.entitySchema(), body)
		if !vr.Valid {
			return &ValidationError{fields: vr.Errors}
		}
		if err := ch.refuseInexactDecimals(ctx, body); err != nil {
			return err
		}
		if ctx, err = ch.checkStateUpsert(ctx, req, body); err != nil {
			return err
		}
		for _, w := range []fkWrite{
			ch.createWrite(ctx, body),
			{body: body, sets: func(f schema.Field) bool { return ch.upsertSets(ctx, f) }, holds: ch.scopedHeld(ctx)},
		} {
			if err := ch.checkBelongsToScope(ctx, req, w); err != nil {
				return err
			}
		}

		clearedDerived, imageSources, err := ch.resetImageDerived(ctx, req, ch.colKeyVals(body), body, func(field schema.Field) bool {
			return !ch.isKeyCol(field.Name) && ch.upsertSets(ctx, field) && ch.upsertSetsGuarded(ctx, field.Name, true)
		})
		if err != nil {
			return err
		}
		var cols []string
		var vals []any
		sent := map[string]bool{}
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate == schema.AutoIncrement {
				if !callerSuppliedIncrement(body[f.Name]) {
					continue
				}
				bind, bindErr := incrementBindValue(body[f.Name])
				if bindErr != nil {
					return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: reasonKeyNotExact}}}}
				}
				cols = append(cols, f.Name)
				vals = append(vals, bind)
				continue
			}
			if f.AutoGenerate != schema.AutoNone {
				cols = append(cols, f.Name)
				vals = append(vals, ch.bindJSONValue(f.Name, body[f.Name]))
				continue
			}
			if f.Name != ch.Entity.Config.Scope.OwnerField && !ch.upsertSets(ctx, f) && !clearedDerived[f.Name] {
				if ch.writesStateDefault(f) {
					cols = append(cols, f.Name)
					vals = append(vals, ch.bindJSONValue(f.Name, f.Default))
				}
				continue
			}
			val, ok := body[f.Name]
			sent[f.Name] = ok
			if !ok {
				if f.Default != nil {
					val = f.Default
				} else {
					continue
				}
			}
			cols = append(cols, f.Name)
			vals = append(vals, ch.bindJSONValue(f.Name, val))
		}
		if ch.Entity.Config.Scope.MultiTenant {
			if tid := tenant.GetTenantID(ctx); tid != "" {
				cols = append(cols, ch.Entity.Config.TenantColumn())
				vals = append(vals, tid)
			}
		}

		setParts := make([]string, 0, len(cols))
		dataSet := 0
		ownerCol := ch.Entity.Config.Scope.OwnerField
		tenantCol := ""
		if ch.Entity.Config.Scope.MultiTenant {
			tenantCol = ch.Entity.Config.TenantColumn()
		}
		for _, c := range cols {
			if ch.isKeyCol(c) {
				continue
			}
			if isAutoField(ch.Entity, c) {
				continue
			}
			if !sent[c] && !(ch.Entity.Config.Scope.MultiTenant && c == ch.Entity.Config.TenantColumn()) {
				continue
			}
			if !ch.upsertSetsGuarded(ctx, c, sent[c]) {
				continue
			}
			setParts = append(setParts, fmt.Sprintf("%s = EXCLUDED.%s", query.QuoteKeyword(c), query.QuoteKeyword(c)))
			if c != ownerCol && c != tenantCol {
				dataSet++
			}
		}

		placeholders := make([]string, len(cols))
		for i := range cols {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}

		if col := autoUpdatedAtColumn(ch.Entity); col != "" && dataSet > 0 {
			vals = append(vals, ch.bindJSONValue(col, generateFieldValue(schema.AutoTimestamp)))
			setParts = append(setParts, fmt.Sprintf("%s = $%d", query.QuoteKeyword(col), len(vals)))
		}
		wrote = !existed || dataSet > 0

		visFields := ch.visibleFields()
		var sb strings.Builder
		sb.WriteString("INSERT INTO ")
		sb.WriteString(query.QuoteKeyword(ch.Entity.GetTable()))
		sb.WriteString(" AS " + upsertTarget)
		if len(cols) == 0 {
			sb.WriteString(" DEFAULT VALUES")
		} else {
			sb.WriteString(" (")
			sb.WriteString(strings.Join(query.QuoteKeywords(cols), ", "))
			sb.WriteString(") VALUES (")
			sb.WriteString(strings.Join(placeholders, ", "))
			sb.WriteString(") ON CONFLICT (")
			sb.WriteString(strings.Join(query.QuoteKeywords(ch.keyCols()), ", "))
			sb.WriteString(") DO ")
			if len(setParts) == 0 {
				sb.WriteString("NOTHING")
			} else {
				sb.WriteString("UPDATE SET ")
				sb.WriteString(strings.Join(setParts, ", "))
				guard := ch.upsertConflictGuard()
				for _, source := range imageSources {
					if guard != "" {
						guard += " AND "
					}
					column := upsertTarget + "." + query.QuoteKeyword(source.column)
					if source.value == nil {
						guard += column + " IS NULL"
					} else {
						vals = append(vals, source.value)
						guard += fmt.Sprintf("%s = $%d", column, len(vals))
					}
				}
				if guard != "" {
					sb.WriteString(" WHERE ")
					sb.WriteString(guard)
				}
			}
		}
		sb.WriteString(" RETURNING ")
		sb.WriteString(strings.Join(query.QuoteKeywords(visFields), ", "))

		row := ch.conn(ctx).QueryRowContext(ctx, sb.String(), vals...)
		res, err := ch.scanOne(row, visFields)
		if errors.Is(err, sql.ErrNoRows) && len(setParts) > 0 {
			if perr := ch.upsertPreflight(ctx, body); perr != nil {
				return perr
			}
			if len(imageSources) > 0 {
				return errImageSourceChanged
			}
			return errUpsertForeignRow
		}
		if errors.Is(err, sql.ErrNoRows) && len(setParts) == 0 {
			kc, ka := ch.keyCond(ch.colKeyVals(body))
			selQB := query.Select(visFields...).
				From(ch.Entity.GetTable()).
				Where(kc, ka...)
			ch.ApplyTenantScope(selQB, req)
			ch.ApplyOwnerScope(selQB, req)
			ch.ApplySoftDeleteFilter(selQB, req)
			selSQL, selArgs := selQB.Build()
			sel := ch.conn(ctx).QueryRowContext(ctx, selSQL, selArgs...)
			res, err = ch.scanOne(sel, visFields)
		}
		if err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
		if err := ch.refuseNullKey(res); err != nil {
			return err
		}
		result = res
		if wrote {
			if id, ok := ch.rowID(result); ok {
				if err := ch.stampRow(ctx, id); err != nil {
					return err
				}
			}
		}
		if err := ch.releaseObjects(ctx, req, "", ch.colKeyVals(body), objs, false); err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
		depResults, err := ch.processDependentCascadeWrites(ctx, req, ch.cascadeParentID(result, body), body, existed)
		if err != nil {
			return err
		}
		maps.Copy(result, belongsToResults)
		maps.Copy(result, depResults)
		if len(belongsToResults)+len(depResults) > 0 {
			wrote = true
		}

		if ch.Hooks != nil && wrote {
			phase := hook.AfterCreate
			if existed {
				phase = hook.AfterUpdate
			}
			if err := ch.afterRow(ctx, phase, result); err != nil {
				return fmt.Errorf("upsert after hook: %w", err)
			}
		}
		if wrote {
			if err := ch.StageEvent(ctx, eventType, result); err != nil {
				return fmt.Errorf("stage event: %w", err)
			}
		}
		out, err = ch.scopedReadBack(ctx, result)
		return err
	})
	if err != nil {
		return nil, err
	}
	if wrote {
		ch.EmitEvent(ctx, eventType, result)
	}
	return out, nil
}

// upsertExistingRow reads the row holding the body's key, unscoped, or
// nil.
func (ch *CrudHandler) upsertExistingRow(ctx context.Context, body map[string]any) (map[string]any, error) {
	key := ch.colKeyVals(body)
	if key == nil {
		return nil, nil
	}
	kc, ka := ch.keyCond(key)
	columns := ch.visibleFields()
	row, err := ch.scanOne(ch.conn(ctx).QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(query.QuoteKeywords(columns), ", "), query.QuoteKeyword(ch.Entity.GetTable()), kc), ka...), columns)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("upsert: read existing row: %w", err)
	}
	return row, nil
}

// upsertPreflight refuses a body whose key names a row of another owner
// or tenant, or a soft-deleted one, reading it unscoped.
func (ch *CrudHandler) upsertPreflight(ctx context.Context, body map[string]any) error {
	ownerField := ch.Entity.Config.Scope.OwnerField
	checkSoftDelete := ch.Entity.Config.Scope.SoftDelete
	checkTenant := ch.Entity.Config.Scope.MultiTenant
	if ownerField == "" && !checkSoftDelete && !checkTenant {
		return nil
	}
	key := ch.colKeyVals(body)
	if key == nil {
		return nil
	}
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
	kc, ka := ch.keyCond(key)
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(query.QuoteKeywords(cols), ", "), query.QuoteKeyword(ch.Entity.GetTable()), kc)
	err := ch.conn(ctx).QueryRowContext(ctx, q, ka...).Scan(ptrs...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
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

// upsertConflictGuard is the DO UPDATE's WHERE: the stored row's owner and
// tenant equal the incoming row's and it is live.
func (ch *CrudHandler) upsertConflictGuard() string {
	scope := ch.Entity.Config.Scope
	tbl := upsertTarget
	var guards []string
	if of := scope.OwnerField; of != "" {
		of = query.QuoteKeyword(of)
		guards = append(guards, fmt.Sprintf("%s.%s = EXCLUDED.%s", tbl, of, of))
	}
	if scope.MultiTenant {
		tc := query.QuoteKeyword(ch.Entity.Config.TenantColumn())
		guards = append(guards, fmt.Sprintf("%s.%s = EXCLUDED.%s", tbl, tc, tc))
	}
	if scope.SoftDelete {
		guards = append(guards, tbl+".deleted_at IS NULL")
	}
	return strings.Join(guards, " AND ")
}

// upsertTarget aliases the upsert's target table, so a table named
// excluded works.
const upsertTarget = "upsert_target"

func isAutoField(ent *entity.Entity, col string) bool {
	for _, f := range ent.GetFields() {
		if f.Name == col && f.AutoGenerate != schema.AutoNone {
			return true
		}
	}
	return false
}

// callerSuppliedIncrement reports whether v is a non-zero AutoIncrement
// key, which the INSERT must carry as its conflict target.
func callerSuppliedIncrement(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float64: // JSON-decoded number
		return x != 0
	case json.Number:
		return x.String() != "0"
	case string:
		return x != "" && x != "0"
	default:
		return true
	}
}

// incrementBindValue binds a caller-supplied AutoIncrement key as int64,
// refusing a float past exact precision.
func incrementBindValue(v any) (any, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case float64:
		if i, ok := exactFloatInt64(x); ok {
			return i, nil
		}
		return nil, fmt.Errorf("primary key %v: float64 beyond exact integer precision; send it as a JSON integer literal or a string", x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		if fl, err := x.Float64(); err == nil {
			if i, ok := exactFloatInt64(fl); ok {
				return i, nil
			}
			if fl == math.Trunc(fl) {
				return nil, fmt.Errorf("primary key %s: beyond exact float64 precision; send it as a JSON integer literal or a string", x.String())
			}
		}
	}
	return v, nil
}

// upsertSets reports whether an upsert writes f from the body on both
// arms.
func (ch *CrudHandler) upsertSets(ctx context.Context, f schema.Field) bool {
	if f.AutoGenerate != schema.AutoNone {
		return false
	}
	if owner := ch.Entity.Config.Scope.OwnerField; owner != "" && f.Name == owner {
		return false
	}
	if ch.Entity.Config.Scope.MultiTenant && f.Name == ch.Entity.Config.TenantColumn() {
		return false
	}
	return !(f.ReadOnly || f.Hidden) || serverWrites(ctx) || ch.stateOverrideColumn(ctx, f.Name)
}
