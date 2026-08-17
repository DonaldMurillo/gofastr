package crud

import (
	"context"
	"database/sql"
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
func loadIncludeNode(ctx context.Context, db DBExecutor, parentTable, parentPK string, node *IncludeNode, ids []string, result map[string]map[string]any, budget *includeBudget) error {
	rel := node.Relation

	// Target is resolved by parseIncludeTree for every segment; a nil one
	// here means a caller built an IncludeNode by hand and skipped the
	// registry lookup. Refuse rather than fall back to `SELECT * FROM
	// rel.Entity`, which is the shape that leaked an unregistered auth
	// table's password_hash (every guard below is keyed off Target).
	if node.Target == nil {
		return fmt.Errorf("eager filtered: relation %q has no resolved target entity", rel.Name)
	}

	// When the related entity is soft-deletable, hide logically-removed
	// rows from the eager-load — the direct read paths do this via
	// ApplySoftDeleteFilter, and an include must not be a back door around
	// it. Rendered as a static (non-parameterised) `deleted_at IS NULL`.
	softDeleteFilter := ""
	if node.Target.Config.Scope.SoftDelete {
		softDeleteFilter = " AND deleted_at IS NULL"
	}

	// Validate relation-derived identifiers before dispatching. The SELECT
	// targets the entity's TABLE, not Relation.Entity — that field is the
	// registry key (the entity NAME), and the two differ whenever a host
	// declares Name != Table.
	safeEntity, err := query.SafeIdent(node.Target.GetTable())
	if err != nil {
		return fmt.Errorf("eager filtered: invalid target table %q: %w", node.Target.GetTable(), err)
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

	// Build the set of Hidden columns on the related entity so the loaders
	// can scrub them from each attached row. The direct read paths project
	// only VisibleFields() (crud.go) — an include must not be a back door
	// that leaks a related entity's Hidden fields (e.g. password_hash).
	hidden := hiddenColumns(node.Target)

	switch rel.Type {
	case entity.RelHasOne, entity.RelHasMany:
		safeFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return fmt.Errorf("eager filtered: invalid FK %q: %w", rel.ForeignKey, err)
		}
		return loadHasManyFiltered(ctx, db, safeEntity, safeFK, rel, node.Target, node.Filters, ids, result, softDeleteFilter, hidden, budget)
	case entity.RelManyToOne:
		safeFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return fmt.Errorf("eager filtered: invalid FK %q: %w", rel.ForeignKey, err)
		}
		return loadBelongsToFiltered(ctx, db, safeParentTable, safeParentPK, safeEntity, safeFK, rel, node.Target, node.Filters, ids, result, softDeleteFilter, hidden, budget)
	case entity.RelManyToMany:
		mtmSoftDelete := softDeleteFilter
		if mtmSoftDelete != "" {
			// The ManyToMany SELECT JOINs target + pivot, so a bare
			// `deleted_at` would be ambiguous — qualify it with the target.
			mtmSoftDelete = " AND " + query.QuoteIdent(safeEntity) + ".deleted_at IS NULL"
		}
		return loadManyToManyFiltered(ctx, db, safeEntity, rel, node.Target, node.Filters, ids, result, mtmSoftDelete, hidden, budget)
	}
	return nil
}

// hiddenColumns returns the set of column names flagged Hidden on the
// target entity, used to scrub eager-loaded rows. nil target → empty set.
func hiddenColumns(target *entity.Entity) map[string]bool {
	if target == nil {
		return nil
	}
	var set map[string]bool
	for _, f := range target.GetFields() {
		if f.Hidden {
			if set == nil {
				set = map[string]bool{}
			}
			set[f.Name] = true
		}
	}
	return set
}

