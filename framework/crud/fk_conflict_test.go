package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// fkHandlers builds parent/child handlers over SQLite with foreign-key
// enforcement on, so a dangling reference fails in the driver.
func fkHandlers(t *testing.T) (parents, children *CrudHandler, db *sql.DB) {
	t.Helper()
	db = setupDB(t,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE fk_parents (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE fk_children (id TEXT PRIMARY KEY, parent_id TEXT NOT NULL REFERENCES fk_parents(id))`,
	)
	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
		t.Fatalf("foreign_keys = %d (%v): the test needs enforcement on", on, err)
	}
	p := entity.Define("fk_parents", entity.EntityConfig{
		Name: "fk_parents", Table: "fk_parents",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	p.SetDB(db)
	c := entity.Define("fk_children", entity.EntityConfig{
		Name: "fk_children", Table: "fk_children",
		Fields: []schema.Field{{Name: "parent_id", Type: schema.String}},
	}.WithTimestamps(false))
	c.SetDB(db)
	return NewCrudHandler(p, db).WithJSONCase(CaseSnake),
		NewCrudHandler(c, db).WithJSONCase(CaseSnake), db
}

func TestForeignKeyViolation_Returns409(t *testing.T) {
	parents, children, db := fkHandlers(t)

	// Create with a dangling reference.
	req := withTestUser(httptest.NewRequest("POST", "/fk_children",
		strings.NewReader(`{"parent_id":"missing"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	children.Create()(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dangling FK create = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "FOREIGN") || strings.Contains(rec.Body.String(), "fk_") {
		t.Errorf("409 body leaked driver detail: %s", rec.Body.String())
	}

	// Delete a parent that still has children.
	if _, err := db.Exec(`INSERT INTO fk_parents (id, name) VALUES ('p1', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO fk_children (id, parent_id) VALUES ('c1', 'p1')`); err != nil {
		t.Fatal(err)
	}
	req = withTestUser(httptest.NewRequest("DELETE", "/fk_parents/p1", nil), "u1")
	req.SetPathValue("id", "p1")
	rec = httptest.NewRecorder()
	parents.Delete()(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced parent = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "FOREIGN") {
		t.Errorf("409 body leaked driver detail: %s", rec.Body.String())
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	for _, msg := range []string{
		"constraint failed: FOREIGN KEY constraint failed (787)",
		`ERROR: update or delete on table "a" violates foreign key constraint "b" on table "c" (SQLSTATE 23503)`,
		`pq: insert or update on table "c" violates foreign key constraint "b"`,
		"Error 1451: Cannot delete or update a parent row",
		"Error 1452: Cannot add or update a child row",
	} {
		if !isForeignKeyViolation(&covStrErr{msg}) {
			t.Errorf("should detect FK violation in %q", msg)
		}
	}
	if isForeignKeyViolation(nil) || isForeignKeyViolation(&covStrErr{"some other error"}) {
		t.Error("nil/unrelated error misclassified")
	}
}
