package query

import "strings"

// CountBuilder builds a SELECT COUNT(*) query with parameterized placeholders.
type CountBuilder struct {
	table  string
	wheres []whereClause
	args   []any
}

// Count creates a new CountBuilder for the given table. wheres/args are
// pre-capped. See query.Select's rationale (CRUD List adds ~4-5 Where
// clauses per count + data builder; without the hint each grows through
// 1 → 2 → 4 → 8 across the request).
func Count(table string) *CountBuilder {
	return &CountBuilder{
		table:  table,
		wheres: make([]whereClause, 0, defaultWhereCap),
		args:   make([]any, 0, defaultArgCap),
	}
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
	return buildFiltered("SELECT COUNT(*) FROM ", cb.table, cb.wheres, cb.args)
}

// buildFiltered writes "<verb><table> [WHERE (…) AND (…)…]" plus the
// flattened argument slice for a builder whose WHERE clauses are all
// ANDed. It is the shared body of CountBuilder.Build and
// DeleteBuilder.Build, which were verbatim copies of each other
// differing only in the SQL verb.
func buildFiltered(verb, table string, wheres []whereClause, args []any) (string, []any) {
	var sb strings.Builder

	sb.WriteString(verb)
	sb.WriteString(sanitizeFragment(table))

	// WHERE
	if len(wheres) > 0 {
		sb.WriteString(" WHERE ")
		paramIdx := 1
		for i, w := range wheres {
			if i > 0 {
				sb.WriteString(" ")
				sb.WriteString(w.connector)
				sb.WriteString(" ")
			}
			// Wrap each condition in parens. See query.go for the
			// SQL-precedence bypass this defends against.
			condition := renumberPlaceholders(w.condition, paramIdx)
			paramIdx += len(w.args)
			sb.WriteByte('(')
			sb.WriteString(condition)
			sb.WriteByte(')')
		}
	}

	return sb.String(), args
}
