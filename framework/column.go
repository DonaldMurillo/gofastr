package framework

import (
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/core/query"
)

// Column-typed query primitives
//
// These types power the typed query builder generated alongside each
// entity's repository. Each column is a small newtype over the underlying
// DB column name; methods on it produce Condition values that the typed
// query attaches to its QueryBuilder via Where().
//
// Conditions are dialect-agnostic — they always emit "$1" placeholders;
// the underlying QueryBuilder renumbers them at Build time so a chain of
// fragments produces "$1 ... $2 ... $3 ..." correctly across both engines.

// Condition is a where-clause fragment plus its bound arguments.
type Condition struct {
	sql  string
	args []any
}

// Apply appends this condition to the query builder.
func (c Condition) Apply(qb *query.QueryBuilder) {
	qb.Where(c.sql, c.args...)
}

// Order is an order-by-clause fragment.
type Order struct {
	column string
	dir    string
}

// Apply appends this order to the query builder.
func (o Order) Apply(qb *query.QueryBuilder) {
	qb.Order(o.column, o.dir)
}

// rawColumn is the shared base for every typed column — it's just the DB
// column name. Asc/Desc/IsNull/IsNotNull are the operations that work on any
// type so they live here.
type rawColumn string

func (c rawColumn) name() string { return string(c) }
func (c rawColumn) Asc() Order   { return Order{column: string(c), dir: "ASC"} }
func (c rawColumn) Desc() Order  { return Order{column: string(c), dir: "DESC"} }
func (c rawColumn) IsNull() Condition {
	return Condition{sql: string(c) + " IS NULL"}
}
func (c rawColumn) IsNotNull() Condition {
	return Condition{sql: string(c) + " IS NOT NULL"}
}

// And combines conditions with AND. Useful inside Or(...) to nest a group
// of ANDed predicates: Or(And(a, b), And(c, d)).
func And(conds ...Condition) Condition {
	if len(conds) == 0 {
		return Condition{sql: "1 = 1"}
	}
	if len(conds) == 1 {
		return conds[0]
	}
	parts := make([]string, 0, len(conds))
	var args []any
	for _, c := range conds {
		parts = append(parts, c.sql)
		args = append(args, c.args...)
	}
	return Condition{
		sql:  "(" + strings.Join(parts, " AND ") + ")",
		args: args,
	}
}

// Or combines conditions with OR. Each conjunct keeps its own internal
// argument order; placeholders are renumbered at QueryBuilder.Build time so
// "$1" in a fragment doesn't collide with another fragment's "$1".
func Or(conds ...Condition) Condition {
	if len(conds) == 0 {
		return Condition{sql: "1 = 0"}
	}
	if len(conds) == 1 {
		return conds[0]
	}
	parts := make([]string, 0, len(conds))
	var args []any
	for _, c := range conds {
		parts = append(parts, c.sql)
		args = append(args, c.args...)
	}
	return Condition{
		sql:  "(" + strings.Join(parts, " OR ") + ")",
		args: args,
	}
}

// Not wraps a condition in NOT (...).
func Not(c Condition) Condition {
	return Condition{sql: "NOT (" + c.sql + ")", args: c.args}
}

// inFragment builds a "col IN ($1, $2, …)" condition from a slice of args.
// Empty input yields a tautologically-false fragment so the SQL still binds
// correctly without a special case at every call site.
func inFragment(col string, args []any) Condition {
	if len(args) == 0 {
		return Condition{sql: "1 = 0"}
	}
	placeholders := make([]string, len(args))
	for i := range args {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return Condition{
		sql:  fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ", ")),
		args: args,
	}
}

// ----------------------------------------------------------------------------
// String columns
// ----------------------------------------------------------------------------

// StringColumn represents a TEXT/VARCHAR column. Use the methods to build
// Conditions: PostsTitle.Eq("hello"), PostsTitle.Like("%foo%"), etc.
type StringColumn struct{ rawColumn }

// NewStringColumn constructs a StringColumn for the given DB column name.
// Codegen calls this; user code rarely needs to.
func NewStringColumn(name string) StringColumn { return StringColumn{rawColumn: rawColumn(name)} }

func (c StringColumn) Eq(v string) Condition {
	return Condition{sql: c.name() + " = $1", args: []any{v}}
}
func (c StringColumn) Neq(v string) Condition {
	return Condition{sql: c.name() + " != $1", args: []any{v}}
}
func (c StringColumn) Like(pattern string) Condition {
	return Condition{sql: c.name() + " LIKE $1", args: []any{pattern}}
}
func (c StringColumn) NotLike(pattern string) Condition {
	return Condition{sql: c.name() + " NOT LIKE $1", args: []any{pattern}}
}
func (c StringColumn) In(values ...string) Condition {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return inFragment(c.name(), args)
}

