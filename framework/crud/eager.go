package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/framework/entity"
)

// EagerLoad fetches related data for a set of parent IDs in batched queries,
// avoiding N+1 problems. Returns a map keyed by parent ID to relation name to related data.
func EagerLoad(ctx context.Context, db DBExecutor, ent *entity.Entity, relations []entity.Relation, ids []string) (map[string]map[string]any, error) {
	if len(ids) == 0 || len(relations) == 0 {
		return make(map[string]map[string]any), nil
	}

	pkCol := "id"

	result := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		result[id] = make(map[string]any)
	}

	tableName := ent.GetTable()

	for _, rel := range relations {
		switch rel.Type {
		case entity.RelHasOne, entity.RelHasMany:
			if err := eagerLoadHasMany(ctx, db, tableName, rel, ids, pkCol, result); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToOne:
			if err := eagerLoadBelongsTo(ctx, db, tableName, rel, ids, result); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		case entity.RelManyToMany:
			if err := eagerLoadManyToMany(ctx, db, tableName, rel, ids, pkCol, result); err != nil {
				return nil, fmt.Errorf("eager load %s: %w", rel.Name, err)
			}
		}
	}

	return result, nil
}

// eagerLoadHasMany handles HasOne and HasMany: target table has a FK pointing back to us.
func eagerLoadHasMany(ctx context.Context, db DBExecutor, table string, rel entity.Relation, ids []string, pkCol string, result map[string]map[string]any) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	q := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)",
		rel.Entity, rel.ForeignKey, strings.Join(placeholders, ", "))

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
		for i, c := range cols {
			row[c] = vals[i]
		}

		parentID := fmt.Sprintf("%v", row[rel.ForeignKey])
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
func eagerLoadBelongsTo(ctx context.Context, db DBExecutor, table string, rel entity.Relation, ids []string, result map[string]map[string]any) error {
	pkCol := "id"

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	srcQuery := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		pkCol, rel.ForeignKey, table, pkCol, strings.Join(placeholders, ", "))

	rows, err := db.QueryContext(ctx, srcQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	sourceToFK := make(map[string]string)
	var fkValues []string
	for rows.Next() {
		var srcID, fkVal string
		if err := rows.Scan(&srcID, &fkVal); err != nil {
			return err
		}
		sourceToFK[srcID] = fkVal
		fkValues = append(fkValues, fkVal)
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

	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)",
		rel.Entity, strings.Join(fkPlaceholders, ", "))

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
		for i, c := range cols {
			row[c] = vals[i]
		}
		targetByID[fmt.Sprintf("%v", row["id"])] = row
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
func eagerLoadManyToMany(ctx context.Context, db DBExecutor, table string, rel entity.Relation, ids []string, pkCol string, result map[string]map[string]any) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s IN (%s)",
		rel.Entity, rel.Through, rel.LocalKey,
		rel.Entity, rel.Through,
		rel.Entity, rel.Through, rel.ForeignKeyTarget,
		rel.Through, rel.LocalKey,
		strings.Join(placeholders, ", "),
	)

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
			} else {
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
