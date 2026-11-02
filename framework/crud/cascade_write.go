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
)

// pivotLink is one many_to_many link: the pivot table and its two key
// columns, each passed through query.SafeIdent, and the coerced key
// values.
type pivotLink struct {
	through, localKey, fkTarget string
	parent, child               any
}

// newPivotLink gates rel's pivot table and key columns as identifiers
// and pairs them with the link's key values.
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

// pivotLinked reports whether the link's pivot row exists as the caller
// reads it: under a registered pivot's tenant, owner and soft-delete
// scope. Read unscoped, another tenant's row stood in for the caller's
// and the caller's insert was skipped.
func (ch *CrudHandler) pivotLinked(ctx context.Context, r *http.Request, pivotHandler *CrudHandler, l pivotLink) (bool, error) {
	cb := query.Count(l.through).
		Where(l.localKey+" = $1", l.parent).
		Where(l.fkTarget+" = $1", l.child)
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

// writePivotLink writes one link unless the caller already holds it. The
// insert runs under a savepoint, so a refused one leaves a Postgres
// transaction usable, and a unique violation is accepted only when the
// recheck finds the link: a concurrent request wrote the same pair. Any
// other violation (a constraint beyond the link's own key) is the
// caller's conflict; it was swallowed, so SQLite committed the parent
// and reported a link that did not exist.
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
			insQ := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES ($1, $2)", l.through, l.localKey, l.fkTarget)
			if _, err := ch.DB.ExecContext(ctx, insQ, l.parent, l.child); err != nil {
				return fmt.Errorf("pivot insert into %s: %w", rel.Through, err)
			}
			return nil
		}
		lw := pivotHandler.linkWrite(map[string]any{rel.LocalKey: l.parent, rel.ForeignKeyTarget: l.child})
		if err := pivotHandler.checkBelongsToScope(ctx, r, lw); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				return &ValidationError{fields: prefixValidationErrors(fieldPrefix, ve.fields)}
			}
			return fmt.Errorf("%s: %w", fieldPrefix, err)
		}
		// A registered pivot is written through its own
		// create: its route's gates (Access.Create, held
		// scopes, CRUD=false) refuse here as there, and
		// its tenant and owner stamps land, so the link
		// is visible under the pivot's own scoping.
		if !pivotHandler.canCascadeWrite(ctx, r, opCreate, "") {
			return fmt.Errorf("%w: permission denied to create %s", errNotFound, pivot.GetName())
		}
		// The body keys are the pivot's field names
		// (linkWrite folds the relation's spelling).
		res, err := pivotHandler.doCreate(ctx, r, maps.Clone(lw.body))
		if err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				return &ValidationError{fields: prefixValidationErrors(fieldPrefix, ve.fields)}
			}
			return fmt.Errorf("%s: %w", fieldPrefix, err)
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

// childHandlerFor builds a tx-bound CrudHandler for the relation target.
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

// linkWrite reads the pivot row a ManyToMany cascade inserts: it writes
// the two link columns, and every other column holds its DDL default, or
// nothing known for an AutoIncrement one. A link column is keyed by the
// pivot field it names: the INSERT's unquoted identifier reaches that
// column in any case, so "POST_ID" over a field post_id is post_id.
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

// coerceLinkPK validates and coerces an ID for ManyToMany linking.
// If the target entity's primary key is schema.Int, float64 / float-based json.Number values
// that are at or beyond ±2^53 (or not exact integers) are rejected with a ValidationError,
// preventing silent precision loss.
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

// coercePKValue coerces a value into an integer type if the entity's primary key (or FK) is an integer schema type.
// This prevents PostgreSQL strict type errors (e.g. integer = double precision or integer = text).
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

