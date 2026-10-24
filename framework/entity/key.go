package entity

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// KeyColumns returns the entity's primary-key columns in declared order:
// EntityConfig.PrimaryKey, or "id" when it is nil. Every read, write,
// cursor, event, audit row and search document names a record through
// these columns.
func (e *Entity) KeyColumns() []string {
	if e == nil {
		return []string{"id"}
	}
	return slices.Clone(e.Config.keyColumns())
}

// ErrCompositeKey reports a relation to or from an entity whose primary
// key is not one column. A relation joins on one key column.
var ErrCompositeKey = errors.New("entity: a relation needs a one-column primary key")

// KeyColumn returns the entity's one primary-key column, the column a
// relation to or from it joins on. An entity keyed on several columns,
// or on none, has no such column: KeyColumn returns ErrCompositeKey.
func (e *Entity) KeyColumn() (string, error) {
	cols := e.KeyColumns()
	if len(cols) != 1 {
		return "", fmt.Errorf("%w: %s is keyed on (%s)", ErrCompositeKey, e.GetName(), strings.Join(cols, ", "))
	}
	return cols[0], nil
}

// ErrBadKey reports a record id that does not split into the entity's
// key columns.
var ErrBadKey = errors.New("entity: malformed record key")

// JoinKey renders a record's key values as the one string the framework
// passes around as its id: in URLs, hooks, events, audit rows, search
// documents and bulk jobs. A single-column key is its value unchanged.
// A longer key path-escapes each value and joins them with "/", so the
// id is also the URL tail: /enrollments/s1/c9.
func JoinKey(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	esc := make([]string, len(parts))
	for i, p := range parts {
		esc[i] = url.PathEscape(p)
	}
	return strings.Join(esc, "/")
}

// SplitKey reverses JoinKey for a key of n columns. It refuses an id
// with the wrong number of values or an empty one, so a truncated URL
// never matches a record on a prefix of its key, and one not spelled as
// JoinKey spells it: "%61/b" names the record "a/b" does, and a check
// keyed on the id string (a Decider, an audit row) must never see a
// second spelling of one record.
func SplitKey(id string, n int) ([]string, error) {
	if n <= 1 {
		return []string{id}, nil
	}
	raw := strings.Split(id, "/")
	if len(raw) != n {
		return nil, fmt.Errorf("%w: want %d values, got %d", ErrBadKey, n, len(raw))
	}
	out := make([]string, n)
	for i, p := range raw {
		v, err := url.PathUnescape(p)
		if err != nil || v == "" || url.PathEscape(v) != p {
			return nil, fmt.Errorf("%w: value %d", ErrBadKey, i+1)
		}
		out[i] = v
	}
	return out, nil
}

// KeyValues reads a record id as the values its key columns hold: one
// per column of cols, each in the type fields declares for it. An Int
// column's value must be canonical decimal and comes back as an int64,
// since both dialects compare "01" and "1.0" equal to 1; every other
// value stays the string SplitKey returned. Every lookup by id goes
// through it, so a check keyed on the id string sees the only spelling
// that reaches the row.
func KeyValues(id string, cols []string, fields []schema.Field) ([]any, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: the table has no key", ErrBadKey)
	}
	parts, err := SplitKey(id, len(cols))
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(parts))
	for i, p := range parts {
		vals[i] = p
		f := slices.IndexFunc(fields, func(f schema.Field) bool { return f.Name == cols[i] })
		if f < 0 || fields[f].Type != schema.Int {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != p {
			return nil, fmt.Errorf("%w: value %d is not a canonical integer", ErrBadKey, i+1)
		}
		vals[i] = n
	}
	return vals, nil
}

// RowKey reads a record's id from a row: the value of each key column,
// joined by JoinKey. names maps a column to the key the row uses for it
// (a camelCase response row, say), tried before the column name; nil
// reads columns as named. It reports false when the row lacks any key
// column.
func RowKey(row map[string]any, cols []string, names func(string) string) (string, bool) {
	parts := make([]string, len(cols))
	for i, c := range cols {
		var v any
		ok := false
		if names != nil {
			v, ok = row[names(c)]
		}
		if !ok {
			v, ok = row[c]
		}
		if !ok || v == nil {
			return "", false
		}
		parts[i] = fmt.Sprint(v)
	}
	return JoinKey(parts), true
}
