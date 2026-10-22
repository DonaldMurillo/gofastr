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

// ErrCompositeKey reports an entity keyed on several columns where one
// is needed: a relation naming one foreign-key column for that key, a
// many_to_many end, or a cascade write's child.
var ErrCompositeKey = errors.New("entity: the key has several columns where one is needed")

// ErrForeignKeyShape reports a relation whose foreign-key columns do not
// line up with the key they reference: a different count, a repeated
// column, or one the entity holding them does not declare.
var ErrForeignKeyShape = errors.New("entity: foreign key does not match the key it references")

// CheckRelationKeys reports whether rel, declared on source, can join to
// target. Its foreign key (Relation.FKColumns) names one column per
// column of the key it references, in that key's order: a belongs_to
// references target's key from columns on source, a has_one or has_many
// references source's key from columns on target. A foreign key of
// several columns names columns its holder declares. So a join table
// keyed on the pair it links belongs to both sides, and an order line
// belongs to an order keyed on (store, no) through two columns.
//
// A many_to_many's pivot holds one link column per side, so both ends
// need a one-column key; a join entity with two belongs_to relations
// links composite-keyed ends instead. A cascade write stamps one
// foreign-key column and tells an existing child from a new one by the
// child's one key column, so it needs both to be one column. A nil end
// is not checked, except that a foreign key of several columns needs
// the key it references.
func CheckRelationKeys(source *Entity, rel Relation, target *Entity) error {
	if fks := rel.FKColumns(); rel.CascadeWrite && len(fks) > 1 {
		return fmt.Errorf("%w: relation %q: a cascade write stamps one foreign-key column, and (%s) are several",
			ErrCompositeKey, rel.Name, strings.Join(fks, ", "))
	}
	switch rel.Type {
	case RelManyToOne:
		return checkForeignKey(rel, source, target)
	case RelHasOne, RelHasMany:
		if err := checkForeignKey(rel, target, source); err != nil {
			return err
		}
		if rel.CascadeWrite && target != nil {
			if _, err := target.KeyColumn(); err != nil {
				return err
			}
		}
	case RelManyToMany:
		if len(rel.ForeignKeys) > 0 {
			return fmt.Errorf("%w: relation %q: a many_to_many links through LocalKey and ForeignKeyTarget, not ForeignKeys",
				ErrForeignKeyShape, rel.Name)
		}
		for _, e := range []*Entity{source, target} {
			if e == nil {
				continue
			}
			if _, err := e.KeyColumn(); err != nil {
				return fmt.Errorf("%w; link it through a join entity that belongs to both sides", err)
			}
		}
	}
	return nil
}

// checkForeignKey checks rel's foreign-key columns, held by holder,
// against the key of ref, the entity they reference.
func checkForeignKey(rel Relation, holder, ref *Entity) error {
	fks := rel.FKColumns()
	if len(fks) > 1 {
		for i, c := range fks {
			if slices.IndexFunc(fks[:i], func(p string) bool { return strings.EqualFold(p, c) }) >= 0 {
				return fmt.Errorf("%w: relation %q names column %q twice", ErrForeignKeyShape, rel.Name, c)
			}
			// Exact, not folded: writes read each column from the body by
			// its field name, so "ORDER_NO" over a field "order_no" would
			// pass the database and skip every write-side check.
			if holder != nil && slices.IndexFunc(holder.GetFields(), func(f schema.Field) bool { return f.Name == c }) < 0 {
				return fmt.Errorf("%w: relation %q: %s declares no field %q", ErrForeignKeyShape, rel.Name, holder.GetName(), c)
			}
			// A transition writes its column with no reference check, so
			// with the caller's other columns it could land on any row.
			if holder != nil && transitionWrites(holder.Config.States, c) {
				return fmt.Errorf("%w: relation %q: %s's transitions write %q, one column of its foreign key (%s)",
					ErrForeignKeyShape, rel.Name, holder.GetName(), c, strings.Join(fks, ", "))
			}
		}
	}
	if ref == nil {
		if len(fks) > 1 {
			return fmt.Errorf("%w: relation %q: the entity its foreign key (%s) references is unknown",
				ErrForeignKeyShape, rel.Name, strings.Join(fks, ", "))
		}
		return nil
	}
	key := ref.KeyColumns()
	n := max(len(fks), 1)
	switch {
	case n == len(key):
		return nil
	case n == 1:
		return fmt.Errorf("%w: %s is keyed on (%s), so relation %q needs one foreign-key column per key column",
			ErrCompositeKey, ref.GetName(), strings.Join(key, ", "), rel.Name)
	default:
		return fmt.Errorf("%w: relation %q names foreign-key columns (%s) and %s is keyed on (%s)",
			ErrForeignKeyShape, rel.Name, strings.Join(fks, ", "), ref.GetName(), strings.Join(key, ", "))
	}
}

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

// ValueFields returns e's fields typed by the values their columns hold.
// A Relation column that is a belongs_to's foreign key to an integer key
// holds that integer, and comes back typed schema.Int: KeyValues then
// refuses its non-canonical spellings, and crud stores it as an integer
// on SQLite too, where the column is TEXT. A target key that is itself
// such a Relation counts as an integer key. Without reg, or when the
// target does not resolve, the fields come back as declared.
func ValueFields(reg Registry, e *Entity) []schema.Field {
	fields := e.GetFields()
	if reg == nil {
		return fields
	}
	out, cloned := fields, false
	for i, f := range fields {
		if f.Type != schema.Relation || f.RawType != "" || !RelationHoldsInt(reg, e, f.Name) {
			continue
		}
		if !cloned {
			out, cloned = slices.Clone(fields), true
		}
		out[i].Type = schema.Int
	}
	return out
}

// RelationHoldsInt reports whether column col of e is a belongs_to's
// foreign key whose target's one-column key holds an integer: an Int
// field, or a Relation that itself holds one.
func RelationHoldsInt(reg Registry, e *Entity, col string) bool {
	return relationHoldsInt(reg, e, col, map[columnRef]bool{})
}

// columnRef names one column of one entity: the cycle guard of a walk
// along relation keys, which may pass through two columns of one entity
// (a self-reference to a key that is itself a reference).
type columnRef struct {
	e   *Entity
	col string
}

func relationHoldsInt(reg Registry, e *Entity, col string, seen map[columnRef]bool) bool {
	ref := columnRef{e, col}
	if seen[ref] {
		return false
	}
	seen[ref] = true
	for _, rel := range e.Config.Relations {
		if rel.Type != RelManyToOne || !strings.EqualFold(rel.ForeignKey, col) {
			continue
		}
		target, err := ResolveTarget(reg, e, rel.Entity)
		if err != nil || target == nil {
			return false
		}
		pk, err := target.KeyColumn()
		if err != nil {
			return false
		}
		fields := target.GetFields()
		j := slices.IndexFunc(fields, func(f schema.Field) bool { return strings.EqualFold(f.Name, pk) })
		if j < 0 {
			return false
		}
		switch fields[j].Type {
		case schema.Int:
			return true
		case schema.Relation:
			return fields[j].RawType == "" && relationHoldsInt(reg, target, fields[j].Name, seen)
		default:
			// Every other key type holds what it says: text, a uuid, a
			// date, never an integer.
			return false
		}
	}
	return false
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
