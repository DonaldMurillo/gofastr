package query

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// QueryBuilder builds a SELECT query with parameterized placeholders.
type QueryBuilder struct {
	table   string
	columns []string
	joins   []joinClause
	wheres  whereClauses
	orderBy []orderClause
	limit   *int
	offset  *int
}

type joinClause struct {
	joinType string
	table    string
	on       string
}

type whereClause struct {
	connector string
	condition string
	argCount  int
}

type whereClauses struct {
	clauses []whereClause
	args    []any
}

func newWhereClauses() whereClauses {
	return whereClauses{
		clauses: make([]whereClause, 0, 4),
		args:    make([]any, 0, 4),
	}
}

func (w *whereClauses) add(connector, condition string, args ...any) {
	w.clauses = append(w.clauses, whereClause{connector: connector, condition: condition, argCount: len(args)})
	w.args = append(w.args, args...)
}

type orderClause struct {
	column   string
	dir      string
	pinNulls bool
}

// Select creates a new QueryBuilder selecting the given columns.
func Select(columns ...string) *QueryBuilder {
	return &QueryBuilder{
		columns: slices.Clone(columns),
		wheres:  newWhereClauses(),
	}
}

// From sets the table to query.
func (qb *QueryBuilder) From(table string) *QueryBuilder {
	qb.table = table
	return qb
}

// Where appends a condition ANDed with everything before it: Where(a).OrWhere(b).Where(c) builds ((a) OR (b)) AND (c).
func (qb *QueryBuilder) Where(condition string, args ...any) *QueryBuilder {
	qb.wheres.add("AND", condition, args...)
	return qb
}