// checkChildOwnership verifies that a child record exists, belongs to the parent, and is readable
// under the caller's tenant, owner, and read scopes (and is not soft-deleted).
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
	preds := eagerScopeFilters(ctx, target)
	readPreds := readScopeFilters(ctx, target)
	clause, args := filterClause(preds, 3)
	readClause, readArgs := renderReadScope(readPreds, "", 3+len(args))
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1 AND %s = $2%s%s",
		table, pkCol, foreignCol, clause, readClause)
	if target.Config.Scope.SoftDelete {
		q += " AND deleted_at IS NULL"
	}
	parentPK, err := ch.keyColumn()
	if err != nil {
		return err
	}
	coercedChildID := coercePKValue(ch.Registry, target, targetPK, childIDVal)
	coercedParentID := coercePKValue(ch.Registry, ch.Entity, parentPK, parentID)
	allArgs := append([]any{coercedChildID, coercedParentID}, args...)
	allArgs = append(allArgs, readArgs...)
	var belongs int
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&belongs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: child %s/%v does not belong to parent %v",
				errNotFound, target.GetName(), childIDVal, parentID)
		}
		return err
	}
	// A child the include would not attach is one the write neither
	// touches nor confirms: the same not-found as a child that is not
	// there.
	if ch.childScopeHidesKey(ctx, r, target, rel, []any{coercedChildID}, fmt.Sprint(childIDVal)) {
		return fmt.Errorf("%w: child %s/%v does not belong to parent %v",
			errNotFound, target.GetName(), childIDVal, parentID)
	}
	return nil
}

// findExistingHasOneChild searches for an existing HasOne child record under parentID,
// enforcing tenant, owner, read scopes, and soft-delete exclusions.
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
	preds := eagerScopeFilters(ctx, target)
	readPreds := readScopeFilters(ctx, target)
	clause, args := filterClause(preds, 2)
	readClause, readArgs := renderReadScope(readPreds, "", 2+len(args))
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1%s%s",
		pkCol, table, foreignCol, clause, readClause)
	if target.Config.Scope.SoftDelete {
		q += " AND deleted_at IS NULL"
	}
	parentPK, err := ch.keyColumn()
	if err != nil {
		return nil, err
	}
	coercedParentID := coercePKValue(ch.Registry, ch.Entity, parentPK, parentID)
	allArgs := append([]any{coercedParentID}, args...)
	allArgs = append(allArgs, readArgs...)
	var existingID any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// A child the caller's own read scope hides is one it does not have,
	// as a ReadScope-hidden one is above.
	if ch.childScopeHidesKey(ctx, r, target, rel, []any{existingID}, fmt.Sprint(existingID)) {
		return nil, nil
	}
	return existingID, nil
}

// checkTargetRowScope verifies that a target entity row exists and is readable
// under the caller's tenant and owner scopes before linking via ManyToMany.
// A target the request may not reach through a relation at all (see
// relationReachable) answers the same not-found as an unreadable row, so a
// link attempt is not an existence oracle over it.
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
	preds := eagerScopeFilters(ctx, target)
	readPreds := readScopeFilters(ctx, target)
	clause, args := filterClause(preds, 2)
	readClause, readArgs := renderReadScope(readPreds, "", 2+len(args))
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1%s%s", pkCol, table, pkCol, clause, readClause)
	if target.Config.Scope.SoftDelete {
		q += " AND deleted_at IS NULL"
	}
	coercedID := coercePKValue(ch.Registry, target, targetPK, idVal)
	allArgs := append([]any{coercedID}, args...)
	allArgs = append(allArgs, readArgs...)
	var hit any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&hit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: target %s/%v is not readable by this caller", errNotFound, target.GetName(), idVal)
		}
		return fmt.Errorf("target %s/%v lookup: %w", target.GetName(), idVal, err)
	}
	// The target's own BeforeList scope, as ?include= over the relation
	// applies it: a row it hides answers what a missing one does, or the
	// link's status is an existence oracle over hidden rows.
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

func (ch *CrudHandler) findCascadeValue(body map[string]any, relName string) (any, bool) {
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
	return nil, false
}

// hasCascadeWrites reports whether body contains any cascade-write relation keys.
func (ch *CrudHandler) hasCascadeWrites(body map[string]any) bool {
	if ch.Entity == nil || len(ch.Entity.Config.Relations) == 0 {
		return false
	}
	for _, rel := range ch.Entity.Config.Relations {
		if !rel.CascadeWrite {
			continue
		}
		if _, ok := ch.findCascadeValue(body, rel.Name); ok {
			return true
		}
	}
	return false
}

