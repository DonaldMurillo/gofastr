package entity

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Condition is a where-clause fragment, numbered from $1, and its bound arguments.
type Condition struct {
	sql        string
	args       []any
	timestamps []string
}

// Apply appends this condition to the query builder.
func (c Condition) Apply(qb *query.QueryBuilder) {
	qb.Where(c.sql, c.args...)
}

// SQL returns the where-clause fragment.
func (c Condition) SQL() string { return c.sql }

// Args returns the bound arguments for the fragment.
func (c Condition) Args() []any { return c.args }

// ComparedTimestamps names the TimestampColumn columns the fragment compares to a value.
func (c Condition) ComparedTimestamps() []string { return c.timestamps }

// Order is an order-by-clause fragment.
type Order struct {
	column string
	dir    string
}

// Apply appends this order to the query builder.
func (o Order) Apply(qb *query.QueryBuilder) {
	qb.Order(o.column, o.dir)
}

// Term is the order's raw column and its direction, "ASC" or "DESC".
func (o Order) Term() (column, dir string) { return o.column, o.dir }

type rawColumn string

func (c rawColumn) name() string { return query.QuoteKeyword(string(c)) }
func (c rawColumn) Asc() Order   { return Order{column: string(c), dir: "ASC"} }
func (c rawColumn) Desc() Order  { return Order{column: string(c), dir: "DESC"} }
func (c rawColumn) IsNull() Condition {
	return Condition{sql: c.name() + " IS NULL"}
}
func (c rawColumn) IsNotNull() Condition {
	return Condition{sql: c.name() + " IS NOT NULL"}
}

// And combines conditions with AND; none is "1 = 1".
func And(conds ...Condition) Condition {
	return combineConditions(conds, " AND ", "1 = 1")
}

// Or combines conditions with OR; none is "1 = 0".
func Or(conds ...Condition) Condition {
	return combineConditions(conds, " OR ", "1 = 0")
}

func combineConditions(conds []Condition, sep, emptySQL string) Condition {
	if len(conds) == 0 {
		return Condition{sql: emptySQL}
	}
	if len(conds) == 1 {
		return conds[0]
	}
	parts := make([]string, 0, len(conds))
	var args []any
	var timestamps []string
	for _, c := range conds {
		parts = append(parts, c.sql)
		args = append(args, c.args...)
		timestamps = append(timestamps, c.timestamps...)
	}
	return Condition{
		sql:        "(" + strings.Join(parts, sep) + ")",
		args:       args,
		timestamps: timestamps,
	}
}

// Not wraps a condition in NOT (...).
func Not(c Condition) Condition {
	return Condition{sql: "NOT (" + c.sql + ")", args: c.args, timestamps: c.timestamps}
}

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

// StringColumn is a TEXT column of the typed query builder.
type StringColumn struct{ rawColumn }

// NewStringColumn returns the StringColumn for a DB column name.
func NewStringColumn(name string) StringColumn { return StringColumn{rawColumn: rawColumn(name)} }

func (c StringColumn) Eq(v string) Condition {
	return Condition{sql: c.name() + " = $1", args: []any{v}}
}
func (c StringColumn) Neq(v string) Condition {
	return Condition{sql: c.name() + " != $1", args: []any{v}}
}

// Like matches a SQL pattern with ASCII case folding and backslash escapes.
func (c StringColumn) Like(pattern string) Condition {
	return c.likePattern(" LIKE ", pattern)
}

// NotLike matches non-NULL values that do not match Like.
func (c StringColumn) NotLike(pattern string) Condition {
	return c.likePattern(" NOT LIKE ", pattern)
}

func (c StringColumn) likePattern(operator, pattern string) Condition {
	var expr strings.Builder
	expr.WriteString(strings.Repeat("replace(", 26))
	expr.WriteString(c.name())
	for upper := 'A'; upper <= 'Z'; upper++ {
		fmt.Fprintf(&expr, ", '%c', '%c')", upper, upper+'a'-'A')
	}
	expr.WriteString(operator)
	expr.WriteString("$1 ESCAPE $2")
	if (len(pattern)-len(strings.TrimRight(pattern, `\`)))%2 != 0 {
		pattern += `\`
	}
	return Condition{sql: expr.String(), args: []any{asciicase.Lower(pattern), `\`}}
}

func (c StringColumn) In(values ...string) Condition {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return inFragment(c.name(), args)
}

// EnumColumn is an enum column compared with typed constants of T.
type EnumColumn[T ~string] struct{ rawColumn }

// NewEnumColumn returns the EnumColumn for a DB column name.
func NewEnumColumn[T ~string](name string) EnumColumn[T] {
	return EnumColumn[T]{rawColumn: rawColumn(name)}
}

func (c EnumColumn[T]) Eq(v T) Condition {
	return Condition{sql: c.name() + " = $1", args: []any{string(v)}}
}
func (c EnumColumn[T]) Neq(v T) Condition {
	return Condition{sql: c.name() + " != $1", args: []any{string(v)}}
}
func (c EnumColumn[T]) In(values ...T) Condition {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = string(v)
	}
	return inFragment(c.name(), args)
}

// IntColumn is an INTEGER column.
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

// FloatColumn is a REAL, DOUBLE PRECISION or DECIMAL column.
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

// BoolColumn is a BOOLEAN column.
type BoolColumn struct{ rawColumn }

func NewBoolColumn(name string) BoolColumn { return BoolColumn{rawColumn: rawColumn(name)} }

func (c BoolColumn) Eq(v bool) Condition { return Condition{sql: c.name() + " = $1", args: []any{v}} }
func (c BoolColumn) IsTrue() Condition   { return c.Eq(true) }
func (c BoolColumn) IsFalse() Condition  { return c.Eq(false) }

// TimestampColumn is a TIMESTAMP column. A time.Time, *time.Time or RFC 3339
// string binds in schema.TimestampLayout; anything else binds as given.
type TimestampColumn struct{ rawColumn }

func NewTimestampColumn(name string) TimestampColumn {
	return TimestampColumn{rawColumn: rawColumn(name)}
}

func (c TimestampColumn) Eq(v any) Condition  { return c.compare(" = $1", v) }
func (c TimestampColumn) Gt(v any) Condition  { return c.compare(" > $1", v) }
func (c TimestampColumn) Gte(v any) Condition { return c.compare(" >= $1", v) }
func (c TimestampColumn) Lt(v any) Condition  { return c.compare(" < $1", v) }
func (c TimestampColumn) Lte(v any) Condition { return c.compare(" <= $1", v) }

func (c TimestampColumn) compare(op string, v any) Condition {
	return Condition{sql: c.name() + op, args: []any{schema.TimestampArg(v)}, timestamps: []string{string(c.rawColumn)}}
}

// UUIDColumn is a UUID or text identity column.
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
