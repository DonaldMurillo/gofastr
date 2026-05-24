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
	args      []any
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
	ub.args = append(ub.args, value)
	return ub
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (ub *UpdateBuilder) Where(condition string, args ...any) *UpdateBuilder {
	ub.wheres = append(ub.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	ub.args = append(ub.args, args...)
	return ub
}

// Returning adds a RETURNING clause.
func (ub *UpdateBuilder) Returning(cols ...string) *UpdateBuilder {
	ub.returning = cols
	return ub
}

// Build produces the final parameterized SQL and argument slice.
func (ub *UpdateBuilder) Build() (string, []any) {
	var sb strings.Builder

	sb.WriteString("UPDATE ")
	sb.WriteString(ub.table)

	// SET clauses
	sb.WriteString(" SET ")
	paramIdx := 1
	for i, s := range ub.sets {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s = $%d", s.column, paramIdx)
		paramIdx++
	}

	// WHERE
	if len(ub.wheres) > 0 {
		sb.WriteString(" WHERE ")
		for i, w := range ub.wheres {
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

	// Returning
	if len(ub.returning) > 0 {
		sb.WriteString(" RETURNING ")
		sb.WriteString(strings.Join(ub.returning, ", "))
	}

	return sb.String(), ub.args
}
