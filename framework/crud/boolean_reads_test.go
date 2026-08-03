package crud

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

type booleanReadRegistry struct {
	entities map[string]*entity.Entity
}

func (r booleanReadRegistry) All() map[string]*entity.Entity { return r.entities }

func (r booleanReadRegistry) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r.entities))
	for _, ent := range r.entities {
		out = append(out, ent)
	}
	return out
}

func (r booleanReadRegistry) Get(name string) (*entity.Entity, error) {
	return r.entities[name], nil
}

func openBooleanReadDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestListAllNormalizesBooleanSchemaForModernc(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE flags (id TEXT, active INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO flags (id, active) VALUES ('f1', 1)`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("flags", entity.EntityConfig{
		Fields: []schema.Field{{Name: "active", Type: schema.Bool}},
	}.WithTimestamps(false))
	rows, err := NewCrudHandler(ent, db).ListAll(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := rows[0]["active"].(bool); !ok || !got {
		t.Fatalf("active = %#v (%T), want true bool", rows[0]["active"], rows[0]["active"])
	}
}

func TestEagerLoadNormalizesBooleanSchemaForModernc(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE children (id TEXT, parent_id TEXT, active INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO children (id, parent_id, active) VALUES ('c1', 'p1', 1)`); err != nil {
		t.Fatal(err)
	}

	parent := entity.Define("parents", entity.EntityConfig{}.WithTimestamps(false))
	child := entity.Define("children", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "parent_id", Type: schema.UUID},
			{Name: "active", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	relations := []entity.Relation{entity.HasMany("children", "children", "parent_id")}
	reg := booleanReadRegistry{entities: map[string]*entity.Entity{
		"parents":  parent,
		"children": child,
	}}

	loaded, err := EagerLoad(context.Background(), db, parent, relations, []string{"p1"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	children, ok := loaded["p1"]["children"].([]map[string]any)
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one child", loaded["p1"]["children"])
	}
	if got, ok := children[0]["active"].(bool); !ok || !got {
		t.Fatalf("active = %#v (%T), want true bool", children[0]["active"], children[0]["active"])
	}
}

// TestScanOneNormalizesBooleanForModernc covers the single-row read path.
// Get, Create, Update and Upsert all return through scanOne, which also
// carries schema.JSON decoding — so the two normalizations have to coexist
// on that one seam. Without this, rerouting the path would silently serve
// booleans as 0/1 again while the list-path tests stayed green.
func TestScanOneNormalizesBooleanForModernc(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE flags (id TEXT, active INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO flags (id, active) VALUES ('f1', 1)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("flags", entity.EntityConfig{
		Fields: []schema.Field{{Name: "active", Type: schema.Bool}},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, db)

	row := db.QueryRow(`SELECT id, active FROM flags WHERE id = 'f1'`)
	got, err := ch.scanOne(row, []string{"id", "active"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["active"].(bool); !ok || !v {
		t.Fatalf("active = %#v (%T), want true bool", got["active"], got["active"])
	}
}