func loadHasManyFiltered(ctx context.Context, db DBExecutor, safeEntity, safeFK string, rel entity.Relation, target *entity.Entity, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool, budget *includeBudget) error {
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
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), target, cols)
	for rows.Next() {
		if err := budget.spend(1); err != nil {
			return err
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
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
			row[c] = convertDatabaseValue(vals[i], boolCols[i])
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

func loadBelongsToFiltered(ctx context.Context, db DBExecutor, safeParentTable, safeParentPK, safeEntity, safeFK string, rel entity.Relation, target *entity.Entity, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool, budget *includeBudget) error {
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
		var srcID string
		var fk sql.NullString
		if err := rows.Scan(&srcID, &fk); err != nil {
			return err
		}
		// A nullable FK that is NULL means the optional relation is
		// absent for this parent; skip it so the parent keeps the
		// relation unset instead of erroring on NULL→string conversion.
		if !fk.Valid {
			continue
		}
		sourceToFK[srcID] = fk.String
		fks = append(fks, fk.String)
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
	boolCols := databaseBoolColumnsForEntity(tgtRows, len(cols), target, cols)
	targetByID := map[string]map[string]any{}
	for tgtRows.Next() {
		if err := budget.spend(1); err != nil {
			return err
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := tgtRows.Scan(ptrs...); err != nil {
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
			row[c] = convertDatabaseValue(vals[i], boolCols[i])
		}
		targetByID[fmt.Sprintf("%v", idVal)] = row
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

func loadManyToManyFiltered(ctx context.Context, db DBExecutor, safeEntity string, rel entity.Relation, target *entity.Entity, filters []filter.ParsedFilter, ids []string, result map[string]map[string]any, softDeleteFilter string, hidden map[string]bool, budget *includeBudget) error {
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
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), target, cols)
	for rows.Next() {
		if err := budget.spend(1); err != nil {
			return err
		}
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
			} else if !hidden[c] {
				row[c] = convertDatabaseValue(vals[i], boolCols[i])
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
// Multiple OpIn filters on the SAME field are coalesced into a single
// `col IN ($a, $b, …)` predicate. parseScopedFilters expands a piped
// `status_in=a|b|c` clause into one OpIn ParsedFilter per value; rendering
// each as `col = $N` ANDed together would require a single row to equal
// every value at once — i.e. match nothing. The IN coalescing restores
// the intended OR semantics.
//
// f.Field values MUST be validated before calling this function.
func filterClause(filters []filter.ParsedFilter, startIdx int) (string, []any) {
	return renderFilterClause(filters, "", startIdx)
}

// filterClauseQualified is like filterClause but prefixes each column with
// the given table name. Used by the ManyToMany loader where the SELECT
// JOINs the target + pivot — bare column names would be ambiguous.
//
// Both `table` and f.Field values MUST be validated before calling.
func filterClauseQualified(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	return renderFilterClause(filters, table, startIdx)
}

// renderFilterClause is the shared body of filterClause/filterClauseQualified.
// table is the optional column qualifier ("" for none). It preserves filter
// order while merging adjacent OpIn predicates on the same field into one
// IN (...) list so multi-value scoped filters keep OR semantics.
func renderFilterClause(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	col := func(field string) string {
		if table != "" {
			return query.QuoteIdent(table) + "." + query.QuoteIdent(field)
		}
		return query.QuoteIdent(field)
	}
	var parts []string
	var args []any
	idx := startIdx
	for i := 0; i < len(filters); i++ {
		f := filters[i]
		if f.Op == filter.OpIn {
			// Greedily absorb the run of same-field OpIn filters.
			phs := []string{fmt.Sprintf("$%d", idx)}
			args = append(args, f.BindValue())
			idx++
			for i+1 < len(filters) && filters[i+1].Op == filter.OpIn && filters[i+1].Field == f.Field {
				i++
				phs = append(phs, fmt.Sprintf("$%d", idx))
				args = append(args, filters[i].BindValue())
				idx++
			}
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col(f.Field), strings.Join(phs, ", ")))
			continue
		}
		if f.Op == filter.OpLike {
			// Same rule as every other depth: `_like` is a literal
			// substring with the caller's wildcards escaped. See
			// nested_filter.go's LIKE branch.
			parts = append(parts, fmt.Sprintf("%s LIKE $%d"+filter.LikeEscapeSuffix, col(f.Field), idx))
			args = append(args, filter.EscapeLikePattern(fmt.Sprintf("%v", f.Value)))
			idx++
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s $%d", col(f.Field), opToSQL(f.Op), idx))
		args = append(args, f.BindValue())
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}
