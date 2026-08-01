package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestCreate_AutoIncrementPK_AssignedByDB proves an auto-incrementing integer
// primary key is assigned by the DATABASE, not stamped by the framework. Two
// creates must yield distinct, monotonically-incrementing ids. The framework
// must omit the column from INSERT so the SQLite rowid alias (or the Postgres
// SERIAL sequence) fills it; sending a 0 placeholder would collide on every
// insert after the first.
func TestCreate_AutoIncrementPK_AssignedByDB(t *testing.T) {
	// "INTEGER PRIMARY KEY" on SQLite aliases the rowid and auto-increments
	// when the column is omitted from INSERT — exactly what SQLType emits.
	dbc := setupDB(t, `CREATE TABLE counters (id INTEGER PRIMARY KEY, label TEXT)`)
	ent := entity.Define("counters", entity.EntityConfig{
		Name: "counters", Table: "counters",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement, ReadOnly: true},
			{Name: "label", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	ch := NewCrudHandler(ent, dbc)

	create := func(label string) string {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/counters",
			strings.NewReader(`{"label":"`+label+`"}`)), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q = %d, body=%s", label, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	first := create("a")
	second := create("b")
	// SQLite rowids start at 1; the DB assigns 1 then 2.
	if !strings.Contains(first, `"id":1`) {
		t.Errorf("first create: expected DB-assigned id 1, got %s", first)
	}
	if !strings.Contains(second, `"id":2`) {
		t.Errorf("second create: expected DB-assigned id 2 (distinct, incremented), got %s", second)
	}
}
