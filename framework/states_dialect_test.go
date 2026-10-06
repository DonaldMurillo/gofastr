package framework

import (
	"database/sql"
	"net/http"
	"testing"

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
