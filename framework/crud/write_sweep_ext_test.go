package crud_test

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// ListAll and CountAll build their scopes in two places; each refuses what
// the other refuses, so a page count never accepts a list call's refusal.
func TestCountAllRefusesWhatListAllRefuses(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`CREATE TABLE gauges (id TEXT PRIMARY KEY, at TEXT NOT NULL, n INTEGER, secret TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gauges (id, at, n, secret) VALUES ('g1', '2026-03-03 10:00:00', 1, 's')`); err != nil {
		t.Fatal(err)
	}
	definition := entity.Define("gauges", entity.EntityConfig{
		Table: "gauges", Unmanaged: true,
		Fields: []schema.Field{
			{Name: "at", Type: schema.Timestamp, Required: true},
			{Name: "n", Type: schema.Int},
			{Name: "secret", Type: schema.String, Hidden: true},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	definition.SetDB(database)
	handler := crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)

	refused := map[string]crud.ListOptions{
		"search without SearchFields": {Search: "x"},
		"filter on a Hidden field":    {Filters: []filter.ParsedFilter{{Field: "secret", Op: filter.OpEq, Value: "s"}}},
		"mistyped filter value":       {Filters: []filter.ParsedFilter{{Field: "n", Op: filter.OpEq, Value: "abc"}}},
		"legacy timestamp compare":    {Filters: []filter.ParsedFilter{{Field: "at", Op: filter.OpGt, Value: "2026-03-03 09:00:00"}}},
	}
	for name, opts := range refused {
		if _, err := handler.ListAll(t.Context(), opts); err == nil {
			t.Errorf("%s: ListAll accepted it", name)
		}
		if _, err := handler.CountAll(t.Context(), opts); err == nil {
			t.Errorf("%s: CountAll accepted it", name)
		}
	}
	control := crud.ListOptions{Filters: []filter.ParsedFilter{{Field: "n", Op: filter.OpEq, Value: "1"}}}
	if _, err := handler.ListAll(t.Context(), control); err != nil {
		t.Fatalf("control ListAll: %v", err)
	}
	if n, err := handler.CountAll(t.Context(), control); err != nil || n != 1 {
		t.Fatalf("control CountAll = %d, %v; want 1", n, err)
	}
}
