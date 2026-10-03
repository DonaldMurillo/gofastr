package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestUpdateAllKeepsSuppliedUpdatedAt: a backfill that supplies updated_at
// writes that value instead of dropping it or restamping now().
func TestUpdateAllKeepsSuppliedUpdatedAt(t *testing.T) {
	db := setupDB(t, `CREATE TABLE stamped (id TEXT PRIMARY KEY, title TEXT, created_at TEXT, updated_at TEXT)`)
	ent := entity.Define("stamped", entity.EntityConfig{
		Name: "stamped", Table: "stamped",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(true))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := context.Background()
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "a"}); err != nil {
		t.Fatal(err)
	}
	const want = "2001-02-03T04:05:06Z"
	n, err := NewTypedQuery[covNote](ch).UpdateAll(ctx, map[string]any{"updated_at": want})
	if err != nil || n != 1 {
		t.Fatalf("UpdateAll = %d, %v; want 1 row", n, err)
	}
	var got string
	if err := db.QueryRow(`SELECT updated_at FROM stamped`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("updated_at = %q, want the supplied %q", got, want)
	}
}
