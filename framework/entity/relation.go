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
	Type   RelationType `json:"type"`
	Name   string       `json:"name"`   // logical name for this relation (e.g. "author", "comments")
	Entity string       `json:"entity"` // target entity/table name
	// ForeignKey names the foreign key's columns, one per column of the
	// key it references, in that key's order: []string{"author_id"}, or
	// []string{"order_store", "order_no"} for an order keyed on two
	// columns. A many_to_many, whose links live in its pivot, has none.
	ForeignKey       []string       `json:"foreign_key,omitempty"`
	Through          string         `json:"through,omitempty"`   // pivot table name (ManyToMany only)
	LocalKey         string         `json:"local_key,omitempty"` // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string         `json:"foreign_key_target,omitempty"`
	OnDelete         OnDeleteAction `json:"on_delete,omitempty"`     // referential action on delete (CASCADE, SET NULL, RESTRICT, NO ACTION)
	CascadeWrite     bool           `json:"cascade_write,omitempty"` // allows atomic parent-child creation/updates
	// Hidden keeps the relation out of every read surface: ?include= and
	// nested filters answer it as a relation that does not exist, and
	// /openapi.json and llm.md leave it out. It is the opt-out for a
	// relation no Hidden field of this entity covers, such as a has_many
	// whose foreign key is Hidden on the target: users has_many notes
	// through a Hidden notes.owner_ref still loads a user's notes unless
	// the relation sets Hidden. See HidesRelation.
	Hidden bool `json:"hidden,omitempty"`
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
	return Relation{Type: RelHasOne, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// HasMany declares a one-to-many relationship. The target entity holds a
// foreign-key column that references the source entity's primary key, or
// one column per key column when that key has several (more).
func HasMany(name, ent, foreignKey string, more ...string) Relation {
	return Relation{Type: RelHasMany, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// BelongsTo declares a many-to-one relationship. The source entity holds a
// foreign-key column that references the target entity's primary key, or
// one column per key column when that key has several (more):
// BelongsTo("order", "order", "order_store", "order_no") references an
// order keyed on (store, no).
func BelongsTo(name, ent, foreignKey string, more ...string) Relation {
	return Relation{Type: RelManyToOne, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// HidesRelation reports whether rel is hidden from reads: it is marked
// Relation.Hidden, or it sits behind a Hidden field of e (it is named
// after one, as the belongs_to a Hidden relation field derives, or it is a
// belongs_to whose foreign key holds a Hidden column). Loading such
// a relation puts the related record, and with it the hidden key, under
// the relation's name, so crud answers ?include= and nested filters on it
// as it answers a relation that does not exist, and neither /openapi.json
// nor llm.md names it.
func (e *Entity) HidesRelation(rel Relation) bool {
	if rel.Hidden {
		return true
	}
	for _, f := range e.Config.Fields {
		if !f.Hidden {
			continue
		}
		if f.Name == rel.Name || (rel.Type == RelManyToOne && slices.Contains(rel.ForeignKey, f.Name)) {
			return true
		}
	}
	return false
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