// childColumns names a cascade child's keys the way its parent's body was
// named. An in-process write runs bodyColumns, which also reads the key
// MarshalEntity derives (line2 for line_2); the HTTP rule left that key
// naming no column, so the child dropped the field. A request keeps the
// wire rule, and two keys folding onto one column are refused, as a
// batch item's are, rather than one winning by map order. field is the
// child's path in the parent body; m is not modified.
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

// processBelongsToCascadeWrites executes cascade writes for BelongsTo relations
// BEFORE the parent record is inserted/updated, so the child's primary key
// can be set as the parent's foreign key.
func (ch *CrudHandler) processBelongsToCascadeWrites(ctx context.Context, r *http.Request, body map[string]any, isUpdate bool) (map[string]any, error) {
	if ch.Entity == nil || len(ch.Entity.Config.Relations) == 0 {
		return nil, nil
	}
	attached := make(map[string]any)
	for _, rel := range ch.Entity.Config.Relations {
		if !rel.CascadeWrite || rel.Type != entity.RelManyToOne {
			continue
		}
		rawVal, ok := ch.findCascadeValue(body, rel.Name)
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

		// The cascade exists to point the parent at the child. A key the
		// parent write does not take (ReadOnly, Hidden, the owner or
		// tenant column) would leave the parent on its old reference while
		// the child is written and handed back as attached, so the
		// cascade is refused before any child is written.
		if fk := oneFK(rel); ch.declaresField(fk) && !ch.parentSetsKey(ctx, ch.fieldByName(fk), isUpdate) {
			return nil, &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{
				Reason: reasonCascadeKeyFixed, Params: map[string]any{"column": fk},
			}}}}
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
			var ve *ValidationError
			if errors.As(err, &ve) {
				return nil, &ValidationError{fields: prefixValidationErrors(rel.Name, ve.fields)}
			}
			return nil, fmt.Errorf("%s: %w", rel.Name, err)
		}

		childPKVal := childResult[targetPK]
		if childPKVal == nil {
			childPKVal = childResult[childHandler.convertKey(targetPK)]
		}
		body[oneFK(rel)] = childPKVal
		// A child the caller's read scope hides goes back as its id only,
		// as on the dependent arms.
		if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
			return nil, fmt.Errorf("%s: %w", rel.Name, err)
		}
		attached[ch.convertKey(rel.Name)] = childResult
	}
	return attached, nil
}

// declaresField reports whether the entity declares a field named name.
func (ch *CrudHandler) declaresField(name string) bool {
	return slices.ContainsFunc(ch.Entity.GetFields(), func(f schema.Field) bool { return f.Name == name })
}

// parentSetsKey reports whether the parent write takes f from the body:
// doUpdate's SET rules on an update, doCreate's column rules on a create.
func (ch *CrudHandler) parentSetsKey(ctx context.Context, f schema.Field, isUpdate bool) bool {
	if isUpdate {
		return ch.updateSets(ctx, f, true)
	}
	return ch.createWrite(ctx, nil).sets(f)
}

// refuseUnwrittenLink refuses a has_one or has_many cascade create whose
// child would not store the link: the child's create skips a foreign key
// it does not declare, or one that is ReadOnly, Hidden or its tenant
// column, so the child would be stored with no parent while the response
// showed it attached. ch is the child's handler.
func (ch *CrudHandler) refuseUnwrittenLink(ctx context.Context, rel entity.Relation) error {
	fk := oneFK(rel)
	if ch.declaresField(fk) && ch.createWrite(ctx, nil).sets(ch.fieldByName(fk)) {
		return nil
	}
	return &ValidationError{fields: map[string][]schema.Violation{rel.Name: {{
		Reason: reasonCascadeKeyFixed, Params: map[string]any{"column": fk},
	}}}}
}

