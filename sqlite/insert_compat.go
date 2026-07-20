package sqlite

import (
	"fmt"
	"strings"
)

func (e *Engine) executeInsertWithConflict(s *InsertStmt, params []Value, tableInfo *TableInfo) (*Result, error) {
	valueRows, err := e.insertValueRows(s, params)
	if err != nil {
		return nil, err
	}
	result := &Result{Columns: append([]string(nil), s.Returning...)}
	for _, values := range valueRows {
		rowValues, rowid, err := buildInsertRow(tableInfo, s.Columns, values)
		if err != nil {
			// INSERT OR IGNORE turns a *constraint* violation (e.g. NOT
			// NULL) into a skipped row rather than an error, matching
			// SQLite. Non-constraint errors (arity, "no such column")
			// still fail. `ON CONFLICT ... DO NOTHING` targets UNIQUE
			// conflicts only and does NOT suppress a NOT NULL violation,
			// so this is gated on OrIgnore alone.
			if s.OrIgnore && isConstraintError(err) {
				continue
			}
			return nil, err
		}
		conflictRowID, conflictRow, conflicted, err := e.findInsertConflict(
			tableInfo, rowValues, s.Conflict, 0,
		)
		if err != nil {
			return nil, err
		}
		if !conflicted && s.Conflict != nil && len(s.Conflict.Target) > 0 {
			if _, _, otherConflict, err := e.findInsertConflict(tableInfo, rowValues, nil, 0); err != nil {
				return nil, err
			} else if otherConflict {
				return nil, &engineError{"UNIQUE constraint failed"}
			}
		}

		if conflicted {
			switch {
			case s.OrIgnore || (s.Conflict != nil && s.Conflict.DoNothing):
				continue
			case s.Conflict != nil:
				updated, err := applyConflictUpdates(tableInfo, conflictRow, rowValues, s.Conflict.Updates, params)
				if err != nil {
					return nil, err
				}
				if err := validateUpdatedRow(tableInfo, conflictRow, updated); err != nil {
					return nil, err
				}
				if _, _, duplicate, err := e.findInsertConflict(tableInfo, updated, nil, conflictRowID); err != nil {
					return nil, err
				} else if duplicate {
					return nil, &engineError{"UNIQUE constraint failed"}
				}
				if err := e.checkForeignKeyInsert(tableInfo, updated); err != nil {
					return nil, err
				}
				if err := e.btree.Insert(tableInfo.RootPage, conflictRowID, valuesToRecord(updated)); err != nil {
					return nil, err
				}
				if err := e.insertIntoIndexes(tableInfo.Name, conflictRowID, updated); err != nil {
					return nil, err
				}
				result.RowsAffected++
				result.LastInsertID = conflictRowID
				appendReturningRow(result, tableInfo, updated)
				continue
			default:
				return nil, &engineError{"UNIQUE constraint failed"}
			}
		}

		if err := e.checkForeignKeyInsert(tableInfo, rowValues); err != nil {
			return nil, err
		}
		if err := e.btree.Insert(tableInfo.RootPage, rowid, valuesToRecord(rowValues)); err != nil {
			return nil, err
		}
		if err := e.insertIntoIndexes(tableInfo.Name, rowid, rowValues); err != nil {
			return nil, err
		}
		result.RowsAffected++
		result.LastInsertID = rowid
		appendReturningRow(result, tableInfo, rowValues)
	}
	return result, nil
}

func (e *Engine) insertValueRows(s *InsertStmt, params []Value) ([][]Value, error) {
	if s.Select != nil {
		selected, err := e.executeSelect(s.Select, params)
		if err != nil {
			return nil, err
		}
		return selected.Rows, nil
	}
	rows := make([][]Value, 0, len(s.Values))
	for _, expressions := range s.Values {
		eval := &ExprEval{Params: params}
		values := make([]Value, len(expressions))
		for i, expression := range expressions {
			value, err := eval.Eval(expression)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		rows = append(rows, values)
	}
	return rows, nil
}

// isConstraintError reports whether err is a row-constraint violation
// (NOT NULL / UNIQUE / PRIMARY KEY / CHECK / FOREIGN KEY) — the class of
// error that `INSERT OR IGNORE` turns into a skipped row. Arity and
// schema errors ("no such column") are not constraint errors and still
// fail under OR IGNORE.
func isConstraintError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "constraint failed")
}

