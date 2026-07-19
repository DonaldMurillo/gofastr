package sqlite

func validateReturningColumns(table *TableInfo, columns []string) error {
	for _, column := range columns {
		if table.ColumnIndex(column) < 0 {
			return &engineError{"no such column: " + column}
		}
	}
	return nil
}
