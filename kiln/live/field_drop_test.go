package live_test

import (
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// oneConnLive is a Live over a one-connection SQLite pool, so the
// connection a rebuild changes pragmas on is the one checked after.
func oneConnLive(t *testing.T) (*protocol.Tools, *sql.DB) {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-field-drop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	d.SetMaxOpenConns(1)
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) })
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l), d
}

func mustOK(t *testing.T, what string, r protocol.Result) {
	t.Helper()
	if !r.OK {
		t.Fatalf("%s: %+v", what, r)
	}
}

// approve proposes and approves plan id for one target.
func approve(t *testing.T, tools *protocol.Tools, id, op, name string) {
	t.Helper()
	mustOK(t, "propose_plan", tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID: id, Steps: []string{op + " " + name},
		Targets: []journal.PlanTarget{{Op: op, Name: name}},
	}))
	mustOK(t, "approve_plan", tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: id}))
}

// columnsOf lists a table's columns, in no particular order. The test
// tables' column names hold no commas.
func columnsOf(t *testing.T, d *sql.DB, table string) []string {
	t.Helper()
	var joined sql.NullString
	if err := d.QueryRow(`SELECT group_concat(name, ',') FROM pragma_table_info(?)`, table).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if !joined.Valid {
		return nil
	}
	return strings.Split(joined.String, ",")
}

func count(t *testing.T, d *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// The pooled connection comes back with foreign keys on and the legacy
// rename off, and no table is left set aside.
func checkConnRestored(t *testing.T, d *sql.DB) {
	t.Helper()
	if n := count(t, d, `PRAGMA foreign_keys`); n != 1 {
		t.Errorf("foreign_keys = %d after the edit, want 1", n)
	}
	if n := count(t, d, `PRAGMA legacy_alter_table`); n != 0 {
		t.Errorf("legacy_alter_table = %d after the edit, want 0", n)
	}
	if n := count(t, d, `SELECT count(*) FROM sqlite_master WHERE name LIKE '_kiln_rebuild_%'`); n != 0 {
		t.Errorf("%d set-aside tables left behind", n)
	}
}

// delete_field on a required field left its NOT NULL column in the
// table, so every later create, which inserts only the declared fields,
// failed (2026-10-27 K11).
func TestDeleteRequiredFieldFreesCreates(t *testing.T) {
	tools, d := oneConnLive(t)
	mustOK(t, "add_entity", tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string", Required: true}, {Name: "body", Type: "string", Required: true}},
	}}))
	mustOK(t, "add_seed", tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "posts", Rows: []map[string]any{{"id": "p1", "title": "a", "body": "b"}},
	}}))
	approve(t, tools, "p1", "delete_field", "posts.body")
	mustOK(t, "delete_field", tools.DeleteField(t.Context(), protocol.DeleteFieldArgs{Entity: "posts", Field: "body", PlanID: "p1"}))

	if slices.Contains(columnsOf(t, d, "posts"), "body") {
		t.Error("the deleted field's column is still in the table")
	}
	if _, err := d.Exec(`INSERT INTO posts (id, title) VALUES ('p2', 't')`); err != nil {
		t.Fatalf("create after delete_field: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM posts WHERE id = 'p1' AND title = 'a'`); n != 1 {
		t.Error("the row from before the delete is gone")
	}
	checkConnRestored(t, d)
}

// libraryWorld adds authors (handle required and unique) and books
// (isbn required and unique, author_id belongs to authors), with one
// linked row in each.
func libraryWorld(t *testing.T, tools *protocol.Tools) {
	t.Helper()
	for _, e := range []*world.Entity{
		{Name: "authors", Fields: []world.Field{{Name: "name", Type: "string"}, {Name: "handle", Type: "string", Required: true, Unique: true}}},
		{Name: "books", Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "isbn", Type: "string", Required: true, Unique: true}, {Name: "author_id", Type: "uuid"}},
			Relations: []world.Relation{{Name: "author", Type: "belongs_to", Entity: "authors", ForeignKey: "author_id"}}},
	} {
		mustOK(t, "add_entity "+e.Name, tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: e}))
	}
	for _, s := range []*world.Seed{
		{Entity: "authors", Rows: []map[string]any{{"id": "a1", "name": "ada", "handle": "@ada"}}},
		{Entity: "books", Rows: []map[string]any{{"id": "b1", "title": "notes", "isbn": "1", "author_id": "a1"}}},
	} {
		mustOK(t, "add_seed "+s.Entity, tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: s}))
	}
}

