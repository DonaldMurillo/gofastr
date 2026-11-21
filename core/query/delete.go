package query

import (
	"slices"
	"strings"
)

// DeleteBuilder builds a DELETE query with parameterized placeholders.
type DeleteBuilder struct {
	table     string
	wheres    whereClauses
	returning []string
}

// Delete creates a new DeleteBuilder for the given table.
func Delete(table string) *DeleteBuilder {
	return &DeleteBuilder{table: table}
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (db *DeleteBuilder) Where(condition string, args ...any) *DeleteBuilder {
	db.wheres.add("AND", condition, args...)
	return db
}

// Returning adds a RETURNING clause naming the deleted rows' columns.
func (db *DeleteBuilder) Returning(cols ...string) *DeleteBuilder {
	db.returning = slices.Clone(cols)
	return db
}

// Matching builds the SELECT of cols from the rows this DELETE would remove.
func (db *DeleteBuilder) Matching(cols ...string) (string, []any) {
	return buildFiltered(selectVerb(cols), db.table, db.wheres)
}

// Build produces the final parameterized SQL and argument slice.
func (db *DeleteBuilder) Build() (string, []any) {
	sqlStr, args := buildFiltered("DELETE FROM ", db.table, db.wheres)
	if len(db.returning) == 0 {
		return sqlStr, args
	}
	cols := make([]string, len(db.returning))
	for i, c := range db.returning {
		cols[i] = identSlot(sanitizeColumn(c))
	}
	return sqlStr + " RETURNING " + strings.Join(cols, ", "), args
}
