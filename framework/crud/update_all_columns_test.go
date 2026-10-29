package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// UpdateAll took its keys through unconvertMapKeys, which reads line2, the
// key MarshalEntity derives for a Line2 field, as no column, and the
// updated_at restamp then counted as a write: the call reported the rows
// it touched having written only the timestamp (2026-10-27 D5). It now
// names columns the way CreateOne and UpdateOne do, and a body that sets
// nothing is refused as doUpdate refuses it.
func TestUpdateAllNamesColumnsLikeUpdateOne(t *testing.T) {
	db := setupDB(t, `CREATE TABLE addrs (id TEXT PRIMARY KEY, line_2 TEXT, created_at TEXT, updated_at TEXT)`)
	ent := entity.Define("addrs", entity.EntityConfig{
		Name: "addrs", Table: "addrs",
		Fields: []schema.Field{{Name: "line_2", Type: schema.String}},
	}.WithTimestamps(true))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := context.Background()
	if _, err := ch.CreateOne(ctx, map[string]any{"line_2": "old"}); err != nil {
		t.Fatal(err)
	}
	stamp := func() string {
		var s string
		if err := db.QueryRow(`SELECT updated_at FROM addrs`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if _, err := db.Exec(`UPDATE addrs SET updated_at = 'before'`); err != nil {
		t.Fatal(err)
	}

	body, err := MarshalEntity(&struct {
		Line2 string `json:"line2"`
	}{Line2: "new"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := NewTypedQuery[covNote](ch).UpdateAll(ctx, body)
	if err != nil || n != 1 {
		t.Fatalf("UpdateAll(%v) = %d, %v; want 1 row", body, n, err)
	}
	var got string
	if err := db.QueryRow(`SELECT line_2 FROM addrs`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "new" {
		t.Fatalf("line_2 = %q after UpdateAll(%v), want new", got, body)
	}
	if _, kept := body["line2"]; !kept || len(body) != 1 {
		t.Fatalf("UpdateAll rewrote the caller's map: %v", body)
	}

	if _, err := db.Exec(`UPDATE addrs SET updated_at = 'before'`); err != nil {
		t.Fatal(err)
	}
	n, err = NewTypedQuery[covNote](ch).UpdateAll(ctx, map[string]any{"no_such_field": "x"})
	if !errors.Is(err, errNoFieldsToUpdate) {
		t.Fatalf("UpdateAll of a body that sets nothing = %d, %v; want errNoFieldsToUpdate", n, err)
	}
	if s := stamp(); s != "before" {
		t.Fatalf("a refused UpdateAll restamped updated_at to %q", s)
	}
}
