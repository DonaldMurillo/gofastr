package query

// DeleteBuilder builds a DELETE query with parameterized placeholders.
type DeleteBuilder struct {
	table  string
	wheres []whereClause
	args   []any
}

// Delete creates a new DeleteBuilder for the given table.
func Delete(table string) *DeleteBuilder {
	return &DeleteBuilder{table: table}
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (db *DeleteBuilder) Where(condition string, args ...any) *DeleteBuilder {
	db.wheres = append(db.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	db.args = append(db.args, args...)
	return db
}

// Build produces the final parameterized SQL and argument slice.
func (db *DeleteBuilder) Build() (string, []any) {
	return buildFiltered("DELETE FROM ", db.table, db.wheres, db.args)
}
