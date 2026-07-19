package query

import (
	"fmt"
	"strings"
)

// QueryBuilder builds a SELECT query with parameterized placeholders.
type QueryBuilder struct {
	table   string
	columns []string
	joins   []joinClause
	wheres  []whereClause
	orderBy []orderClause
	limit   *int
	offset  *int
	args    []any
}

type joinClause struct {
	joinType string // "JOIN" or "LEFT JOIN"
	table    string
	on       string
}

type whereClause struct {
	connector string // "AND" or "OR"
	condition string
	args      []any
}

type orderClause struct {
	column string
	dir    string
}

// Select creates a new QueryBuilder selecting the given columns.
//
// wheres/args are pre-capped (defaultWhereCap / defaultArgCap) because the
// CRUD List handler adds ~4-5 WHERE clauses per query × 2 queries
// (count + data). Starting at nil forces repeated slice growth (1 → 2 →
// 4 → 8) for the common case; a small capacity hint lets the runtime
// allocate once at the expected size.
func Select(columns ...string) *QueryBuilder {
	return &QueryBuilder{
		columns: columns,
		wheres:  make([]whereClause, 0, defaultWhereCap),
		args:    make([]any, 0, defaultArgCap),
	}
}

const (
	// defaultWhereCap is sized for the CRUD List count/data builders, which
	// append ~4-5 Where clauses each (filters + tenant + owner + soft-delete
	// + nested + hook-where). 4 doubles to 8 on the first overflow, so the
	// common 1-2-where path (the FilteredList bench, Get-by-id, typed-query
	// single-row reads) doesn't pay for headroom it won't use while the
	// 5-where production path saves the 1 → 2 → 4 growth churn.
	defaultWhereCap = 4
	// defaultArgCap mirrors defaultWhereCap for the flat args slice.
	defaultArgCap = 4
)

// From sets the table to query.
func (qb *QueryBuilder) From(table string) *QueryBuilder {
	qb.table = table
	return qb
}

// Where appends a WHERE condition (ANDed with previous conditions).
func (qb *QueryBuilder) Where(condition string, args ...any) *QueryBuilder {
	qb.wheres = append(qb.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	qb.args = append(qb.args, args...)
	return qb
}

// OrWhere appends a WHERE condition (ORed with previous conditions).
func (qb *QueryBuilder) OrWhere(condition string, args ...any) *QueryBuilder {
	qb.wheres = append(qb.wheres, whereClause{
		connector: "OR",
		condition: condition,
		args:      args,
	})
	qb.args = append(qb.args, args...)
	return qb
}

// Join adds an INNER JOIN clause.
func (qb *QueryBuilder) Join(table, on string) *QueryBuilder {
	qb.joins = append(qb.joins, joinClause{
		joinType: "JOIN",
		table:    table,
		on:       on,
	})
	return qb
}

// LeftJoin adds a LEFT JOIN clause.
func (qb *QueryBuilder) LeftJoin(table, on string) *QueryBuilder {
	qb.joins = append(qb.joins, joinClause{
		joinType: "LEFT JOIN",
		table:    table,
		on:       on,
	})
	return qb
}

// Order adds an ORDER BY clause.
func (qb *QueryBuilder) Order(column string, dir string) *QueryBuilder {
	qb.orderBy = append(qb.orderBy, orderClause{column: column, dir: dir})
	return qb
}

// Limit sets the LIMIT clause.
func (qb *QueryBuilder) Limit(n int) *QueryBuilder {
	qb.limit = &n
	return qb
}

// Offset sets the OFFSET clause.
func (qb *QueryBuilder) Offset(n int) *QueryBuilder {
	qb.offset = &n
	return qb
}

// Cursor adds keyset/cursor-based pagination.
// dir "forward" → WHERE field > value, dir "backward" → WHERE field < value.
func (qb *QueryBuilder) Cursor(field string, value any, dir string) *QueryBuilder {
	// Sanitize the field eagerly so a payload like
	// `id) DESC; DROP TABLE audit_logs; --` cannot appear in either
	// the WHERE condition or the ORDER BY column. The value flows
	// through a placeholder and so does not need sanitisation.
	field = sanitizeFragment(field)
	op := ">"
	orderDir := "ASC"
	if dir == "backward" {
		op = "<"
		orderDir = "DESC"
	}
	// Use $1 placeholder; Build will renumber it correctly
	condition := fmt.Sprintf("%s %s $1", field, op)
	qb.args = append(qb.args, value)
	qb.wheres = append(qb.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      []any{value}, // Carry args so Build's paramIdx advances
	})
	// Ensure ORDER BY the cursor field in the direction of the comparison
	// so the returned page is adjacent to the cursor: forward (id > $1)
	// sorts ASC, backward (id < $1) sorts DESC. A bare ASC here would make
	// a backward page return the lowest ids in the table instead of the
	// rows immediately before the cursor.
	qb.orderBy = append(qb.orderBy, orderClause{column: field, dir: orderDir})
	return qb
}

