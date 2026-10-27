package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// An upsert with nothing to write inserts a row of defaults. With the
// auto-increment key omitted and no other column, it rendered
// INSERT INTO t () VALUES (), which no database accepts.
func TestUpsertEmptyBodyInsertsDefaults(t *testing.T) {
	cfg := makeEntityConfig("counter_items", "counter_items", "", []schema.Field{
		{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
		{Name: "title", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE counter_items (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT)`)
	row, err := ch.UpsertOne(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("upsert of an empty body: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM counter_items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 || row["id"] == nil {
		t.Errorf("rows = %d, returned %v; want one new row with its id", n, row)
	}
}
