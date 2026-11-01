package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Decimal the column cannot store as sent is a 400 naming the field on
// both dialects. Postgres refused each of these at write time (22P02 for
// the exponent, 22003 past the precision) as a 500, or rounded the fifth
// fraction digit away, while SQLite stored the value as written and
// answered 201. Postgres is not in this test's container; the SQLite
// side is the half that used to accept them, and the refusal is in
// schema.Validate, ahead of either driver.
func TestDecimalWritePastColumnRefused(t *testing.T) {
	db := setupDB(t, `CREATE TABLE ledger (id TEXT PRIMARY KEY, amount DECIMAL(19,4), rate NUMERIC(6,2))`)
	ent := entity.Define("ledger", entity.EntityConfig{
		Name:  "ledger",
		Table: "ledger",
		Fields: []schema.Field{
			{Name: "amount", Type: schema.Decimal},
			{Name: "rate", Type: schema.Decimal, RawType: "NUMERIC(6,2)"},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/ledger")
	send := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body)).
			WithContext(handler.SetUser(t.Context(), &testUser{id: "alice"}))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	for _, c := range []struct{ body, field, code string }{
		{`{"amount":"1e-1001"}`, "amount", "decimal_exponent"},
		{`{"amount":"1e1001"}`, "amount", "decimal_exponent"},
		{`{"amount":"1e16"}`, "amount", "decimal_integer_digits"},
		{`{"amount":"12345678901234567"}`, "amount", "decimal_integer_digits"},
		{`{"amount":"1.23456"}`, "amount", "decimal_fraction_digits"},
		{`{"rate":"12345.5"}`, "rate", "decimal_integer_digits"},
		{`{"rate":"1.234"}`, "rate", "decimal_fraction_digits"},
	} {
		code, body := send(http.MethodPost, "/ledger", c.body)
		if code != http.StatusBadRequest || !strings.Contains(body, `"`+c.field+`"`) || !strings.Contains(body, c.code) {
			t.Errorf("create %s answered %d %s, want 400 naming %q with %s", c.body, code, body, c.field, c.code)
		}
	}

	// A value the column holds is stored as sent and reads back as it.
	code, body := send(http.MethodPost, "/ledger", `{"amount":"999999999999999.1234","rate":"9999.99"}`)
	if code != http.StatusCreated {
		t.Fatalf("create in range answered %d: %s", code, body)
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM ledger`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	code, body = send(http.MethodPatch, "/ledger/"+id, `{"amount":"0.00001"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "decimal_fraction_digits") {
		t.Errorf("update 0.00001 answered %d %s, want 400 decimal_fraction_digits", code, body)
	}
	code, body = send(http.MethodPatch, "/ledger/"+id, `{"amount":"1.23450000"}`)
	if code != http.StatusOK {
		t.Fatalf("update with trailing zeros answered %d: %s", code, body)
	}
	var amount string
	if err := db.QueryRow(`SELECT CAST(amount AS TEXT) FROM ledger WHERE id = ?`, id).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != "1.2345" {
		t.Errorf("stored amount = %q, want 1.2345", amount)
	}
}
