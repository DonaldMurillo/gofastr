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
		targetKey, err := safeColumns(target.KeyColumns())
		if err != nil {
			return nil, fmt.Errorf("eager load %s: invalid key column: %w", rel.Name, err)
		}

		switch rel.Type {
		case entity.RelHasOne, entity.RelHasMany:
			if err := eagerLoadHasMany(ctx, db, safeRelEntity, safeFKs, rel, ids, result, softDeleteFilter, scopeFilters, readPreds, served, target); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToOne:
			if err := eagerLoadBelongsTo(ctx, db, tableName, keyCols, targetKey, safeRelEntity, safeFKs, rel, ids, result, softDeleteFilter, scopeFilters, readPreds, served, target); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToMany:
			targetPK := targetKey[0]
			mtmSoftDelete := softDeleteFilter
			if mtmSoftDelete != "" {
				mtmSoftDelete = " AND " + query.QuoteKeyword(safeRelEntity) + ".deleted_at IS NULL"
			}
			pivot, err := entity.PivotEntity(registry, ent, rel.Through)
			if err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
			if err := eagerLoadManyToMany(ctx, db, safeRelEntity, rel, ids, targetPK, result, mtmSoftDelete, scopeFilters, readPreds, pivot, pivotScopes(ctx, pivot), served, target); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		}
		decodeEntityReadColumns(target, gatherLoadedRows(result, rel.Name))
	}

	return result, nil
}

func eagerLoadHasMany(ctx context.Context, db DBExecutor, safeEntity string, safeFKs []string, rel entity.Relation, ids []string, result map[string]map[string]any, softDeleteFilter string, scopeFilters, readPreds []filter.ParsedFilter, served servedCols, target *entity.Entity) error {
	where, args, ok, err := keyInClause(safeFKs, ids, 1)
	if err != nil || !ok {
		return err
	}
	scopeClause, scopeArgs := filterClause(scopeFilters, len(args)+1)
	readClause, readArgs := renderReadScope(readPreds, "", len(args)+len(scopeArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT * FROM %s WHERE %s%s%s%s",
		query.QuoteKeyword(safeEntity), where, softDeleteFilter, scopeClause, readClause)
	args = append(args, scopeArgs...)
	args = append(args, readArgs...)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), entityFieldsOf(target), cols)

	for rows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			return err
		}

		row := make(map[string]any, len(cols))
		for i, c := range cols {
			served.set(row, c, convertDatabaseValue(vals[i], boolCols[i]))
		}
		if parentID, ok := scannedKey(cols, vals, safeFKs); ok {
			attachChildRow(rel, parentID, row, result)
		}
	}
	return rows.Err()
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

func eagerLoadBelongsTo(ctx context.Context, db DBExecutor, table string, keyCols, targetKey []string, safeEntity string, safeFKs []string, rel entity.Relation, ids []string, result map[string]map[string]any, softDeleteFilter string, scopeFilters, readPreds []filter.ParsedFilter, served servedCols, target *entity.Entity) error {
	sourceToFK, uniqueFKs, err := belongsToFKs(ctx, db, table, keyCols, safeFKs, ids)
	if err != nil || len(uniqueFKs) == 0 {
		return err
	}
	where, fkArgs, ok, err := keyInClause(targetKey, uniqueFKs, 1)
	if err != nil || !ok {
		return err
	}

	scopeClause, scopeArgs := filterClause(scopeFilters, len(fkArgs)+1)
	readClause, readArgs := renderReadScope(readPreds, "", len(fkArgs)+len(scopeArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE %s%s%s%s",
		query.QuoteKeyword(safeEntity), where, softDeleteFilter, scopeClause, readClause)
	fkArgs = append(fkArgs, scopeArgs...)
	fkArgs = append(fkArgs, readArgs...)

	tgtRows, err := db.QueryContext(ctx, tgtQuery, fkArgs...)
	if err != nil {
		return err
	}
	defer tgtRows.Close()

	cols, err := tgtRows.Columns()
	if err != nil {
		return err
	}
	boolCols := databaseBoolColumnsForEntity(tgtRows, len(cols), entityFieldsOf(target), cols)

	targetByID := make(map[string]map[string]any)
	for tgtRows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := tgtRows.Scan(valPtrs...); err != nil {
			return err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			served.set(row, c, convertDatabaseValue(vals[i], boolCols[i]))
		}
		if id, ok := scannedKey(cols, vals, targetKey); ok {
			targetByID[id] = row
		}
	}
	if err := tgtRows.Err(); err != nil {
		return err
	}

	for srcID, fkVal := range sourceToFK {
		if tgt, ok := targetByID[fkVal]; ok {
			if entry, ok := result[srcID]; ok {
				entry[rel.Name] = tgt
			}
		}
	}

	return nil
}

func eagerLoadManyToMany(ctx context.Context, db DBExecutor, safeEntity string, rel entity.Relation, ids []string, targetPK string, result map[string]map[string]any, softDeleteFilter string, scopeFilters, readPreds []filter.ParsedFilter, pivot *entity.Entity, pivotPreds []filter.ParsedFilter, served servedCols, target *entity.Entity) error {
	safeThrough, err := query.SafeIdent(rel.Through)
	if err != nil {
		return fmt.Errorf("invalid through table %q: %w", rel.Through, err)
	}
	safeLocalKey, err := query.SafeIdent(rel.LocalKey)
	if err != nil {
		return fmt.Errorf("invalid local key %q: %w", rel.LocalKey, err)
	}
	safeFKTarget, err := query.SafeIdent(rel.ForeignKeyTarget)
	if err != nil {
		return fmt.Errorf("invalid FK target %q: %w", rel.ForeignKeyTarget, err)
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}

	scopeClause, scopeArgs := filterClauseQualified(scopeFilters, safeEntity, len(ids)+1)
	readClause, readArgs := renderReadScope(readPreds, safeEntity, len(ids)+len(scopeArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	pivotWhere, pivotArgs := pivotClause(pivot, pivotPreds, safeThrough, len(ids)+len(scopeArgs)+len(readArgs)+1)
	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.%s = %s.%s WHERE %s.%s IN (%s)%s%s%s",
		query.QuoteKeyword(safeEntity),
		query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeLocalKey),
		query.QuoteKeyword(safeEntity),
		query.QuoteKeyword(safeThrough),
		query.QuoteKeyword(safeEntity), query.QuoteKeyword(targetPK), query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeFKTarget),
		query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeLocalKey),
		strings.Join(placeholders, ", "),
		scopeClause,
		readClause,
		pivotWhere,
	) + softDeleteFilter
	args = append(args, scopeArgs...)
	args = append(args, readArgs...)
	args = append(args, pivotArgs...)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), entityFieldsOf(target), cols)

	for rows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			return err
		}

		row := make(map[string]any, len(cols)-1)
		var parentID string
		for i, c := range cols {
			if c == "__parent_id" {
				parentID = fmt.Sprintf("%v", vals[i])
			} else {
				served.set(row, c, convertDatabaseValue(vals[i], boolCols[i]))
			}
		}

		if entry, ok := result[parentID]; ok {
			var slice []map[string]any
			if prev, ok := entry[rel.Name]; ok {
				slice = prev.([]map[string]any)
			}
			slice = append(slice, row)
			entry[rel.Name] = slice
		}
	}

	return rows.Err()
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
