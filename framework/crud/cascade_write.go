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
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// pivotLink is one many_to_many link; newPivotLink is its only
// constructor and gates each name through query.SafeIdent.
type pivotLink struct {
	through, localKey, fkTarget string
	parent, child               any
}

func newPivotLink(rel entity.Relation, parent, child any) (pivotLink, error) {
	through, err := query.SafeIdent(rel.Through)
	if err != nil {
		return pivotLink{}, fmt.Errorf("relation %q through table %q: %w", rel.Name, rel.Through, err)
	}
	localKey, err := query.SafeIdent(rel.LocalKey)
	if err != nil {
		return pivotLink{}, fmt.Errorf("relation %q local key %q: %w", rel.Name, rel.LocalKey, err)
	}
	fkTarget, err := query.SafeIdent(rel.ForeignKeyTarget)
	if err != nil {
		return pivotLink{}, fmt.Errorf("relation %q foreign key target %q: %w", rel.Name, rel.ForeignKeyTarget, err)
	}
	return pivotLink{through: through, localKey: localKey, fkTarget: fkTarget, parent: parent, child: child}, nil
}

// pivotLinked reports whether the link's pivot row exists under a
// registered pivot's tenant, owner and soft-delete scope.
func (ch *CrudHandler) pivotLinked(ctx context.Context, r *http.Request, pivotHandler *CrudHandler, l pivotLink) (bool, error) {
	cb := query.Count(l.through).
		Where(query.QuoteKeyword(l.localKey)+" = $1", l.parent).
		Where(query.QuoteKeyword(l.fkTarget)+" = $1", l.child)
	if pivotHandler != nil {
		pivotHandler.ApplyTenantScopeCount(cb, r)
		pivotHandler.ApplyOwnerScopeCount(cb, r)
		pivotHandler.ApplySoftDeleteFilterCount(cb, r)
	}
	q, args := cb.Build()
	var n int
	if err := ch.DB.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// writePivotLink writes one link unless the caller already holds it,
// through the pivot's own create when it is registered. A unique violation
// is accepted only when a recheck finds the link.
func (ch *CrudHandler) writePivotLink(ctx context.Context, r *http.Request, rel entity.Relation, pivot *entity.Entity, pivotHandler *CrudHandler, fieldPrefix string, l pivotLink) error {
	linked, err := ch.pivotLinked(ctx, r, pivotHandler, l)
	if err != nil {
		return fmt.Errorf("pivot check in %s: %w", rel.Through, err)
	}
	if linked {
		return nil
	}
	var created map[string]any
	insert := func(ctx context.Context) error {
		if pivotHandler == nil {
			//gofastr:allow(emitident) each name went through query.SafeIdent in newPivotLink, the only constructor
			insQ := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES ($1, $2)", query.QuoteKeyword(l.through), query.QuoteKeyword(l.localKey), query.QuoteKeyword(l.fkTarget))
			if _, err := ch.DB.ExecContext(ctx, insQ, l.parent, l.child); err != nil {
				return fmt.Errorf("pivot insert into %s: %w", rel.Through, err)
			}
			return nil
		}
		lw := pivotHandler.linkWrite(map[string]any{rel.LocalKey: l.parent, rel.ForeignKeyTarget: l.child})
		if err := pivotHandler.checkBelongsToScope(ctx, r, lw); err != nil {
			return wrapCascadeChildError(fieldPrefix, err)
		}
		if !pivotHandler.canCascadeWrite(ctx, r, opCreate, "") {
			return fmt.Errorf("%w: permission denied to create %s", errNotFound, pivot.GetName())
		}
		res, err := pivotHandler.doCreate(ctx, r, maps.Clone(lw.body))
		if err != nil {
			return wrapCascadeChildError(fieldPrefix, err)
		}
		created = res
		return nil
	}
	if tx, ok := fwdb.TxFromContext(ctx); ok {
		err = fwdb.InSavepoint(ctx, tx, func(ctx context.Context, _ *sql.Tx) error { return insert(ctx) })
	} else {
		err = insert(ctx)
	}
	if err != nil {
		if !isUniqueViolation(err) {
			return err
		}
		if linked, cerr := ch.pivotLinked(ctx, r, pivotHandler, l); cerr != nil || !linked {
			return err
		}
		return nil
	}
	if created != nil {
		pivotHandler.emitCascade(ctx, event.EntityCreated, created)
	}
	return nil
}

func (ch *CrudHandler) childHandlerFor(target *entity.Entity) *CrudHandler {
	child := NewCrudHandler(target, ch.DB).WithJSONCase(ch.JSONCase)
	child.Registry = ch.Registry
	child.Storage = ch.Storage
	child.ImageDeriver = ch.ImageDeriver
	child.StripUploadMetadata = ch.StripUploadMetadata
	child.Events = ch.Events
	child.Outbox = ch.Outbox
	child.ChildHooks = ch.ChildHooks
	if ch.ChildHooks != nil {
		child.Hooks = ch.ChildHooks(target.GetName())
	}
	child.BasePath = ch.BasePath
	return child
}

// linkWrite reads the pivot row a ManyToMany cascade inserts, keyed by
// the pivot field each link column names, folded.
func (ch *CrudHandler) linkWrite(cols map[string]any) fkWrite {
	link := make(map[string]any, len(cols))
	for c, v := range cols {
		if i := slices.IndexFunc(ch.Entity.GetFields(), func(f schema.Field) bool { return strings.EqualFold(f.Name, c) }); i >= 0 {
			c = ch.Entity.GetFields()[i].Name
		}
		link[c] = v
	}
	return fkWrite{
		body: link,
		sets: func(f schema.Field) bool {
			_, ok := link[f.Name]
			return ok
		},
		holds: func(c string) (any, bool) {
			f := ch.fieldByName(c)
			return f.Default, f.AutoGenerate != schema.AutoIncrement
		},
	}
}

// coerceLinkPK coerces a ManyToMany link id onto an Int key, refusing a
// float that is not an exact integer.
func coerceLinkPK(reg entity.Registry, target *entity.Entity, targetPK string, fieldPrefix string, val any) (any, error) {
	if target == nil || val == nil {
		return val, nil
	}
	isPKInt := false
	for _, f := range entity.ValueFields(reg, target) {
		if f.Name == targetPK && f.Type == schema.Int {
			isPKInt = true
			break
		}
	}
	if !isPKInt {
		return val, nil
	}

	switch x := val.(type) {
	case float64:
		if i, ok := exactFloatInt64(x); ok {
			return i, nil
		}
		if x == math.Trunc(x) && !math.IsNaN(x) && !math.IsInf(x, 0) {
			return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: reasonBeyondPrecision}}}}
		}
		return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: schema.ReasonNotInteger}}}}
	case float32:
		if i, ok := exactFloatInt64(float64(x)); ok {
			return i, nil
		}
		if float64(x) == math.Trunc(float64(x)) {
			return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: reasonBeyondPrecision}}}}
		}
		return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: schema.ReasonNotInteger}}}}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		fl, err := x.Float64()
		if err == nil {
			if i, ok := exactFloatInt64(fl); ok {
				return i, nil
			}
			if fl == math.Trunc(fl) {
				return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: reasonBeyondPrecision}}}}
			}
		}
		return nil, &ValidationError{fields: map[string][]schema.Violation{fieldPrefix: {{Reason: schema.ReasonNotInteger}}}}
	}
	return val, nil
}

