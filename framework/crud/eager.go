package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// EagerLoad loads relations for the parent record ids in batched queries
// and returns, by parent id, each relation's row or rows.
//
// With a registry each relation's target is resolved against ent's version
// and its loaded rows get the target's own read scrubs: soft-deleted rows
// excluded, only declared non-Hidden columns served, owner, tenant and read
// scope applied, and values decoded as the target's own read returns them.
// It errors when a target cannot be resolved. Without a registry rows are
// served raw, so a caller loading a scoped, soft-deleting or Hidden-field
// target must pass one.
func EagerLoad(ctx context.Context, db DBExecutor, ent *entity.Entity, relations []entity.Relation, ids []string, reg ...entity.Registry) (map[string]map[string]any, error) {
	if len(ids) == 0 || len(relations) == 0 {
		return make(map[string]map[string]any), nil
	}

	var registry entity.Registry
	if len(reg) > 0 {
		registry = reg[0]
	}

	var err error
	keyCols := ent.KeyColumns()

	result := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		result[id] = make(map[string]any)
	}

	tableName := ent.GetTable()
	if _, err := query.SafeIdent(tableName); err != nil {
		return nil, fmt.Errorf("eager load: invalid parent table %q: %w", tableName, err)
	}

	for _, rel := range relations {
		var safeFKs []string
		if rel.Type != entity.RelManyToMany {
			if safeFKs, err = relationFKs(rel); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		}

		var target *entity.Entity
		if registry != nil {
			t, err := entity.ResolveTarget(registry, ent, rel.Entity)
			if err != nil {
				return nil, fmt.Errorf("eager load %s: cannot resolve relation target %q: %w", rel.Name, rel.Entity, err)
			}
			if t == nil {
				return nil, fmt.Errorf("eager load %s: relation target %q resolved to no entity", rel.Name, rel.Entity)
			}
			target = t
		}
		relTable := rel.Entity
		if target != nil && target.GetTable() != "" {
			relTable = target.GetTable()
		}
		safeRelEntity, err := query.SafeIdent(relTable)
		if err != nil {
			return nil, fmt.Errorf("eager load %s: invalid relation table %q: %w", rel.Name, relTable, err)
		}
		softDeleteFilter := ""
		if target != nil && target.Config.Scope.SoftDelete {
			softDeleteFilter = " AND deleted_at IS NULL"
		}
		served := servedColumns(target)
		scopeFilters := eagerScopeFilters(ctx, target)
		readPreds := readScopeFilters(ctx, target)
		if err := entity.CheckRelationKeys(ent, rel, target); err != nil {
			return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
		}
		targetKey, err := query.SafeColumns(target.KeyColumns(), "column")
		if err != nil {
			return nil, fmt.Errorf("eager load %s: invalid key column: %w", rel.Name, err)
		}

		switch rel.Type {
		case entity.RelHasOne, entity.RelHasMany:
			if err := loadHasManyFiltered(ctx, db, safeRelEntity, safeFKs, rel, target, scopeFilters, readPreds, hookScope{}, ids, result, softDeleteFilter, served, nil); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToOne:
			if err := loadBelongsToFiltered(ctx, db, tableName, keyCols, safeRelEntity, safeFKs, targetKey, rel, target, scopeFilters, readPreds, hookScope{}, ids, result, softDeleteFilter, served, nil); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToMany:
			mtmSoftDelete := softDeleteFilter
			if mtmSoftDelete != "" {
				mtmSoftDelete = " AND " + query.QuoteKeyword(safeRelEntity) + ".deleted_at IS NULL"
			}
			pivot, err := entity.PivotEntity(registry, ent, rel.Through)
			if err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
			node := &IncludeNode{Relation: rel, Target: target, Filters: scopeFilters, ReadScopes: readPreds, Pivot: pivot, PivotScopes: pivotScopes(ctx, pivot)}
			pivotKeys, err := validatePivotIdentifiers(rel)
			if err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
			if err := loadManyToManyFiltered(ctx, db, safeRelEntity, targetKey[0], pivotKeys, node, hookScope{}, ids, result, mtmSoftDelete, served, nil); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		}
		decodeEntityReadColumns(target, gatherLoadedRows(result, rel.Name))
	}

	return result, nil
}

// attachChildRow puts a child row under its parent: HasOne replaces the
// slot, HasMany appends.
func attachChildRow(rel entity.Relation, parentID string, row map[string]any, result map[string]map[string]any) {
	if existing, ok := result[parentID]; ok {
		if rel.Type == entity.RelHasOne {
			existing[rel.Name] = row
		} else {
			var slice []map[string]any
			if prev, ok := existing[rel.Name]; ok {
				slice = prev.([]map[string]any)
			}
			slice = append(slice, row)
			existing[rel.Name] = slice
		}
	}
}

// eagerScopeFilters builds a relation target's owner and tenant predicates.
func eagerScopeFilters(ctx context.Context, target *entity.Entity) []filter.ParsedFilter {
	if target == nil {
		return nil
	}
	var out []filter.ParsedFilter
	if ownerField := target.Config.Scope.OwnerField; ownerField != "" {
		crossOwner := owner.IsCrossOwner(ctx)
		if !crossOwner {
			probe := &CrudHandler{Entity: target}
			crossOwner = probe.crossOwnerReadGranted(ctx)
		}
		if !crossOwner {
			var val string
			if id, ok := owner.Get(ctx); ok && id != nil {
				val = fmt.Sprintf("%v", id)
			}
			out = append(out, filter.ParsedFilter{
				Field: ownerField,
				Op:    filter.OpEq,
				Value: val,
			})
		}
	}
	if target.Config.Scope.MultiTenant && !tenant.IsCrossTenant(ctx) {
		out = append(out, filter.ParsedFilter{
			Field: target.Config.TenantColumn(),
			Op:    filter.OpEq,
			Value: tenant.GetTenantID(ctx),
		})
	}
	return out
}

// scopedTargetQuery builds a one-row target lookup: the caller's
// equality conditions and args first, then the target's owner, tenant
// and read scopes and its soft-delete guard.
func scopedTargetQuery(ctx context.Context, target *entity.Entity, table, selectExpr string, conds []string, args []any) (string, []any) {
	preds := eagerScopeFilters(ctx, target)
	readPreds := readScopeFilters(ctx, target)
	clause, scopeArgs := filterClause(preds, len(args)+1)
	readClause, readArgs := renderReadScope(readPreds, "", len(args)+1+len(scopeArgs))
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s%s%s", selectExpr, query.QuoteKeyword(table), strings.Join(conds, " AND "), clause, readClause)
	if target.Config.Scope.SoftDelete {
		q += " AND deleted_at IS NULL"
	}
	return q, append(append(args, scopeArgs...), readArgs...)
}
