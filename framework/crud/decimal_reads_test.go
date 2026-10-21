package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A Decimal is a string on the wire, the type the OpenAPI schema and every
// generated client give it. SQLite's DECIMAL column has numeric affinity,
// so the driver scans "12.50" back as a float and "0" as an int.
func TestDecimalReadsAsString(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE accounts (id TEXT, mrr DECIMAL(19,4))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (id, mrr) VALUES ('a1', '12.50'), ('a2', '0')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("accounts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "mrr", Type: schema.Decimal}},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, db)
	rows, err := ch.ListAll(context.Background(), ListOptions{Sorts: []filter.ParsedSort{{Field: "id"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"12.5", "0"} {
		if got, ok := rows[i]["mrr"].(string); !ok || got != want {
			t.Errorf("row %d mrr = %#v (%T), want %q", i, rows[i]["mrr"], rows[i]["mrr"], want)
		}
	}
	one, err := ch.GetOne(context.Background(), "a1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := one["mrr"].(string); !ok || got != "12.5" {
		t.Errorf("get mrr = %#v (%T), want \"12.5\"", one["mrr"], one["mrr"])
	}
}

// An included row is decoded by its own entity's fields.
func TestIncludedDecimalReadsAsString(t *testing.T) {
	db := openBooleanReadDB(t)
	for _, stmt := range []string{
		`CREATE TABLE orders (id TEXT PRIMARY KEY)`,
		`CREATE TABLE lines (id TEXT PRIMARY KEY, order_id TEXT, amount DECIMAL(19,4))`,
		`INSERT INTO orders (id) VALUES ('o1')`,
		`INSERT INTO lines (id, order_id, amount) VALUES ('l1', 'o1', '7.25')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	lines := entity.Define("lines", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order_id", Type: schema.String},
			{Name: "amount", Type: schema.Decimal},
		},
	}.WithTimestamps(false))
	orders := entity.Define("orders", entity.EntityConfig{
		Relations: []entity.Relation{entity.HasMany("lines", "lines", "order_id")},
	}.WithTimestamps(false))
	orders.SetDB(db)
	ch := NewCrudHandler(orders, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"orders": orders, "lines": lines}}
	got, err := ch.GetOne(context.Background(), "o1", []string{"lines"})
	if err != nil {
		t.Fatal(err)
	}
	included, _ := got["lines"].([]map[string]any)
	if len(included) != 1 {
		t.Fatalf("lines = %#v, want one line", got["lines"])
	}
	if amount, ok := included[0]["amount"].(string); !ok || amount != "7.25" {
		t.Errorf("amount = %#v (%T), want \"7.25\"", included[0]["amount"], included[0]["amount"])
	}
}

// A group's value is the value a list read carries for the same column.
func TestGroupCountDecimalIsString(t *testing.T) {
	db := openBooleanReadDB(t)
	for _, stmt := range []string{
		`CREATE TABLE prices (id TEXT PRIMARY KEY, amount DECIMAL(19,4))`,
		`INSERT INTO prices (id, amount) VALUES ('a', '9.99'), ('b', '9.99')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	ent := entity.Define("prices", entity.EntityConfig{
		Fields: []schema.Field{{Name: "amount", Type: schema.Decimal}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	groups, err := NewCrudHandler(ent, db).GroupCountAll(context.Background(), "amount", ListOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Value != "9.99" || groups[0].Count != 2 {
		t.Errorf("groups = %#v, want one group \"9.99\" of 2", groups)
	}
}