// coercePKValue binds val as the type of ent's column colName.
func coercePKValue(reg entity.Registry, ent *entity.Entity, colName string, val any) any {
	if ent == nil || val == nil {
		return val
	}
	for _, f := range entity.ValueFields(reg, ent) {
		if f.Name == colName {
			switch f.Type {
			case schema.Int:
				switch v := val.(type) {
				case float64:
					if !math.IsNaN(v) && !math.IsInf(v, 0) && v == math.Trunc(v) && v >= -9.223372036854775808e18 && v < 9.223372036854776e18 {
						return int64(v)
					}
					return val
				case int:
					return int64(v)
				case int32:
					return int64(v)
				case int64:
					return v
				case uint:
					if uint64(v) > math.MaxInt64 { // uint64: a 32-bit uint cannot hold MaxInt64
						return val
					}
					return int64(v)
				case uint64:
					if v > math.MaxInt64 {
						return val
					}
					return int64(v)
				case json.Number:
					if i, err := v.Int64(); err == nil {
						return i
					}
				case string:
					if i, err := strconv.ParseInt(v, 10, 64); err == nil {
						return i
					}
				}
			default:
				return val
			}
			break
		}
	}
	return val
}

// checkChildOwnership refuses, with errNotFound, a child that is missing,
// belongs to another parent, or is hidden from the caller.
func (ch *CrudHandler) checkChildOwnership(ctx context.Context, r *http.Request, rel entity.Relation, target *entity.Entity, targetPK, fkCol string, childIDVal, parentID any) error {
	if serverWrites(ctx) {
		return nil
	}
	if target == nil {
		return nil
	}
	if targetPK == "" {
		targetPK = "id"
	}
	table, err := query.SafeIdent(target.GetTable())
	if err != nil {
		return fmt.Errorf("target table %q: %w", target.GetTable(), err)
	}
	pkCol, err := query.SafeIdent(targetPK)
	if err != nil {
		return fmt.Errorf("target key %q: %w", targetPK, err)
	}
	foreignCol, err := query.SafeIdent(fkCol)
	if err != nil {
		return fmt.Errorf("foreign key %q: %w", fkCol, err)
	}
	conds := []string{
		fmt.Sprintf("%s = $1", query.QuoteKeyword(pkCol)),
		fmt.Sprintf("%s = $2", query.QuoteKeyword(foreignCol)),
	}
	parentPK, err := ch.keyColumn()
	if err != nil {
		return err
	}
	coercedChildID := coercePKValue(ch.Registry, target, targetPK, childIDVal)
	coercedParentID := coercePKValue(ch.Registry, ch.Entity, parentPK, parentID)
	q, allArgs := scopedTargetQuery(ctx, target, table, "1", conds, []any{coercedChildID, coercedParentID})
	var belongs int
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&belongs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: child %s/%v does not belong to parent %v",
				errNotFound, target.GetName(), childIDVal, parentID)
		}
		return err
	}
	if ch.childScopeHidesKey(ctx, r, target, rel, []any{coercedChildID}, fmt.Sprint(childIDVal)) {
		return fmt.Errorf("%w: child %s/%v does not belong to parent %v",
			errNotFound, target.GetName(), childIDVal, parentID)
	}
	return nil
}

