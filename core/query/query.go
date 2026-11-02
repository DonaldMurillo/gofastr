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
		columns: slices.Clone(columns),
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
// After an OrWhere it ANDs with the whole OR group:
// Where(a).OrWhere(b).Where(c) builds ((a) OR (b)) AND (c).
func (qb *QueryBuilder) Where(condition string, args ...any) *QueryBuilder {
	qb.wheres = append(qb.wheres, whereClause{
		connector: "AND",
		condition: condition,
		args:      args,
	})
	qb.args = append(qb.args, args...)
	return qb
}

// OrWhere appends a WHERE condition (ORed with previous conditions,
// taken together: Where(a).Where(b).OrWhere(c) builds (a) AND (b) OR (c),
// which SQL reads as (a AND b) OR c).
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
	//
	// A builder that already orders by field gets no second key for it:
	// ORDER BY id ASC, id ASC is the same order spelled twice. Keeping
	// that order's direction in step with dir, and ordering by no other
	// column first, is the caller's to check (the DSL's after() refuses
	// both): a keyset cut on one column over rows sorted by another skips
	// and repeats rows.
	for _, o := range qb.orderBy {
		if o.column == field {
			return qb
		}
	}
	qb.orderBy = append(qb.orderBy, orderClause{column: field, dir: orderDir})
	return qb
}

// Build produces the final parameterized SQL and argument slice.
// It does not mutate the QueryBuilder: safe to call multiple times.
func (qb *QueryBuilder) Build() (string, []any) {
	var sb strings.Builder

	// Copy args so Build doesn't mutate the builder on repeated calls
	args := make([]any, len(qb.args))
	copy(args, qb.args)

	// SELECT columns: each variadic column is sanitized to drop
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
		appendWhereClauses(&sb, qb.wheres, 1)
	}

	// ORDER BY: column gets fragment sanitisation, direction is
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

	// LIMIT. SQLite refuses an OFFSET with no LIMIT before it, so an
	// offset alone carries the largest LIMIT both dialects accept
	// (SQLite's -1 and Postgres's ALL are each one dialect's only).
	if qb.limit != nil {
		fmt.Fprintf(&sb, " LIMIT $%d", len(args)+1)
		args = append(args, *qb.limit)
	} else if qb.offset != nil {
		fmt.Fprintf(&sb, " LIMIT $%d", len(args)+1)
		args = append(args, int64(math.MaxInt64))
	}

	// OFFSET
	if qb.offset != nil {
		fmt.Fprintf(&sb, " OFFSET $%d", len(args)+1)
		args = append(args, *qb.offset)
	}

	return sb.String(), args
}

// appendWhereClauses writes each WHERE condition to sb in order,
// separated by its connector, with placeholders renumbered starting at
// paramIdx. Each condition is wrapped in parens so a caller's
// OR-containing clause can't combine with framework-injected AND
// scopes via SQL precedence (which would let `tenant_id = X AND
// visibility = 'pub' OR author_id = Y AND owner_id = Z` group as
// `(... AND pub) OR (... AND Z)`, bypassing tenant scope on the OR
// branch). Wrapping each condition makes the AND/OR tree reflect the
// caller's intent. It is the one WHERE loop: QueryBuilder.Build,
// UpdateBuilder.Build and buildFiltered (Count, Delete, Matching) all
// call it.
//
// The connectors fold left to right, as Where and OrWhere document
// ("ANDed/ORed with previous conditions"): an AND that follows an OR
// parenthesizes everything before it, so Where(a).OrWhere(b).Where(c)
// renders ((a) OR (b)) AND (c). Written flat it was (a) OR (b) AND (c),
// which SQL reads as a OR (b AND c): a tenant, owner or soft-delete
// scope appended after a caller's OrWhere bound only to the last
// condition, and every row matching a came back unscoped. A chain with
// no OR before an AND (every chain the framework builds itself) writes
// no extra parens, byte for byte what it wrote before.
//
// A condition that ends inside a -- line comment gets a newline before
// its closing paren, or the comment would swallow the paren and every
// clause written after it.
func appendWhereClauses(sb *strings.Builder, wheres []whereClause, paramIdx int) {
	// Each AND that ends an OR run closes one group, so open that many
	// up front.
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
		paramIdx += len(w.args)
		sb.WriteByte('(')
		sb.WriteString(condition)
		if inLineComment {
			sb.WriteByte('\n')
		}
		sb.WriteByte(')')
	}
}

