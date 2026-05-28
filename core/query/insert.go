package query

import (
	"fmt"
	"strings"
)

// InsertBuilder builds an INSERT query with parameterized placeholders.
type InsertBuilder struct {
	table     string
	columns   []string
	values    []any
	returning []string
}

// Insert creates a new InsertBuilder for the given table.
func Insert(table string) *InsertBuilder {
	return &InsertBuilder{table: table}
}

// Columns sets the columns to insert into.
func (ib *InsertBuilder) Columns(cols ...string) *InsertBuilder {
	ib.columns = cols
	return ib
}

// Values sets the values to insert.
func (ib *InsertBuilder) Values(vals ...any) *InsertBuilder {
	ib.values = vals
	return ib
}

// Returning adds a RETURNING clause.
func (ib *InsertBuilder) Returning(cols ...string) *InsertBuilder {
	ib.returning = cols
	return ib
}

// Build produces the final parameterized SQL and argument slice.
func (ib *InsertBuilder) Build() (string, []any) {
	var sb strings.Builder

	sb.WriteString("INSERT INTO ")
	sb.WriteString(sanitizeFragment(ib.table))

	// Columns — each column is sanitized so SQL meta-sequences in
	// a hostile column name cannot escape the parenthesised list.
	sb.WriteString(" (")
	sanitized := make([]string, len(ib.columns))
	for i, c := range ib.columns {
		sanitized[i] = sanitizeColumn(c)
	}
	sb.WriteString(strings.Join(sanitized, ", "))
	sb.WriteString(")")

	// Values
	sb.WriteString(" VALUES (")
	placeholders := make([]string, len(ib.values))
	for i := range ib.values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(")")

	// Returning — each column sanitized for the same reason as Columns.
	if len(ib.returning) > 0 {
		sb.WriteString(" RETURNING ")
		sanitizedRet := make([]string, len(ib.returning))
		for i, c := range ib.returning {
			sanitizedRet[i] = sanitizeColumn(c)
		}
		sb.WriteString(strings.Join(sanitizedRet, ", "))
	}

	return sb.String(), ib.values
}