// findExistingHasOneChild returns the id of parentID's HasOne child the
// caller can read, or nil.
func (ch *CrudHandler) findExistingHasOneChild(ctx context.Context, r *http.Request, rel entity.Relation, target *entity.Entity, targetPK, fkCol string, parentID any) (any, error) {
	if target == nil {
		return nil, nil
	}
	if targetPK == "" {
		targetPK = "id"
	}
	table, err := query.SafeIdent(target.GetTable())
	if err != nil {
		return nil, fmt.Errorf("target table %q: %w", target.GetTable(), err)
	}
	pkCol, err := query.SafeIdent(targetPK)
	if err != nil {
		return nil, fmt.Errorf("target key %q: %w", targetPK, err)
	}
	foreignCol, err := query.SafeIdent(fkCol)
	if err != nil {
		return nil, fmt.Errorf("foreign key %q: %w", fkCol, err)
	}
	parentPK, err := ch.keyColumn()
	if err != nil {
		return nil, err
	}
	coercedParentID := coercePKValue(ch.Registry, ch.Entity, parentPK, parentID)
	q, allArgs := scopedTargetQuery(ctx, target, table, query.QuoteKeyword(pkCol),
		[]string{fmt.Sprintf("%s = $1", query.QuoteKeyword(foreignCol))}, []any{coercedParentID})
	var existingID any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if ch.childScopeHidesKey(ctx, r, target, rel, []any{existingID}, fmt.Sprint(existingID)) {
		return nil, nil
	}
	return existingID, nil
}