// SQLite refuses DROP COLUMN on a UNIQUE column, so the table is
// rebuilt from the registry: rows kept, the column and its constraint
// gone, the indexes and foreign keys the entity declares still there.
func TestDeleteUniqueFieldRebuildsTable(t *testing.T) {
	for _, tc := range []struct {
		name, entity, field string
		create              string // an insert after the delete
		kept                string // counts the row from before the delete
		keyed, keyedWhat    string // counts what the rebuild must keep on books
		orphan              string // an insert the foreign key must refuse
	}{{
		name: "parent", entity: "authors", field: "handle",
		create: `INSERT INTO authors (id, name) VALUES ('a2', 'bo')`,
		kept:   `SELECT count(*) FROM authors WHERE id = 'a1' AND name = 'ada'`,
		// books still points at authors, not at the table set aside.
		keyed:     `SELECT count(*) FROM pragma_foreign_key_list('books') WHERE "table" = 'authors'`,
		keyedWhat: "foreign keys to authors",
		orphan:    `INSERT INTO books (id, title, isbn, author_id) VALUES ('b2', 'x', '2', 'nobody')`,
	}, {
		name: "child", entity: "books", field: "isbn",
		create:    `INSERT INTO books (id, title, author_id) VALUES ('b2', 'x', 'a1')`,
		kept:      `SELECT count(*) FROM books WHERE id = 'b1' AND author_id = 'a1'`,
		keyed:     `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'books' AND sql LIKE '%author_id%'`,
		keyedWhat: "indexes on author_id",
		orphan:    `INSERT INTO books (id, title, author_id) VALUES ('b3', 'y', 'nobody')`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			tools, d := oneConnLive(t)
			libraryWorld(t, tools)
			approve(t, tools, "p1", "delete_field", tc.entity+"."+tc.field)
			mustOK(t, "delete_field", tools.DeleteField(t.Context(), protocol.DeleteFieldArgs{Entity: tc.entity, Field: tc.field, PlanID: "p1"}))

			if slices.Contains(columnsOf(t, d, tc.entity), tc.field) {
				t.Error("the deleted field's column is still in the table")
			}
			if _, err := d.Exec(tc.create); err != nil {
				t.Fatalf("create after delete_field: %v", err)
			}
			if n := count(t, d, tc.kept); n != 1 {
				t.Error("the row from before the delete is gone")
			}
			if n := count(t, d, tc.keyed); n != 1 {
				t.Errorf("books has %d %s after the rebuild, want 1", n, tc.keyedWhat)
			}
			if _, err := d.Exec(tc.orphan); err == nil {
				t.Error("a book naming no author was accepted: the foreign key no longer holds")
			}
			checkConnRestored(t, d)
		})
	}
}

// update_entity that makes a required field optional drops its NOT
// NULL, and one that leaves a field out drops its column.
func TestUpdateEntityRelaxesColumns(t *testing.T) {
	tools, d := oneConnLive(t)
	mustOK(t, "add_entity", tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string", Required: true}, {Name: "body", Type: "string", Required: true}},
	}}))
	mustOK(t, "add_seed", tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "posts", Rows: []map[string]any{{"id": "p1", "title": "a", "body": "b"}},
	}}))
	approve(t, tools, "p1", "update_entity", "posts")
	mustOK(t, "update_entity", tools.UpdateEntity(t.Context(), protocol.UpdateEntityArgs{PlanID: "p1", Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
	}}))
	if slices.Contains(columnsOf(t, d, "posts"), "body") {
		t.Error("the field update_entity left out is still a column")
	}
	if _, err := d.Exec(`INSERT INTO posts (id) VALUES ('p2')`); err != nil {
		t.Fatalf("a create without the now-optional title: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM posts WHERE id = 'p1' AND title = 'a'`); n != 1 {
		t.Error("the row from before the update is gone")
	}
	checkConnRestored(t, d)
}