// Build produces the final parameterized SQL and argument slice.
// It does not mutate the QueryBuilder — safe to call multiple times.
func (qb *QueryBuilder) Build() (string, []any) {
	var sb strings.Builder

	// Copy args so Build doesn't mutate the builder on repeated calls
	args := make([]any, len(qb.args))
	copy(args, qb.args)

	// SELECT columns — each variadic column is sanitized to drop
	// SQL meta-sequences. Column slots only ever hold dotted idents
	// or "*" in practice; sanitizeColumn also collapses whitespace so
	// a payload that smuggles a sub-SELECT can't survive verbatim.
	cols := "*"
	if len(qb.columns) > 0 {
		sanitized := make([]string, len(qb.columns))
		for i, c := range qb.columns {
			sanitized[i] = sanitizeColumn(c)
		}
		cols = strings.Join(sanitized, ", ")
	}
	sb.WriteString("SELECT ")
	sb.WriteString(cols)

	// FROM table
	sb.WriteString(" FROM ")
	sb.WriteString(sanitizeFragment(qb.table))

	// JOINs
	for _, j := range qb.joins {
		sb.WriteString(" ")
		sb.WriteString(j.joinType)
		sb.WriteString(" ")
		sb.WriteString(sanitizeFragment(j.table))
		sb.WriteString(" ON ")
		sb.WriteString(sanitizeFragment(j.on))
	}

	// WHERE
	if len(qb.wheres) > 0 {
		sb.WriteString(" WHERE ")
		paramIdx := 1
		for i, w := range qb.wheres {
			if i > 0 {
				sb.WriteString(" ")
				sb.WriteString(w.connector)
				sb.WriteString(" ")
			}
			// Re-number placeholders in the condition. Wrap in parens
			// so a caller's OR-containing clause can't combine with
			// framework-injected AND scopes via SQL precedence (which
			// would let `tenant_id = X AND visibility = 'pub' OR
			// author_id = Y AND owner_id = Z` group as `(... AND pub)
			// OR (...AND Z)` — bypassing tenant scope on the OR
			// branch). Wrapping each condition makes the AND/OR tree
			// reflect the caller's intent.
			condition := renumberPlaceholders(w.condition, paramIdx)
			paramIdx += len(w.args)
			sb.WriteByte('(')
			sb.WriteString(condition)
			sb.WriteByte(')')
		}
	}

	// ORDER BY — column gets fragment sanitisation, direction is
	// hard-clamped to ASC/DESC/empty so a CRLF / DROP smuggle in the
	// direction slot can't appear in the emitted SQL.
	if len(qb.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, o := range qb.orderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(sanitizeFragment(o.column))
			dir := sanitizeDirection(o.dir)
			if dir != "" {
				sb.WriteString(" ")
				sb.WriteString(dir)
			}
		}
	}

	// LIMIT
	if qb.limit != nil {
		fmt.Fprintf(&sb, " LIMIT $%d", len(args)+1)
		args = append(args, *qb.limit)
	}

	// OFFSET
	if qb.offset != nil {
		fmt.Fprintf(&sb, " OFFSET $%d", len(args)+1)
		args = append(args, *qb.offset)
	}

	return sb.String(), args
}

// renumberPlaceholders replaces $N placeholders in a condition string
// with the correct sequential parameter index.
func renumberPlaceholders(condition string, startIdx int) string {
	var sb strings.Builder
	i := 0
	for i < len(condition) {
		if condition[i] == '$' && i+1 < len(condition) && condition[i+1] >= '0' && condition[i+1] <= '9' {
			// Found a placeholder, replace with sequential index
			fmt.Fprintf(&sb, "$%d", startIdx)
			startIdx++
			// Skip the original placeholder number
			i++
			for i < len(condition) && condition[i] >= '0' && condition[i] <= '9' {
				i++
			}
		} else {
			sb.WriteByte(condition[i])
			i++
		}
	}
	return sb.String()
}
