package query

import "strings"

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
	var sb strings.Builder

	sb.WriteString("DELETE FROM ")
	sb.WriteString(sanitizeFragment(db.table))

	// WHERE
	if len(db.wheres) > 0 {
		sb.WriteString(" WHERE ")
		paramIdx := 1
		for i, w := range db.wheres {
			if i > 0 {
				sb.WriteString(" ")
				sb.WriteString(w.connector)
				sb.WriteString(" ")
			}
			// Wrap each condition in parens — see query.go for the
			// SQL-precedence bypass this defends against.
			condition := renumberPlaceholders(w.condition, paramIdx)
			paramIdx += len(w.args)
			sb.WriteByte('(')
			sb.WriteString(condition)
			sb.WriteByte(')')
		}
	}

	return sb.String(), db.args
}
