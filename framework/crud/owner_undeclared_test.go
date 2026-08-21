package crud

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// setupUndeclaredOwnerWorld builds the README "Donald's Way" shape: an
// entity whose Scope names an owner column that is NOT declared in
// Fields. The table is created by migrate.AutoMigrate, the production
// boot path, so the test sees exactly what a running app gets: before
// the Define fix, a table with no user_id column at all.
func setupUndeclaredOwnerWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t)
	ent := entity.Define("ulogs", entity.EntityConfig{
		Table:  "ulogs",
		Fields: []schema.Field{{Name: "notes", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))
	reg := newTestRegistry(t)
	reg.add(t, ent)
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	ent.SetDB(db)
	installOwnerExtractor(t)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info iterate: %v", err)
	}
	return cols
}

// AutoMigrate builds columns from GetFields(). When OwnerField names an
// undeclared column, the table must still get it, otherwise every
// owner-scoped read targets a column that does not exist.
func TestOwnerScope_UndeclaredOwnerColumnAutoCreated(t *testing.T) {
	_, db := setupUndeclaredOwnerWorld(t)
	if !tableColumns(t, db, "ulogs")["user_id"] {
		t.Fatalf("user_id column missing from AutoMigrate'd table %v — OwnerField without a declared field must still create the column", tableColumns(t, db, "ulogs"))
	}
}

// Regression: authenticated POST on an owner-scoped entity that never
// declared the owner field returned 201 but persisted an UNOWNED row,
// InjectOwner stamped body["user_id"] and doCreate silently dropped it,
// because the INSERT column list comes from GetFields().
func TestOwnerScope_UndeclaredOwnerPersistedOnCreate(t *testing.T) {
	ch, db := setupUndeclaredOwnerWorld(t)

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/ulogs", strings.NewReader(`{"notes":"hello"}`)), "carol")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	got := decodeSingleResponse(t, rec.Body.Bytes())
	// Hidden from the API surface...
	if _, leaked := got["user_id"]; leaked {
		t.Errorf("injected owner column leaked into API response: %+v", got)
	}
	// ...but persisted so scoping works.
	id, _ := got["id"].(string)
	var uid string
	if err := db.QueryRow("SELECT user_id FROM ulogs WHERE id = ?", id).Scan(&uid); err != nil {
		t.Fatalf("reading stored owner failed (column missing or row not found): %v", err)
	}
	if uid != "carol" {
		t.Fatalf("stored user_id = %q, want carol — the InjectOwner stamp was dropped on insert", uid)
	}
}

// Regression: authenticated GET/LIST on such an entity 500'd with
// "no such column: user_id". ApplyOwnerScope adds WHERE user_id = ?
// against a column AutoMigrate never created.
func TestOwnerScope_UndeclaredOwnerScopesReads(t *testing.T) {
	ch, _ := setupUndeclaredOwnerWorld(t)

	post := func(user, notes string) string {
		t.Helper()
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/ulogs", strings.NewReader(`{"notes":"`+notes+`"}`)), user)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create as %s: status = %d, body=%s", user, rec.Code, rec.Body.String())
		}
		got := decodeSingleResponse(t, rec.Body.Bytes())
		id, _ := got["id"].(string)
		return id
	}
	bobID := post("bob", "bob row")
	post("alice", "alice row")

	// List as alice: 200 with exactly her row (pre-fix: 500 no such column).
	listReq := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ulogs", nil), "alice")
	listRec := httptest.NewRecorder()
	ch.List()(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("authenticated list status = %d, want 200 (pre-fix: 500 no such column: user_id). body=%s", listRec.Code, listRec.Body.String())
	}
	var resp ListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, listRec.Body.String())
	}
	if resp.Total != 1 || len(resp.Data) != 1 {
		t.Fatalf("alice sees total=%d len=%d, want her 1 row only", resp.Total, len(resp.Data))
	}
	if _, leaked := resp.Data[0]["user_id"]; leaked {
		t.Errorf("injected owner column leaked into list response: %+v", resp.Data[0])
	}

	// Cross-user GET by id: bob's row must 404 for alice.
	getReq := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ulogs/"+bobID, nil), "alice")
	getReq.SetPathValue("id", bobID)
	getRec := httptest.NewRecorder()
	ch.Get()(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("cross-user GET status = %d, want 404. body=%s", getRec.Code, getRec.Body.String())
	}
}