// renumberPlaceholders rewrites the $N placeholders in a condition string
// into a fresh sequential run starting at startIdx.
//
// Semantics are POSITIONAL by encounter, not value-mapping: the first
// placeholder token found becomes $startIdx, the next $startIdx+1, and so
// on, regardless of the original digits. This is the contract the whole
// fragment-composition model depends on: And/Or/Not (framework/entity's
// Condition), the DSL, and nested-filter EXISTS subqueries each emit their
// own args numbered from $1, and Build concatenates the arg slices in the
// same order it renumbers. A composed "(name = $1 OR name = $1)" with two
// args therefore correctly becomes "(name = $1 OR name = $2)". Callers who
// need one argument referenced twice must pass it twice; a repeated $N is
// NOT a back-reference to the same bind.
//
// Quote-aware: a $N inside a single-quoted SQL string literal is data, not
// a placeholder, and is left untouched (a doubled ” is an escaped quote
// that does not close the literal). This is the one behavior the older
// positional renumberer got wrong: it rewrote digits inside literals.
func renumberPlaceholders(condition string, startIdx int) string {
	out, _ := renumberCondition(condition, startIdx)
	return out
}

// renumberCondition is renumberPlaceholders that also reports whether
// the condition ends inside an unterminated -- line comment, which is
// still open when the caller writes the next byte.
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
			// A string literal is data: copy it byte for byte, including
			// any $N inside it. E'…' honours backslash escapes, so \' does
			// NOT end that literal, reading it as a terminator used to
			// drop the lexer out of the string early and renumber the rest
			// of it.
			i = copyStringLiteral(&sb, condition, i)
		case c == '-' && i+1 < len(condition) && condition[i+1] == '-':
			// A -- line comment runs to the newline. Its text is not SQL,
			// so a $N inside it is not a placeholder; renumbering it
			// consumed an index and shifted every real placeholder after.
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
			// Block comments nest in PostgreSQL, so track the depth rather
			// than scanning for the first */.
			//
			// SQLite does not nest: it closes the comment at the first */.
			// A comment whose text holds a /* (a glob like /api/*) never
			// reaches depth 0 here, so the nesting scan read the rest of
			// the condition as comment and left its placeholders
			// unrenumbered, while SQLite ran that rest as SQL with the
			// wrong binds. Postgres refuses such a fragment as an
			// unterminated comment, so when the nesting scan runs off the
			// end SQLite's reading is the only one that executes: close at
			// the first */ instead. A fragment whose comments balance
			// keeps the nesting reading.
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
			// $tag$…$tag$ (and bare $$…$$) is a dollar-quoted literal,
			// also data. Its body regularly contains $1-looking text.
			if body, ok := dollarQuoteEnd(condition, i); ok {
				sb.WriteString(condition[i:body])
				i = body
				continue
			}
			if i+1 < len(condition) && isASCIIDigit(condition[i+1]) {
				// A placeholder token: consume the digit run and emit the
				// next sequential index, whatever the original digits were.
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

// copyStringLiteral copies the single-quoted literal starting at s[i] into
// sb and returns the index just past it. An unterminated literal copies to
// the end of the string.
func copyStringLiteral(sb *strings.Builder, s string, i int) int {
	// E'…' (Postgres escape-string syntax) treats backslash as an escape;
	// a plain '…' does not. The E is a prefix only when it starts a token:
	// an E that ends a keyword or identifier (ESCAPE'\', LIKE'x\',
	// ELSE'\') is part of that word, and the quote opens a plain
	// literal. Reading it as E'…' took \' for an escaped quote, kept the
	// lexer in the literal to the end, and left every later placeholder
	// unrenumbered. The byte before the E is judged the way the $N case
	// judges the byte before a $.
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
			// A doubled '' is an escaped quote that does not close the
			// literal.
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

// dollarQuoteEnd reports whether a dollar-quoted literal opens at s[i] and,
// if so, returns the index just past its closing tag. An unterminated
// literal runs to the end of the string.
func dollarQuoteEnd(s string, i int) (int, bool) {
	j := i + 1
	// $1 is a placeholder, never a tag: tags cannot start with a digit.
	if j < len(s) && isASCIIDigit(s[j]) {
		return 0, false
	}
	for j < len(s) && isIdentByte(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return 0, false
	}
	tag := s[i : j+1] // "$$" or "$tag$"
	rest := s[j+1:]
	end := strings.Index(rest, tag)
	if end < 0 {
		return len(s), true
	}
	return j + 1 + end + len(tag), true
}