func buildInsertRow(tableInfo *TableInfo, columns []string, values []Value) ([]Value, int64, error) {
	// Arity: a row must supply exactly as many values as the INSERT names
	// — the length of an explicit column list, or the table width for the
	// positional form. SQLite rejects a mismatch rather than silently
	// defaulting the shortfall or dropping the excess. These are NOT
	// constraint errors, so `INSERT OR IGNORE` does not suppress them.
	if len(columns) > 0 {
		if len(values) != len(columns) {
			return nil, 0, &engineError{fmt.Sprintf("%d values for %d columns", len(values), len(columns))}
		}
	} else if len(values) != len(tableInfo.Columns) {
		return nil, 0, &engineError{fmt.Sprintf("table %s has %d columns but %d values were supplied", tableInfo.Name, len(tableInfo.Columns), len(values))}
	}
	rowValues := make([]Value, len(tableInfo.Columns))
	// provided[i] is true when column i was explicitly supplied by the
	// INSERT statement (either by name in `columns` or positionally). Only
	// omitted columns receive the column's declared DEFAULT — an explicit
	// NULL must still trip the NOT NULL check below even when a default
	// exists. (SQLite semantics: DEFAULT fires for *missing* values only.)
	provided := make([]bool, len(tableInfo.Columns))
	if len(columns) > 0 {
		for i, column := range columns {
			columnIndex := tableInfo.ColumnIndex(column)
			if columnIndex < 0 {
				return nil, 0, &engineError{"no such column: " + column}
			}
			if i < len(values) {
				rowValues[columnIndex] = ApplyAffinity(values[i], tableInfo.Columns[columnIndex].Affinity)
				provided[columnIndex] = true
			}
		}
	} else {
		for i := range tableInfo.Columns {
			if i < len(values) {
				rowValues[i] = ApplyAffinity(values[i], tableInfo.Columns[i].Affinity)
				provided[i] = true
			}
		}
	}
	for i := range rowValues {
		if !provided[i] && rowValues[i].IsNull() {
			// Resolve the column's declared DEFAULT for an *omitted* column.
			// Two shapes:
			//   (a) col.Default != nil — constant default pre-evaluated at
			//       CREATE TABLE time (literals, TRUE/FALSE). Fast path.
			//   (b) col.DefaultExpr != nil with col.Default == nil — a
			//       non-constant default such as CURRENT_TIMESTAMP that
			//       must be re-evaluated for EVERY insert. The expression
			//       is evaluated with a fresh ExprEval (no row context),
			//       which lets evalColumnRef fall back to the CURRENT_*
			//       keywords.
			// In both cases the column affinity is applied to the result.
			// An explicit NULL (provided[i] == true) is intentionally NOT
			// overwritten here so the NOT NULL check below still trips.
			col := tableInfo.Columns[i]
			if col.Default != nil {
				rowValues[i] = ApplyAffinity(*col.Default, col.Affinity)
			} else if col.DefaultExpr != nil {
				if v, err := (&ExprEval{}).Eval(col.DefaultExpr); err == nil {
					rowValues[i] = ApplyAffinity(v, col.Affinity)
				}
			}
		}
		if tableInfo.Columns[i].NotNull && rowValues[i].IsNull() {
			return nil, 0, &engineError{"NOT NULL constraint failed: " + tableInfo.Name + "." + tableInfo.Columns[i].Name}
		}
	}
	rowid := tableInfo.NextAutoIncrement()
	if tableInfo.HasRowIDAlias() {
		pkIndex := tableInfo.PrimaryKey
		if rowValues[pkIndex].IsNull() {
			rowValues[pkIndex] = IntegerValue(rowid)
		} else {
			rowid = rowValues[pkIndex].IntVal
			tableInfo.SetAutoIncrement(rowid)
		}
	}
	return rowValues, rowid, nil
}

