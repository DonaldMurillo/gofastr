package live_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// A delete_entity whose DROP TABLE SQLite refuses (a book row still
// points at the author) left the table untouched, but Apply took the
// failure for a lost database and rederived it: every table dropped and
// only journaled seeds put back, so the CRUD rows went (2026-10-31 bug
// hunt S51). A refused drop changes nothing, and the rows stay.
func TestRefusedDeleteKeepsCRUDRows(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-delete-refused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	var fk int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("fixture: foreign_keys = %d (%v), want 1", fk, err)
	}
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	ctx := t.Context()
	linkedWorld(t, tools)
	if _, err := d.Exec(`INSERT INTO authors (id, name) VALUES ('crud-1', 'kept')`); err != nil {
		t.Fatalf("crud row: %v", err)
	}
	if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID: "p1", Steps: []string{"drop authors"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "authors"}},
	}); !r.OK {
		t.Fatalf("propose_plan: %+v", r)
	}
	if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
		t.Fatalf("approve_plan: %+v", r)
	}
	if r := tools.DeleteEntity(ctx, protocol.DeleteEntityArgs{Name: "authors", PlanID: "p1"}); r.OK {
		t.Fatalf("delete_entity of a referenced table succeeded: %+v", r)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM authors WHERE id = 'crud-1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("CRUD author rows after a refused delete = %d (%v), want 1", n, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM books`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("book rows after a refused delete = %d (%v), want 1", n, err)
	}
}
