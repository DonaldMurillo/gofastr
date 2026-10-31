package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// statesNoDefaultDDL is the invoices table with no DEFAULT on status: the
// table AutoMigrate builds for a state field declared without a Default.
const statesNoDefaultDDL = `CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	number TEXT NOT NULL,
	status TEXT,
	amount INTEGER,
	paid_on TEXT,
	created_at TEXT,
	updated_at TEXT
)`

// noStateDefault drops the status Default, leaving Initial ["draft","open"].
func noStateDefault(cfg *entity.EntityConfig) {
	for i := range cfg.Fields {
		if cfg.Fields[i].Name == "status" {
			cfg.Fields[i].Default = nil
		}
	}
}

// A create that leaves out a state field with no Default starts at the
// first Initial value. It used to store NULL, a state no move starts from,
// and a Required one failed every create from a form that cannot send it
// (2026-10-27 bug bash D7, U7).
func TestStateCreateNoDefaultStartsInitial(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*entity.EntityConfig)
	}{
		{"optional", func(*entity.EntityConfig) {}},
		{"required", func(cfg *entity.EntityConfig) {
			for i := range cfg.Fields {
				if cfg.Fields[i].Name == "status" {
					cfg.Fields[i].Required = true
				}
			}
		}},
		{"hidden", hideStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := statesWorldDDL(t, statesNoDefaultDDL, noStateDefault, tc.mut)
			rr := httptest.NewRecorder()
			ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
				Method: http.MethodPost, Path: "/invoices", Body: `{"number":"INV-1"}`,
			}))
			if rr.Code != http.StatusCreated {
				t.Fatalf("create = %d (%s), want 201", rr.Code, rr.Body.String())
			}
			if got := readStateInvoiceNull(t, db, "INV-1"); got != "draft" {
				t.Fatalf("stored status %q, want the first Initial value draft", got)
			}
		})
	}
}

// The upsert insert arm starts at the first Initial value too, and its
// conflict arm still keeps the stored state of a row it updates.
func TestStateUpsertNoDefaultStartsInitial(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*entity.EntityConfig)
	}{
		{"optional", func(*entity.EntityConfig) {}},
		{"hidden", hideStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := statesWorldDDL(t, statesNoDefaultDDL, noStateDefault, tc.mut)
			if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "i1", "number": "INV-1"}); err != nil {
				t.Fatal(err)
			}
			if got := readStateInvoiceNull(t, db, "INV-1"); got != "draft" {
				t.Fatalf("upsert insert stored status %q, want draft", got)
			}
			seedStateInvoice(t, db, "i2", "open", nil)
			if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "i2", "number": "row i2"}); err != nil {
				t.Fatal(err)
			}
			if got := readStateInvoiceNull(t, db, "row i2"); got != "open" {
				t.Fatalf("upsert onto an open row left status %q, want open", got)
			}
		})
	}
}

// hideStatus marks the status field Hidden.
func hideStatus(cfg *entity.EntityConfig) {
	for i := range cfg.Fields {
		if cfg.Fields[i].Name == "status" {
			cfg.Fields[i].Hidden = true
		}
	}
}

// readStateInvoiceNull reads the status of the row numbered number, ""
// for NULL.
func readStateInvoiceNull(t *testing.T, db *sql.DB, number string) string {
	t.Helper()
	var status sql.NullString
	if err := db.QueryRow(`SELECT status FROM invoices WHERE number = ?`, number).Scan(&status); err != nil {
		t.Fatalf("read invoice %s: %v", number, err)
	}
	return status.String
}

// Only the state field gets that write: any other Hidden field with a
// Default is still left to the table's own DDL default.
func TestHiddenNonStateDefaultKeepsDDL(t *testing.T) {
	ddl := `CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	number TEXT NOT NULL,
	status TEXT,
	amount INTEGER DEFAULT 7,
	paid_on TEXT,
	created_at TEXT,
	updated_at TEXT
)`
	hideAmount := func(cfg *entity.EntityConfig) {
		for i := range cfg.Fields {
			if cfg.Fields[i].Name == "amount" {
				cfg.Fields[i].Hidden = true
				cfg.Fields[i].Default = 5
			}
		}
	}
	ch, db := statesWorldDDL(t, ddl, noStateDefault, hideAmount)
	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices", Body: `{"number":"INV-1"}`,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "i2", "number": "INV-2"}); err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"INV-1", "INV-2"} {
		var amount int
		if err := db.QueryRow(`SELECT amount FROM invoices WHERE number = ?`, number).Scan(&amount); err != nil {
			t.Fatal(err)
		}
		if amount != 7 {
			t.Fatalf("%s stored amount %d, want the DDL default 7", number, amount)
		}
	}
}
