package sqlite

import "sort"

func (e *Engine) validateUniqueIndex(table *TableInfo, columns []string, predicate Expr) error {
	cursor, err := e.btree.Scan(table.RootPage)
	if err != nil {
		return err
	}
	defer cursor.Close()

	var seen [][]Value
	for cursor.Next() {
		_, record, err := cursor.Get()
		if err != nil {
			return err
		}
		row := recordToValues(record, table)
		// A partial unique index only constrains rows the predicate
		// selects. Skip rows outside the predicate entirely so a table
		// that already has duplicates in un-indexed rows can still
		// receive a partial unique index, mirroring SQLite.
		if predicate != nil && !rowMatchesPredicate(table, row, predicate) {
			continue
		}
		key := make([]Value, len(columns))
		hasNull := false
		for i, column := range columns {
			index := table.ColumnIndex(column)
			if index < 0 {
				return &engineError{"no such column: " + column}
			}
			key[i] = row[index]
			hasNull = hasNull || key[i].IsNull()
		}
		if hasNull {
			continue
		}
		seen = append(seen, key)
	}
	sort.Slice(seen, func(i, j int) bool { return lessUniqueKey(seen[i], seen[j]) })
	for i := 1; i < len(seen); i++ {
		if equalUniqueKey(seen[i-1], seen[i]) {
			return &engineError{"UNIQUE constraint failed"}
		}
	}
	return nil
}

func equalUniqueKey(left, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if CompareValues(left[i], right[i]) != CompareEqual {
			return false
		}
	}
	return true
}

func lessUniqueKey(left, right []Value) bool {
	limit := min(len(left), len(right))
	for i := 0; i < limit; i++ {
		comparison := CompareValues(left[i], right[i])
		if comparison != CompareEqual {
			return comparison == CompareLess
		}
	}
	return len(left) < len(right)
}
