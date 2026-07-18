package sqlite

import "strings"

// ExistsExpr represents EXISTS (SELECT ...) and carries enough structure for
// the evaluator to preserve references to the outer row.
type ExistsExpr struct {
	Select *SelectStmt
}

func (ExistsExpr) exprNode() {}

func (e *ExprEval) evalExists(ex ExistsExpr) (Value, error) {
	if e.Engine == nil {
		return NullValue, &evalError{"EXISTS subquery requires engine"}
	}
	found, err := e.Engine.correlatedExists(ex.Select, e)
	if err != nil {
		return NullValue, err
	}
	if found {
		return IntegerValue(1), nil
	}
	return IntegerValue(0), nil
}

// correlatedExists evaluates an EXISTS subquery one joined row at a time so
// the WHERE expression can resolve both the inner aliases and the caller's
// outer aliases.
func (e *Engine) correlatedExists(s *SelectStmt, outer *ExprEval) (bool, error) {
	if s.From == nil || s.From.Table == nil {
		result, err := e.executeSelect(s, outer.Params)
		return err == nil && len(result.Rows) > 0, err
	}

	tables, innerTableMap, err := e.buildJoinPlan(s)
	if err != nil {
		return false, err
	}
	innerLen := combinedRowLen(tables)
	innerColumns := map[string]int{"rowid": 0}
	for _, table := range tables {
		for i, column := range table.info.Columns {
			name := strings.ToLower(column.Name)
			if _, exists := innerColumns[name]; !exists {
				innerColumns[name] = table.offset + i
			}
		}
	}

	evaluate := func(inner []Value) (bool, error) {
		row := make([]Value, 0, innerLen+len(outer.Row))
		row = append(row, inner...)
		row = append(row, outer.Row...)

		columnMap := make(map[string]int, len(innerColumns)+len(outer.ColumnMap))
		for name, index := range innerColumns {
			columnMap[name] = index
		}
		for name, index := range outer.ColumnMap {
			if _, exists := columnMap[name]; !exists {
				columnMap[name] = innerLen + index
			}
		}
		tableMap := make(map[string]map[string]int, len(innerTableMap)+len(outer.TableMap))
		for table, columns := range innerTableMap {
			tableMap[table] = columns
		}
		for table, columns := range outer.TableMap {
			if _, exists := tableMap[table]; exists {
				continue
			}
			shifted := make(map[string]int, len(columns))
			for name, index := range columns {
				shifted[name] = innerLen + index
			}
			tableMap[table] = shifted
		}

		if s.Where == nil {
			return true, nil
		}
		value, err := (&ExprEval{
			Row:       row,
			ColumnMap: columnMap,
			TableMap:  tableMap,
			Params:    outer.Params,
			Engine:    e,
		}).Eval(s.Where)
		if err != nil {
			return false, err
		}
		truth, ok := value.AsInt64()
		return ok && truth != 0, nil
	}

	cursor, err := e.btree.Scan(tables[0].info.RootPage)
	if err != nil {
		return false, err
	}
	defer cursor.Close()
	for cursor.Next() {
		rowID, record, err := cursor.Get()
		if err != nil {
			return false, err
		}
		combined := make([]Value, innerLen)
		combined[0] = IntegerValue(rowID)
		for i, value := range recordToValues(record, tables[0].info) {
			combined[tables[0].offset+i] = value
		}

		found := false
		emit := func(joined []Value) error {
			match, err := evaluate(joined)
			if err != nil {
				return err
			}
			if match {
				found = true
				return errEarlyExit
			}
			return nil
		}
		if len(tables) == 1 {
			if err := emit(combined); err != nil && err != errEarlyExit {
				return false, err
			}
		} else if err := e.probeCorrelatedJoins(combined, tables, s.From.Joins, 1, outer, emit); err != nil && err != errEarlyExit {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
