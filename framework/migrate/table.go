package migrate

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Non-entity table path
//
// Some tables don't want the entity machinery — no auto-CRUD, no HTTP routes,
// no validation, no auto-injected id/timestamps/tenant columns. Join tables,
// analytics roll-ups, tables owned by stored procedures, or just a user who is
// stubborn about declaring entities. Table is a raw schema declaration for
// exactly those. It carries ONLY the columns you write — nothing is injected.
//
// A Table reconciles with entities because ToEntity adapts it into the same
// *entity.Entity the migration engine already consumes: register both in one
// registry and AutoMigrate / DiffSchema / GenerateMigration treat them
// uniformly, including foreign keys that cross between an entity and a Table.
type Table struct {
	Name        string  // table name; also the registry key for FK references
	Columns     []Column // exactly the columns emitted — no auto-injection
	Indices     []Index
	ForeignKeys []ForeignKey
}

// Index is the secondary-index declaration for a raw Table, aliased from the
// entity package so raw-table users don't need to import it.
type Index = entity.Index

// Column is one column of a raw Table.
type Column struct {
	Name         string              // column name
	Type         schema.FieldType    // portable type; ignored when RawType is set
	RawType      string              // explicit SQL type, overrides Type (e.g. "NUMERIC(10,2)")
	NotNull      bool                // emit NOT NULL
	Unique       bool                // emit UNIQUE
	PrimaryKey   bool                // single-column PRIMARY KEY marker
	Default      any                 // default value (rendered via the same SQLDefault path)
	AutoGenerate schema.AutoGenerate // e.g. AutoUUID → DEFAULT gen_random_uuid() on Postgres
}

// ForeignKey declares a foreign key from a Table column to another table's
// primary key. RefTable is the registered name of the target (an entity name
// or another Table's Name); the reference targets that table's primary key.
type ForeignKey struct {
	Column   string // local column
	RefTable string // target table/entity name (references its primary key)
}

// ToEntity adapts the raw Table into an *entity.Entity with no auto-injection,
// suitable for registering alongside real entities. Mark exactly one column
// PrimaryKey for FK targets; a table with no primary key is allowed but cannot
// be referenced by a foreign key.
func (t Table) ToEntity() *entity.Entity {
	fields := make([]schema.Field, 0, len(t.Columns))
	pk := ""
	pkCount := 0
	for _, c := range t.Columns {
		fields = append(fields, schema.Field{
			Name:         c.Name,
			Type:         c.Type,
			RawType:      c.RawType,
			Required:     c.NotNull,
			Unique:       c.Unique,
			Default:      c.Default,
			AutoGenerate: c.AutoGenerate,
		})
		if c.PrimaryKey {
			pk = c.Name
			pkCount++
		}
	}
	// Fail loud rather than silently keeping only the last PK column.
	if pkCount > 1 {
		panic(fmt.Sprintf("migrate: table %q marks %d columns PrimaryKey — composite primary keys are not supported on a raw Table; use a single PK plus a Unique index", t.Name, pkCount))
	}
	var relations []entity.Relation
	for _, fk := range t.ForeignKeys {
		relations = append(relations, entity.Relation{
			Type:       entity.RelManyToOne,
			Name:       fk.Column,
			Entity:     fk.RefTable,
			ForeignKey: fk.Column,
		})
	}
	ent := &entity.Entity{Config: entity.EntityConfig{
		Name:      t.Name,
		Table:     t.Name,
		Fields:    fields,
		Indices:   t.Indices,
		Relations: relations,
		// Raw table: timestamps / soft-delete / multi-tenant all off, so the
		// diff engine treats every column as user-owned (no managed-column
		// skip) — exactly what a raw table wants.
	}}
	ent.PrimaryKey = pk
	return ent
}