// checkTargetRowScope refuses, with errNotFound, a ManyToMany target the
// caller cannot read or reach through a relation.
func (ch *CrudHandler) checkTargetRowScope(ctx context.Context, r *http.Request, rel entity.Relation, target *entity.Entity, targetPK string, idVal any) error {
	if serverWrites(ctx) {
		return nil
	}
	if target == nil {
		return nil
	}
	if (r == nil || !inProcess(r)) && !(&CrudHandler{Entity: target}).relationReachable(ctx, "read") {
		return fmt.Errorf("%w: target %s/%v is not readable by this caller", errNotFound, target.GetName(), idVal)
	}
	if targetPK == "" {
		targetPK = "id"
	}
	table, err := query.SafeIdent(target.GetTable())
	if err != nil {
		return fmt.Errorf("target table %q: %w", target.GetTable(), err)
	}
	pkCol, err := query.SafeIdent(targetPK)
	if err != nil {
		return fmt.Errorf("target key %q: %w", targetPK, err)
	}
	coercedID := coercePKValue(ch.Registry, target, targetPK, idVal)
	q, allArgs := scopedTargetQuery(ctx, target, table, query.QuoteKeyword(pkCol),
		[]string{fmt.Sprintf("%s = $1", query.QuoteKeyword(pkCol))}, []any{coercedID})
	var hit any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&hit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: target %s/%v is not readable by this caller", errNotFound, target.GetName(), idVal)
		}
		return fmt.Errorf("target %s/%v lookup: %w", target.GetName(), idVal, err)
	}
	if ch.childScopeHidesKey(ctx, r, target, rel, []any{coercedID}, fmt.Sprint(idVal)) {
		return fmt.Errorf("%w: target %s/%v is not readable by this caller", errNotFound, target.GetName(), idVal)
	}
	return nil
}

func prefixValidationErrors(prefix string, fields map[string][]schema.Violation) map[string][]schema.Violation {
	out := make(map[string][]schema.Violation, len(fields))
	for k, msgs := range fields {
		out[prefix+"."+k] = msgs
	}
	return out
}

func (ch *CrudHandler) findCascadeValue(body map[string]any, relation entity.Relation) (any, bool) {
	if !relation.CascadeWrite {
		return nil, false
	}
	relName := relation.Name
	if val, ok := body[relName]; ok && val != nil {
		return val, true
	}
	conv := ch.convertKey(relName)
	if val, ok := body[conv]; ok && val != nil {
		return val, true
	}
	wk := ch.wireKeyColumn(relName)
	if val, ok := body[wk]; ok && val != nil {
		return val, true
	}
	if val, ok := body[casing.ToSnake(conv)]; ok && val != nil {
		return val, true
	}
	return nil, false
}

func (ch *CrudHandler) hasCascadeWrites(body map[string]any) bool {
	if ch.Entity == nil || len(ch.Entity.Config.Relations) == 0 {
		return false
	}
	for _, rel := range ch.Entity.Config.Relations {
		if _, ok := ch.findCascadeValue(body, rel); ok {
			return true
		}
	}
	return false
}

// cascadeSetKeys names the foreign keys the belongs_to cascades in body
// will set.
func (ch *CrudHandler) cascadeSetKeys(body map[string]any) map[string]bool {
	if ch.Entity == nil {
		return nil
	}
	keys := map[string]bool{}
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type != entity.RelManyToOne {
			continue
		}
		if v, ok := ch.findCascadeValue(body, rel); ok && v != nil {
			keys[oneFK(rel)] = true
		}
	}
	return keys
}

// refuseCascadeUpsertKey refuses an upsert's belongs_to cascade that
// sets a column of the entity's key.
func (ch *CrudHandler) refuseCascadeUpsertKey(body map[string]any) error {
	if ch.Entity == nil {
		return nil
	}
	keys := ch.keyCols()
	fields := map[string][]schema.Violation{}
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type != entity.RelManyToOne {
			continue
		}
		if v, ok := ch.findCascadeValue(body, rel); !ok || v == nil {
			continue
		}
		for _, fk := range rel.ForeignKey {
			if slices.Contains(keys, fk) {
				fields[rel.Name] = []schema.Violation{{Reason: reasonCascadeUpsertKey, Params: map[string]any{"column": fk}}}
				break
			}
		}
	}
	if len(fields) > 0 {
		return &ValidationError{fields: fields}
	}
	return nil
}