// OrWhere appends a condition ORed with everything before it.
func (qb *QueryBuilder) OrWhere(condition string, args ...any) *QueryBuilder {
	qb.wheres.add("OR", condition, args...)
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

// OrderPinNulls is Order with nulls pinned last ascending and first descending, as OrderTerm writes it.
func (qb *QueryBuilder) OrderPinNulls(column string, dir string) *QueryBuilder {
	qb.orderBy = append(qb.orderBy, orderClause{column: column, dir: dir, pinNulls: true})
	return qb
}

// OrderTerm renders one ORDER BY term; pinNulls spells Postgres's default null position.
func OrderTerm(column, dir string, pinNulls bool) string {
	term := identSlot(sanitizeFragment(column))
	dir = sanitizeDirection(dir)
	if dir != "" {
		term += " " + dir
	}
	switch {
	case !pinNulls:
	case dir == "DESC":
		term += " NULLS FIRST"
	default:
		term += " NULLS LAST"
	}
	return term
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

// Cursor adds a keyset condition, field > value for "forward" and field < value for "backward", ordered toward the cursor.
func (qb *QueryBuilder) Cursor(field string, value any, dir string) *QueryBuilder {
	field = identSlot(sanitizeFragment(field))
	op := ">"
	orderDir := "ASC"
	if dir == "backward" {
		op = "<"
		orderDir = "DESC"
	}
	condition := fmt.Sprintf("%s %s $1", field, op)
	qb.wheres.add("AND", condition, value)
	for _, o := range qb.orderBy {
		if identSlot(sanitizeFragment(o.column)) == field {
			return qb
		}
	}
	qb.orderBy = append(qb.orderBy, orderClause{column: field, dir: orderDir})
	return qb
}

// Build produces the SQL and arguments without changing the builder.
func (qb *QueryBuilder) Build() (string, []any) {
	var sb strings.Builder

	args := make([]any, len(qb.wheres.args))
	copy(args, qb.wheres.args)

	cols := "*"
	if len(qb.columns) > 0 {
		sanitized := make([]string, len(qb.columns))
		for i, c := range qb.columns {
			sanitized[i] = identSlot(sanitizeColumn(c))
		}
		cols = strings.Join(sanitized, ", ")
	}
	sb.WriteString("SELECT ")
	sb.WriteString(cols)

	sb.WriteString(" FROM ")
	sb.WriteString(identSlot(sanitizeFragment(qb.table)))

	for _, j := range qb.joins {
		sb.WriteString(" ")
		sb.WriteString(j.joinType)
		sb.WriteString(" ")
		sb.WriteString(identSlot(sanitizeFragment(j.table)))
		sb.WriteString(" ON ")
		sb.WriteString(sanitizeFragment(j.on))
	}

	if len(qb.wheres.clauses) > 0 {
		sb.WriteString(" WHERE ")
		appendWhereClauses(&sb, qb.wheres.clauses, 1)
	}

	if len(qb.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, o := range qb.orderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(OrderTerm(o.column, o.dir, o.pinNulls))
		}
	}

	if qb.limit != nil {
		fmt.Fprintf(&sb, " LIMIT $%d", len(args)+1)
		args = append(args, *qb.limit)
	} else if qb.offset != nil {
		fmt.Fprintf(&sb, " LIMIT $%d", len(args)+1)
		args = append(args, int64(math.MaxInt64))
	}

	if qb.offset != nil {
		fmt.Fprintf(&sb, " OFFSET $%d", len(args)+1)
		args = append(args, *qb.offset)
	}

	return sb.String(), args
}

func appendWhereClauses(sb *strings.Builder, wheres []whereClause, paramIdx int) {
	groups, sawOr := 0, false
	for i, w := range wheres {
		switch {
		case i == 0:
		case w.connector == "OR":
			sawOr = true
		case sawOr:
			groups++
			sawOr = false
		}
	}
	for range groups {
		sb.WriteByte('(')
	}
	sawOr = false
	for i, w := range wheres {
		if i > 0 {
			if w.connector == "OR" {
				sawOr = true
			} else if sawOr {
				sb.WriteByte(')')
				sawOr = false
			}
			sb.WriteString(" ")
			sb.WriteString(w.connector)
			sb.WriteString(" ")
		}
		condition, inLineComment := renumberCondition(w.condition, paramIdx)
		paramIdx += w.argCount
		sb.WriteByte('(')
		sb.WriteString(condition)
		if inLineComment {
			sb.WriteByte('\n')
		}
		sb.WriteByte(')')
	}
}

func renumberPlaceholders(condition string, startIdx int) string {
	out, _ := renumberCondition(condition, startIdx)
	return out
}

func renumberCondition(condition string, startIdx int) (string, bool) {
	inLineComment := false
	var sb strings.Builder
	next := startIdx
	i := 0
	for i < len(condition) {
		c := condition[i]
		switch {
		case c == '"' || c == '`':
			quote, j := c, i+1
			for j < len(condition) {
				if condition[j] == quote {
					j++
					if j < len(condition) && condition[j] == quote {
						j++
						continue
					}
					break
				}
				j++
			}
			sb.WriteString(condition[i:j])
			i = j
		case c == '\'':
			i = copyStringLiteral(&sb, condition, i)
		case c == '-' && i+1 < len(condition) && condition[i+1] == '-':
			j := strings.IndexByte(condition[i:], '\n')
			if j < 0 {
				sb.WriteString(condition[i:])
				i = len(condition)
				inLineComment = true
				continue
			}
			sb.WriteString(condition[i : i+j+1])
			i += j + 1
		case c == '/' && i+1 < len(condition) && condition[i+1] == '*':
			depth, j := 1, i+2
			for j < len(condition) && depth > 0 {
				if j+1 < len(condition) && condition[j] == '/' && condition[j+1] == '*' {
					depth++
					j += 2
					continue
				}
				if j+1 < len(condition) && condition[j] == '*' && condition[j+1] == '/' {
					depth--
					j += 2
					continue
				}
				j++
			}
			if depth > 0 {
				if k := strings.Index(condition[i+2:], "*/"); k >= 0 {
					j = i + 2 + k + 2
				}
			}
			sb.WriteString(condition[i:j])
			i = j
		case c == '$' && (i == 0 || (!isIdentByte(condition[i-1]) && condition[i-1] != '$' && condition[i-1] < 0x80)):
			if body, ok := dollarQuoteEnd(condition, i); ok {
				sb.WriteString(condition[i:body])
				i = body
				continue
			}
			if i+1 < len(condition) && isASCIIDigit(condition[i+1]) {
				j := i + 1
				for j < len(condition) && isASCIIDigit(condition[j]) {
					j++
				}
				fmt.Fprintf(&sb, "$%d", next)
				next++
				i = j
				continue
			}
			sb.WriteByte(c)
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String(), inLineComment
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || isASCIIDigit(b)
}

func copyStringLiteral(sb *strings.Builder, s string, i int) int {
	escapes := i > 0 && (s[i-1] == 'E' || s[i-1] == 'e') &&
		(i == 1 || (!isIdentByte(s[i-2]) && s[i-2] != '$' && s[i-2] < 0x80))
	sb.WriteByte('\'')
	i++
	for i < len(s) {
		if escapes && s[i] == '\\' && i+1 < len(s) {
			sb.WriteByte(s[i])
			sb.WriteByte(s[i+1])
			i += 2
			continue
		}
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				sb.WriteString("''")
				i += 2
				continue
			}
			sb.WriteByte('\'')
			return i + 1
		}
		sb.WriteByte(s[i])
		i++
	}
	return i
}

func dollarQuoteEnd(s string, i int) (int, bool) {
	j := i + 1
	if j < len(s) && isASCIIDigit(s[j]) {
		return 0, false
	}
	for j < len(s) && isIdentByte(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return 0, false
	}
	tag := s[i : j+1]
	rest := s[j+1:]
	end := strings.Index(rest, tag)
	if end < 0 {
		return len(s), true
	}
	return j + 1 + end + len(tag), true
}
