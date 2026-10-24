package query

import (
	"slices"
	"strings"
)

// DeleteBuilder builds a DELETE query with parameterized placeholders.
type DeleteBuilder struct {
	table     string
	wheres    []whereClause
	args      []any
	returning []string
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

// Returning adds a RETURNING clause naming the deleted rows' columns.
func (db *DeleteBuilder) Returning(cols ...string) *DeleteBuilder {
	db.returning = slices.Clone(cols)
	return db
}

// Matching builds the SELECT of cols from the rows this DELETE would
// remove: the same table and WHERE conditions, with their arguments.
func (db *DeleteBuilder) Matching(cols ...string) (string, []any) {
	return buildFiltered(selectVerb(cols), db.table, db.wheres, db.args)
}

// Build produces the final parameterized SQL and argument slice.
func (db *DeleteBuilder) Build() (string, []any) {
	sqlStr, args := buildFiltered("DELETE FROM ", db.table, db.wheres, db.args)
	if len(db.returning) == 0 {
		return sqlStr, args
	}
	// Each column sanitized, as UpdateBuilder.Returning does.
	cols := make([]string, len(db.returning))
	for i, c := range db.returning {
		cols[i] = sanitizeColumn(c)
	}
	return sqlStr + " RETURNING " + strings.Join(cols, ", "), args
}
