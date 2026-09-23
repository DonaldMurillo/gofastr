package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// childHandlerFor builds a tx-bound CrudHandler for the relation target.
func (ch *CrudHandler) childHandlerFor(target *entity.Entity) *CrudHandler {
	child := NewCrudHandler(target, ch.DB).WithJSONCase(ch.JSONCase)
	pk := target.PrimaryKey
	if pk == "" {
		pk = "id"
	}
	child.PrimaryKey = pk
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

// coerceLinkPK validates and coerces an ID for ManyToMany linking.
// If the target entity's primary key is schema.Int, float64 / float-based json.Number values
// that are at or beyond ±2^53 (or not exact integers) are rejected with a ValidationError,
// preventing silent precision loss.
func coerceLinkPK(target *entity.Entity, targetPK string, fieldPrefix string, val any) (any, error) {
	if target == nil || val == nil {
		return val, nil
	}
	isPKInt := false
	for _, f := range target.GetFields() {
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
			return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
				"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
			}}}
		}
		return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
			"must be an integer",
		}}}
	case float32:
		if i, ok := exactFloatInt64(float64(x)); ok {
			return i, nil
		}
		if float64(x) == math.Trunc(float64(x)) {
			return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
				"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
			}}}
		}
		return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
			"must be an integer",
		}}}
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
				return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
					"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
				}}}
			}
		}
		return nil, &ValidationError{fields: map[string][]string{fieldPrefix: {
			"must be an integer",
		}}}
	}
	return val, nil
}

// coercePKValue coerces a value into an integer type if the entity's primary key (or FK) is an integer schema type.
// This prevents PostgreSQL strict type errors (e.g. integer = double precision or integer = text).
func coercePKValue(ent *entity.Entity, colName string, val any) any {
	if ent == nil || val == nil {
		return val
	}
	for _, f := range ent.GetFields() {
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
					if v > math.MaxInt64 {
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
func (ch *CrudHandler) checkChildOwnership(ctx context.Context, target *entity.Entity, targetPK, fkCol string, childIDVal, parentID any) error {
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
	coercedChildID := coercePKValue(target, targetPK, childIDVal)
	coercedParentID := coercePKValue(ch.Entity, ch.PrimaryKey, parentID)
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
	return nil
}

// findExistingHasOneChild searches for an existing HasOne child record under parentID,
// enforcing tenant, owner, read scopes, and soft-delete exclusions.
func (ch *CrudHandler) findExistingHasOneChild(ctx context.Context, target *entity.Entity, targetPK, fkCol string, parentID any) (any, error) {
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
	coercedParentID := coercePKValue(ch.Entity, ch.PrimaryKey, parentID)
	allArgs := append([]any{coercedParentID}, args...)
	allArgs = append(allArgs, readArgs...)
	var existingID any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return existingID, nil
}

// checkTargetRowScope verifies that a target entity row exists and is readable
// under the caller's tenant and owner scopes before linking via ManyToMany.
func (ch *CrudHandler) checkTargetRowScope(ctx context.Context, target *entity.Entity, targetPK string, idVal any) error {
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
	coercedID := coercePKValue(target, targetPK, idVal)
	allArgs := append([]any{coercedID}, args...)
	allArgs = append(allArgs, readArgs...)
	var hit any
	if err := ch.DB.QueryRowContext(ctx, q, allArgs...).Scan(&hit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: target %s/%v is not readable by this caller", errNotFound, target.GetName(), idVal)
		}
		return fmt.Errorf("target %s/%v lookup: %w", target.GetName(), idVal, err)
	}
	return nil
}

func prefixValidationErrors(prefix string, fields map[string][]string) map[string][]string {
	out := make(map[string][]string, len(fields))
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
			return nil, &ValidationError{fields: map[string][]string{rel.Name: {"must be an object"}}}
		}

		target, err := entity.ResolveTarget(ch.Registry, ch.Entity, rel.Entity)
		if err != nil {
			return nil, fmt.Errorf("relation %q target %q cannot be resolved: %w", rel.Name, rel.Entity, err)
		}
		if target == nil {
			return nil, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
		}

		childHandler := ch.childHandlerFor(target)
		childMap = childHandler.unconvertMapKeys(childMap)
		targetPK := target.PrimaryKey
		if targetPK == "" {
			targetPK = "id"
		}

		var childResult map[string]any
		childIDVal, hasChildID := childMap[targetPK]
		if !hasChildID {
			childIDVal, hasChildID = childMap[ch.convertKey(targetPK)]
		}
		if isUpdate && hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
			if r != nil && !childHandler.CanWriteRecordScoped(ctx, opUpdate, fmt.Sprint(childIDVal)) {
				return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
			}
			childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
			if err == nil {
				childHandler.EmitEvent(ctx, event.EntityUpdated, childResult)
			}
		} else {
			if r != nil && !childHandler.CanWriteRecordScoped(ctx, opCreate, "") {
				return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
			}
			childResult, err = childHandler.doCreate(ctx, r, childMap)
			if err == nil {
				childHandler.EmitEvent(ctx, event.EntityCreated, childResult)
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
			childPKVal = childResult[ch.convertKey(targetPK)]
		}
		body[rel.ForeignKey] = childPKVal
		attached[ch.convertKey(rel.Name)] = childResult
	}
	return attached, nil
}

