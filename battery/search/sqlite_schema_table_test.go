package search

import (
	"context"
	"testing"
)

// SQLiteFTS{Table: "aux.docs"} names a table in an attached database.
// The owner migration probed pragma_table_info("aux.docs"), which finds
// nothing, so every boot rebuilt the table and the rebuild's RENAME TO
// "aux"."docs" is a syntax error SQLite refuses: the index never booted.
func TestSQLiteAttachedSchemaTableBoots(t *testing.T) {
	db := openSQLiteDB(t)
	if err := sqliteFTS5Available(db); err != nil {
		t.Skipf("FTS5 not available: %v", err)
	}
	ctx := context.Background()
	if _, err := db.Exec(`ATTACH DATABASE ':memory:' AS aux`); err != nil {
		t.Fatal(err)
	}
	// A table an earlier release made, before the owner column.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE aux.docs USING fts5(id UNINDEXED, type UNINDEXED, fields UNINDEXED, text, tokenize='porter unicode61')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO aux.docs (id, type, fields, text) VALUES ('old', 't', NULL, 'gopher burrow')`); err != nil {
		t.Fatal(err)
	}
	var idx *SQLiteFTS
	for boot := 0; boot < 2; boot++ {
		var err error
		idx, err = NewSQLiteFTS(db, SQLiteFTSConfig{Table: "aux.docs"})
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.EnsureSchema(ctx); err != nil {
			t.Fatalf("boot %d: EnsureSchema: %v", boot, err)
		}
		if boot == 0 {
			// Indexed between boots: the second boot must not rebuild
			// the table again and lose the owner.
			if err := idx.Index(ctx, Document{ID: "new", Type: "t", Owner: "u1", Text: "gopher den"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	res, err := idx.Search(ctx, Query{Text: "gopher"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("search found %d documents, want the old and the new one: %+v", len(res), res)
	}
	for _, r := range res {
		if r.Document.ID == "new" && r.Document.Owner != "u1" {
			t.Fatalf("new document's owner = %q after a reboot, want u1", r.Document.Owner)
		}
	}
	var mainTables int
	if err := db.QueryRow(`SELECT count(*) FROM main.sqlite_master WHERE name LIKE 'docs%'`).Scan(&mainTables); err != nil {
		t.Fatal(err)
	}
	if mainTables != 0 {
		t.Fatalf("%d docs tables landed in main, want all of them in aux", mainTables)
	}
}
