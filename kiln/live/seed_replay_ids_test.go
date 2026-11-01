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

// A seed row with no id got a fresh UUID each time it was applied, and
// the journal kept the row without it. A book seeded against the
// author's live id then pointed at nothing after a restart: replay gave
// the author a new id and the book seed was skipped with a warning
// (2026-10-31 bug hunt S52). The generated id is journaled with the seed
// now, so a restart over the same journal reproduces it.
func TestSeedIDsSurviveRestart(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-seed-replay-ids")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	j := journal.NewMemory()
	l, err := live.New(j, factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	ctx := t.Context()
	for _, e := range []*world.Entity{
		{Name: "authors", Fields: []world.Field{{Name: "name", Type: "string"}}},
		{Name: "books", Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "author_id", Type: "uuid"}},
			Relations: []world.Relation{{Name: "author", Type: "belongs_to", Entity: "authors", ForeignKey: "author_id"}}},
	} {
		if r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: e}); !r.OK {
			t.Fatalf("add_entity %s: %+v", e.Name, r)
		}
	}
	if r := tools.AddSeed(ctx, protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "authors", Rows: []map[string]any{{"name": "ada"}},
	}}); !r.OK {
		t.Fatalf("add_seed authors: %+v", r)
	}
	var authorID string
	if err := d.QueryRow(`SELECT id FROM authors WHERE name = 'ada'`).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	if r := tools.AddSeed(ctx, protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "books", Rows: []map[string]any{{"title": "notes", "author_id": authorID}},
	}}); !r.OK {
		t.Fatalf("add_seed books: %+v", r)
	}

	// Restart: a new runtime over the same journal re-derives the DB.
	if _, err := live.New(j, factory); err != nil {
		t.Fatalf("restart: %v", err)
	}
	var got string
	if err := d.QueryRow(`SELECT id FROM authors WHERE name = 'ada'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != authorID {
		t.Fatalf("author id after restart = %q, want %q (the id the book references)", got, authorID)
	}
	var books int
	if err := d.QueryRow(`SELECT count(*) FROM books WHERE author_id = ?`, authorID).Scan(&books); err != nil || books != 1 {
		t.Fatalf("book rows after restart = %d (%v), want 1", books, err)
	}
}
