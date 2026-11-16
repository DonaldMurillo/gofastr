package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// loadIncludeNode loads one include node's rows for the parent ids into
// result, shaped as EagerLoad's.
func loadIncludeNode(ctx context.Context, db DBExecutor, parentTable string, parentKey []string, node *IncludeNode, ids []string, result map[string]map[string]any, budget *includeBudget) error {
	rel := node.Relation

	if node.Target == nil {
		return fmt.Errorf("eager filtered: relation %q has no resolved target entity", rel.Name)
	}

	softDeleteFilter := ""
	if node.Target.Config.Scope.SoftDelete {
		softDeleteFilter = " AND deleted_at IS NULL"
	}

	safeEntity, err := query.SafeIdent(node.Target.GetTable())
	if err != nil {
		return fmt.Errorf("eager filtered: invalid target table %q: %w", node.Target.GetTable(), err)
	}
	safeParentTable, err := query.SafeIdent(parentTable)
	if err != nil {
		return fmt.Errorf("eager filtered: invalid parent table %q: %w", parentTable, err)
	}
	if rel.Type == entity.RelManyToMany && len(parentKey) != 1 {
		return fmt.Errorf("eager filtered: relation %q: %w", rel.Name, entity.ErrCompositeKey)
	}
	for _, k := range parentKey {
		if _, err := query.SafeIdent(k); err != nil {
			return fmt.Errorf("eager filtered: invalid parent PK %q: %w", k, err)
		}
	}

	for _, f := range node.Filters {
		if _, err := query.SafeIdent(f.Field); err != nil {
			return fmt.Errorf("eager filtered: invalid filter field %q: %w", f.Field, err)
		}
	}

	served := servedColumns(node.Target)

	targetKey := node.Target.KeyColumns()
	if rel.Type == entity.RelManyToMany && len(targetKey) > 1 {
		targetKey = targetKey[:1]
	}
	hookPred, hookArgs := hookScopeSubquery(safeEntity, safeEntity, targetKey, node.HookScopes)
	scope := hookScope{sql: hookPred, args: hookArgs}

	switch rel.Type {
	case entity.RelHasOne, entity.RelHasMany:
		safeFKs, err := relationFKs(rel)
		if err != nil {
			return fmt.Errorf("eager filtered: %w", err)
		}
		if len(safeFKs) != len(parentKey) {
			shape := entity.ErrForeignKeyShape
			if len(safeFKs) == 1 {
				shape = entity.ErrCompositeKey
			}
			return fmt.Errorf("eager filtered: relation %q: %w: its parent is keyed on (%s)", rel.Name, shape, strings.Join(parentKey, ", "))
		}
		return loadHasManyFiltered(ctx, db, safeEntity, safeFKs, rel, node.Target, node.Filters, node.ReadScopes, scope, ids, result, softDeleteFilter, served, budget)
	case entity.RelManyToOne:
		safeFKs, err := relationFKs(rel)
		if err != nil {
			return fmt.Errorf("eager filtered: %w", err)
		}
		return loadBelongsToFiltered(ctx, db, safeParentTable, parentKey, safeEntity, safeFKs, rel, node.Target, node.Filters, node.ReadScopes, scope, ids, result, softDeleteFilter, served, budget)
	case entity.RelManyToMany:
		mtmSoftDelete := softDeleteFilter
		if mtmSoftDelete != "" {
			mtmSoftDelete = " AND " + query.QuoteKeyword(safeEntity) + ".deleted_at IS NULL"
		}
		return loadManyToManyFiltered(ctx, db, safeEntity, rel, node, scope, ids, result, mtmSoftDelete, served, budget)
	}
	return nil
}

// servedCols maps each served column, case-folded, to its declared name; nil
// serves every column.
type servedCols map[string]string

func (s servedCols) set(row map[string]any, c string, v any) {
	if s == nil {
		row[c] = v
		return
	}
	if name, ok := s[foldIdent(c)]; ok {
		row[name] = v
	}
}

