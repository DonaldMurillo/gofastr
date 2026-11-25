package query

import (
	"fmt"
	"slices"
	"strings"
)

// UpdateBuilder builds an UPDATE query with parameterized placeholders.
type UpdateBuilder struct {
	table     string
	sets      []setClause
	wheres    whereClauses
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
	ub.wheres.add("AND", condition, args...)
	return ub
}

// Returning adds a RETURNING clause.
func (ub *UpdateBuilder) Returning(cols ...string) *UpdateBuilder {
	ub.returning = slices.Clone(cols)
	return ub
}

// Matching builds the SELECT of cols from the rows this UPDATE would change.
func (ub *UpdateBuilder) Matching(cols ...string) (string, []any) {
	return buildFiltered(selectVerb(cols), ub.table, ub.wheres)
}

func selectVerb(cols []string) string {
	safe := make([]string, len(cols))
	for i, c := range cols {
		safe[i] = identSlot(sanitizeColumn(c))
	}
	return "SELECT " + strings.Join(safe, ", ") + " FROM "
}

// Build produces the SQL and arguments, every SET value before every WHERE argument.
// Build panics if no assignments were added with Set.
func (ub *UpdateBuilder) Build() (string, []any) {
	if len(ub.sets) == 0 {
		panic("query: UPDATE " + ub.table + " has no Set assignments")
	}

	var sb strings.Builder

	sb.WriteString("UPDATE ")
	sb.WriteString(identSlot(sanitizeFragment(ub.table)))

	sb.WriteString(" SET ")
	paramIdx := 1
	args := make([]any, 0, len(ub.sets)+len(ub.wheres.args))
	for i, s := range ub.sets {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s = $%d", identSlot(sanitizeColumn(s.column)), paramIdx)
		paramIdx++
		args = append(args, s.value)
	}

	if len(ub.wheres.clauses) > 0 {
		sb.WriteString(" WHERE ")
		appendWhereClauses(&sb, ub.wheres.clauses, paramIdx)
		args = append(args, ub.wheres.args...)
	}

	if len(ub.returning) > 0 {
		sb.WriteString(" RETURNING ")
		sanitizedRet := make([]string, len(ub.returning))
		for i, c := range ub.returning {
			sanitizedRet[i] = identSlot(sanitizeColumn(c))
		}
		sb.WriteString(strings.Join(sanitizedRet, ", "))
	}

	return sb.String(), args
}
