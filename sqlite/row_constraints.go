package sqlite

import "strings"

func validateUpdatedRow(table *TableInfo, previous, updated []Value) error {
	for i, column := range table.Columns {
		if column.NotNull && updated[i].IsNull() {
			return &engineError{"NOT NULL constraint failed: " + table.Name + "." + column.Name}
		}
	}
	if table.HasRowIDAlias() {
		index := table.PrimaryKey
		if CompareValues(previous[index], updated[index]) != CompareEqual {
			return &engineError{"PRIMARY KEY update is not supported"}
		}
	}
	return nil
}

// uniqueConstraintInfo describes a single UNIQUE constraint that applies
// to a table: the column set it covers and, for a partial unique index,
// the predicate a row must match for the constraint to be enforced.
type uniqueConstraintInfo struct {
	Columns   []string
	Predicate Expr // nil for an unconditional constraint
}

func (e *Engine) uniqueConstraints(table *TableInfo) []uniqueConstraintInfo {
	constraints := make([]uniqueConstraintInfo, 0, len(table.UniqueConstraints))
	for i := range table.UniqueConstraints {
		constraints = append(constraints, uniqueConstraintInfo{
			Columns: append([]string(nil), table.UniqueConstraints[i]...),
		})
	}
	for _, index := range e.schema.IndexesForTable(table.Name) {
		if !index.Unique {
			continue
		}
		// A partial unique index (index.WhereExpr != nil) is a DISTINCT
		// constraint from any unconditional UNIQUE on the same columns:
		// it only fires on rows the predicate selects, so it does not
		// deduplicate against the column-only constraints.
		duplicate := false
		if index.WhereExpr == nil {
			for _, constraint := range constraints {
				if constraint.Predicate == nil && sameColumns(constraint.Columns, index.Columns) {
					duplicate = true
					break
				}
			}
		}
		if !duplicate {
			constraints = append(constraints, uniqueConstraintInfo{
				Columns:   append([]string(nil), index.Columns...),
				Predicate: index.WhereExpr,
			})
		}
	}
	return constraints
}

// rowMatchesPredicate reports whether a row satisfies a partial-index
// or partial-constraint predicate. A nil predicate always matches.
// The row is the values slice indexed by table column position (the
// same shape buildInsertRow / recordToValues produce).
func rowMatchesPredicate(table *TableInfo, row []Value, predicate Expr) bool {
	if predicate == nil {
		return true
	}
	columnMap := make(map[string]int, len(table.Columns)+1)
	columnMap["rowid"] = 0
	for i, col := range table.Columns {
		columnMap[strings.ToLower(col.Name)] = i + 1
	}
	indexed := make([]Value, len(table.Columns)+1)
	copy(indexed[1:], row)
	eval := &ExprEval{
		Row:       indexed,
		ColumnMap: columnMap,
		TableMap: map[string]map[string]int{
			strings.ToLower(table.Name): columnMap,
		},
		Engine: nil,
	}
	val, err := eval.Eval(predicate)
	if err != nil || val.IsNull() {
		return false
	}
	b, ok := val.AsInt64()
	return ok && b != 0
}
