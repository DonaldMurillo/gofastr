package live_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// linkedWorld adds authors and books (books.author_id references
// authors) and seeds one book linked to one author.
func linkedWorld(t *testing.T, tools *protocol.Tools) {
	t.Helper()
	for _, e := range []*world.Entity{
		{Name: "authors", Fields: []world.Field{{Name: "name", Type: "string"}}},
		{Name: "books", Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "author_id", Type: "uuid"}},
			Relations: []world.Relation{{Name: "author", Type: "belongs_to", Entity: "authors", ForeignKey: "author_id"}}},
	} {
		if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: e}); !res.OK {
			t.Fatalf("add_entity %s: %+v", e.Name, res)
		}
	}
	for _, s := range []*world.Seed{
		{Entity: "authors", Rows: []map[string]any{{"id": "a1", "name": "ada"}}},
		{Entity: "books", Rows: []map[string]any{{"title": "notes", "author_id": "a1"}}},
	} {
		if res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: s}); !res.OK {
			t.Fatalf("add_seed %s: %+v", s.Entity, res)
		}
	}
}

// reset_session dropped tables in name order and stopped at the first
// error: DROP TABLE authors fails SQLite's foreign-key check while a
// book row points at it, the reset still answered OK, and re-adding the
// entities served the old rows (2026-10-27 K10).
func TestResetDropsLinkedTables(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-reset-fk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	// One connection, so the one the reset drops tables on is the one
	// checked for foreign_keys afterwards.
	d.SetMaxOpenConns(1)
	var fk int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("fixture: foreign_keys = %d (%v), want 1", fk, err)
	}
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	linkedWorld(t, tools)
	var books int
	if err := d.QueryRow(`SELECT count(*) FROM books WHERE author_id = 'a1'`).Scan(&books); err != nil || books != 1 {
		t.Fatalf("fixture: linked book rows = %d (%v), want 1", books, err)
	}

	if res := tools.ResetSession(t.Context(), protocol.ResetSessionArgs{}); !res.OK {
		t.Fatalf("reset_session: %+v", res)
	}
	var tables int
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('authors', 'books')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("reset left %d of the world's tables behind", tables)
	}

	// The connection goes back to the pool with foreign keys on again.
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("after reset foreign_keys = %d (%v), want 1", fk, err)
	}
}

// The re-derive's failure reaches the caller. The drop used to be logged
// and the reset answered OK; a database that cannot be rebuilt now fails
// it.
func TestResetReportsRederiveFailure(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-reset-err")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	d.SetMaxOpenConns(1) // the query_only below holds for every statement
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	linkedWorld(t, tools)
	// Every write now fails, so the drop does: the shape of any failure
	// the rebuild cannot get past.
	if _, err := d.Exec(`PRAGMA query_only = ON`); err != nil {
		t.Fatal(err)
	}
	if res := tools.ResetSession(t.Context(), protocol.ResetSessionArgs{}); res.OK {
		t.Fatal("reset_session answered OK over a database it could not rebuild")
	}
}

// Boot re-derives the database too; one it cannot rebuild fails New
// instead of serving rows the journal does not hold.
func TestBootFailsWhenRederiveFails(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-boot-err")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`CREATE TABLE stale (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`PRAGMA query_only = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) }); err == nil {
		t.Fatal("live.New booted over a database it could not rebuild")
	}
}
