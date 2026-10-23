package entity

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// KeyColumns returns the entity's primary-key columns in declared order.
// Every read, write, cursor, event, audit row and search document names
// a record through these columns, never through PrimaryKey directly, so
// a key of more than one column reaches all of them at once.
func (e *Entity) KeyColumns() []string {
	if e == nil || e.PrimaryKey == "" {
		return []string{"id"}
	}
	return []string{e.PrimaryKey}
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
// never matches a record on a prefix of its key.
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
		if err != nil || v == "" {
			return nil, fmt.Errorf("%w: value %d", ErrBadKey, i+1)
		}
		out[i] = v
	}
	return out, nil
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
