package crud_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// decimalStorageRoutes serves CRUD for a table of custom Decimal RawTypes
// over a fresh in-memory SQLite database.
func decimalStorageRoutes(t *testing.T) (*router.Router, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`CREATE TABLE decimal_storage (id TEXT PRIMARY KEY, units NUMERIC(30,0), tiny NUMERIC, memo TEXT, amount DECIMAL(19,4), real_value REAL, float_value FLOAT, double_value DOUBLE PRECISION, point_value "FLOATING POINT", text_value "REAL TEXT")`); err != nil {
		t.Fatal(err)
	}
	definition := entity.Define("decimal_storage", entity.EntityConfig{
		Name:  "decimal_storage",
		Table: "decimal_storage",
		Fields: []schema.Field{
			{Name: "units", Type: schema.Decimal, RawType: "NUMERIC(30,0)"},
			{Name: "tiny", Type: schema.Decimal, RawType: "NUMERIC"},
			{Name: "memo", Type: schema.Decimal, RawType: "TEXT"},
			{Name: "amount", Type: schema.Decimal},
			{Name: "real_value", Type: schema.Decimal, RawType: "REAL"},
			{Name: "float_value", Type: schema.Decimal, RawType: "FLOAT"},
			{Name: "double_value", Type: schema.Decimal, RawType: "DOUBLE PRECISION"},
			{Name: "point_value", Type: schema.Decimal, RawType: "FLOATING POINT"},
			{Name: "text_value", Type: schema.Decimal, RawType: "REAL TEXT"},
		},
	}.WithTimestamps(false))
	definition.SetDB(database)
	routes := router.New()
	crud.RegisterCrudRoutes(routes, crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake), "/decimal_storage")
	return routes, database
}

func sendDecimalRequest(routes *router.Router, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request = request.WithContext(handler.SetUser(request.Context(), "decimal-user"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

func decimalResponseRow(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil || envelope.Data == nil {
		t.Fatalf("decode %d: %v", response.Code, err)
	}
	return envelope.Data
}

func sameDecimalText(got any, want string) bool {
	gotValue, gotOK := new(big.Rat).SetString(fmt.Sprint(got))
	wantValue, wantOK := new(big.Rat).SetString(want)
	return gotOK && wantOK && gotValue.Cmp(wantValue) == 0
}

// SQLite stores literal digits that fit a signed 64-bit integer exactly,
// so a NUMERIC(30,0) field takes every one of them, the int64 bounds
// included, and reads back the same number. Whole values SQLite reads
// through a float and nonzero values a float rounds to zero are refused
// instead of stored changed. The default DECIMAL(19,4) column keeps its
// own limit, and a TEXT column keeps any literal as sent.
func TestDecimalSQLiteStorageBoundaries(t *testing.T) {
	routes, database := decimalStorageRoutes(t)
	for _, sample := range []struct{ field, literal string }{
		{"units", "1234567890123456789"},
		{"units", "9223372036854775807"},
		{"units", "-9223372036854775808"},
		{"units", "123456789012345678"},
		{"units", "1000000000000000000"},
		{"units", "1e18"},
		{"tiny", "0.0000000000000000001"},
		{"tiny", "1e-307"},
		{"tiny", "0e-400"},
		{"memo", "1e-400"},
		{"amount", "123456789012345"},
		{"real_value", "123456789012345"},
		{"float_value", "123456789012345"},
		{"double_value", "123456789012345"},
		{"point_value", "9223372036854775807"},
		{"text_value", "9223372036854775807"},
	} {
		created := sendDecimalRequest(routes, http.MethodPost, "/decimal_storage", fmt.Sprintf(`{%q:%q}`, sample.field, sample.literal))
		if created.Code != http.StatusCreated {
			t.Errorf("create %s=%s: %d %s", sample.field, sample.literal, created.Code, created.Body.String())
			continue
		}
		identifier := fmt.Sprint(decimalResponseRow(t, created)["id"])
		read := sendDecimalRequest(routes, http.MethodGet, "/decimal_storage/"+identifier, "")
		if read.Code != http.StatusOK {
			t.Fatalf("read %s: %d %s", identifier, read.Code, read.Body.String())
		}
		if got := decimalResponseRow(t, read)[sample.field]; !sameDecimalText(got, sample.literal) {
			t.Errorf("%s=%s read back %v", sample.field, sample.literal, got)
		}
		var stored string
		if err := database.QueryRow(fmt.Sprintf(`SELECT CAST(%s AS TEXT) FROM decimal_storage WHERE id = ?`, sample.field), identifier).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !sameDecimalText(stored, sample.literal) {
			t.Errorf("%s=%s stored as %s", sample.field, sample.literal, stored)
		}
	}
	before := decimalStorageRows(t, database)
	for _, sample := range []struct{ field, literal, code string }{
		{"units", "9223372036854775808", "decimal_significant_digits"},
		{"units", "-9223372036854775809", "decimal_significant_digits"},
		{"units", "123456789012345678.0", "decimal_significant_digits"},
		{"tiny", "1e-400", "decimal_underflow"},
		{"tiny", "-1e-320", "decimal_underflow"},
		{"amount", "1234567890123456789", "decimal_integer_digits"},
		{"real_value", "9223372036854775807", "decimal_significant_digits"},
		{"real_value", "1234567890123456789", "decimal_significant_digits"},
		{"float_value", "9007199254740993", "decimal_significant_digits"},
		{"double_value", "9007199254740993", "decimal_significant_digits"},
	} {
		refused := sendDecimalRequest(routes, http.MethodPost, "/decimal_storage", fmt.Sprintf(`{%q:%q}`, sample.field, sample.literal))
		if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), sample.code) {
			t.Errorf("create %s=%s: %d %s, want 400 %s", sample.field, sample.literal, refused.Code, refused.Body.String(), sample.code)
		}
	}
	if after := decimalStorageRows(t, database); after != before {
		t.Errorf("refused writes stored %d rows", after-before)
	}
}

func decimalStorageRows(t *testing.T, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decimal_storage`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
