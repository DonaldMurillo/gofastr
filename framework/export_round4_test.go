package framework

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Two held references that share a column each keep its value: the
// first emptied tenant on the row the second then read, so the second
// was restored with tenant NULL and both relationships were lost.
func TestHeldRefsShareAColumn(t *testing.T) {
	rows := []map[string]any{{"id": "c1", "tenant": "T", "a_id": "A", "b_id": "B"}}
	refs := []fkEdge{{cols: []string{"tenant", "a_id"}}, {cols: []string{"tenant", "b_id"}}}
	_, held, err := holdLaterRefs(rows, []string{"id"}, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || !slices.Equal(held[1].vals, []any{"T", "B"}) {
		t.Errorf("held = %+v, want the second reference as [T B]", held)
	}
}

// A row with a NULL key cannot have a reference set on it later: the
// UPDATE ... WHERE id = NULL matched nothing and the import succeeded
// with the reference gone.
func TestHeldRefNullKeyRefused(t *testing.T) {
	rows := []map[string]any{{"id": nil, "a_id": "a1"}}
	_, _, err := holdLaterRefs(rows, []string{"id"}, []fkEdge{{cols: []string{"a_id"}, label: "b.a"}})
	if err == nil || !strings.Contains(err.Error(), "NULL") {
		t.Errorf("err = %v, want a refusal naming the NULL key", err)
	}
}

// A deferred reference whose row is not there fails the import instead
// of updating nothing.
func TestDeferredRefMissingRowFails(t *testing.T) {
	db := openSQLiteMem(t)
	cascadeExec(t, db, `CREATE TABLE b (id TEXT PRIMARY KEY, a_id TEXT)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = applyDeferredRefs(context.Background(), tx, "b", []string{"id"},
		[]deferredRef{{key: []any{"gone"}, cols: []string{"a_id"}, vals: []any{"a1"}}})
	if err == nil {
		t.Error("a deferred reference to a missing row was applied without error")
	}
}

// A reference to cut before the deletes that runs through the key is
// refused: emptying it emptied the key the delete then matched on, so
// the row stayed and the erasure reported it gone.
func TestEraseCutThroughKeyRefused(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("nodes", entity.EntityConfig{
		Table:  "nodes",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("self", "nodes", "id").OnDeleteAction(entity.OnDeleteRestrict),
		},
	}.WithTimestamps(false)))
	cascadeExec(t, db,
		`CREATE TABLE nodes (id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT, owner_id TEXT)`,
		`INSERT INTO nodes (id, owner_id) VALUES ('n1','u1')`)
	_, err := app.EraseUserData(context.Background(), "u1")
	var n int
	if qerr := db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE owner_id = 'u1'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if err == nil && n != 0 {
		t.Errorf("erasure succeeded and left %d of the user's rows", n)
	}
}

// An exported page that ends on a NULL key cannot name the next one:
// "id > NULL" matched nothing and the export dropped every later row
// under a valid checksum. It is refused instead.
func TestExportNullKeyRefused(t *testing.T) {
	db := openSQLiteMem(t)
	cascadeExec(t, db, `CREATE TABLE notes (id TEXT PRIMARY KEY, body TEXT)`)
	cascadeExec(t, db, `INSERT INTO notes (id, body) VALUES (NULL, 'x'), ('a', 'y')`)
	rows, err := rawReadAll(context.Background(), db, migrate.DialectSQLite, "notes", []string{"id", "body"}, []string{"id"}, 1)
	if err == nil {
		t.Errorf("export of a NULL-key table read %d rows without error", len(rows))
	}
}