// processDependentCascadeWrites handles nested mutations for dependent relations
// (HasOne, HasMany, ManyToMany) after the parent entity has been created or updated.
func (ch *CrudHandler) processDependentCascadeWrites(ctx context.Context, r *http.Request, parentID any, body map[string]any, isUpdate bool) (map[string]any, error) {
	if ch.Entity == nil {
		return nil, nil
	}
	attached := make(map[string]any)
	for _, rel := range ch.Entity.Config.Relations {
		// Belongs-to cascades ran before the parent was written and
		// need only the target's key; the parent's own key, which the
		// dependent arms read below, may span several columns.
		if !rel.CascadeWrite || rel.Type == entity.RelManyToOne {
			continue
		}
		rawVal, ok := ch.findCascadeValue(body, rel.Name)
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
			if isUpdate {
				if hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
					if err := ch.checkChildOwnership(ctx, r, rel, target, targetPK, oneFK(rel), childIDVal, parentID); err != nil {
						return nil, err
					}
					if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(childIDVal)) {
						return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
					if err == nil {
						childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
					}
				} else {
					existingID, scanErr := ch.findExistingHasOneChild(ctx, r, rel, target, targetPK, oneFK(rel), parentID)
					if scanErr != nil {
						return nil, scanErr
					}
					if existingID != nil {
						if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(existingID)) {
							return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
						}
						childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(existingID), childMap)
						if err == nil {
							childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
						}
					} else {
						if err := childHandler.refuseUnwrittenLink(ctx, rel); err != nil {
							return nil, err
						}
						if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
							return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
						}
						childResult, err = childHandler.doCreate(ctx, r, childMap)
						if err == nil {
							childHandler.emitCascade(ctx, event.EntityCreated, childResult)
						}
					}
				}
			} else {
				if err := childHandler.refuseUnwrittenLink(ctx, rel); err != nil {
					return nil, err
				}
				if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
					return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
				}
				childResult, err = childHandler.doCreate(ctx, r, childMap)
				if err == nil {
					childHandler.emitCascade(ctx, event.EntityCreated, childResult)
				}
			}
			if err != nil {
				var ve *ValidationError
				if errors.As(err, &ve) {
					return nil, &ValidationError{fields: prefixValidationErrors(rel.Name, ve.fields)}
				}
				return nil, fmt.Errorf("%s: %w", rel.Name, err)
			}
			// A child the caller's read scope hides goes back as its id only.
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
				childMap, ok := item.(map[string]any)
				if !ok {
					return nil, &ValidationError{fields: map[string][]schema.Violation{fmt.Sprintf("%s.%d", rel.Name, idx): {{Reason: reasonNotObject}}}}
				}
				childMap, err = childHandler.childColumns(r, fmt.Sprintf("%s.%d", rel.Name, idx), childMap)
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
					if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(childIDVal)) {
						return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
					if err == nil {
						childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
					}
				} else {
					if err := childHandler.refuseUnwrittenLink(ctx, rel); err != nil {
						return nil, err
					}
					if !childHandler.canCascadeWrite(ctx, r, opCreate, "") {
						return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doCreate(ctx, r, childMap)
					if err == nil {
						childHandler.emitCascade(ctx, event.EntityCreated, childResult)
					}
				}
				if err != nil {
					var ve *ValidationError
					if errors.As(err, &ve) {
						return nil, &ValidationError{fields: prefixValidationErrors(fmt.Sprintf("%s.%d", rel.Name, idx), ve.fields)}
					}
					return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
				}
				// A child the caller's read scope hides goes back as its id only.
				if childResult, err = ch.cascadeReadBack(ctx, r, rel, childHandler, childResult); err != nil {
					return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
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
							// Under the child's wire key, as the bare-id arms below.
							childResult = map[string]any{childHandler.convertKey(targetPK): childID}
						} else {
							if !childHandler.canCascadeWrite(ctx, r, opUpdate, fmt.Sprint(childID)) {
								return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
							}
							var err error
							childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childID), childMap)
							if err != nil {
								var ve *ValidationError
								if errors.As(err, &ve) {
									return nil, &ValidationError{fields: prefixValidationErrors(fmt.Sprintf("%s.%d", rel.Name, idx), ve.fields)}
								}
								return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
							}
							childHandler.emitCascade(ctx, event.EntityUpdated, childResult)
							// The update may move the child out of the caller's read scope.
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
							var ve *ValidationError
							if errors.As(err, &ve) {
								return nil, &ValidationError{fields: prefixValidationErrors(fmt.Sprintf("%s.%d", rel.Name, idx), ve.fields)}
							}
							return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
						}
						childHandler.emitCascade(ctx, event.EntityCreated, childResult)
						childID = childResult[targetPK]
						if childID == nil {
							childID = childResult[childHandler.convertKey(targetPK)]
						}
						// A child the caller's read scope hides goes back as its id only.
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
