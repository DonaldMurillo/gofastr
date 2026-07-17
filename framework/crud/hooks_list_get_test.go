package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func setupHookableHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		owner TEXT NOT NULL,
		body TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "owner", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	return ch, db
}

func TestBeforeListHookCanFilter(t *testing.T) {
	ch, db := setupHookableHandler(t)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES
		('n1','alice','a-one'),
		('n2','bob','b-one'),
		('n3','alice','a-two')`); err != nil {
		t.Fatal(err)
	}

	// The hook should be able to inject a WHERE clause that scopes the
	// query to alice. Today no BeforeList hook fires from List(), so this
	// test must FAIL until we wire it up.
	hookFired := false
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		hookFired = true
		p, ok := data.(*hook.ListPayload)
		if !ok {
			t.Fatalf("BeforeList payload type = %T, want *hook.ListPayload", data)
		}
		p.AddWhere("owner = $1", "alice")
		return nil
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if !hookFired {
		t.Fatal("BeforeList hook did not fire")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Errorf("BeforeList filter ignored: total=%d, want 2", resp.Total)
	}
}

func TestBeforeGetHookCanFilter(t *testing.T) {
	ch, db := setupHookableHandler(t)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES
		('n1','alice','secret'),
		('n2','bob','other')`); err != nil {
		t.Fatal(err)
	}

	hookFired := false
	ch.Hooks.RegisterHook(hook.BeforeGet, func(ctx context.Context, data any) error {
		hookFired = true
		p, ok := data.(*hook.GetPayload)
		if !ok {
			t.Fatalf("BeforeGet payload type = %T, want *hook.GetPayload", data)
		}
		if p.ID != "n1" {
			t.Errorf("BeforeGet ID=%q, want n1", p.ID)
		}
		// Scope to bob only — alice's row should 404.
		p.AddWhere("owner = $1", "bob")
		return nil
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes/n1", nil), "u1")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	if !hookFired {
		t.Fatal("BeforeGet hook did not fire")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d (want 404). body=%s", rec.Code, rec.Body.String())
	}
}

// TestUpdate_RejectsPresentEmptyRequired pins the data-corruption check at
// the CRUD handler level — even with ValidatePartial, sending an explicit
// empty-string for a Required field must fail validation, NOT silently
// blank the column.
func TestUpdate_RejectsPresentEmptyRequired(t *testing.T) {
	ch, db := setupHookableHandler(t)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES ('n1', 'alice', 'original')`); err != nil {
		t.Fatal(err)
	}

	req := withTestUser(httptest.NewRequest(http.MethodPut, "/notes/n1",
		strings.NewReader(`{"owner":""}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("PUT with empty Required succeeded (data corruption): body=%s", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	// Confirm row unchanged.
	var owner string
	if err := db.QueryRow(`SELECT owner FROM notes WHERE id = ?`, "n1").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "alice" {
		t.Errorf("row was mutated: owner = %q", owner)
	}
}

// TestUpdate_AbsentFieldNoOp pins the partial-update happy path: missing a
// Required field in the body just skips it (the existing row already
// satisfies the requirement), the body's non-Required fields apply,
// AND the existing Required field's value is preserved on disk.
func TestUpdate_AbsentFieldNoOp(t *testing.T) {
	ch, db := setupHookableHandler(t)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES ('n1', 'alice', 'original')`); err != nil {
		t.Fatal(err)
	}

	req := withTestUser(httptest.NewRequest(http.MethodPut, "/notes/n1",
		strings.NewReader(`{"body":"updated"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("partial PUT failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Confirm the field we DID send was applied.
	var body string
	if err := db.QueryRow(`SELECT body FROM notes WHERE id=?`, "n1").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "updated" {
		t.Errorf("body not applied: got %q, want %q", body, "updated")
	}
	// Confirm the Required field we DID NOT send was preserved.
	var owner string
	if err := db.QueryRow(`SELECT owner FROM notes WHERE id=?`, "n1").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "alice" {
		t.Errorf("absent Required field was clobbered: owner = %q, want alice", owner)
	}
}

func TestAfterListHookCanMutateResults(t *testing.T) {
	ch, db := setupHookableHandler(t)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES
		('n1','alice','sensitive')`); err != nil {
		t.Fatal(err)
	}

	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			t.Fatalf("AfterList payload type = %T", data)
		}
		for i := range p.Results {
			p.Results[i]["body"] = "REDACTED"
		}
		return nil
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	var resp ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0]["body"] != "REDACTED" {
		t.Errorf("AfterList mutation lost: %+v", resp.Data)
	}
}
