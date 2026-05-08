package framework

// RelationType enumerates the kinds of entity relationships.
type RelationType int

const (
	RelHasOne     RelationType = iota // target has a FK pointing back to us
	RelHasMany                        // target has a FK pointing back to us (many rows)
	RelManyToOne                      // we hold a FK pointing to the target (BelongsTo)
	RelManyToMany                     // linked through a pivot/join table
)

// Relation describes a relationship between two entities.
type Relation struct {
	Type             RelationType `json:"type"`
	Name             string       `json:"name"`                // logical name for this relation (e.g. "author", "comments")
	Entity           string       `json:"entity"`              // target entity/table name
	ForeignKey       string       `json:"foreign_key"`         // FK column name
	Through          string       `json:"through,omitempty"`   // pivot table name (ManyToMany only)
	LocalKey         string       `json:"local_key,omitempty"` // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string       `json:"foreign_key_target,omitempty"`
}

// HasOne declares a one-to-one relationship. The target entity holds a
// foreign-key column that references the source entity's primary key.
func HasOne(name, entity, foreignKey string) Relation {
	return Relation{
		Type:       RelHasOne,
		Name:       name,
		Entity:     entity,
		ForeignKey: foreignKey,
	}
}

// HasMany declares a one-to-many relationship. The target entity holds a
// foreign-key column that references the source entity's primary key.
func HasMany(name, entity, foreignKey string) Relation {
	return Relation{
		Type:       RelHasMany,
		Name:       name,
		Entity:     entity,
		ForeignKey: foreignKey,
	}
}

// BelongsTo declares a many-to-one relationship. The source entity holds a
// foreign-key column that references the target entity's primary key.
func BelongsTo(name, entity, foreignKey string) Relation {
	return Relation{
		Type:       RelManyToOne,
		Name:       name,
		Entity:     entity,
		ForeignKey: foreignKey,
	}
}

// ManyToMany declares a many-to-many relationship through a pivot/join table.
func ManyToMany(name, entity, throughTable, sourceFK, targetFK string) Relation {
	return Relation{
		Type:             RelManyToMany,
		Name:             name,
		Entity:           entity,
		Through:          throughTable,
		LocalKey:         sourceFK,
		ForeignKeyTarget: targetFK,
	}
}
