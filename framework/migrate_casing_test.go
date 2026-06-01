package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestMixedCaseIdentifiers_MigrateThenCrud is the regression guard for the
// quoting bug: a mixed-case column/table must round-trip between the migration
// layer (DDL) and the runtime query layer (crud). If migrate quoted identifiers
// while crud doesn't, Postgres would preserve case in the schema but fold the
// runtime SQL to lowercase → "column does not exist". Both layers must use the
// same (unquoted, case-folding) convention.
func TestMixedCaseIdentifiers_MigrateThenCrud(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ent := entity.Define("Widgets", entity.EntityConfig{
			Table: "Widgets", // mixed-case table
			Fields: []schema.Field{
				{Name: "UserName", Type: schema.String}, // mixed-case column
				{Name: "Score", Type: schema.Int},
			},
		}.WithTimestamps(false))
		reg := NewRegistry()
		reg.Register(ent)
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		// The crud runtime path must be able to write and read these columns.
		ch := crud.NewCrudHandler(ent, db)
		ch.Registry = reg
		row, err := ch.CreateOne(context.Background(), map[string]any{"UserName": "alice", "Score": 7})
		if err != nil {
			t.Fatalf("CreateOne on mixed-case schema: %v", err)
		}
		got, err := ch.ListAll(context.Background(), crud.ListOptions{})
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(got) != 1 || got[0]["UserName"] != "alice" {
			t.Fatalf("mixed-case round-trip failed: created %+v, listed %+v", row, got)
		}
	})
}
