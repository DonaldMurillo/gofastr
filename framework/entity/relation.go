package entity

import "slices"

// RelationType enumerates the kinds of entity relationships.
type RelationType int

const (
	RelHasOne     RelationType = iota // target has a FK pointing back to us
	RelHasMany                        // target has a FK pointing back to us (many rows)
	RelManyToOne                      // we hold a FK pointing to the target (BelongsTo)
	RelManyToMany                     // linked through a pivot/join table
)

// OnDeleteAction defines the referential action when a referenced row is deleted.
type OnDeleteAction string

const (
	OnDeleteNoAction OnDeleteAction = "NO ACTION"
	OnDeleteRestrict OnDeleteAction = "RESTRICT"
	OnDeleteCascade  OnDeleteAction = "CASCADE"
	OnDeleteSetNull  OnDeleteAction = "SET NULL"
)

// Relation describes a relationship between two entities.
type Relation struct {
	Type       RelationType `json:"type"`
	Name       string       `json:"name"`        // logical name for this relation (e.g. "author", "comments")
	Entity     string       `json:"entity"`      // target entity/table name
	ForeignKey string       `json:"foreign_key"` // FK column name
	// ForeignKeys names the foreign key's columns when the key it
	// references has several, in that key's column order; ForeignKey is
	// empty then. FKColumns reads either spelling.
	ForeignKeys      []string       `json:"foreign_keys,omitempty"`
	Through          string         `json:"through,omitempty"`   // pivot table name (ManyToMany only)
	LocalKey         string         `json:"local_key,omitempty"` // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string         `json:"foreign_key_target,omitempty"`
	OnDelete         OnDeleteAction `json:"on_delete,omitempty"`     // referential action on delete (CASCADE, SET NULL, RESTRICT, NO ACTION)
	CascadeWrite     bool           `json:"cascade_write,omitempty"` // allows atomic parent-child creation/updates
}

// FKColumns returns the relation's foreign-key columns: ForeignKeys, or
// ForeignKey alone. A many_to_many, whose links live in its pivot, has
// none.
func (r Relation) FKColumns() []string {
	if len(r.ForeignKeys) > 0 {
		return slices.Clone(r.ForeignKeys)
	}
	if r.ForeignKey == "" {
		return nil
	}
	return []string{r.ForeignKey}
}

// foreignKeys spells a foreign key the way Relation holds it: one column
// in ForeignKey, several in ForeignKeys.
func foreignKeys(r Relation, first string, more []string) Relation {
	if len(more) == 0 {
		r.ForeignKey = first
		return r
	}
	r.ForeignKeys = append([]string{first}, more...)
	return r
}

// OnDeleteAction sets the ON DELETE referential action.
func (r Relation) OnDeleteAction(action OnDeleteAction) Relation {
	r.OnDelete = action
	return r
}

// OnDeleteCascade configures the relation foreign key with ON DELETE CASCADE.
func (r Relation) OnDeleteCascade() Relation {
	r.OnDelete = OnDeleteCascade
	return r
}

// Cascade enables atomic cascade writes for this relation.
func (r Relation) Cascade() Relation {
	r.CascadeWrite = true
	return r
}

// WithCascadeWrite sets whether cascade writes are enabled.
func (r Relation) WithCascadeWrite(enable bool) Relation {
	r.CascadeWrite = enable
	return r
}

// HasOne declares a one-to-one relationship. The target entity holds a
// foreign-key column that references the source entity's primary key, or
// one column per key column when that key has several (more).
func HasOne(name, ent, foreignKey string, more ...string) Relation {
	return foreignKeys(Relation{Type: RelHasOne, Name: name, Entity: ent}, foreignKey, more)
}

// HasMany declares a one-to-many relationship. The target entity holds a
// foreign-key column that references the source entity's primary key, or
// one column per key column when that key has several (more).
func HasMany(name, ent, foreignKey string, more ...string) Relation {
	return foreignKeys(Relation{Type: RelHasMany, Name: name, Entity: ent}, foreignKey, more)
}

// BelongsTo declares a many-to-one relationship. The source entity holds a
// foreign-key column that references the target entity's primary key, or
// one column per key column when that key has several (more):
// BelongsTo("order", "order", "order_store", "order_no") references an
// order keyed on (store, no).
func BelongsTo(name, ent, foreignKey string, more ...string) Relation {
	return foreignKeys(Relation{Type: RelManyToOne, Name: name, Entity: ent}, foreignKey, more)
}

// ManyToMany declares a many-to-many relationship through a pivot/join table.
func ManyToMany(name, ent, throughTable, sourceFK, targetFK string) Relation {
	return Relation{
		Type:             RelManyToMany,
		Name:             name,
		Entity:           ent,
		Through:          throughTable,
		LocalKey:         sourceFK,
		ForeignKeyTarget: targetFK,
	}
}