func (e *Engine) findInsertConflict(
	tableInfo *TableInfo,
	candidate []Value,
	conflict *InsertConflict,
	excludeRowID int64,
) (int64, []Value, bool, error) {
	allConstraints := e.uniqueConstraints(tableInfo)
	constraints := allConstraints
	if conflict != nil && len(conflict.Target) > 0 {
		filtered := constraints[:0:0]
		for _, constraint := range allConstraints {
			if sameColumns(constraint.Columns, conflict.Target) {
				filtered = append(filtered, constraint)
			}
		}
		constraints = filtered
		if len(constraints) == 0 {
			return 0, nil, false, &engineError{"ON CONFLICT clause does not match a UNIQUE constraint"}
		}
	}
	if len(constraints) == 0 {
		return 0, nil, false, nil
	}
	cursor, err := e.btree.Scan(tableInfo.RootPage)
	if err != nil {
		return 0, nil, false, err
	}
	defer cursor.Close()
	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			return 0, nil, false, err
		}
		if excludeRowID != 0 && rowid == excludeRowID {
			continue
		}
		existing := recordToValues(record, tableInfo)
		for _, constraint := range constraints {
			// A partial UNIQUE constraint only applies to rows the
			// predicate selects on BOTH sides — the existing row and
			// the candidate. If either side is out of the predicate,
			// the constraint cannot fire between them.
			if constraint.Predicate != nil {
				if !rowMatchesPredicate(tableInfo, existing, constraint.Predicate) {
					continue
				}
				if !rowMatchesPredicate(tableInfo, candidate, constraint.Predicate) {
					continue
				}
			}
			if rowsConflict(tableInfo, existing, candidate, constraint.Columns) {
				return rowid, existing, true, nil
			}
		}
	}
	return 0, nil, false, nil
}

func rowsConflict(tableInfo *TableInfo, left, right []Value, columns []string) bool {
	for _, column := range columns {
		index := tableInfo.ColumnIndex(column)
		if index < 0 || index >= len(left) || index >= len(right) {
			return false
		}
		if left[index].IsNull() || right[index].IsNull() {
			return false
		}
		if CompareValues(left[index], right[index]) != CompareEqual {
			return false
		}
	}
	return len(columns) > 0
}

func sameColumns(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !strings.EqualFold(left[i], right[i]) {
			return false
		}
	}
	return true
}

func applyConflictUpdates(
	tableInfo *TableInfo,
	existing, excluded []Value,
	updates []SetClause,
	params []Value,
) ([]Value, error) {
	row := append(append([]Value(nil), existing...), excluded...)
	oldColumns := make(map[string]int, len(tableInfo.Columns))
	excludedColumns := make(map[string]int, len(tableInfo.Columns))
	for i, column := range tableInfo.Columns {
		oldColumns[strings.ToLower(column.Name)] = i
		excludedColumns[strings.ToLower(column.Name)] = len(existing) + i
	}
	eval := &ExprEval{
		Row:       row,
		ColumnMap: oldColumns,
		TableMap: map[string]map[string]int{
			strings.ToLower(tableInfo.Name): oldColumns,
			"excluded":                      excludedColumns,
		},
		Params: params,
	}
	updated := append([]Value(nil), existing...)
	for _, assignment := range updates {
		index := tableInfo.ColumnIndex(assignment.Column)
		if index < 0 {
			return nil, &engineError{"no such column: " + assignment.Column}
		}
		value, err := eval.Eval(assignment.Expr)
		if err != nil {
			return nil, err
		}
		updated[index] = ApplyAffinity(value, tableInfo.Columns[index].Affinity)
	}
	return updated, nil
}

func appendReturningRow(result *Result, tableInfo *TableInfo, row []Value) {
	if len(result.Columns) == 0 {
		return
	}
	returned := make([]Value, len(result.Columns))
	for i, column := range result.Columns {
		index := tableInfo.ColumnIndex(column)
		if index < 0 {
			returned[i] = TextValue(fmt.Sprintf("<unknown column %s>", column))
			continue
		}
		returned[i] = row[index]
	}
	result.Rows = append(result.Rows, returned)
}
