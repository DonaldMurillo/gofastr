package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/filter"
)

// loadIncludeNode is the filter-aware sibling of EagerLoad. It runs a single
// relation's batched fetch for the given parent IDs, applying any scoped
// filters declared on the IncludeNode (e.g. include=comments(status=draft)).
//
// Filters are appended as additional WHERE predicates after the IN-clause
// that ties children to their parents. EXISTS isn't used here because we
// want the matching child rows themselves attached to each parent — a
// straight WHERE on the inner SELECT is the correct shape.
//
// Result map keys/values mirror EagerLoad: outer key = parent id, inner map =
// relation name → loaded row(s).
func loadIncludeNode(ctx context.Context, db DBExecutor, parentTable, parentPK string, node *IncludeNode, ids []string, result map[string]map[string]any) error {
	rel := node.Relation

	switch rel.Type {
	case entity.RelHasOne, entity.RelHasMany:
		return loadHasManyFiltered(ctx, db, rel, node.Filters, ids, result)
	case entity.RelManyToOne:
		return loadBelongsToFiltered(ctx, db, parentTable, parentPK, rel, node.Filters, ids, result)
	case entity.RelManyToMany:
		return loadManyToManyFiltered(ctx, db, rel, node.Filters, ids, result)
	}
	return nil
}

func loadHasManyFiltered(ctx context.Context, db DBExecutor, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	extra, extraArgs := filterClause(filters, len(ids)+1)
	q := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)%s",
		rel.Entity, rel.ForeignKey, strings.Join(placeholders, ", "), extra)
	args = append(args, extraArgs...)

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
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
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

func loadBelongsToFiltered(ctx context.Context, db DBExecutor, parentTable, parentPK string, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	srcQuery := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		parentPK, rel.ForeignKey, parentTable, parentPK, strings.Join(placeholders, ", "))

	rows, err := db.QueryContext(ctx, srcQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	sourceToFK := map[string]string{}
	var fks []string
	for rows.Next() {
		var srcID, fk string
		if err := rows.Scan(&srcID, &fk); err != nil {
			return err
		}
		sourceToFK[srcID] = fk
		fks = append(fks, fk)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(fks) == 0 {
		return nil
	}

	seen := map[string]bool{}
	unique := fks[:0]
	for _, fk := range fks {
		if !seen[fk] {
			seen[fk] = true
			unique = append(unique, fk)
		}
	}

	fkPlaceholders := make([]string, len(unique))
	fkArgs := make([]any, len(unique))
	for i, fk := range unique {
		fkPlaceholders[i] = fmt.Sprintf("$%d", i+1)
		fkArgs[i] = fk
	}
	extra, extraArgs := filterClause(filters, len(unique)+1)
	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)%s",
		rel.Entity, strings.Join(fkPlaceholders, ", "), extra)
	fkArgs = append(fkArgs, extraArgs...)

	tgtRows, err := db.QueryContext(ctx, tgtQuery, fkArgs...)
	if err != nil {
		return err
	}
	defer tgtRows.Close()

	cols, err := tgtRows.Columns()
	if err != nil {
		return err
	}
	targetByID := map[string]map[string]any{}
	for tgtRows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := tgtRows.Scan(ptrs...); err != nil {
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

	for srcID, fk := range sourceToFK {
		if tgt, ok := targetByID[fk]; ok {
			if entry, ok := result[srcID]; ok {
				entry[rel.Name] = tgt
			}
		}
	}
	return nil
}

func loadManyToManyFiltered(ctx context.Context, db DBExecutor, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	extra, extraArgs := filterClauseQualified(filters, rel.Entity, len(ids)+1)
	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s IN (%s)%s",
		rel.Entity, rel.Through, rel.LocalKey,
		rel.Entity, rel.Through,
		rel.Entity, rel.Through, rel.ForeignKeyTarget,
		rel.Through, rel.LocalKey,
		strings.Join(placeholders, ", "),
		extra,
	)
	args = append(args, extraArgs...)

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
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
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

// filterClause builds " AND col OP $N" fragments for each filter, returning
// the SQL suffix + the bound arguments. startIdx is the first $N to use
// (callers know how many placeholders precede this fragment in the outer
// query).
func filterClause(filters []filter.ParsedFilter, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	var parts []string
	var args []any
	idx := startIdx
	for _, f := range filters {
		parts = append(parts, fmt.Sprintf("%s %s $%d", f.Field, opToSQL(f.Op), idx))
		args = append(args, f.Value)
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}

// filterClauseQualified is like filterClause but prefixes each column with
// the given table name. Used by the ManyToMany loader where the SELECT
// JOINs the target + pivot — bare column names would be ambiguous.
func filterClauseQualified(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	var parts []string
	var args []any
	idx := startIdx
	for _, f := range filters {
		parts = append(parts, fmt.Sprintf("%s.%s %s $%d", table, f.Field, opToSQL(f.Op), idx))
		args = append(args, f.Value)
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}