// cascadeParentID is the written row's single key, else the body's.
func (ch *CrudHandler) cascadeParentID(result, body map[string]any) any {
	parentPK, err := ch.keyColumn()
	if err != nil {
		return nil
	}
	for _, m := range []map[string]any{result, body} {
		for _, k := range []string{parentPK, ch.convertKey(parentPK)} {
			if v, ok := m[k]; ok && v != nil {
				return v
			}
		}
	}
	return nil
}

// refuseHiddenCascade refuses a nested write through a relation the
// entity hides, unless ctx carries WithServerWrites.
func (ch *CrudHandler) refuseHiddenCascade(ctx context.Context, rel entity.Relation) error {
	if serverWrites(ctx) || !ch.Entity.HidesRelation(rel) {
		return nil
	}
	return &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{Reason: reasonCascadeHidden}}}}
}

// refuseHiddenDependents runs refuseHiddenCascade over the dependent
// cascades in body, before any row is written.
func (ch *CrudHandler) refuseHiddenDependents(ctx context.Context, body map[string]any) error {
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type == entity.RelManyToOne {
			continue
		}
		if v, ok := ch.findCascadeValue(body, rel); !ok || v == nil {
			continue
		}
		if err := ch.refuseHiddenCascade(ctx, rel); err != nil {
			return err
		}
	}
	return nil
}

// childColumns keys a cascade child's body by column, by the rule its
// parent's body used; m is not modified.
func (ch *CrudHandler) childColumns(r *http.Request, field string, m map[string]any) (map[string]any, error) {
	if inProcess(r) {
		out := maps.Clone(m)
		if err := ch.bodyColumns(out); err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		return out, nil
	}
	seen := make(map[string]string, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		col := ch.wireKeyColumn(k)
		if prev, dup := seen[col]; dup {
			return nil, &ValidationError{fields: map[string][]schema.Violation{field: {{
				Reason: reasonKeysFold, Params: map[string]any{"first": prev, "second": k, "column": col},
			}}}}
		}
		seen[col] = k
	}
	return ch.unconvertMapKeys(m), nil
}

// processBelongsToCascadeWrites writes BelongsTo children before the
// parent and sets the parent's foreign keys to them.
func (ch *CrudHandler) processBelongsToCascadeWrites(ctx context.Context, r *http.Request, body map[string]any, isUpdate bool) (map[string]any, error) {
	if ch.Entity == nil || len(ch.Entity.Config.Relations) == 0 {
		return nil, nil
	}
	if err := ch.refuseHiddenDependents(ctx, body); err != nil {
		return nil, err
	}
	attached := make(map[string]any)
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type != entity.RelManyToOne {
			continue
		}
		rawVal, ok := ch.findCascadeValue(body, rel)
		if !ok {
			continue
		}
		childMap, ok := rawVal.(map[string]any)
		if !ok {
			return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{Reason: reasonNotObject}}}}
		}

		target, err := entity.ResolveTarget(ch.Registry, ch.Entity, rel.Entity)
		if err != nil {
			return nil, fmt.Errorf("relation %q target %q cannot be resolved: %w", rel.Name, rel.Entity, err)
		}
		if target == nil {
			return nil, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
		}

		if fk := oneFK(rel); !ch.declaresField(fk) || !ch.parentSetsKey(ctx, ch.fieldByName(fk), isUpdate) {
			return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{
				Reason: reasonCascadeKeyFixed, Params: map[string]any{"column": fk},
			}}}}
		}
		if err := ch.refuseHiddenCascade(ctx, rel); err != nil {
			return nil, err
		}

		childHandler := ch.childHandlerFor(target)
		childMap, err = childHandler.childColumns(r, rel.Name, childMap)
		if err != nil {
			return nil, err
		}
		targetPK, err := target.KeyColumn()
		if err != nil {
			return nil, fmt.Errorf("cascade write for relation %q: %w", rel.Name, err)
		}

		var childResult map[string]any
		childIDVal, hasChildID := childMap[targetPK]
		if !hasChildID {
			childIDVal, hasChildID = childMap[childHandler.convertKey(targetPK)]
		}
		if isUpdate && hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
			if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(childIDVal)) {
				return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
			}
			childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
			if err == nil {
				childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
			}
		} else {
			if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
				return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
			}
			childResult, err = childHandler.doCreate(ctx, r, childMap)
			if err == nil {
				childHandler.emitCascade(ctx, event.EntityCreated, childResult)
			}
		}
		if err != nil {
			return nil, wrapCascadeChildError(rel.Name, err)
		}

		childPKVal := childResult[targetPK]
		if childPKVal == nil {
			childPKVal = childResult[childHandler.convertKey(targetPK)]
		}
		body[oneFK(rel)] = childPKVal
		if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
			return nil, fmt.Errorf("%s: %w", rel.Name, err)
		}
		attached[ch.convertKey(rel.Name)] = childResult
	}
	return attached, nil
}

