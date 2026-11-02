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

// doCreate runs the BeforeCreate → INSERT → AfterCreate chain for a single
// record. It is the per-item primitive shared by Create and BatchCreate.
//
// Pre-conditions: ch is a tx-bound copy (its DB is *sql.Tx) and ctx is the
// tx-derived context. body is the snake_cased payload; this method mutates
// it in-place when injecting tenant_id and auto-generated values.
func (ch *CrudHandler) doCreate(ctx context.Context, r *http.Request, body map[string]any) (map[string]any, error) {
	if ch.Entity != nil && ch.Entity.Config.Scope.OwnerField != "" && !serverWrites(ctx) {
		if err := ch.requireOwnerContext(ctx); err != nil {
			return nil, err
		}
	}
	// A storage key in a file field must be one this request uploaded
	// (media_provenance.go). Checked on the caller's body, before hooks
	// add their own values.
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
		// A typed hook decodes the body into its struct: a form's
		// strings must be the values they spell first.
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
	// Blank form values go first: a "" for a number must read as "not
	// provided" before the integer coercion sees it. The hooks already
	// saw the body through coerceBeforeHooks; this pass covers what they
	// added.
	dropEmptyFormValues(ch.entitySchema(), body, false)
	if err := ch.coerceNumberColumnValues(body); err != nil {
		return nil, err
	}
	vr := schema.ValidateAll(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}
	if err := ch.refuseBlankKeyParts(body); err != nil {
		return nil, err
	}

	// The belongs_to FKs in this body must resolve for THIS caller: a
	// relation onto an owner/tenant-scoped entity is a write-side trust
	// boundary exactly as ?include= is a read-side one (issue: order_items
	// targeting another customer's order).
	if err := ch.checkBelongsToScope(ctx, r, ch.createWrite(ctx, body)); err != nil {
		return nil, err
	}
	// The state field starts at an Initial value and no stamp is set
	// (states.go). After the hooks, so a hook cannot set them either.
	if err := ch.checkStateCreate(ctx, body); err != nil {
		return nil, err
	}

	var cols []string
	var vals []any
	for _, f := range ch.Entity.GetFields() {
		if f.AutoGenerate == schema.AutoIncrement {
			// Omit: the database assigns the value, the Postgres SERIAL
			// sequence or the SQLite "INTEGER PRIMARY KEY" rowid alias.
			// Sending the 0 placeholder would collide on every insert
			// after the first. RETURNING (below) reads the assigned id back.
			continue
		}
		if f.AutoGenerate != schema.AutoNone {
			cols = append(cols, f.Name)
			// A hook's time.Time lands in the stored layout, not the driver's.
			vals = append(vals, ch.bindJSONValue(f.Name, body[f.Name]))
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
	row := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...)

	result, err := ch.scanOne(row, visFields)
	if err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}

	// A dependent cascade joins on one parent key column; a composite
	// parent has none, and processDependentCascadeWrites refuses it.
	var parentID any
	if parentPK, err := ch.keyColumn(); err == nil {
		parentID = result[parentPK]
		if parentID == nil {
			parentID = result[ch.convertKey(parentPK)]
		}
		if parentID == nil {
			if idVal, ok := body[parentPK]; ok && idVal != nil {
				parentID = idVal
			} else if idVal, ok := body[ch.convertKey(parentPK)]; ok && idVal != nil {
				parentID = idVal
			}
		}
	}

	depResults, err := ch.processDependentCascadeWrites(ctx, r, parentID, body, false)
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

// doUpdate runs the BeforeUpdate → UPDATE → AfterUpdate chain for a single
// record by id. Same pre-conditions as doCreate.
func (ch *CrudHandler) doUpdate(ctx context.Context, r *http.Request, id string, body map[string]any) (map[string]any, error) {
	if ch.Entity != nil && ch.Entity.Config.Scope.OwnerField != "" && !serverWrites(ctx) {
		if err := ch.requireOwnerContext(ctx); err != nil {
			return nil, err
		}
	}
	// A row the caller's BeforeGet hooks hide answers GET's 404, before
	// anything else reads or writes it (hook_scope.go).
	if err := ch.refuseHiddenTarget(ctx, r, id, targetLive); err != nil {
		return nil, err
	}
	// A storage key in a file field must be one this request uploaded or
	// one already on this row (media_provenance.go).
	idKey, _ := ch.idVals(id)
	if err := ch.checkMediaProvenance(ctx, body, ch.currentMediaKeys(ctx, r, id, idKey)); err != nil {
		return nil, err
	}
	// Snapshot the pre-change row inside the same transaction so the audit
	// hook can diff old vs new. Best-effort, a SELECT failure here must
	// not block the update itself (the audit log already tolerates a
	// missing pre-image and just emits {"old": null, "new": ...}).
	if pre, err := ch.selectPreImage(ctx, r, id); err == nil && pre != nil {
		ctx = WithAuditPreImage(ctx, pre)
	}
	// A hook cannot move this record mid-write (RunTransition).
	if ch.Entity != nil {
		ctx = withRecordWrite(ctx, ch.Entity.GetName(), id)
	}

	if ch.Hooks != nil {
		// As on create: the typed hook's decode sees a form's values, not
		// its strings.
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

	// Partial validation, only check fields the caller actually sent.
	// Missing fields aren't treated as "required" violations because the
	// existing row already satisfies them; the UPDATE only touches the
	// columns present in the body.
	// Blank form values go first: a "" for a number must read as "not
	// provided" before the integer coercion sees it. As on create, this
	// pass covers what the hooks added.
	dropEmptyFormValues(ch.entitySchema(), body, true)
	if err := ch.coerceNumberColumnValues(body); err != nil {
		return nil, err
	}
	vr := schema.ValidatePartial(ch.entitySchema(), body)
	if !vr.Valid {
		return nil, &ValidationError{fields: vr.Errors}
	}

	// The state field and stamps change only through a move: the stored
	// value written back is dropped, any other is refused (states.go).
	ctx, err = ch.checkStateUpdate(ctx, r, id, body)
	if err != nil {
		return nil, err
	}
	// Same write-side trust boundary as doCreate: a PATCH may retarget a
	// belongs_to FK at a foreign row as freely as a POST could. It checks
	// the reference the row will hold, after the SET below drops what it
	// does not write.
	w, err := ch.updateWrite(ctx, r, id, body)
	if err != nil {
		return nil, err
	}
	if err := ch.checkBelongsToScope(ctx, r, w); err != nil {
		return nil, err
	}

	if err := ch.stampRow(ctx, id); err != nil {
		return nil, err
	}
	// A file the write replaces or clears is deleted once it commits
	// (media_cleanup.go).
	objs, err := ch.objectsBefore(ctx, r, id, idKey, body, false)
	if err != nil {
		return nil, err
	}

	ub := query.Update(ch.Entity.GetTable())
	anySet := false
	for _, f := range ch.Entity.GetFields() {
		if !ch.updateSets(ctx, f, true) {
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
		// Restamp updated_at to now(). The field loop above skips every
		// AutoGenerate field (so a client can't forge the timestamp), which
		// would otherwise leave updated_at frozen at its creation value. Only
		// stamp when a real update is happening (anySet) and the entity actually
		// declares an auto-timestamp updated_at column.
		if col := autoUpdatedAtColumn(ch.Entity); col != "" {
			ub.Set(col, generateFieldValue(schema.AutoTimestamp))
		}

		kc, ka := ch.keyWhere(id)
		ub.Where(kc, ka...)
		ch.ApplyTenantScopeUpdate(ub, r)
		ch.ApplyOwnerScopeUpdate(ub, r)
		// A soft-deleted row is logically gone: the read paths hide it
		// (ApplySoftDeleteFilter on Get/List/cursor/pre-image) and so must the
		// write path, otherwise an owner could mutate / resurrect a record the
		// system considers deleted, which the upsert path already refuses
		// (errSoftDeletedResurrection). Match-nothing ⇒ scanRow gets ErrNoRows
		// ⇒ errNotFound, same 404 a deleted row gives on Get.
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

// checkBelongsToScope refuses a create/update body whose BelongsTo
// (many-to-one) FK targets a row the caller cannot read. The read side
// already treats such a relation as a trust boundary: ?include= and
// EagerLoad resolve the target under eagerScopeFilters, so a foreign
// parent never surfaces. The write side now resolves the SAME predicate:
// for every BelongsTo relation whose FK the body carries, the target row
// must exist under the target's own owner/tenant/read scopes, or the
// write fails with errNotFound — the exact status reading that foreign
// row answers, so existence is not leaked by a 403.
//
// Callers holding an explicit cross-scope grant (owner.AllowCrossOwner,
// the target's CrossOwnerRead permission, tenant.AllowCrossTenant) pass
// unchanged: eagerScopeFilters emits nothing for them, matching what
// their reads already see. WithServerWrites contexts (host-driven
// in-process writes: jobs, imports, admin tooling) are exempt for the
// same reason ReadOnly field writes are: the host opted into privileged
// writes and owns the reference. A NULL/absent FK, an unscoped target,
// or a target the registry does not know is skipped — no registry means
// no read-side resolver either.
//
// relationReachable is NOT applied here, unlike checkTargetRowScope's
// many_to_many link, and on purpose. It refuses every row of a target
// with Exposure.CRUD=false, or one whose "<table>:read" a held-scope
// token lacks, so an ordinary reference (posts.author_id onto auth's
// users, CRUD off by default; an items-only token filing an item under
// its own order) would stop working. An m2m link writes a pivot row, a
// generated write surface onto the target; an FK is a column of the
// caller's own row, and the write returns that row, never the target's
// fields. Nor is it an oracle: a scoped target answers the same 404 for
// a hidden row and a missing one, and an unscoped target is not looked
// up, so both write. TestBelongsToFKSkipsRelationReachableOnPurpose pins
// this.
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
		// A reference the write does not name does not move.
		raws, named := w.tuple(ch, rel)
		if !named {
			continue
		}
		// A key with a null column names no row, as a join on it matches
		// none. A blank string is a value: a row keyed on it is someone's
		// row.
		if slices.Contains(raws, nil) {
			continue
		}
		// A reference the write leaves as stored crosses no boundary: the
		// caller only re-sends what its own row holds.
		if w.kept != nil && w.kept(rel, raws) {
			continue
		}
		parts := make([]string, len(raws))
		for i, v := range raws {
			parts[i] = fmt.Sprint(v)
		}
		fk := entity.JoinKey(parts)
		// An unknown name is skipped (see above); only a name the registry
		// holds but cannot resolve (several versions) is refused.
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
			conds[i] = fmt.Sprintf("%s = $%d", c, i+1)
			allArgs[i] = coercePKValue(ch.Registry, target, targetKey[i], raws[i])
		}
		preds := eagerScopeFilters(ctx, target)
		readPreds := readScopeFilters(ctx, target)
		clause, args := filterClause(preds, len(allArgs)+1)
		readClause, readArgs := renderReadScope(readPreds, "", len(allArgs)+1+len(args))
		if readClause != "" {
			readClause = " AND " + readClause
		}
		q := fmt.Sprintf("SELECT %s FROM %s WHERE %s%s%s", keyCols[0], table, strings.Join(conds, " AND "), clause, readClause)
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
// Before hooks hide from this caller (BeforeList, plus BeforeGet with the
// row's id, as ?include= over a belongs_to applies them) with the
// not-found a row outside its declared scopes answers. raws are the
// reference's key values in the target's key order.
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

// referenceRelations is every reference a write's scope check reads: the
// belongs-to relations, and a Many relation field, whose column holds
// one target id as a string. Define derives no relation for a Many
// field (it emits no FK), so without this a write stored another owner's
// id there unchecked (2026-10-27 D43).
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

// updateSets reports whether an update writes f from the body. Key
// columns and generated fields never change. ReadOnly and Hidden fields
// are client-unsettable unless the caller opted in via
// WithServerWrites(ctx), or, where overrides apply, a state override
// releases them. The owner field is the framework's: the owner scope pins
// the WHERE to the caller's id, but a SET would still let a legitimate
// owner hand the row to another user ("transfer-by-tamper"). The tenant
// column has the same hazard when MultiTenant is on.
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

// fkWrite is one write as the foreign-key checks read it: the body, the
// columns the write takes from it, and what a column it does not take
// holds afterwards. The checks read each reference as the row will hold
// it, never as the body spells it: a column the write drops from the
// body keeps its stored value, its default, or the caller's tenant.
type fkWrite struct {
	body map[string]any
	// sets reports whether the write takes f from the body.
	sets func(f schema.Field) bool
	// holds returns the value column c holds after the write when the
	// write does not take it from the body, or false when the write
	// cannot know it: an upsert's two arms and a bulk update's rows can
	// each leave a different value there.
	holds func(c string) (any, bool)
	// kept reports whether rel's tuple, as the row will hold it, is the
	// one the row holds now. Only an update reads the stored row; every
	// other write leaves it nil and checks each reference it names.
	kept func(rel entity.Relation, raws []any) bool
}

// takes reports whether the write takes column c from the body and the
// body names it.
func (w fkWrite) takes(ch *CrudHandler, c string) bool {
	_, ok := w.body[c]
	return ok && w.sets(ch.fieldByName(c))
}

// tuple returns rel's foreign key as the row will hold it, and whether
// the write names it at all. A column the write cannot know reads nil;
// checkForeignKeyTuples refuses such a write before anything reads it.
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

// unknownHeld is fkWrite.holds for a column whose stored value the write
// has not read.
func unknownHeld(string) (any, bool) { return nil, false }

// scopedHeld is fkWrite.holds for the writes that cannot read one stored
// row: the upsert's conflict update and the bulk update. Each row they
// touch is scoped to the caller's tenant and owner and keeps both, so
// those columns hold the caller's; every other column they keep is
// unknown. A context with no tenant (a cross-tenant bulk update), or no
// owner or a cross-owner grant (which lifts the bulk update's owner
// filter), leaves that column unknown.
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

// createWrite reads a create as the INSERT writes it. It takes a field
// from the body under the rules of doCreate's column loop; a column it
// skips holds its default, or the caller's tenant for the tenant column.
// The database assigns an AutoIncrement column, so its value is unknown
// until the row exists.
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

// updateWrite reads a PATCH as its SET writes it. A column the SET does
// not write (a key column, a ReadOnly one, the tenant) holds its stored
// value. Every column of each reference the body names is read once,
// under the scopes the UPDATE runs under, so a reference the PATCH leaves
// as stored is kept without a check (2026-10-27 D42): the record form
// re-sends a target the caller can no longer read.
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
		v, ok := stored[ch.convertKey(c)] // rows are keyed by wire name
		return v, ok
	}
	// Values compare as the scope check keys them. A spelling that differs
	// from the stored one ("01" for 1) is not kept, so it is checked. A
	// stored null is never kept: it prints as "<nil>", a spelling a body
	// can send.
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
// foreign key. Among the columns the write takes from the body it names
// some and not others, or sets some to null and others to a value: the
// row would point at a reference nobody wrote, half old and half new. A
// write that names the key while it cannot know what a column it does
// not take holds is refused too (validation.kept_foreign_key): the upsert
// and the bulk update cannot read one stored tuple to check. It runs for
// every caller, server writes included, because the shape is wrong
// whoever sends it.
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

// readScopeHidesRow reports whether the caller's read scope hides the row a
// write just produced. A write's RETURNING is a read handed to the caller,
// and the write gates (owner, tenant, Access) do not carry ReadScope, so a
// caller allowed to write a row outside its scope would otherwise read every
// column of a row GET answers 404 for. Callers answer with identityOnly when
// this is true; the write itself stands.
//
// It asks the database rather than evaluating the predicates in Go, under
// the same readScopeFilters every read path renders, so the answer is the
// one GET would give for this row. Run it on the tx-bound handler so it sees
// the uncommitted write. Unrestricted callers and entities without a
// ReadScope cost nothing: no predicates, no query. A WithServerWrites
// caller is trusted server Go and reads back the whole row.
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
		return true, nil // cannot name the row: fail closed
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

// getScopeHidesRow reports whether the BeforeGet chain GET runs would hide
// a row an HTTP write just committed (2026-10-27 D4). readScopeHidesRow
// covers the declared ReadScope; an app that scopes reads with a BeforeGet
// hook and grants writes more widely handed a writer every column of a row
// GET answers 404 for, a no-op PATCH being enough to read it. A write to a
// row the hook already hides now answers 404 before it runs
// (refuseHiddenTarget); this still covers a create, and a write that moves
// its row out of the caller's sight.
//
// The write routes call it after commit, outside the transaction, where
// GET would run the chain: a hook that reads the database would otherwise
// wait on the write's own connection. A hook error, or a row it cannot
// name, hides the row: the write stands, and the response names it by id.
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

// scopedReadBack returns row, or only its id when the caller's read scope
// hides it. See readScopeHidesRow.
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
	// A row the caller's BeforeGet hooks hide answers GET's 404
	// (hook_scope.go).
	if err := ch.refuseHiddenTarget(ctx, r, id, targetLive); err != nil {
		return err
	}
	// Snapshot the row before deletion so the audit hook can record what
	// went away. Without this the audit row only carries a record_id,
	// useful for "who did it" but useless for "what was lost".
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
		// Don't re-soft-delete an already-deleted row: without this the
		// UPDATE matches the trashed row, bumps deleted_at, and reports
		// success, making a re-delete of a logically-gone record look
		// like a fresh delete. Filtering to live rows makes affected==0,
		// which maps to errNotFound below.
		ub.Where("deleted_at IS NULL")
		n, err := ch.execOwn(ctx, id, ub.Matching, func(returning []string) (string, []any) {
			return ub.Returning(returning...).Build()
		})
		if err != nil {
			return fmt.Errorf("soft delete: %w", err)
		}
		affected = n
	} else {
		// The row's stored objects go with it once the delete commits
		// (media_cleanup.go). A soft delete keeps them for a restore.
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

// selectPreImage SELECTs the row matching `id` (with the same tenant /
// owner / soft-delete scopes that the mutating statement will use) so
// audit hooks can capture an old-state snapshot. Returns (nil, nil) when
// the row doesn't exist or the SELECT fails, callers treat that as
// "no snapshot available" rather than aborting the surrounding mutation.
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

// selectWriteTarget reads the row an update without scalar fields cascades
// into, under the WHERE the scalar UPDATE applies: tenant, the owner WRITE
// scope (no CrossOwnerRead lift) and deleted_at IS NULL. The read-scoped
// selectPreImage let a caller who may read another owner's row attach
// children or pivot links to it.
func (ch *CrudHandler) selectWriteTarget(ctx context.Context, r *http.Request, id string) (map[string]any, error) {
	return ch.selectStored(ctx, r, id, ch.visibleFields())
}

// selectStored reads cols of id's record under the caller's tenant and
// owner scopes, skipping a soft-deleted row, as the update will find it.
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

// dropEmptyFormValues treats an empty string as "not provided" for every
// field whose type is not free text. An HTML form posts every control it
// has, so a number, date, or relation the user left blank arrives as ""
// (the framework's own resource-engine forms included); the validator
// read that as "must be an integer" and the form failed on a field the
// user never touched, with the default it declared going unused. On
// create a blank optional field is removed from the body, so the
// declared default applies; on update it becomes nil, so blanking the
// control clears the column (dropping it kept the value while the save
// reported success). A blank required field becomes nil either way, so
// the error names the real problem ("is required"). String and Text keep
// "" as a value: empty text is a thing a user says. A file input cannot
// carry the stored value, so a blank Image or File on update means "no
// new file" and leaves the column alone.
//
// It returns the fields it removed that the update reading would have set
// to nil instead (the blank optional non-file fields of a create), so an
// upsert that lands on an existing row can clear them as update does.
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
			// Empty text is a value the user can mean.
			continue
		case schema.Image, schema.File:
			if update {
				delete(body, f.Name)
				continue
			}
		default:
			// Int, Float, Decimal, Bool, Enum, UUID, Timestamp, Date,
			// JSON, Relation: "" is not a value of the type, only a
			// blank control.
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