// ----------------------------------------------------------------------------
// Integer columns
// ----------------------------------------------------------------------------

// IntColumn represents an INTEGER column.
type IntColumn struct{ rawColumn }

func NewIntColumn(name string) IntColumn { return IntColumn{rawColumn: rawColumn(name)} }

func (c IntColumn) Eq(v int) Condition  { return Condition{sql: c.name() + " = $1", args: []any{v}} }
func (c IntColumn) Neq(v int) Condition { return Condition{sql: c.name() + " != $1", args: []any{v}} }
func (c IntColumn) Gt(v int) Condition  { return Condition{sql: c.name() + " > $1", args: []any{v}} }
func (c IntColumn) Gte(v int) Condition { return Condition{sql: c.name() + " >= $1", args: []any{v}} }
func (c IntColumn) Lt(v int) Condition  { return Condition{sql: c.name() + " < $1", args: []any{v}} }
func (c IntColumn) Lte(v int) Condition { return Condition{sql: c.name() + " <= $1", args: []any{v}} }
func (c IntColumn) In(values ...int) Condition {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return inFragment(c.name(), args)
}

// ----------------------------------------------------------------------------
// Float / decimal columns
// ----------------------------------------------------------------------------

// FloatColumn represents a REAL/DOUBLE PRECISION/DECIMAL column.
type FloatColumn struct{ rawColumn }

func NewFloatColumn(name string) FloatColumn { return FloatColumn{rawColumn: rawColumn(name)} }

func (c FloatColumn) Eq(v float64) Condition {
	return Condition{sql: c.name() + " = $1", args: []any{v}}
}
func (c FloatColumn) Neq(v float64) Condition {
	return Condition{sql: c.name() + " != $1", args: []any{v}}
}
func (c FloatColumn) Gt(v float64) Condition {
	return Condition{sql: c.name() + " > $1", args: []any{v}}
}
func (c FloatColumn) Gte(v float64) Condition {
	return Condition{sql: c.name() + " >= $1", args: []any{v}}
}
func (c FloatColumn) Lt(v float64) Condition {
	return Condition{sql: c.name() + " < $1", args: []any{v}}
}
func (c FloatColumn) Lte(v float64) Condition {
	return Condition{sql: c.name() + " <= $1", args: []any{v}}
}

// ----------------------------------------------------------------------------
// Bool columns
// ----------------------------------------------------------------------------

// BoolColumn represents a BOOLEAN column.
type BoolColumn struct{ rawColumn }

func NewBoolColumn(name string) BoolColumn { return BoolColumn{rawColumn: rawColumn(name)} }

func (c BoolColumn) Eq(v bool) Condition { return Condition{sql: c.name() + " = $1", args: []any{v}} }
func (c BoolColumn) IsTrue() Condition   { return c.Eq(true) }
func (c BoolColumn) IsFalse() Condition  { return c.Eq(false) }

// ----------------------------------------------------------------------------
// Timestamp / Date columns
// ----------------------------------------------------------------------------

// TimestampColumn represents a TIMESTAMP/TIMESTAMPTZ column. Method semantics
// mirror IntColumn but accept any value the driver knows how to bind
// (time.Time, RFC3339 strings, etc.) so callers don't have to choose a
// canonical form here.
type TimestampColumn struct{ rawColumn }

func NewTimestampColumn(name string) TimestampColumn {
	return TimestampColumn{rawColumn: rawColumn(name)}
}

func (c TimestampColumn) Eq(v any) Condition {
	return Condition{sql: c.name() + " = $1", args: []any{v}}
}
func (c TimestampColumn) Gt(v any) Condition {
	return Condition{sql: c.name() + " > $1", args: []any{v}}
}
func (c TimestampColumn) Gte(v any) Condition {
	return Condition{sql: c.name() + " >= $1", args: []any{v}}
}
func (c TimestampColumn) Lt(v any) Condition {
	return Condition{sql: c.name() + " < $1", args: []any{v}}
}
func (c TimestampColumn) Lte(v any) Condition {
	return Condition{sql: c.name() + " <= $1", args: []any{v}}
}

// ----------------------------------------------------------------------------
// UUID columns (treated like strings for identity comparisons)
// ----------------------------------------------------------------------------

// UUIDColumn represents a UUID/text-shaped identity column.
type UUIDColumn struct{ rawColumn }

func NewUUIDColumn(name string) UUIDColumn { return UUIDColumn{rawColumn: rawColumn(name)} }

func (c UUIDColumn) Eq(v string) Condition { return Condition{sql: c.name() + " = $1", args: []any{v}} }
func (c UUIDColumn) Neq(v string) Condition {
	return Condition{sql: c.name() + " != $1", args: []any{v}}
}
func (c UUIDColumn) In(values ...string) Condition {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return inFragment(c.name(), args)
}