// foldIdent lowercases ASCII letters, as Postgres folds an unquoted name.
func foldIdent(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func servedColumns(target *entity.Entity) servedCols {
	if target == nil {
		return nil
	}
	set := servedCols{}
	for _, k := range target.KeyColumns() {
		set[foldIdent(k)] = k
	}
	for _, f := range target.GetFields() {
		if !f.Hidden {
			set[foldIdent(f.Name)] = f.Name
		}
	}
	return set
}

// hookScope is a Before-hook predicate numbered from $1; a loader renders it
// first and numbers its own placeholders from next.
type hookScope struct {
	sql  string
	args []any
}

func (h hookScope) next() int { return len(h.args) + 1 }

func (h hookScope) and(where string) string {
	if h.sql == "" {
		return where
	}
	return h.sql + " AND " + where
}

const hookScopeShell = "SELECT 1 FROM t WHERE "

// also is h AND o as one scope numbered from $1, renumbered by the query
// builder.
func (h hookScope) also(o hookScope) hookScope {
	if o.sql == "" {
		return h
	}
	if h.sql == "" {
		return o
	}
	sql, args := query.Select("1").From("t").Where(h.sql, h.args...).Where(o.sql, o.args...).Build()
	where, ok := strings.CutPrefix(sql, hookScopeShell)
	if !ok {
		return hookScope{sql: "1 = 0"}
	}
	return hookScope{sql: where, args: args}
}

func loadHasManyFiltered(ctx context.Context, db DBExecutor, safeEntity string, safeFKs []string, rel entity.Relation, target *entity.Entity, filters, readScopes []filter.ParsedFilter, scope hookScope, ids []string, result map[string]map[string]any, softDeleteFilter string, served servedCols, budget *includeBudget) error {
	where, keyArgs, ok, err := keyInClause(safeFKs, ids, scope.next())
	if err != nil || !ok {
		return err
	}
	args := append(append([]any(nil), scope.args...), keyArgs...)
	extra, extraArgs := filterClause(filters, len(args)+1)
	readClause, readArgs := renderReadScope(readScopes, "", len(args)+len(extraArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	q := fmt.Sprintf("SELECT * FROM %s WHERE %s%s%s%s",
		query.QuoteKeyword(safeEntity), scope.and(where), extra, readClause, softDeleteFilter)
	args = append(args, extraArgs...)
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
		for i, c := range cols {
			served.set(row, c, convertDatabaseValue(vals[i], boolCols[i]))
		}
		if parentID, ok := scannedKey(cols, vals, safeFKs); ok {
			attachChildRow(rel, parentID, row, result)
		}
	}
	return rows.Err()
}

func loadBelongsToFiltered(ctx context.Context, db DBExecutor, parentTable string, parentKey []string, safeEntity string, safeFKs []string, rel entity.Relation, target *entity.Entity, filters, readScopes []filter.ParsedFilter, scope hookScope, ids []string, result map[string]map[string]any, softDeleteFilter string, served servedCols, budget *includeBudget) error {
	targetKey, err := safeColumns(target.KeyColumns())
	if err != nil {
		return fmt.Errorf("eager filtered: invalid target key: %w", err)
	}
	if len(targetKey) != len(safeFKs) {
		return fmt.Errorf("eager filtered: relation %q: %w: %s is keyed on (%s)", rel.Name, entity.ErrForeignKeyShape, target.GetName(), strings.Join(targetKey, ", "))
	}
	sourceToFK, unique, err := belongsToFKs(ctx, db, parentTable, parentKey, safeFKs, ids)
	if err != nil || len(unique) == 0 {
		return err
	}
	where, keyArgs, ok, err := keyInClause(targetKey, unique, scope.next())
	if err != nil || !ok {
		return err
	}
	fkArgs := append(append([]any(nil), scope.args...), keyArgs...)
	extra, extraArgs := filterClause(filters, len(fkArgs)+1)
	readClause, readArgs := renderReadScope(readScopes, "", len(fkArgs)+len(extraArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	tgtQuery := fmt.Sprintf("SELECT * FROM %s WHERE %s%s%s%s",
		query.QuoteKeyword(safeEntity), scope.and(where), extra, readClause, softDeleteFilter)
	fkArgs = append(fkArgs, extraArgs...)
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

	for srcID, fk := range sourceToFK {
		if tgt, ok := targetByID[fk]; ok {
			if entry, ok := result[srcID]; ok {
				entry[rel.Name] = tgt
			}
		}
	}
	return nil
}

func loadManyToManyFiltered(ctx context.Context, db DBExecutor, safeEntity string, rel entity.Relation, node *IncludeNode, scope hookScope, ids []string, result map[string]map[string]any, softDeleteFilter string, served servedCols, budget *includeBudget) error {
	target, filters, readScopes := node.Target, node.Filters, node.ReadScopes
	targetPK, err := target.KeyColumn()
	if err != nil {
		return err
	}
	if targetPK, err = query.SafeIdent(targetPK); err != nil {
		return fmt.Errorf("eager filtered: invalid target key %q: %w", targetPK, err)
	}
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
	pivotHook, pivotHookArgs := pivotHookScope(node.Pivot, safeThrough, node.PivotHookScopes)
	scope = scope.also(hookScope{sql: pivotHook, args: pivotHookArgs})

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(scope.args)+len(ids))
	args = append(args, scope.args...)
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, id)
	}

	extra, extraArgs := filterClauseQualified(filters, safeEntity, len(args)+1)
	readClause, readArgs := renderReadScope(readScopes, safeEntity, len(args)+len(extraArgs)+1)
	if readClause != "" {
		readClause = " AND " + readClause
	}
	pivotWhere, pivotArgs := pivotClause(node.Pivot, node.PivotScopes, safeThrough, len(args)+len(extraArgs)+len(readArgs)+1)
	q := fmt.Sprintf(
		"SELECT %s.*, %s.%s AS __parent_id FROM %s JOIN %s ON %s.%s = %s.%s WHERE %s%s.%s IN (%s)%s%s%s%s",
		query.QuoteKeyword(safeEntity),
		query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeLocalKey),
		query.QuoteKeyword(safeEntity), query.QuoteKeyword(safeThrough),
		query.QuoteKeyword(safeEntity), query.QuoteKeyword(targetPK), query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeFKTarget),
		scope.and(""), query.QuoteKeyword(safeThrough), query.QuoteKeyword(safeLocalKey),
		strings.Join(placeholders, ", "),
		extra,
		readClause,
		pivotWhere,
		softDeleteFilter,
	)
	args = append(args, extraArgs...)
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

