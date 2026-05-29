package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
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

	// When the related entity is soft-deletable, hide logically-removed
	// rows from the eager-load — the direct read paths do this via
	// ApplySoftDeleteFilter, and an include must not be a back door around
	// it. Rendered as a static (non-parameterised) `deleted_at IS NULL`.
	softDeleteFilter := ""
	if node.Target != nil && node.Target.Config.SoftDelete {
		softDeleteFilter = " AND deleted_at IS NULL"
	}

	// Validate relation-derived identifiers before dispatching.
	safeEntity, err := query.SafeIdent(rel.Entity)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid relation entity %q: %w", rel.Entity, err)
	}
	safeParentTable, err := query.SafeIdent(parentTable)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid parent table %q: %w", parentTable, err)
	}
	safeParentPK, err := query.SafeIdent(parentPK)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid parent PK %q: %w", parentPK, err)
	}

	// Validate filter field names up front.
	for _, f := range node.Filters {
		if _, err := query.SafeIdent(f.Field); err != nil {
			return fmt.Errorf("eager filtered: invalid filter field %q: %w", f.Field, err)
		}
	}

	switch rel.Type {
	case entity.RelHasOne, entity.RelHasMany:
		safeFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return fmt.Errorf("eager filtered: invalid FK %q: %w", rel.ForeignKey, err)
		}
		return loadHasManyFiltered(ctx, db, safeEntity, safeFK, rel, node.Filters, ids, result, softDeleteFilter)
	case entity.RelManyToOne:
		safeFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return fmt.Errorf("eager filtered: invalid FK %q: %w", rel.ForeignKey, err)
		}
		return loadBelongsToFiltered(ctx, db, safeParentTable, safeParentPK, safeEntity, safeFK, rel, node.Filters, ids, result, softDeleteFilter)
	case entity.RelManyToMany:
		mtmSoftDelete := softDeleteFilter
		if mtmSoftDelete != "" {
			// The ManyToMany SELECT JOINs target + pivot, so a bare
			// `deleted_at` would be ambiguous — qualify it with the target.
			mtmSoftDelete = " AND " + query.QuoteIdent(safeEntity) + ".deleted_at IS NULL"
		}
		return loadManyToManyFiltered(ctx, db, safeEntity, rel, node.Filters, ids, result, mtmSoftDelete)
	}
	return nil
}

func loadHasManyFiltered(ctx context.Context, db DBExecutor, safeEntity, safeFK string, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	extra, extraArgs := filterClause(filters, len(ids)+1)
	q := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)%s%s",
		query.QuoteIdent(safeEntity), query.QuoteIdent(safeFK), strings.Join(placeholders, ", "), extra, softDeleteFilter)
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
		parentID := fmt.Sprintf("%v", row[safeFK])
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

func loadBelongsToFiltered(ctx context.Context, db DBExecutor, safeParentTable, safeParentPK, safeEntity, safeFK string, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	srcQuery := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		query.QuoteIdent(safeParentPK), query.QuoteIdent(safeFK),
		query.QuoteIdent(safeParentTable), query.QuoteIdent(safeParentPK),
		strings.Join(placeholders, ", "))

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
	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)%s%s",
		query.QuoteIdent(safeEntity), strings.Join(fkPlaceholders, ", "), extra, softDeleteFilter)
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

func loadManyToManyFiltered(ctx context.Context, db DBExecutor, safeEntity string, rel entity.Relation, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string) error {
	safeThrough, err := query.SafeIdent(rel.Through)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid through table %q: %w", rel.Through, err)
	}
	safeLocalKey, err := query.SafeIdent(rel.LocalKey)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid local key %q: %w", rel.LocalKey, err)
	}
	safeFKTarget, err := query.SafeIdent(rel.ForeignKeyTarget)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid FK target %q: %w", rel.ForeignKeyTarget, err)
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	extra, extraArgs := filterClauseQualified(filters, safeEntity, len(ids)+1)
	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s IN (%s)%s%s",
		query.QuoteIdent(safeEntity),
		query.QuoteIdent(safeThrough), query.QuoteIdent(safeLocalKey),
		query.QuoteIdent(safeEntity), query.QuoteIdent(safeThrough),
		query.QuoteIdent(safeEntity), query.QuoteIdent(safeThrough), query.QuoteIdent(safeFKTarget),
		query.QuoteIdent(safeThrough), query.QuoteIdent(safeLocalKey),
		strings.Join(placeholders, ", "),
		extra,
		softDeleteFilter,
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
//
// f.Field values MUST be validated before calling this function.
func filterClause(filters []filter.ParsedFilter, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	var parts []string
	var args []any
	idx := startIdx
	for _, f := range filters {
		parts = append(parts, fmt.Sprintf("%s %s $%d", query.QuoteIdent(f.Field), opToSQL(f.Op), idx))
		args = append(args, f.Value)
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}

// filterClauseQualified is like filterClause but prefixes each column with
// the given table name. Used by the ManyToMany loader where the SELECT
// JOINs the target + pivot — bare column names would be ambiguous.
//
// Both `table` and f.Field values MUST be validated before calling.
func filterClauseQualified(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	var parts []string
	var args []any
	idx := startIdx
	for _, f := range filters {
		parts = append(parts, fmt.Sprintf("%s.%s %s $%d", query.QuoteIdent(table), query.QuoteIdent(f.Field), opToSQL(f.Op), idx))
		args = append(args, f.Value)
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}
