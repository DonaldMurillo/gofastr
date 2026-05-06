package framework

// RelationType enumerates the kinds of entity relationships.
type RelationType int

const (
	RelHasOne    RelationType = iota // target has a FK pointing back to us
	RelHasMany                       // target has a FK pointing back to us (many rows)
	RelManyToOne                     // we hold a FK pointing to the target (BelongsTo)
	RelManyToMany                    // linked through a pivot/join table
)

// Relation describes a relationship between two entities.
type Relation struct {
	Type             RelationType
	Name             string // logical name for this relation (e.g. "author", "comments")
	Entity           string // target entity/table name
	ForeignKey       string // FK column name
	Through          string // pivot table name (ManyToMany only)
	LocalKey         string // column on the local side of a ManyToMany pivot
	ForeignKeyTarget string // column on the target side of a ManyToMany pivot
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
