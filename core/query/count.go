package query

import (
	"slices"
	"strings"
)

// CountBuilder builds a SELECT COUNT(*) query with parameterized placeholders.
type CountBuilder struct {
	table  string
	wheres []whereClause
	args   []any
}

// Count creates a new CountBuilder for the given table.
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

func buildFiltered(verb, table string, wheres []whereClause, args []any) (string, []any) {
	var sb strings.Builder

	sb.WriteString(verb)
	sb.WriteString(identSlot(sanitizeFragment(table)))

	if len(wheres) > 0 {
		sb.WriteString(" WHERE ")
		appendWhereClauses(&sb, wheres, 1)
	}

	return sb.String(), slices.Clone(args)
}