// Rows the rebuilt table refuses (here a NULL in a required column that
// SQLite's ADD COLUMN could only add as nullable) refuse the edit and
// put the table back as it was: rows, columns and indexes. The database
// is not re-derived, so rows created through CRUD survive.
func TestRebuildRefusalKeepsTable(t *testing.T) {
	tools, d := oneConnLive(t)
	libraryWorld(t, tools)
	if _, err := d.Exec(`INSERT INTO books (id, title, isbn, author_id) VALUES ('crud1', 'kept', '9', 'a1')`); err != nil {
		t.Fatal(err)
	}
	mustOK(t, "add_field", tools.AddField(t.Context(), protocol.AddFieldArgs{Entity: "books", Field: world.Field{Name: "code", Type: "string", Required: true}}))
	approve(t, tools, "p1", "delete_field", "books.isbn")
	r := tools.DeleteField(t.Context(), protocol.DeleteFieldArgs{Entity: "books", Field: "isbn", PlanID: "p1"})
	if r.OK {
		t.Fatalf("delete_field succeeded over rows the rebuilt table refuses: %+v", r)
	}
	if !slices.Contains(columnsOf(t, d, "books"), "isbn") {
		t.Error("the refused delete took the column anyway")
	}
	if n := count(t, d, `SELECT count(*) FROM books WHERE id IN ('b1', 'crud1')`); n != 2 {
		t.Errorf("%d of the 2 rows survived the refused delete", n)
	}
	if n := count(t, d, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'books' AND sql LIKE '%author_id%'`); n != 1 {
		t.Errorf("books has %d indexes on author_id after the refusal, want 1", n)
	}
	if _, err := d.Exec(`INSERT INTO books (id, title, isbn) VALUES ('b4', 'z', '9')`); err == nil {
		t.Error("isbn is no longer unique after the refusal")
	}
	checkConnRestored(t, d)
}

// An update_entity that drops two fields, where the first drops in place
// and the second needs a rebuild the rows refuse, keeps both columns:
// the in-place drop rolls back with the refusal.
func TestRefusedDropKeepsEveryColumn(t *testing.T) {
	tools, d := oneConnLive(t)
	mustOK(t, "add_entity", tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{
			{Name: "title", Type: "string"},
			{Name: "body", Type: "string", Required: true},
			{Name: "tag", Type: "string", Required: true, Unique: true},
		},
	}}))
	if _, err := d.Exec(`INSERT INTO posts (id, title, body, tag) VALUES ('crud1', 'kept', 'b', 't')`); err != nil {
		t.Fatal(err)
	}
	mustOK(t, "add_field", tools.AddField(t.Context(), protocol.AddFieldArgs{Entity: "posts", Field: world.Field{Name: "code", Type: "string", Required: true}}))
	approve(t, tools, "p1", "update_entity", "posts")
	if r := tools.UpdateEntity(t.Context(), protocol.UpdateEntityArgs{PlanID: "p1", Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "code", Type: "string", Required: true}},
	}}); r.OK {
		t.Fatalf("update_entity succeeded over rows the rebuilt table refuses: %+v", r)
	}
	cols := columnsOf(t, d, "posts")
	for _, c := range []string{"body", "tag"} {
		if !slices.Contains(cols, c) {
			t.Errorf("the refused update took column %s", c)
		}
	}
	if n := count(t, d, `SELECT count(*) FROM posts WHERE id = 'crud1' AND body = 'b'`); n != 1 {
		t.Error("the row lost its body in the refused update")
	}
	checkConnRestored(t, d)
}
