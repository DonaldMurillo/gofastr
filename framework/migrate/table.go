package migrate

import (
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Table is a raw schema declaration: exactly the columns written, with no
// injected id, timestamps or tenant column and no CRUD. ToEntity adapts it
// so it migrates beside entities, foreign keys between the two included.
type Table struct {
	Name        string   // table name; also the registry key for FK references
	Columns     []Column // exactly the columns emitted, no auto-injection
	Indices     []Index
	ForeignKeys []ForeignKey
}

// Index is the secondary-index declaration for a raw Table.
type Index = entity.Index

// Column is one column of a raw Table.
type Column struct {
	Name         string              // column name
	Type         schema.FieldType    // portable type; ignored when RawType is set
	RawType      string              // explicit SQL type, overrides Type (e.g. "NUMERIC(10,2)")
	NotNull      bool                // emit NOT NULL
	Unique       bool                // emit UNIQUE
	PrimaryKey   bool                // a primary-key column; several make a composite key, in column order
	Default      any                 // default value (rendered via the same SQLDefault path)
	AutoGenerate schema.AutoGenerate // e.g. AutoUUID → DEFAULT gen_random_uuid() on Postgres
}

// ForeignKey declares a foreign key from Table columns to the primary key
// of RefTable, a registered entity or Table name.
type ForeignKey struct {
	// Columns names the local columns, one per key column of RefTable in
	// key order: []string{"user_id"}, or two for a table keyed on two.
	Columns  []string
	RefTable string // target table/entity name (references its primary key)
}

// ToEntity adapts the Table into an *entity.Entity with nothing injected
// and CRUD off. The PrimaryKey columns make the key, in column order; a
// table with no key is allowed but cannot be referenced.
func (t Table) ToEntity() *entity.Entity {
	fields := make([]schema.Field, 0, len(t.Columns))
	pk := []string{}
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
			pk = append(pk, c.Name)
		}
	}
	var relations []entity.Relation
	for _, fk := range t.ForeignKeys {
		relations = append(relations, entity.Relation{
			Type:       entity.RelManyToOne,
			Name:       strings.Join(fk.Columns, "_"),
			Entity:     fk.RefTable,
			ForeignKey: slices.Clone(fk.Columns),
		})
	}
	timestamps, crud := false, false
	ent := &entity.Entity{Config: entity.EntityConfig{
		Name:       t.Name,
		Table:      t.Name,
		Fields:     fields,
		PrimaryKey: pk,
		Indices:    t.Indices,
		Relations:  relations,
		Timestamps: &timestamps,
		Scope:      &entity.ScopeConfig{},
		Pagination: &entity.PaginationConfig{},
		Exposure:   &entity.ExposureConfig{CRUD: &crud},
	}}
	return ent
}
