package framework

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The refusals run against each dialect's SQL: a PUT that changes the
// state field answers 422, a PUT writing the stored value back passes, and
// a move whose From no longer holds the stored value answers 409 with the
// first move's state and stamp left in place.
func TestStatesRefusalsPerDialect(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ta := TestHarness(t, statesAuditApp(t, db))

		resp := ta.Post("/invoices", map[string]any{"number": "INV-1", "status": "open"})
		resp.AssertStatus(t, 201)
		var created struct {
			Data map[string]any `json:"data"`
		}
		if err := resp.JSON(&created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		id, _ := created.Data["id"].(string)

		ta.Put("/invoices/"+id, map[string]any{"number": "INV-1", "status": "paid"}).AssertStatus(t, 422)
		ta.Put("/invoices/"+id, map[string]any{"number": "INV-2", "status": "open"}).AssertStatus(t, 200)

		pay := func() *TestResponse {
			return ta.Request(http.MethodPost, "/invoices/"+id+"/transitions/pay", nil).
				WithBody(map[string]any{}).Execute()
		}
		pay().AssertStatus(t, 200)
		pay().AssertStatus(t, 409)

		readBack := "SELECT status, paid_on FROM invoices WHERE id = ?"
		if dialect == migrate.DialectPostgres {
			readBack = "SELECT status, paid_on FROM invoices WHERE id = $1"
		}
		var status string
		var paidOn sql.NullString
		if err := db.QueryRow(readBack, id).Scan(&status, &paidOn); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if status != "paid" || !paidOn.Valid || paidOn.String == "" {
			t.Fatalf("after pay: status %q paid_on %+v, want paid with a stamp", status, paidOn)
		}
	})
}

// Two moves of one record on two Postgres connections: the winner commits
// after the loser read "open" and before its UPDATE, and the loser's
// statement, pinned to From, matches zero rows under READ COMMITTED and
// answers a typed conflict naming the winner's state. (SQLite's version of
// the race, which fails the write with SQLITE_BUSY, is in crud.)
func TestStatesMoveRacePostgres(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		if dialect != migrate.DialectPostgres {
			t.Skip("SQLite's race is TestRunTransitionSQLiteBusyConflict in framework/crud")
		}
		db.SetMaxOpenConns(2)
		app := statesAuditApp(t, db)
		handler, err := app.CrudHandler("invoices")
		if err != nil {
			t.Fatal(err)
		}
		created, err := handler.CreateOne(context.Background(), map[string]any{"number": "INV-1", "status": "open"})
		if err != nil {
			t.Fatal(err)
		}
		id := created["id"].(string)

		raced := false
		var winner error
		app.HookRegistry("invoices").RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
			if crud.TransitionFromContext(ctx) != "pay" || raced {
				return nil
			}
			raced = true
			_, winner = handler.RunTransition(context.Background(), id, "void")
			return nil
		})

		_, err = handler.RunTransition(context.Background(), id, "pay")
		if winner != nil {
			t.Fatalf("winning move: %v", winner)
		}
		tce, ok := errors.AsType[*crud.TransitionConflictError](err)
		if !ok {
			t.Fatalf("losing move = %v (%T), want *crud.TransitionConflictError", err, err)
		}
		if tce.Current != "void" {
			t.Fatalf("conflict current = %q, want the winner's void", tce.Current)
		}
		var status string
		var paidOn sql.NullString
		if err := db.QueryRow("SELECT status, paid_on FROM invoices WHERE id = $1", id).Scan(&status, &paidOn); err != nil {
			t.Fatal(err)
		}
		if status != "void" || paidOn.Valid {
			t.Fatalf("stored %q paid_on %v, want void and no stamp", status, paidOn)
		}
	})
}
