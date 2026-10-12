package query

import (
	"fmt"
	"strings"
)

// UpdateBuilder builds an UPDATE query with parameterized placeholders.
type UpdateBuilder struct {
	table     string
	sets      []setClause
	wheres    []whereClause
	returning []string
}

type setClause struct {
	column string
	value  any
}

// Update creates a new UpdateBuilder for the given table.
func Update(table string) *UpdateBuilder {
	return &UpdateBuilder{table: table}
}

// Set adds a column = value assignment.
func (ub *UpdateBuilder) Set(column string, value any) *UpdateBuilder {
	ub.sets = append(ub.sets, setClause{column: column, value: value})
	return ub
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (ub *UpdateBuilder) Where(condition string, args ...any) *UpdateBuilder {
	ub.wheres = append(ub.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	return ub
}

// Returning adds a RETURNING clause.
func (ub *UpdateBuilder) Returning(cols ...string) *UpdateBuilder {
	ub.returning = cols
	return ub
}

// Build produces the final parameterized SQL and argument slice. The
// args follow the placeholders, every SET value before every WHERE arg,
// whatever order Set and Where were called in.
func (ub *UpdateBuilder) Build() (string, []any) {
	var sb strings.Builder

	sb.WriteString("UPDATE ")
	sb.WriteString(sanitizeFragment(ub.table))

	// SET clauses: column slot is sanitized; the value flows through
	// a placeholder. A payload like `role = 'admin' --` collapses to a
	// non-injection token.
	sb.WriteString(" SET ")
	paramIdx := 1
	args := make([]any, 0, len(ub.sets))
	for i, s := range ub.sets {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s = $%d", sanitizeColumn(s.column), paramIdx)
		paramIdx++
		args = append(args, s.value)
	}

	// WHERE
	if len(ub.wheres) > 0 {
		sb.WriteString(" WHERE ")
		appendWhereClauses(&sb, ub.wheres, paramIdx)
		for _, w := range ub.wheres {
			args = append(args, w.args...)
		}
	}

	// Returning: each column sanitized.
	if len(ub.returning) > 0 {
		sb.WriteString(" RETURNING ")
		sanitizedRet := make([]string, len(ub.returning))
		for i, c := range ub.returning {
			sanitizedRet[i] = sanitizeColumn(c)
		}
		sb.WriteString(strings.Join(sanitizedRet, ", "))
	}

	return sb.String(), args
}