func (ch *CrudHandler) declaresField(name string) bool {
	return slices.ContainsFunc(ch.Entity.GetFields(), func(f schema.Field) bool { return f.Name == name })
}

func (ch *CrudHandler) parentSetsKey(ctx context.Context, f schema.Field, isUpdate bool) bool {
	if isUpdate {
		return ch.updateSets(ctx, f, true)
	}
	return ch.createWrite(ctx, nil).sets(f)
}

// refuseUnwrittenLink refuses a has_one or has_many child create that
// would not store its link column; ch is the child's handler.
func (ch *CrudHandler) refuseUnwrittenLink(ctx context.Context, rel entity.Relation) error {
	fk := oneFK(rel)
	if ch.declaresField(fk) && ch.createWrite(ctx, nil).sets(ch.fieldByName(fk)) {
		return nil
	}
	return &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{
		Reason: reasonCascadeKeyFixed, Params: map[string]any{"column": fk},
	}}}}
}

// wrapCascadeChildError reports a failed child write under its relation
// path ("notes", "notes.2"), prefixing a ValidationError's field names
// rather than its message.
func wrapCascadeChildError(path string, err error) error {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return &ValidationError{fields: prefixValidationErrors(path, ve.fields)}
	}
	return fmt.Errorf("%s: %w", path, err)
}

// cascadeChildUpdate guards the child row id with the cascade write
// permission, updates it, and emits EntityUpdated on success.
func cascadeChildUpdate(ctx context.Context, r *http.Request, childHandler *CrudHandler, target *entity.Entity, path, id string, childMap map[string]any) (map[string]any, error) {
	if !childHandler.canCascadeWrite(ctx, r, opUpdate, id) {
		return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
	}
	res, err := childHandler.doUpdate(ctx, r, id, childMap)
	if err != nil {
		return nil, wrapCascadeChildError(path, err)
	}
	childHandler.emitCascade(ctx, event.EntityUpdated, res)
	return res, nil
}

// cascadeChildCreate refuses an unwritten link, guards the create with the
// cascade write permission, creates the row, and emits EntityCreated.
func cascadeChildCreate(ctx context.Context, r *http.Request, childHandler *CrudHandler, rel entity.Relation, target *entity.Entity, path string, childMap map[string]any) (map[string]any, error) {
	if err := childHandler.refuseUnwrittenLink(ctx, rel); err != nil {
		return nil, err
	}
	if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
		return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
	}
	res, err := childHandler.doCreate(ctx, r, childMap)
	if err != nil {
		return nil, wrapCascadeChildError(path, err)
	}
	childHandler.emitCascade(ctx, event.EntityCreated, res)
	return res, nil
}

