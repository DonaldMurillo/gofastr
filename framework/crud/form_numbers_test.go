package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A form posts every value as text, and the runtime's form RPC sends it
// as a JSON string: "42" for a number input.

func priceList(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	return setupSecurityTestHandler(t, makeEntityConfig("price_list", "price_list", "", []schema.Field{
		{Name: "price", Type: schema.Float, Required: true},
	}), `CREATE TABLE price_list (id TEXT PRIMARY KEY, price REAL)`)
}

func createPrice(t *testing.T, ch *CrudHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/price_list", Body: body, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	return rr
}

func TestFloatTakesNumericText(t *testing.T) {
	ch, db := priceList(t)
	rr := createPrice(t, ch, `{"price":"42.5"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var price float64
	var typ string
	if err := db.QueryRow("SELECT price, typeof(price) FROM price_list").Scan(&price, &typ); err != nil {
		t.Fatal(err)
	}
	if price != 42.5 || typ != "real" {
		t.Fatalf("stored %v as %s, want 42.5 as real", price, typ)
	}
}

func TestNumberTextRefusesOddSpellings(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-Infinity", "0x1p4", "0x10", "1_000", "1e999", "4 2", "abc"} {
		ch, _ := priceList(t)
		if rr := createPrice(t, ch, `{"price":"`+v+`"}`); rr.Code != http.StatusBadRequest {
			t.Errorf("price %q: status=%d body=%s, want 400", v, rr.Code, rr.Body.String())
		}
	}
}
