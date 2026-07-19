package sqlite

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

func (e *Engine) uniqueConstraints(table *TableInfo) [][]string {
	constraints := make([][]string, len(table.UniqueConstraints))
	for i := range table.UniqueConstraints {
		constraints[i] = append([]string(nil), table.UniqueConstraints[i]...)
	}
	for _, index := range e.schema.IndexesForTable(table.Name) {
		if !index.Unique {
			continue
		}
		duplicate := false
		for _, constraint := range constraints {
			if sameColumns(constraint, index.Columns) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			constraints = append(constraints, append([]string(nil), index.Columns...))
		}
	}
	return constraints
}