// processDependentCascadeWrites writes the HasOne, HasMany and
// ManyToMany children after the parent.
func (ch *CrudHandler) processDependentCascadeWrites(ctx context.Context, r *http.Request, parentID any, body map[string]any, isUpdate bool) (map[string]any, error) {
	if ch.Entity == nil {
		return nil, nil
	}
	attached := make(map[string]any)
	for _, rel := range ch.Entity.Config.Relations {
		if rel.Type == entity.RelManyToOne {
			continue
		}
		rawVal, ok := ch.findCascadeValue(body, rel)
		if !ok || rawVal == nil {
			continue
		}
		if ch.Registry == nil {
			return nil, fmt.Errorf("cascade write for relation %q requires an entity registry", rel.Name)
		}

		target, err := entity.ResolveTarget(ch.Registry, ch.Entity, rel.Entity)
		if err != nil || target == nil {
			return nil, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
		}
		childHandler := ch.childHandlerFor(target)
		parentPK, err := ch.keyColumn()
		if err != nil {
			return nil, fmt.Errorf("cascade write for relation %q: %w", rel.Name, err)
		}
		targetPK, err := target.KeyColumn()
		if err != nil {
			return nil, fmt.Errorf("cascade write for relation %q: %w", rel.Name, err)
		}

		switch rel.Type {
		case entity.RelHasOne:
			childMap, ok := rawVal.(map[string]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{Reason: reasonNotObject}}}}
			}
			childMap, err = childHandler.childColumns(r, rel.Name, childMap)
			if err != nil {
				return nil, err
			}
			childMap[oneFK(rel)] = parentID

			var childResult map[string]any
			childIDVal, hasChildID := childMap[targetPK]
			if !hasChildID {
				childIDVal, hasChildID = childMap[childHandler.convertKey(targetPK)]
			}
			if isUpdate && hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
				if err := ch.checkChildOwnership(ctx, r, rel, target, targetPK, oneFK(rel), childIDVal, parentID); err != nil {
					return nil, err
				}
				childResult, err = cascadeChildUpdate(ctx, r, childHandler, target, rel.Name, fmt.Sprint(childIDVal), childMap)
			} else if isUpdate {
				existingID, scanErr := ch.findExistingHasOneChild(ctx, r, rel, target, targetPK, oneFK(rel), parentID)
				if scanErr != nil {
					return nil, scanErr
				}
				if existingID != nil {
					childResult, err = cascadeChildUpdate(ctx, r, childHandler, target, rel.Name, fmt.Sprint(existingID), childMap)
				} else {
					childResult, err = cascadeChildCreate(ctx, r, childHandler, rel, target, rel.Name, childMap)
				}
			} else {
				childResult, err = cascadeChildCreate(ctx, r, childHandler, rel, target, rel.Name, childMap)
			}
			if err != nil {
				return nil, err
			}
			if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
				return nil, fmt.Errorf("%s: %w", rel.Name, err)
			}
			attached[ch.convertKey(rel.Name)] = childResult
		case entity.RelHasMany:
			slice, ok := rawVal.([]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{Reason: reasonNotArray}}}}
			}
			childResults := make([]map[string]any, 0, len(slice))
			for idx, item := range slice {
				path := fmt.Sprintf("%s.%d", rel.Name, idx)
				childMap, ok := item.(map[string]any)
				if !ok {
					return nil, &ValidationError{fields: map[string][]schema.Violation{path: {{Reason: reasonNotObject}}}}
				}
				childMap, err = childHandler.childColumns(r, path, childMap)
				if err != nil {
					return nil, err
				}
				childMap[oneFK(rel)] = parentID

				var childResult map[string]any
				childIDVal, hasChildID := childMap[targetPK]
				if !hasChildID {
					childIDVal, hasChildID = childMap[childHandler.convertKey(targetPK)]
				}
				if isUpdate && hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
					if err := ch.checkChildOwnership(ctx, r, rel, target, targetPK, oneFK(rel), childIDVal, parentID); err != nil {
						return nil, err
					}
					childResult, err = cascadeChildUpdate(ctx, r, childHandler, target, path, fmt.Sprint(childIDVal), childMap)
				} else {
					childResult, err = cascadeChildCreate(ctx, r, childHandler, rel, target, path, childMap)
				}
				if err != nil {
					return nil, err
				}
				if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
					return nil, fmt.Errorf("%s: %w", path, err)
				}
				childResults = append(childResults, childResult)
			}
			attached[ch.convertKey(rel.Name)] = childResults

		case entity.RelManyToMany:
			slice, ok := rawVal.([]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{Reason: reasonNotArray}}}}
			}
			childResults := make([]map[string]any, 0, len(slice))
			if _, err := newPivotLink(rel, nil, nil); err != nil {
				return nil, err
			}
			pivot, err := entity.PivotEntity(ch.Registry, ch.Entity, rel.Through)
			if err != nil {
				return nil, fmt.Errorf("relation %q: %w", rel.Name, err)
			}
			var pivotHandler *CrudHandler
			if pivot != nil {
				pivotHandler = ch.childHandlerFor(pivot)
			}
			for idx, item := range slice {
				var childID any
				var childResult map[string]any
				fieldPrefix := fmt.Sprintf("%s.%d", rel.Name, idx)

				switch it := item.(type) {
				case map[string]any:
					childMap, err := childHandler.childColumns(r, fieldPrefix, it)
					if err != nil {
						return nil, err
					}
					idVal, hasID := childMap[targetPK]
					if !hasID {
						idVal, hasID = childMap[childHandler.convertKey(targetPK)]
					}
					if hasID {
						coerced, err := coerceLinkPK(ch.Registry, target, targetPK, fieldPrefix, idVal)
						if err != nil {
							return nil, err
						}
						childID = coerced
						if err := ch.checkTargetRowScope(ctx, r, rel, target, targetPK, childID); err != nil {
							return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
						}
						if len(childMap) == 1 {
							childResult = map[string]any{childHandler.convertKey(targetPK): childID}
						} else {
							if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(childID)) {
								return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
							}
							var err error
							childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childID), childMap)
							if err != nil {
								return nil, wrapCascadeChildError(fmt.Sprintf("%s.%d", rel.Name, idx), err)
							}
							childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
							if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
								return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
							}
						}
					} else {
						if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
							return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
						}
						var err error
						childResult, err = childHandler.doCreate(ctx, r, childMap)
						if err != nil {
							return nil, wrapCascadeChildError(fmt.Sprintf("%s.%d", rel.Name, idx), err)
						}
						childHandler.emitCascade(ctx, event.EntityCreated, childResult)
						childID = childResult[targetPK]
						if childID == nil {
							childID = childResult[childHandler.convertKey(targetPK)]
						}
						if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
							return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
						}
					}
				case string, int, int64, int32, uint, uint64, float64:
					coerced, err := coerceLinkPK(ch.Registry, target, targetPK, fieldPrefix, it)
					if err != nil {
						return nil, err
					}
					childID = coerced
					childResult = map[string]any{childHandler.convertKey(targetPK): childID}
					if err := ch.checkTargetRowScope(ctx, r, rel, target, targetPK, childID); err != nil {
						return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
					}
				case json.Number:
					coerced, err := coerceLinkPK(ch.Registry, target, targetPK, fieldPrefix, it)
					if err != nil {
						return nil, err
					}
					childID = coerced
					childResult = map[string]any{childHandler.convertKey(targetPK): childID}
					if err := ch.checkTargetRowScope(ctx, r, rel, target, targetPK, childID); err != nil {
						return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
					}
				default:
					return nil, &ValidationError{fields: map[string][]schema.Violation{fmt.Sprintf("%s.%d", rel.Name, idx): {{Reason: reasonNotObjectOrID}}}}
				}

				coercedParentID := coercePKValue(ch.Registry, ch.Entity, parentPK, parentID)
				coercedChildID := coercePKValue(ch.Registry, target, targetPK, childID)

				link, err := newPivotLink(rel, coercedParentID, coercedChildID)
				if err != nil {
					return nil, err
				}
				if err := ch.writePivotLink(ctx, r, rel, pivot, pivotHandler, fieldPrefix, link); err != nil {
					return nil, err
				}
				childResults = append(childResults, childResult)
			}
			attached[ch.convertKey(rel.Name)] = childResults
		}
	}
	return attached, nil
}
