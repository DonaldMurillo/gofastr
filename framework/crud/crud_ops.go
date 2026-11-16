package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// doCreate writes one record on a tx-bound handler; body is keyed by
// column and is modified in place.
func (ch *CrudHandler) doCreate(ctx context.Context, r *http.Request, body map[string]any) (map[string]any, error) {
	if ch.Entity != nil && ch.Entity.Config.Scope.OwnerField != "" && !serverWrites(ctx) {
		if err := ch.requireOwnerContext(ctx); err != nil {
			return nil, err
		}
	}
	if err := ch.checkMediaProvenance(ctx, body, nil); err != nil {
		return nil, err
	}
	ch.InjectTenant(body, ctx)
	ch.InjectOwner(body, ctx)
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate != schema.AutoNone {
			body[f.Name] = generateFieldValue(f.AutoGenerate)
		}
	}

	if ch.Hooks != nil {
		ch.coerceBeforeHooks(ch.entitySchema(), body, false)
		if err := runHooks(ch.Hooks, ctx, hook.BeforeCreate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	belongsToResults, err := ch.processBelongsToCascadeWrites(ctx, r, body, false)
	if err != nil {
		return nil, err
	}

	if err := ch.validateMediaURLs(body); err != nil {
		return nil, err
	}
	dropEmptyFormValues(ch.entitySchema(), body, false)
	if err := ch.coerceNumberColumnValues(body); err != nil {
		return nil, err
	}
	vr := schema.ValidateAll(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}
	if err := ch.refuseInexactDecimals(ctx, body); err != nil {
		return nil, err
	}
	if err := ch.refuseBlankKeyParts(body); err != nil {
		return nil, err
	}

	if err := ch.checkBelongsToScope(ctx, r, ch.createWrite(ctx, body)); err != nil {
		return nil, err
	}
	if err := ch.checkStateCreate(ctx, body); err != nil {
		return nil, err
	}

	var cols []string
	var vals []any
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate == schema.AutoIncrement {
			continue
		}
		if f.AutoGenerate != schema.AutoNone {
			cols = append(cols, f.Name)
			vals = append(vals, ch.bindJSONValue(f.Name, body[f.Name]))
			continue
		}
		if ch.Entity.Config.Scope.MultiTenant && f.Name == ch.Entity.Config.TenantColumn() {
			continue
		}
		if (f.ReadOnly || f.Hidden) && f.Name != ch.Entity.Config.Scope.OwnerField {
			if !serverWrites(ctx) && !ch.stateOverrideColumn(ctx, f.Name) {
				if ch.writesStateDefault(f) {
					cols = append(cols, f.Name)
					vals = append(vals, ch.bindJSONValue(f.Name, f.Default))
				}
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
		vals = append(vals, ch.bindJSONValue(f.Name, val))
	}

	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(ctx)
		if tenantID == "" {
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
	row := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...)

	result, err := ch.scanOne(row, visFields)
	if err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}
	if err := ch.refuseNullKey(result); err != nil {
		return nil, err
	}
	if id, ok := ch.rowID(result); ok {
		if err := ch.stampRow(ctx, id); err != nil {
			return nil, err
		}
	}

	depResults, err := ch.processDependentCascadeWrites(ctx, r, ch.cascadeParentID(result, body), body, false)
	if err != nil {
		return nil, err
	}

	for k, v := range belongsToResults {
		result[k] = v
	}
	for k, v := range depResults {
		result[k] = v
	}

	if ch.Hooks != nil {
		if err := ch.afterRow(ctx, hook.AfterCreate, result); err != nil {
			return nil, fmt.Errorf("after-create hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityCreated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

// doUpdate patches record id on a tx-bound handler.
func (ch *CrudHandler) doUpdate(ctx context.Context, r *http.Request, id string, body map[string]any) (map[string]any, error) {
	if ch.Entity != nil && ch.Entity.Config.Scope.OwnerField != "" && !serverWrites(ctx) {
		if err := ch.requireOwnerContext(ctx); err != nil {
			return nil, err
		}
	}
	if err := ch.refuseHiddenTarget(ctx, r, id, targetLive, hook.BeforeUpdate); err != nil {
		return nil, err
	}
	idKey, _ := ch.idVals(id)
	if err := ch.checkMediaProvenance(ctx, body, ch.currentMediaKeys(ctx, r, id, idKey)); err != nil {
		return nil, err
	}
	if pre, err := ch.selectPreImage(ctx, r, id); err == nil && pre != nil {
		ctx = WithAuditPreImage(ctx, pre)
	}
	if ch.Entity != nil {
		ctx = withRecordWrite(ctx, ch.Entity.GetName(), id)
	}

	if ch.Hooks != nil {
		ch.coerceBeforeHooks(ch.entitySchema(), body, true)
		if err := runHooks(ch.Hooks, ctx, hook.BeforeUpdate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	belongsToResults, err := ch.processBelongsToCascadeWrites(ctx, r, body, true)
	if err != nil {
		return nil, err
	}

	if err := ch.validateMediaURLs(body); err != nil {
		return nil, err
	}

	dropEmptyFormValues(ch.entitySchema(), body, true)
	if err := ch.coerceNumberColumnValues(body); err != nil {
		return nil, err
	}
	vr := schema.ValidatePartial(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}
	if err := ch.refuseInexactDecimals(ctx, body); err != nil {
		return nil, err
	}

	ctx, err = ch.checkStateUpdate(ctx, r, id, body)
	if err != nil {
		return nil, err
	}
	w, err := ch.updateWrite(ctx, r, id, body)
	if err != nil {
		return nil, err
	}
	if err := ch.checkBelongsToScope(ctx, r, w); err != nil {
		return nil, err
	}
	clearedDerived, imageSources, err := ch.resetImageDerived(ctx, r, idKey, body, func(field schema.Field) bool {
		return ch.updateSets(ctx, field, true)
	})
	if err != nil {
		return nil, err
	}

	if err := ch.stampRow(ctx, id); err != nil {
		return nil, err
	}
	objs, err := ch.objectsBefore(ctx, r, id, idKey, body, false)
	if err != nil {
		return nil, err
	}

	ub := query.Update(ch.Entity.GetTable())
	anySet := false
	for _, f := range ch.Entity.GetFields() {
		if !ch.updateSets(ctx, f, true) && !clearedDerived[f.Name] {
			continue
		}
		val, ok := body[f.Name]
		if !ok {
			continue
		}
		ub.Set(f.Name, ch.bindJSONValue(f.Name, val))
		anySet = true
	}

	var result map[string]any
	if !anySet {
		if !ch.hasCascadeWrites(body) {
			return nil, errNoFieldsToUpdate
		}
		var selErr error
		result, selErr = ch.selectWriteTarget(ctx, r, id)
		if selErr != nil {
			if errors.Is(selErr, sql.ErrNoRows) {
				return nil, errNotFound
			}
			return nil, selErr
		}
		if result == nil {
			return nil, errNotFound
		}
	} else {
		if col := autoUpdatedAtColumn(ch.Entity); col != "" {
			ub.Set(col, generateFieldValue(schema.AutoTimestamp))
		}

		kc, ka := ch.keyWhere(id)
		ub.Where(kc, ka...)
		for _, source := range imageSources {
			column := query.QuoteKeyword(source.column)
			if source.value == nil {
				ub.Where(column + " IS NULL")
			} else {
				ub.Where(column+" = $1", source.value)
			}
		}
		ch.ApplyTenantScopeUpdate(ub, r)
		ch.ApplyOwnerScopeUpdate(ub, r)
		if ch.Entity.Config.Scope.SoftDelete {
			ub.Where("deleted_at IS NULL")
		}
		visFields := ch.visibleFields()
		ub.Returning(ch.keyedCols(visFields)...)

		sqlStr, args := ub.Build()
		var scanErr error
		result, scanErr = ch.writeOwn(ctx, id, visFields, sqlStr, args)
		if scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) || errors.Is(scanErr, errNotFound) {
				return nil, errNotFound
			}
			return nil, fmt.Errorf("update: %w", scanErr)
		}
		if err := ch.releaseObjects(ctx, r, id, idKey, objs, false); err != nil {
			return nil, fmt.Errorf("update: %w", err)
		}
	}

	depResults, err := ch.processDependentCascadeWrites(ctx, r, id, body, true)
	if err != nil {
		return nil, err
	}

	for k, v := range belongsToResults {
		result[k] = v
	}
	for k, v := range depResults {
		result[k] = v
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

// checkBelongsToScope refuses a write whose BelongsTo foreign key names
// a row the caller cannot read, with errNotFound.
func (ch *CrudHandler) checkBelongsToScope(ctx context.Context, r *http.Request, w fkWrite) error {
	if err := ch.checkForeignKeyTuples(w); err != nil {
		return err
	}
	if serverWrites(ctx) {
		return nil
	}
	rels := ch.referenceRelations()
	if len(rels) == 0 || ch.Registry == nil {
		return nil
	}
	var registered map[string]*entity.Entity
	for _, rel := range rels {
		raws, named := w.tuple(ch, rel)
		if !named {
			continue
		}
		if slices.Contains(raws, nil) {
			continue
		}
		if w.kept != nil && w.kept(rel, raws) {
			continue
		}
		parts := make([]string, len(raws))
		for i, v := range raws {
			parts[i] = fmt.Sprint(v)
		}
		fk := entity.JoinKey(parts)
		if registered == nil {
			registered = ch.Registry.All()
		}
		if _, known := registered[rel.Entity]; !known {
			continue
		}
		target, err := entity.ResolveTarget(ch.Registry, ch.Entity, rel.Entity)
		if err != nil {
			return fmt.Errorf("check belongs_to %q: cannot resolve target %q: %w", rel.Name, rel.Entity, err)
		}
		if target == nil {
			return fmt.Errorf("check belongs_to %q: unknown target entity %q", rel.Name, rel.Entity)
		}
		hasReadScope := len(readScopeFilters(ctx, target)) > 0
		if target.Config.Scope.OwnerField == "" && !target.Config.Scope.MultiTenant && !target.Config.Scope.SoftDelete && !hasReadScope {
			if err := ch.belongsToHookScope(ctx, r, rel, target, raws, fk); err != nil {
				return err
			}
			continue
		}
		table, err := query.SafeIdent(target.GetTable())
		if err != nil {
			return fmt.Errorf("relation %q target table %q: %w", rel.Name, target.GetTable(), err)
		}
		if err := entity.CheckRelationKeys(ch.Entity, rel, target); err != nil {
			return fmt.Errorf("check belongs_to %q: %w", rel.Name, err)
		}
		targetKey := target.KeyColumns()
		keyCols, err := safeColumns(targetKey)
		if err != nil {
			return fmt.Errorf("relation %q target key: %w", rel.Name, err)
		}
		conds := make([]string, len(keyCols))
		allArgs := make([]any, len(keyCols))
		for i, c := range keyCols {
			conds[i] = fmt.Sprintf("%s = $%d", query.QuoteKeyword(c), i+1)
			allArgs[i] = coercePKValue(ch.Registry, target, targetKey[i], raws[i])
		}
		preds := eagerScopeFilters(ctx, target)
		readPreds := readScopeFilters(ctx, target)
		clause, args := filterClause(preds, len(allArgs)+1)
		readClause, readArgs := renderReadScope(readPreds, "", len(allArgs)+1+len(args))
		if readClause != "" {
			readClause = " AND " + readClause
		}
		q := fmt.Sprintf("SELECT %s FROM %s WHERE %s%s%s", query.QuoteKeyword(keyCols[0]), query.QuoteKeyword(table), strings.Join(conds, " AND "), clause, readClause)
		if target.Config.Scope.SoftDelete {
			q += " AND deleted_at IS NULL"
		}
		allArgs = append(allArgs, args...)
		allArgs = append(allArgs, readArgs...)
		var hit any
		if err := ch.conn(ctx).QueryRowContext(ctx, q, allArgs...).Scan(&hit); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: relation %q target %s/%s is not readable by this caller", errNotFound, rel.Name, rel.Entity, fk)
			}
			return fmt.Errorf("relation %q target lookup: %w", rel.Name, err)
		}
		if err := ch.belongsToHookScope(ctx, r, rel, target, raws, fk); err != nil {
			return err
		}
	}
	return nil
}

// belongsToHookScope refuses a reference onto a row the target's own
// Before hooks hide from this caller.
func (ch *CrudHandler) belongsToHookScope(ctx context.Context, r *http.Request, rel entity.Relation, target *entity.Entity, raws []any, fk string) error {
	vals := make([]any, len(raws))
	for i, c := range target.KeyColumns() {
		if i < len(raws) {
			vals[i] = coercePKValue(ch.Registry, target, c, raws[i])
		}
	}
	if ch.childScopeHidesKey(ctx, r, target, rel, vals, fk) {
		return fmt.Errorf("%w: relation %q target %s/%s is not readable by this caller", errNotFound, rel.Name, rel.Entity, fk)
	}
	return nil
}

// referenceRelations is the belongs-to relations plus each Many relation
// field, whose column holds one target id.
func (ch *CrudHandler) referenceRelations() []entity.Relation {
	var out []entity.Relation
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type == entity.RelManyToOne {
			out = append(out, rel)
		}
	}
	for _, f := range ch.Entity.Config.Fields {
		if f.Type != schema.Relation || !f.Many || f.To == "" {
			continue
		}
		out = append(out, entity.BelongsTo(f.Name, f.To, f.Name))
	}
	return out
}

// updateSets reports whether an update writes f from the body.
func (ch *CrudHandler) updateSets(ctx context.Context, f schema.Field, overrides bool) bool {
	if ch.isKeyCol(f.Name) || f.AutoGenerate != schema.AutoNone {
		return false
	}
	if (f.ReadOnly || f.Hidden) && !serverWrites(ctx) && !(overrides && ch.stateOverrideColumn(ctx, f.Name)) {
		return false
	}
	if owner := ch.Entity.Config.Scope.OwnerField; owner != "" && f.Name == owner {
		return false
	}
	return !ch.Entity.Config.Scope.MultiTenant || f.Name != ch.Entity.Config.TenantColumn()
}

// fkWrite is one write as the foreign-key checks read it: each reference
// as the row will hold it, not as the body spells it.
type fkWrite struct {
	body map[string]any
	sets func(f schema.Field) bool
	// holds is what a column the write does not take holds afterwards,
	// or false when the write cannot know.
	holds func(c string) (any, bool)
	// kept reports whether rel's tuple is the one the row holds now; nil
	// except on an update.
	kept func(rel entity.Relation, raws []any) bool
}

func (w fkWrite) takes(ch *CrudHandler, c string) bool {
	_, ok := w.body[c]
	return ok && w.sets(ch.fieldByName(c))
}

func (w fkWrite) tuple(ch *CrudHandler, rel entity.Relation) ([]any, bool) {
	fks := rel.ForeignKey
	if !slices.ContainsFunc(fks, func(c string) bool { return w.takes(ch, c) }) {
		return nil, false
	}
	vals := make([]any, len(fks))
	for i, c := range fks {
		if w.takes(ch, c) {
			vals[i] = w.body[c]
		} else {
			vals[i], _ = w.holds(c)
		}
	}
	return vals, true
}

func unknownHeld(string) (any, bool) { return nil, false }

// scopedHeld is fkWrite.holds for the upsert's conflict arm and the bulk
// update: the caller's tenant and owner columns, else unknown.
func (ch *CrudHandler) scopedHeld(ctx context.Context) func(string) (any, bool) {
	return func(c string) (any, bool) {
		if ch.Entity.Config.Scope.MultiTenant && c == ch.Entity.Config.TenantColumn() {
			if id := tenant.GetTenantID(ctx); id != "" {
				return id, true
			}
		}
		if of := ch.Entity.Config.Scope.OwnerField; of != "" && c == of && !owner.IsCrossOwner(ctx) {
			if id, ok := owner.Get(ctx); ok {
				return id, true
			}
		}
		return nil, false
	}
}

// createWrite reads a create as doCreate's column loop writes it.
func (ch *CrudHandler) createWrite(ctx context.Context, body map[string]any) fkWrite {
	return fkWrite{
		body: body,
		sets: func(f schema.Field) bool {
			switch {
			case f.AutoGenerate == schema.AutoIncrement:
				return false
			case f.AutoGenerate != schema.AutoNone:
				return true
			case ch.Entity.Config.Scope.MultiTenant && f.Name == ch.Entity.Config.TenantColumn():
				return false
			case (f.ReadOnly || f.Hidden) && f.Name != ch.Entity.Config.Scope.OwnerField:
				return serverWrites(ctx) || ch.stateOverrideColumn(ctx, f.Name)
			}
			return true
		},
		holds: func(c string) (any, bool) {
			if ch.Entity.Config.Scope.MultiTenant && c == ch.Entity.Config.TenantColumn() {
				return tenant.GetTenantID(ctx), true
			}
			f := ch.fieldByName(c)
			if f.AutoGenerate == schema.AutoIncrement {
				return nil, false
			}
			return f.Default, true
		},
	}
}

// updateWrite reads a PATCH as its SET writes it, with the stored values
// of the references the body names, read under the UPDATE's scopes.
func (ch *CrudHandler) updateWrite(ctx context.Context, r *http.Request, id string, body map[string]any) (fkWrite, error) {
	w := fkWrite{
		body:  body,
		sets:  func(f schema.Field) bool { return ch.updateSets(ctx, f, true) },
		holds: unknownHeld,
	}
	var read []string
	for _, rel := range ch.referenceRelations() {
		if _, named := w.tuple(ch, rel); !named {
			continue
		}
		for _, c := range rel.ForeignKey {
			if !slices.Contains(read, c) {
				read = append(read, c)
			}
		}
	}
	if len(read) == 0 {
		return w, nil
	}
	stored, err := ch.selectStored(ctx, r, id, read)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored == nil) {
		return fkWrite{}, errNotFound
	}
	if err != nil {
		return fkWrite{}, err
	}
	w.holds = func(c string) (any, bool) {
		v, ok := stored[ch.convertKey(c)]
		return v, ok
	}
	w.kept = func(rel entity.Relation, raws []any) bool {
		for i, c := range rel.ForeignKey {
			v, _ := w.holds(c)
			if v == nil || fmt.Sprint(v) != fmt.Sprint(raws[i]) {
				return false
			}
		}
		return true
	}
	return w, nil
}

// checkForeignKeyTuples refuses a write that sets part of a multi-column
// foreign key, or names one while a column it does not take is unknown.
func (ch *CrudHandler) checkForeignKeyTuples(w fkWrite) error {
	var fields map[string][]schema.Violation
	add := func(c string, v schema.Violation) {
		if fields == nil {
			fields = map[string][]schema.Violation{}
		}
		fields[c] = append(fields[c], v)
	}
	for _, rel := range ch.Entity.Config.Relations {
		fks := rel.ForeignKey
		if rel.Type != entity.RelManyToOne || len(fks) < 2 {
			continue
		}
		var settable, named, null int
		var kept []string
		for _, c := range fks {
			switch {
			case w.takes(ch, c):
				named++
				settable++
				if w.body[c] == nil {
					null++
				}
			case w.sets(ch.fieldByName(c)):
				settable++
			default:
				if _, known := w.holds(c); !known {
					kept = append(kept, c)
				}
			}
		}
		if named == 0 {
			continue
		}
		if named != settable || (null != 0 && null != named) {
			for _, c := range fks {
				add(c, schema.Violation{Reason: reasonPartialKey, Params: map[string]any{
					"relation": rel.Name,
					"columns":  strings.Join(fks, ", "),
				}})
			}
			continue
		}
		if len(kept) > 0 {
			for _, c := range fks {
				if w.takes(ch, c) {
					add(c, schema.Violation{Reason: reasonKeptKey, Params: map[string]any{
						"relation": rel.Name,
						"kept":     strings.Join(kept, ", "),
					}})
				}
			}
		}
	}
	if fields != nil {
		return &ValidationError{fields: fields}
	}
	return nil
}

// readScopeHidesRow reports whether the caller's ReadScope hides the row
// a write produced, asking the database on the tx-bound handler.
func (ch *CrudHandler) readScopeHidesRow(ctx context.Context, row map[string]any) (bool, error) {
	if serverWrites(ctx) {
		return false, nil
	}
	preds := readScopeFilters(ctx, ch.Entity)
	if len(preds) == 0 {
		return false, nil
	}
	vals, ok := ch.rowKeyVals(row)
	if !ok {
		return true, nil
	}
	kc, ka := ch.keyCond(vals)
	qb := query.Select(ch.keyCols()[0]).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	applyReadScopeWhere(func(sql string, args ...any) { qb.Where(sql, args...) }, preds)
	sqlStr, args := qb.Build()
	var hit any
	if err := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...).Scan(&hit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("read-scope check: %w", err)
	}
	return false, nil
}

// getScopeHidesRow reports whether the BeforeGet chain would hide a row
// an HTTP write committed; run after commit, outside the transaction.
func (ch *CrudHandler) getScopeHidesRow(r *http.Request, row map[string]any) bool {
	if ch.Hooks == nil || len(ch.Hooks.HooksFor(hook.BeforeGet)) == 0 {
		return false
	}
	vals, ok := ch.rowKeyVals(row)
	if !ok {
		return true
	}
	id, _ := ch.rowID(row)
	return ch.hookScopeHidesKey(r.Context(), r, vals, id, false)
}

func (ch *CrudHandler) scopedReadBack(ctx context.Context, row map[string]any) (map[string]any, error) {
	hidden, err := ch.readScopeHidesRow(ctx, row)
	if err != nil {
		return nil, err
	}
	if hidden {
		return ch.identityOnly(row), nil
	}
	return row, nil
}

func autoUpdatedAtColumn(ent *entity.Entity) string {
	for _, f := range ent.GetFields() {
		if f.Name == "updated_at" && f.AutoGenerate == schema.AutoTimestamp {
			return f.Name
		}
	}
	return ""
}

// doDelete deletes, or soft-deletes, record id on a tx-bound handler.
func (ch *CrudHandler) doDelete(ctx context.Context, r *http.Request, id string) error {
	if err := ch.refuseHiddenTarget(ctx, r, id, targetLive, hook.BeforeDelete); err != nil {
		return err
	}
	if pre, err := ch.selectPreImage(ctx, r, id); err == nil && pre != nil {
		ctx = WithAuditPreImage(ctx, pre)
	}

	if ch.Hooks != nil {
		if err := runHooks(ch.Hooks, ctx, hook.BeforeDelete, id); err != nil {
			return &beforeHookError{err: err}
		}
	}

	if err := ch.stampRow(ctx, id); err != nil {
		return err
	}
	var affected int64
	kc, ka := ch.keyWhere(id)
	if ch.Entity.Config.Scope.SoftDelete {
		ub := query.Update(ch.Entity.GetTable()).
			Set("deleted_at", time.Now().UTC()).
			Where(kc, ka...)
		ch.ApplyTenantScopeUpdate(ub, r)
		ch.ApplyOwnerScopeUpdate(ub, r)
		ub.Where("deleted_at IS NULL")
		n, err := ch.execOwn(ctx, id, ub.Matching, func(returning []string) (string, []any) {
			return ub.Returning(returning...).Build()
		})
		if err != nil {
			return fmt.Errorf("soft delete: %w", err)
		}
		affected = n
	} else {
		idKey, _ := ch.idVals(id)
		objs, err := ch.objectsBefore(ctx, r, id, idKey, nil, false)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		db := query.Delete(ch.Entity.GetTable()).
			Where(kc, ka...)
		ch.ApplyTenantScopeDelete(db, r)
		ch.ApplyOwnerScopeDelete(db, r)
		n, err := ch.execOwn(ctx, id, db.Matching, func(returning []string) (string, []any) {
			return db.Returning(returning...).Build()
		})
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		affected = n
		if n > 0 {
			if err := ch.releaseObjects(ctx, r, id, idKey, objs, true); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
		}
	}
	if affected == 0 {
		return errNotFound
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

// selectPreImage reads row id for the audit pre-image; callers ignore a
// failure.
func (ch *CrudHandler) selectPreImage(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	cols := ch.visibleFields()
	kc, ka := ch.keyWhere(id)
	qb := query.Select(ch.keyedCols(cols)...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	sqlStr, args := qb.Build()
	return ch.scanOwn(ctx, id, cols, sqlStr, args)
}

// selectWriteTarget reads the row a cascade-only update writes into,
// under the scalar UPDATE's scope.
func (ch *CrudHandler) selectWriteTarget(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	return ch.selectStored(ctx, r, id, ch.visibleFields())
}

// selectStored reads cols of row id under the tenant and owner write
// scope, live rows only.
func (ch *CrudHandler) selectStored(ctx context.Context, r *http.Request, id string, cols []string) (map[string]any, error) {
	kc, ka := ch.keyWhere(id)
	qb := query.Select(ch.keyedCols(cols)...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	if ch.Entity.Config.Scope.SoftDelete {
		qb.Where("deleted_at IS NULL")
	}
	sqlStr, args := qb.Build()
	return ch.scanOwn(ctx, id, cols, sqlStr, args)
}

// dropEmptyFormValues reads a blank form value of a non-text field as
// not provided: removed on create, nil on update or when required, and
// left alone for a file on update. It returns the create-removed fields an
// update would have set to nil.
func dropEmptyFormValues(s schema.Schema, body map[string]any, update bool) (cleared []string) {
	for _, f := range s.Fields {
		v, ok := body[f.Name]
		if !ok {
			continue
		}
		str, isString := v.(string)
		if !isString || str != "" {
			continue
		}
		switch f.Type {
		case schema.String, schema.Text:
			continue
		case schema.Image, schema.File:
			if update {
				delete(body, f.Name)
				continue
			}
		default:
		}
		if f.Required || update {
			body[f.Name] = nil
			continue
		}
		delete(body, f.Name)
		if f.Type != schema.Image && f.Type != schema.File {
			cleared = append(cleared, f.Name)
		}
	}
	return cleared
}
