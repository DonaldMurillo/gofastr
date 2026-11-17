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

// KeyColumns returns a copy of the key columns in order: PrimaryKey, or "id".
func (e *Entity) KeyColumns() []string {
	if e == nil {
		return []string{"id"}
	}
	return slices.Clone(e.Config.keyColumns())
}

// ErrCompositeKey reports an entity keyed on several columns where one is needed.
var ErrCompositeKey = errors.New("entity: the key has several columns where one is needed")

// ErrForeignKeyShape reports foreign-key columns that do not line up with the key they reference.
var ErrForeignKeyShape = errors.New("entity: foreign key does not match the key it references")

// CheckRelationKeys reports whether rel, declared on source, can join to target: one
// foreign-key column per referenced key column. A nil end is not checked.
func CheckRelationKeys(source *Entity, rel Relation, target *Entity) error {
	if fks := rel.ForeignKey; rel.CascadeWrite && len(fks) > 1 {
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
		if len(rel.ForeignKey) > 1 {
			return fmt.Errorf("%w: relation %q: a many_to_many links through LocalKey and ForeignKeyTarget, not ForeignKey",
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

func checkForeignKey(rel Relation, holder, ref *Entity) error {
	fks := rel.ForeignKey
	if len(fks) > 1 {
		for i, c := range fks {
			if slices.IndexFunc(fks[:i], func(p string) bool { return strings.EqualFold(p, c) }) >= 0 {
				return fmt.Errorf("%w: relation %q names column %q twice", ErrForeignKeyShape, rel.Name, c)
			}
			if holder != nil && slices.IndexFunc(holder.GetFields(), func(f schema.Field) bool { return f.Name == c }) < 0 {
				return fmt.Errorf("%w: relation %q: %s declares no field %q", ErrForeignKeyShape, rel.Name, holder.GetName(), c)
			}
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

// KeyColumn returns the entity's one key column, or ErrCompositeKey.
func (e *Entity) KeyColumn() (string, error) {
	cols := e.KeyColumns()
	if len(cols) != 1 {
		return "", fmt.Errorf("%w: %s is keyed on (%s)", ErrCompositeKey, e.GetName(), strings.Join(cols, ", "))
	}
	return cols[0], nil
}

// ErrBadKey reports a record id that does not split into the key's columns.
var ErrBadKey = errors.New("entity: malformed record key")

// JoinKey renders key values as a record id: one value as is, several path-escaped and joined with "/".
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

// SplitKey reverses JoinKey for n columns, refusing a wrong count, an empty
// value or any spelling JoinKey would not produce.
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

// ValueFields returns e's fields with each RelationHoldsInt column typed schema.Int.
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

// RelationHoldsInt reports whether column col of e is a belongs_to foreign key
// whose target's one-column key holds an integer, through chains of such keys.
func RelationHoldsInt(reg Registry, e *Entity, col string) bool {
	return relationHoldsInt(reg, e, col, map[columnRef]bool{})
}

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
		if rel.Type != RelManyToOne || len(rel.ForeignKey) != 1 || !strings.EqualFold(rel.ForeignKey[0], col) {
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
			return false
		}
	}
	return false
}

// KeyValues splits id into one value per column of cols: an Int column's value
// must be canonical decimal and comes back int64, others stay strings.
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

// RowKey returns a row's record id, trying names(col) before col; false when one is missing.
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

// CanHoldNull reports whether f's column can hold NULL: not a key column, not Required and not generated.
func CanHoldNull(f schema.Field, keyCols []string) bool {
	return !slices.Contains(keyCols, f.Name) && !f.Required && f.AutoGenerate == schema.AutoNone
}