// processDependentCascadeWrites handles nested mutations for dependent relations
// (HasOne, HasMany, ManyToMany) after the parent entity has been created or updated.
func (ch *CrudHandler) processDependentCascadeWrites(ctx context.Context, r *http.Request, parentID any, body map[string]any, isUpdate bool) (map[string]any, error) {
	if ch.Entity == nil {
		return nil, nil
	}
	attached := make(map[string]any)
	for _, rel := range ch.Entity.Config.Relations {
		if !rel.CascadeWrite {
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
		targetPK := target.PrimaryKey
		if targetPK == "" {
			targetPK = "id"
		}

		switch rel.Type {
		case entity.RelHasOne:
			childMap, ok := rawVal.(map[string]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]string{rel.Name: {"must be an object"}}}
			}
			childMap = childHandler.unconvertMapKeys(childMap)
			childMap[rel.ForeignKey] = parentID

			var childResult map[string]any
			childIDVal, hasChildID := childMap[targetPK]
			if !hasChildID {
				childIDVal, hasChildID = childMap[ch.convertKey(targetPK)]
			}
			if isUpdate {
				if hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
					if err := ch.checkChildOwnership(ctx, target, targetPK, rel.ForeignKey, childIDVal, parentID); err != nil {
						return nil, err
					}
					if r != nil && !childHandler.CanWriteRecordScoped(ctx, opUpdate, fmt.Sprint(childIDVal)) {
						return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
					if err == nil {
						childHandler.EmitEvent(ctx, event.EntityUpdated, childResult)
					}
				} else {
					existingID, scanErr := ch.findExistingHasOneChild(ctx, target, targetPK, rel.ForeignKey, parentID)
					if scanErr != nil {
						return nil, scanErr
					}
					if existingID != nil {
						if r != nil && !childHandler.CanWriteRecordScoped(ctx, opUpdate, fmt.Sprint(existingID)) {
							return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
						}
						childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(existingID), childMap)
						if err == nil {
							childHandler.EmitEvent(ctx, event.EntityUpdated, childResult)
						}
					} else {
						if r != nil && !childHandler.CanWriteRecordScoped(ctx, opCreate, "") {
							return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
						}
						childResult, err = childHandler.doCreate(ctx, r, childMap)
						if err == nil {
							childHandler.EmitEvent(ctx, event.EntityCreated, childResult)
						}
					}
				}
			} else {
				if r != nil && !childHandler.CanWriteRecordScoped(ctx, opCreate, "") {
					return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
				}
				childResult, err = childHandler.doCreate(ctx, r, childMap)
				if err == nil {
					childHandler.EmitEvent(ctx, event.EntityCreated, childResult)
				}
			}
			if err != nil {
				var ve *ValidationError
				if errors.As(err, &ve) {
					return nil, &ValidationError{fields: prefixValidationErrors(rel.Name, ve.fields)}
				}
				return nil, fmt.Errorf("%s: %w", rel.Name, err)
			}
			attached[ch.convertKey(rel.Name)] = childResult

		case entity.RelHasMany:
			slice, ok := rawVal.([]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]string{rel.Name: {"must be an array"}}}
			}
			childResults := make([]map[string]any, 0, len(slice))
			for idx, item := range slice {
				childMap, ok := item.(map[string]any)
				if !ok {
					return nil, &ValidationError{fields: map[string][]string{fmt.Sprintf("%s.%d", rel.Name, idx): {"must be an object"}}}
				}
				childMap = childHandler.unconvertMapKeys(childMap)
				childMap[rel.ForeignKey] = parentID

				var childResult map[string]any
				childIDVal, hasChildID := childMap[targetPK]
				if !hasChildID {
					childIDVal, hasChildID = childMap[ch.convertKey(targetPK)]
				}
				if isUpdate && hasChildID && childIDVal != nil && fmt.Sprint(childIDVal) != "" {
					if err := ch.checkChildOwnership(ctx, target, targetPK, rel.ForeignKey, childIDVal, parentID); err != nil {
						return nil, err
					}
					if r != nil && !childHandler.CanWriteRecordScoped(ctx, opUpdate, fmt.Sprint(childIDVal)) {
						return nil, fmt.Errorf("%w: permission denied to update %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doUpdate(ctx, r, fmt.Sprint(childIDVal), childMap)
					if err == nil {
						childHandler.EmitEvent(ctx, event.EntityUpdated, childResult)
					}
				} else {
					if r != nil && !childHandler.CanWriteRecordScoped(ctx, opCreate, "") {
						return nil, fmt.Errorf("%w: permission denied to create %s", errNotFound, target.GetName())
					}
					childResult, err = childHandler.doCreate(ctx, r, childMap)
					if err == nil {
						childHandler.EmitEvent(ctx, event.EntityCreated, childResult)
					}
				}
				if err != nil {
					var ve *ValidationError
					if errors.As(err, &ve) {
						return nil, &ValidationError{fields: prefixValidationErrors(fmt.Sprintf("%s.%d", rel.Name, idx), ve.fields)}
					}
					return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
				}
				childResults = append(childResults, childResult)
			}
			attached[ch.convertKey(rel.Name)] = childResults

		case entity.RelManyToMany:
			slice, ok := rawVal.([]any)
			if !ok {
				return nil, &ValidationError{fields: map[string][]string{rel.Name: {"must be an array"}}}
			}
			childResults := make([]map[string]any, 0, len(slice))
			through, err := query.SafeIdent(rel.Through)
			if err != nil {
				return nil, fmt.Errorf("relation %q through table %q: %w", rel.Name, rel.Through, err)
			}
			localKey, err := query.SafeIdent(rel.LocalKey)
			if err != nil {
				return nil, fmt.Errorf("relation %q local key %q: %w", rel.Name, rel.LocalKey, err)
			}
			fkTarget, err := query.SafeIdent(rel.ForeignKeyTarget)
			if err != nil {
				return nil, fmt.Errorf("relation %q foreign key target %q: %w", rel.Name, rel.ForeignKeyTarget, err)
			}
			for idx, item := range slice {
				var childID any
				var childResult map[string]any
				fieldPrefix := fmt.Sprintf("%s.%d", rel.Name, idx)

				switch it := item.(type) {
				case map[string]any:
					childMap := childHandler.unconvertMapKeys(it)
					idVal, hasID := childMap[targetPK]
					if !hasID {
						idVal, hasID = childMap[ch.convertKey(targetPK)]
					}
					if hasID {
						coerced, err := coerceLinkPK(target, targetPK, fieldPrefix, idVal)
						if err != nil {
							return nil, err
						}
						childID = coerced
						if err := ch.checkTargetRowScope(ctx, target, targetPK, childID); err != nil {
							return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
						}
						if len(childMap) == 1 {
							childResult = childMap
						} else {
							if r != nil && !childHandler.CanWriteRecordScoped(ctx, opUpdate, fmt.Sprint(childID)) {
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
							childHandler.EmitEvent(ctx, event.EntityUpdated, childResult)
						}
					} else {
						if r != nil && !childHandler.CanWriteRecordScoped(ctx, opCreate, "") {
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
						childHandler.EmitEvent(ctx, event.EntityCreated, childResult)
						childID = childResult[targetPK]
						if childID == nil {
							childID = childResult[ch.convertKey(targetPK)]
						}
					}
				case string, int, int64, int32, uint, uint64, float64:
					coerced, err := coerceLinkPK(target, targetPK, fieldPrefix, it)
					if err != nil {
						return nil, err
					}
					childID = coerced
					childResult = map[string]any{childHandler.convertKey(targetPK): childID}
					if err := ch.checkTargetRowScope(ctx, target, targetPK, childID); err != nil {
						return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
					}
				case json.Number:
					coerced, err := coerceLinkPK(target, targetPK, fieldPrefix, it)
					if err != nil {
						return nil, err
					}
					childID = coerced
					childResult = map[string]any{childHandler.convertKey(targetPK): childID}
					if err := ch.checkTargetRowScope(ctx, target, targetPK, childID); err != nil {
						return nil, fmt.Errorf("%s.%d: %w", rel.Name, idx, err)
					}
				default:
					return nil, &ValidationError{fields: map[string][]string{fmt.Sprintf("%s.%d", rel.Name, idx): {"must be an object or ID"}}}
				}

				coercedParentID := coercePKValue(ch.Entity, ch.PrimaryKey, parentID)
				coercedChildID := coercePKValue(target, targetPK, childID)

				var exists int
				checkQ := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1 AND %s = $2",
					through, localKey, fkTarget)
				err := ch.DB.QueryRowContext(ctx, checkQ, coercedParentID, coercedChildID).Scan(&exists)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						insQ := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES ($1, $2)",
							through, localKey, fkTarget)
						if _, insErr := ch.DB.ExecContext(ctx, insQ, coercedParentID, coercedChildID); insErr != nil {
							if !isUniqueViolation(insErr) {
								return nil, fmt.Errorf("pivot insert into %s: %w", rel.Through, insErr)
							}
						}
					} else {
						return nil, fmt.Errorf("pivot check in %s: %w", rel.Through, err)
					}
				}
				childResults = append(childResults, childResult)
			}
			attached[ch.convertKey(rel.Name)] = childResults
		}
	}
	return attached, nil
}
