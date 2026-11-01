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
// want the matching child rows themselves attached to each parent, a
// straight WHERE on the inner SELECT is the correct shape.
//
// Result map keys/values mirror EagerLoad: outer key = parent id, inner map =
// relation name → loaded row(s).
func loadIncludeNode(ctx context.Context, db DBExecutor, parentTable string, parentKey []string, node *IncludeNode, ids []string, result map[string]map[string]any, budget *includeBudget) error {
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
	// rows from the eager-load, the direct read paths do this via
	// ApplySoftDeleteFilter, and an include must not be a back door around
	// it. Rendered as a static (non-parameterised) `deleted_at IS NULL`.
	softDeleteFilter := ""
	if node.Target.Config.Scope.SoftDelete {
		softDeleteFilter = " AND deleted_at IS NULL"
	}

	// Validate relation-derived identifiers before dispatching. The SELECT
	// targets the entity's TABLE, not Relation.Entity, that field is the
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
	// The ids are record ids. A has_one or has_many relation matches them
	// against the child's foreign key, one column per key column; a
	// many_to_many joins on the parent's one key column; a belongs_to reads
	// the parent's foreign key by its whole key. parseIncludeTree has
	// checked each shape (entity.CheckRelationKeys).
	if rel.Type == entity.RelManyToMany && len(parentKey) != 1 {
		return fmt.Errorf("eager filtered: relation %q: %w", rel.Name, entity.ErrCompositeKey)
	}
	for _, k := range parentKey {
		if _, err := query.SafeIdent(k); err != nil {
			return fmt.Errorf("eager filtered: invalid parent PK %q: %w", k, err)
		}
	}

	// Validate filter field names up front.
	for _, f := range node.Filters {
		if _, err := query.SafeIdent(f.Field); err != nil {
			return fmt.Errorf("eager filtered: invalid filter field %q: %w", f.Field, err)
		}
	}

	// The columns each attached row may carry. The direct read paths project
	// only visibleFields() (crud.go), and an include must not be a back door
	// past that projection: not to a Hidden field (e.g. password_hash), and
	// not to a column the target does not declare at all.
	served := servedColumns(node.Target)

	// The target's own BeforeList/BeforeGet clauses (node.HookScopes), as
	// one key-IN subquery on its table. It is numbered from $1, so each
	// loader puts it first and numbers its own placeholders after it.
	targetKey := node.Target.KeyColumns()
	if rel.Type == entity.RelManyToMany && len(targetKey) > 1 {
		targetKey = targetKey[:1] // refused below by target.KeyColumn()
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
			// The same split entity.CheckRelationKeys draws: one column
			// against a composite key is the composite-key refusal.
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
			// The ManyToMany SELECT JOINs target + pivot, so a bare
			// `deleted_at` would be ambiguous, qualify it with the target.
			mtmSoftDelete = " AND " + safeEntity + ".deleted_at IS NULL"
		}
		return loadManyToManyFiltered(ctx, db, safeEntity, rel, node, scope, ids, result, mtmSoftDelete, served, budget)
	}
	return nil
}

// servedCols is the allow-list of columns an eager-loaded row may carry. A nil
// set keeps every column: that is EagerLoad called with no registry, where the
// target schema is unknown and the documented contract is the raw row.
//
// It maps each column, folded the way Postgres folds the unquoted identifiers
// the DDL writes, to its declared spelling: a declared "Sku" serves the column
// Postgres reports as "sku", under the key "Sku" a direct read of the target
// uses. Keyed by the reported "sku", the included row missed the Date, JSON
// and Decimal decoding and the wire names, all of which look up declared names.
type servedCols map[string]string

// set stores column c's scanned value v in row under its declared name, or
// leaves it out when the target does not serve c.
func (s servedCols) set(row map[string]any, c string, v any) {
	if s == nil {
		row[c] = v
		return
	}
	if name, ok := s[foldIdent(c)]; ok {
		row[name] = v
	}
}

// foldIdent lowercases ASCII letters only, as Postgres folds an unquoted
// identifier; identifiers are SafeIdent-validated ASCII.
func foldIdent(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// servedColumns returns the columns an included row may carry: the target's
// declared, non-Hidden fields plus its primary key. It is the same allow-list
// visibleFields gives the target's own read routes.
//
// It used to be a deny-list of the declared Hidden fields, which the loaders
// subtracted from SELECT *. A column the resolved target does not declare at
// all was never subtracted: another API version's field on a shared table, a
// field removed from the declaration whose column additive migration kept, an
// unmanaged table's extra columns. Each reached the parent's response while
// every direct read of the target refused it. The loaders capture the FK, PK
// and pivot values they need to attach rows before this filter applies.
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

// hookScope is a target's Before-hook predicate (hookScopeSubquery), numbered
// from $1. A loader renders it first and starts its own placeholders at next.
type hookScope struct {
	sql  string
	args []any
}

// next is the first placeholder index after the scope's own.
func (h hookScope) next() int { return len(h.args) + 1 }

// and prefixes where with the scope predicate.
func (h hookScope) and(where string) string {
	if h.sql == "" {
		return where
	}
	return h.sql + " AND " + where
}

// hookScopeShell is the prefix of the SELECT hookScope.also renders.
const hookScopeShell = "SELECT 1 FROM t WHERE "

// also is h AND o as one scope numbered from $1. Each half is numbered from
// $1 on its own, and the query builder is the one place placeholders are
// renumbered (positionally, after the args before them), so the pair is
// composed as the WHERE of a SELECT the builder renders and cut back out.
// A shell that comes back in another shape matches no row.
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
		safeEntity, scope.and(where), extra, readClause, softDeleteFilter)
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
		safeEntity, scope.and(where), extra, readClause, softDeleteFilter)
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
	// The join entity's own BeforeList clauses narrow the link rows, ANDed
	// into the scope that leads the WHERE.
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
		safeEntity,
		safeThrough, safeLocalKey,
		safeEntity, safeThrough,
		safeEntity, targetPK, safeThrough, safeFKTarget,
		scope.and(""), safeThrough, safeLocalKey,
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

// filterClause builds " AND col OP $N" fragments for each filter, returning
// the SQL suffix + the bound arguments. startIdx is the first $N to use
// (callers know how many placeholders precede this fragment in the outer
// query).
//
// Multiple OpIn filters on the SAME field are coalesced into a single
// `col IN ($a, $b, …)` predicate. parseScopedFilters expands a piped
// `status_in=a|b|c` clause into one OpIn ParsedFilter per value; rendering
// each as `col = $N` ANDed together would require a single row to equal
// every value at once, i.e. match nothing. The IN coalescing restores
// the intended OR semantics.
//
// f.Field values MUST be validated before calling this function.
func filterClause(filters []filter.ParsedFilter, startIdx int) (string, []any) {
	return renderFilterClause(filters, "", startIdx)
}

// filterClauseQualified is like filterClause but prefixes each column with
// the given table name. Used by the ManyToMany loader where the SELECT
// JOINs the target + pivot, bare column names would be ambiguous.
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
	// Identifiers go in unquoted, the way the DDL wrote them, so each is
	// validated here as well as by the caller; one that fails matches no
	// row rather than reaching the SQL.
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
			return table + "." + field
		}
		return field
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
			// Same rule as every other depth: `_like` is a literal
			// substring with the caller's wildcards escaped, matched
			// without regard to case. See filter.LikeClause.
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