// filterClause renders filters as " AND col OP $N ...", numbered from
// startIdx, with a run of same-list IN values as one IN list.
func filterClause(filters []filter.ParsedFilter, startIdx int) (string, []any) {
	return renderFilterClause(filters, "", startIdx)
}

// filterClauseQualified is filterClause with each column qualified by table.
func filterClauseQualified(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	return renderFilterClause(filters, table, startIdx)
}

func renderFilterClause(filters []filter.ParsedFilter, table string, startIdx int) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	if table != "" && !isSafeTable(table) {
		return " AND 1 = 0", nil
	}
	for _, f := range filters {
		if !isSafeIdentifier(f.Field) {
			return " AND 1 = 0", nil
		}
	}
	col := func(field string) string {
		if table != "" {
			return query.QuoteKeyword(table + "." + field)
		}
		return query.QuoteKeyword(field)
	}
	var parts []string
	var args []any
	idx := startIdx
	for i := 0; i < len(filters); i++ {
		f := filters[i]
		if f.Op == filter.OpIn {
			phs := []string{fmt.Sprintf("$%d", idx)}
			args = append(args, f.BindValue())
			idx++
			for i+1 < len(filters) && f.SameINList(filters[i+1]) {
				i++
				phs = append(phs, fmt.Sprintf("$%d", idx))
				args = append(args, filters[i].BindValue())
				idx++
			}
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col(f.Field), strings.Join(phs, ", ")))
			continue
		}
		if f.Op == filter.OpLike {
			clause, arg := filter.LikeClause(col(f.Field), idx, f.Value)
			parts = append(parts, clause)
			args = append(args, arg)
			idx++
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s $%d", col(f.Field), opToSQL(f.Op), idx))
		args = append(args, f.BindValue())
		idx++
	}
	return " AND " + strings.Join(parts, " AND "), args
}
