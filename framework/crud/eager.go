package crud

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EagerLoad fetches related data for a set of parent IDs in batched queries,
// avoiding N+1 problems. Returns a map keyed by parent ID to relation name to related data.
//
// SECURITY: when an optional entity.Registry is supplied, each relation's
// target entity is resolved and the same scrubbing the live include path
// applies (eager_filtered.go) is applied here too — soft-deleted target
// rows are excluded (`deleted_at IS NULL`) and Hidden columns (e.g.
// password_hash) are never populated. Without a registry the target
// schema is unknown, so no per-target scrub can be applied; callers that
// load relations whose targets are soft-deletable or carry Hidden fields
// MUST pass the registry.
func EagerLoad(ctx context.Context, db DBExecutor, ent *entity.Entity, relations []entity.Relation, ids []string, reg ...entity.Registry) (map[string]map[string]any, error) {
	if len(ids) == 0 || len(relations) == 0 {
		return make(map[string]map[string]any), nil
	}

	var registry entity.Registry
	if len(reg) > 0 {
		registry = reg[0]
	}

	pkCol := "id"

	result := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		result[id] = make(map[string]any)
	}

	tableName := ent.GetTable()
	// Validate the parent table name once upfront.
	if _, err := query.SafeIdent(tableName); err != nil {
		return nil, fmt.Errorf("eager load: invalid parent table %q: %w", tableName, err)
	}

	for _, rel := range relations {
		// Validate all relation-derived identifiers before building SQL.
		safeRelEntity, err := query.SafeIdent(rel.Entity)
		if err != nil {
			return nil, fmt.Errorf("eager load %s: invalid relation entity %q: %w", rel.Name, rel.Entity, err)
		}
		safeFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return nil, fmt.Errorf("eager load %s: invalid FK %q: %w", rel.Name, rel.ForeignKey, err)
		}

		// Resolve the relation's target entity (when a registry is given) so
		// we can scrub soft-deleted rows + Hidden columns, exactly like the
		// live include path. nil target → no scrub (unknown schema).
		var target *entity.Entity
		if registry != nil {
			if t, err := registry.Get(rel.Entity); err == nil {
				target = t
			}
		}
		softDeleteFilter := ""
		if target != nil && target.Config.SoftDelete {
			softDeleteFilter = " AND deleted_at IS NULL"
		}
		hidden := hiddenColumns(target)

		switch rel.Type {
		case entity.RelHasOne, entity.RelHasMany:
			if err := eagerLoadHasMany(ctx, db, safeRelEntity, safeFK, rel, ids, pkCol, result, softDeleteFilter, hidden); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToOne:
			if err := eagerLoadBelongsTo(ctx, db, tableName, safeRelEntity, safeFK, rel, ids, result, softDeleteFilter, hidden); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToMany:
			mtmSoftDelete := softDeleteFilter
			if mtmSoftDelete != "" {
				// The ManyToMany SELECT JOINs target + pivot, so a bare
				// `deleted_at` would be ambiguous — qualify it with the target.
				mtmSoftDelete = " AND " + query.QuoteIdent(safeRelEntity) + ".deleted_at IS NULL"
			}
			if err := eagerLoadManyToMany(ctx, db, safeRelEntity, safeFK, rel, ids, pkCol, result, mtmSoftDelete, hidden); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		}
	}

	return result, nil
}

// eagerLoadHasMany handles HasOne and HasMany: target table has a FK pointing back to us.
func eagerLoadHasMany(ctx context.Context, db DBExecutor, safeEntity, safeFK string, rel entity.Relation, ids []string, pkCol string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	q := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)%s",
		query.QuoteIdent(safeEntity), query.QuoteIdent(safeFK), strings.Join(placeholders, ", "), softDeleteFilter)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

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
		var fkVal any
		for i, c := range cols {
			if c == safeFK {
				fkVal = vals[i]
			}
			if hidden[c] {
				continue
			}
			row[c] = vals[i]
		}

		parentID := fmt.Sprintf("%v", fkVal)
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

	return rows.Err()
}

// eagerLoadBelongsTo handles BelongsTo (ManyToOne): we hold a FK pointing to the target.
func eagerLoadBelongsTo(ctx context.Context, db DBExecutor, table, safeEntity, safeFK string, rel entity.Relation, ids []string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool) error {
	pkCol := "id"

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	srcQuery := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		query.QuoteIdent(pkCol), query.QuoteIdent(safeFK),
		query.QuoteIdent(table), query.QuoteIdent(pkCol),
		strings.Join(placeholders, ", "))

	rows, err := db.QueryContext(ctx, srcQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	sourceToFK := make(map[string]string)
	var fkValues []string
	for rows.Next() {
		var srcID string
		var fkVal sql.NullString
		if err := rows.Scan(&srcID, &fkVal); err != nil {
			return err
		}
		// A nullable FK that is NULL means the optional relation is
		// absent for this parent; skip it so the parent keeps the
		// relation unset instead of erroring on NULL→string conversion.
		if !fkVal.Valid {
			continue
		}
		sourceToFK[srcID] = fkVal.String
		fkValues = append(fkValues, fkVal.String)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(fkValues) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(fkValues))
	uniqueFKs := fkValues[:0]
	for _, fk := range fkValues {
		if !seen[fk] {
			seen[fk] = true
			uniqueFKs = append(uniqueFKs, fk)
		}
	}

	fkPlaceholders := make([]string, len(uniqueFKs))
	fkArgs := make([]any, len(uniqueFKs))
	for i, fk := range uniqueFKs {
		fkPlaceholders[i] = fmt.Sprintf("$%d", i+1)
		fkArgs[i] = fk
	}

	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)%s",
		query.QuoteIdent(safeEntity), strings.Join(fkPlaceholders, ", "), softDeleteFilter)

	tgtRows, err := db.QueryContext(ctx, tgtQuery, fkArgs...)
	if err != nil {
		return err
	}
	defer tgtRows.Close()

	cols, err := tgtRows.Columns()
	if err != nil {
		return err
	}

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
		var idVal any
		for i, c := range cols {
			if c == "id" {
				idVal = vals[i]
			}
			if hidden[c] {
				continue
			}
			row[c] = vals[i]
		}
		targetByID[fmt.Sprintf("%v", idVal)] = row
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

// eagerLoadManyToMany handles ManyToMany through a pivot table.
func eagerLoadManyToMany(ctx context.Context, db DBExecutor, safeEntity, safeFK string, rel entity.Relation, ids []string, pkCol string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool) error {
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
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s IN (%s)",
		query.QuoteIdent(safeEntity),
		query.QuoteIdent(safeThrough), query.QuoteIdent(safeLocalKey),
		query.QuoteIdent(safeEntity),
		query.QuoteIdent(safeThrough),
		query.QuoteIdent(safeEntity), query.QuoteIdent(safeThrough), query.QuoteIdent(safeFKTarget),
		query.QuoteIdent(safeThrough), query.QuoteIdent(safeLocalKey),
		strings.Join(placeholders, ", "),
	) + softDeleteFilter

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

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
			} else if !hidden[c] {
				row[c] = vals[i]
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
