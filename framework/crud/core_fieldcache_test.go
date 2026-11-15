package crud_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// App wiring and generated typed handlers assign JSONCase directly after
// NewCrudHandler built the camelCase snapshot; the next request serves
// the case now set.
func TestJSONCaseAssignedAfterConstruction(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`CREATE TABLE things (id TEXT PRIMARY KEY, item_name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO things (id, item_name) VALUES ('t1', 'lamp')`); err != nil {
		t.Fatal(err)
	}
	definition := entity.Define("things", entity.EntityConfig{
		Table: "things", Unmanaged: true,
		Fields:   []schema.Field{{Name: "item_name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	definition.SetDB(database)
	handler := crud.NewCrudHandler(definition, database)
	for _, c := range []struct {
		jsonCase crud.JSONCase
		key      string
	}{{crud.CaseCamel, `"itemName"`}, {crud.CaseSnake, `"item_name"`}} {
		handler.JSONCase = c.jsonCase
		response := httptest.NewRecorder()
		handler.List()(response, httptest.NewRequest(http.MethodGet, "/things", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), c.key) {
			t.Errorf("JSONCase %s: %d %s, want key %s", c.jsonCase, response.Code, response.Body.String(), c.key)
		}
	}
}

// Two columns whose camelCase forms meet (item_name, item__name) leave
// that form naming neither: an in-process body keyed itemName writes no
// column instead of whichever field was declared first.
func TestBodyKeyTwoColumnsShareNamesNeither(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`CREATE TABLE pairs (id TEXT PRIMARY KEY, item_name TEXT, item__name TEXT)`); err != nil {
		t.Fatal(err)
	}
	definition := entity.Define("pairs", entity.EntityConfig{
		Table: "pairs", Unmanaged: true,
		Fields:   []schema.Field{{Name: "item_name", Type: schema.String}, {Name: "item__name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	definition.SetDB(database)
	handler := crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)
	row, err := handler.CreateOne(t.Context(), map[string]any{"id": "p1", "itemName": "lamp"})
	if err == nil && (row["item_name"] == "lamp" || row["item__name"] == "lamp") {
		t.Fatalf("itemName was written to a column: %v", row)
	}
}
