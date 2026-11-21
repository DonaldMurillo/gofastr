package query

import (
	"slices"
	"strings"
)

// CountBuilder builds a SELECT COUNT(*) query with parameterized placeholders.
type CountBuilder struct {
	table  string
	wheres whereClauses
}

// Count creates a new CountBuilder for the given table.
func Count(table string) *CountBuilder {
	return &CountBuilder{
		table:  table,
		wheres: newWhereClauses(),
	}
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (cb *CountBuilder) Where(condition string, args ...any) *CountBuilder {
	cb.wheres.add("AND", condition, args...)
	return cb
}

// Build produces the final parameterized SQL and argument slice.
func (cb *CountBuilder) Build() (string, []any) {
	return buildFiltered("SELECT COUNT(*) FROM ", cb.table, cb.wheres)
}

func buildFiltered(verb, table string, wheres whereClauses) (string, []any) {
	var sb strings.Builder

	sb.WriteString(verb)
	sb.WriteString(identSlot(sanitizeFragment(table)))

	if len(wheres.clauses) > 0 {
		sb.WriteString(" WHERE ")
		appendWhereClauses(&sb, wheres.clauses, 1)
	}

	return sb.String(), slices.Clone(wheres.args)
}
