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

// OnDeleteAction is the referential action when a referenced row is deleted.
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
	// ForeignKey names one column per column of the referenced key, in its order. A many_to_many has none.
	ForeignKey       []string       `json:"foreign_key,omitempty"`
	Through          string         `json:"through,omitempty"`   // pivot table name (ManyToMany only)
	LocalKey         string         `json:"local_key,omitempty"` // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string         `json:"foreign_key_target,omitempty"`
	OnDelete         OnDeleteAction `json:"on_delete,omitempty"`     // referential action on delete (CASCADE, SET NULL, RESTRICT, NO ACTION)
	CascadeWrite     bool           `json:"cascade_write,omitempty"` // allows atomic parent-child creation/updates
	// Hidden keeps the relation off every read surface: ?include=, nested filters, OpenAPI and llm.md.
	Hidden bool `json:"hidden,omitempty"`
}

// OnDeleteAction sets the ON DELETE referential action.
func (r Relation) OnDeleteAction(action OnDeleteAction) Relation {
	r.OnDelete = action
	return r
}

// OnDeleteCascade sets ON DELETE CASCADE.
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

// HasOne declares a one-to-one relation; the target holds the foreign key, one column per source key column.
func HasOne(name, ent, foreignKey string, more ...string) Relation {
	return Relation{Type: RelHasOne, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// HasMany declares a one-to-many relation; the target holds the foreign key, one column per source key column.
func HasMany(name, ent, foreignKey string, more ...string) Relation {
	return Relation{Type: RelHasMany, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// BelongsTo declares a many-to-one relation; the source holds the foreign key, one column per target key column.
func BelongsTo(name, ent, foreignKey string, more ...string) Relation {
	return Relation{Type: RelManyToOne, Name: name, Entity: ent, ForeignKey: append([]string{foreignKey}, more...)}
}

// HidesRelation reports whether rel is hidden from reads: Hidden, named after a Hidden
// field, or a belongs_to whose foreign key holds a Hidden or NoQuery column.
func (e *Entity) HidesRelation(rel Relation) bool {
	if rel.Hidden {
		return true
	}
	for _, f := range e.Config.Fields {
		holdsKey := rel.Type == RelManyToOne && slices.Contains(rel.ForeignKey, f.Name)
		if f.Hidden && (f.Name == rel.Name || holdsKey) {
			return true
		}
		if f.NoQuery && holdsKey {
			return true
		}
	}
	return false
}

// ManyToMany declares a many-to-many relation through a pivot table.
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
