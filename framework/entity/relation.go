package entity

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
	Type             RelationType   `json:"type"`
	Name             string         `json:"name"`                // logical name for this relation (e.g. "author", "comments")
	Entity           string         `json:"entity"`              // target entity/table name
	ForeignKey       string         `json:"foreign_key"`         // FK column name
	Through          string         `json:"through,omitempty"`   // pivot table name (ManyToMany only)
	LocalKey         string         `json:"local_key,omitempty"` // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string         `json:"foreign_key_target,omitempty"`
	OnDelete         OnDeleteAction `json:"on_delete,omitempty"`     // referential action on delete (CASCADE, SET NULL, RESTRICT, NO ACTION)
	CascadeWrite     bool           `json:"cascade_write,omitempty"` // allows atomic parent-child creation/updates
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
// foreign-key column that references the source entity's primary key.
func HasOne(name, ent, foreignKey string) Relation {
	return Relation{
		Type:       RelHasOne,
		Name:       name,
		Entity:     ent,
		ForeignKey: foreignKey,
	}
}

// HasMany declares a one-to-many relationship. The target entity holds a
// foreign-key column that references the source entity's primary key.
func HasMany(name, ent, foreignKey string) Relation {
	return Relation{
		Type:       RelHasMany,
		Name:       name,
		Entity:     ent,
		ForeignKey: foreignKey,
	}
}

// BelongsTo declares a many-to-one relationship. The source entity holds a
// foreign-key column that references the target entity's primary key.
func BelongsTo(name, ent, foreignKey string) Relation {
	return Relation{
		Type:       RelManyToOne,
		Name:       name,
		Entity:     ent,
		ForeignKey: foreignKey,
	}
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
