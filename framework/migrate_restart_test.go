package framework

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ticketFields is the starting schema; evolvedTicketFields adds the column the
// 2026-07-26 backend eval's maintenance pass added (a priority on an existing
// ticket table).
func ticketFields() []schema.Field {
	return []schema.Field{{Name: "title", Type: schema.String, Required: true}}
}

func evolvedTicketFields() []schema.Field {
	return []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "priority", Type: schema.String},
	}
}

// bootTickets constructs a fresh App over an existing DB and takes it through
// the real Start path, which is where auto-migration runs. Each call models one
// process restart against the same database.
func bootTickets(t *testing.T, db *sql.DB, fields []schema.Field) {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Entity("tickets", entity.EntityConfig{
		Table:  "tickets",
		Fields: fields,
	}.WithTimestamps(false))
	stop := covStartAndStop(t, app)
	stop()
}

func countTickets(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tickets`).Scan(&n); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	return n
}

// TestRepeatedStartsPreserveExistingRows closes the second of the two
// highest-leverage next moves the 2026-07-26 backend eval left open:
//
//	"GoFastr run two also lost the original grader-created ticket after
//	 repeated post-migration starts, while run one preserved it."
//
// A row disappearing on restart is the worst failure class in the framework
// and it had no regression net at all — nothing in framework/ or cmd/ booted
// an app twice against a populated database and checked the rows were still
// there. The nondeterminism between the eval's two runs is exactly what a test
// like this is for: it either holds every time or it does not hold.
//
// The sequence is the eval's: seed a row, restart unchanged, evolve the schema
// the way the maintenance ticket did, then restart repeatedly on the evolved
// schema. The row must survive all of it, with its data intact — a surviving
// row whose title was blanked is the same bug wearing a different hat.
//
// This passed the first time it was run, which for a data-loss guard is a
// reason for suspicion rather than confidence, so both tests here were
// mutation-checked: deleting all rows before restart #3 fails the first with
// "after post-migration restart #3: 0 rows, want 1", and deleting ten of fifty
// between boots fails the second with "40 rows, want 50". The assertions are
// live, not vacuous.
//
// Limitation, stated rather than papered over: a "restart" here is a fresh App
// over the same *sql.DB, not a new OS process reopening the file. That covers
// the auto-migration path, which is where the reported loss occurred, but it
// does not cover anything that depends on the connection itself being new.
func TestRepeatedStartsPreserveExistingRows(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// First boot creates the table.
		bootTickets(t, db, ticketFields())

		if _, err := db.Exec(`INSERT INTO tickets (title) VALUES (?)`, "grader-created"); err != nil {
			// Postgres uses $1; retry rather than branch on dialect.
			if _, err2 := db.Exec(`INSERT INTO tickets (title) VALUES ($1)`, "grader-created"); err2 != nil {
				t.Fatalf("seed row: %v / %v", err, err2)
			}
		}
		if n := countTickets(t, db); n != 1 {
			t.Fatalf("seeded %d rows, want 1", n)
		}

		// Restart on the SAME schema. Auto-migration must be a no-op here.
		bootTickets(t, db, ticketFields())
		if n := countTickets(t, db); n != 1 {
			t.Fatalf("after an unchanged restart: %d rows, want 1 — auto-migration dropped data it had no reason to touch", n)
		}

		// Evolve the schema, as the eval's maintenance pass did.
		bootTickets(t, db, evolvedTicketFields())
		if n := countTickets(t, db); n != 1 {
			t.Fatalf("after adding a column: %d rows, want 1 — the migration recreated the table instead of altering it", n)
		}

		// "Repeated post-migration starts" is the reported trigger: the loss
		// showed up after the schema change, on a later boot, not the first.
		for i := range 4 {
			bootTickets(t, db, evolvedTicketFields())
			if n := countTickets(t, db); n != 1 {
				t.Fatalf("after post-migration restart #%d: %d rows, want 1", i+1, n)
			}
		}

		// A row that survived with its column blanked is the same data loss.
		var title string
		if err := db.QueryRow(`SELECT title FROM tickets`).Scan(&title); err != nil {
			t.Fatalf("read surviving row: %v", err)
		}
		if title != "grader-created" {
			t.Fatalf("surviving row title = %q, want %q — the row outlived the migration but its data did not", title, "grader-created")
		}
	})
}

// The same guarantee for many rows: a migration that rebuilds a table can
// truncate it partway rather than wholesale, which a single-row check misses.
func TestRepeatedStartsPreserveEveryRow(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		bootTickets(t, db, ticketFields())

		const seeded = 50
		for i := range seeded {
			title := fmt.Sprintf("ticket-%03d", i)
			if _, err := db.Exec(`INSERT INTO tickets (title) VALUES (?)`, title); err != nil {
				if _, err2 := db.Exec(`INSERT INTO tickets (title) VALUES ($1)`, title); err2 != nil {
					t.Fatalf("seed row %d: %v / %v", i, err, err2)
				}
			}
		}
		if n := countTickets(t, db); n != seeded {
			t.Fatalf("seeded %d rows, want %d", n, seeded)
		}

		bootTickets(t, db, evolvedTicketFields())
		bootTickets(t, db, evolvedTicketFields())

		if n := countTickets(t, db); n != seeded {
			t.Fatalf("after a schema change and a restart: %d rows, want %d", n, seeded)
		}
	})
}
