package query

import "strings"

// CountBuilder builds a SELECT COUNT(*) query with parameterized placeholders.
type CountBuilder struct {
	table  string
	wheres []whereClause
	args   []any
}

// Count creates a new CountBuilder for the given table.
func Count(table string) *CountBuilder {
	return &CountBuilder{table: table}
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (cb *CountBuilder) Where(condition string, args ...any) *CountBuilder {
	cb.wheres = append(cb.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	cb.args = append(cb.args, args...)
	return cb
}

// Build produces the final parameterized SQL and argument slice.
func (cb *CountBuilder) Build() (string, []any) {
	var sb strings.Builder

	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(cb.table)

	// WHERE
	if len(cb.wheres) > 0 {
		sb.WriteString(" WHERE ")
		paramIdx := 1
		for i, w := range cb.wheres {
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

	return sb.String(), cb.args
}
