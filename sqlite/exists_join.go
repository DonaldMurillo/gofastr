package sqlite

import (
	"fmt"
	"strings"
)

// probeCorrelatedJoins evaluates inner/cross joins with the complete outer
// expression context. Unsupported outer-join semantics fail closed instead of
// being approximated inside a destructive NOT EXISTS predicate.
func (e *Engine) probeCorrelatedJoins(
	combined []Value,
	tables []joinEntry,
	joins []JoinClause,
	joinIndex int,
	outer *ExprEval,
	emit func([]Value) error,
) error {
	if joinIndex >= len(tables) {
		return emit(combined)
	}
	join := joins[joinIndex-1]
	if join.Type != JoinInner && join.Type != JoinCross {
		return fmt.Errorf("sqlite: correlated EXISTS does not support join type %d", join.Type)
	}
	table := tables[joinIndex]
	cursor, err := e.btree.Scan(table.info.RootPage)
	if err != nil {
		return err
	}
	defer cursor.Close()
	for cursor.Next() {
		rowID, record, err := cursor.Get()
		if err != nil {
			return err
		}
		next := append([]Value(nil), combined...)
		if table.offset == 0 {
			next[0] = IntegerValue(rowID)
		}
		for i, value := range recordToValues(record, table.info) {
			next[table.offset+i] = value
		}
		if join.On != nil {
			eval := correlatedJoinEval(e, next, tables[:joinIndex+1], outer)
			value, err := eval.Eval(join.On)
			if err != nil {
				return err
			}
			truth, ok := value.AsInt64()
			if value.IsNull() || !ok || truth == 0 {
				continue
			}
		}
		if err := e.probeCorrelatedJoins(next, tables, joins, joinIndex+1, outer, emit); err != nil {
			return err
		}
	}
	return nil
}

func correlatedJoinEval(engine *Engine, inner []Value, tables []joinEntry, outer *ExprEval) *ExprEval {
	row := append(append([]Value(nil), inner...), outer.Row...)
	innerLen := len(inner)
	columnMap := make(map[string]int)
	tableMap := make(map[string]map[string]int)
	for _, table := range tables {
		columns := make(map[string]int, len(table.info.Columns))
		for i, column := range table.info.Columns {
			name := strings.ToLower(column.Name)
			index := table.offset + i
			columns[name] = index
			if _, exists := columnMap[name]; !exists {
				columnMap[name] = index
			}
		}
		tableMap[table.alias] = columns
	}
	for name, index := range outer.ColumnMap {
		if _, exists := columnMap[name]; !exists {
			columnMap[name] = innerLen + index
		}
	}
	for name, columns := range outer.TableMap {
		if _, exists := tableMap[name]; exists {
			continue
		}
		shifted := make(map[string]int, len(columns))
		for column, index := range columns {
			shifted[column] = innerLen + index
		}
		tableMap[name] = shifted
	}
	return &ExprEval{
		Row: row, ColumnMap: columnMap, TableMap: tableMap,
		Params: outer.Params, Engine: engine,
	}
}
