package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A caller-supplied created_at reached the upsert INSERT as sent, so SQLite
// stored "…T00:00:00Z" beside rows in schema.TimestampLayout: text order
// broke, a filter on the instant missed it, and a cursor over the column
// repeated rows.
func TestUpsertBindsStoredTimestamp(t *testing.T) {
	ch, db := stampedItems(t, nil)
	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id": "a", "title": "x", "created_at": "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT created_at FROM items WHERE id = 'a'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-01T00:00:00.000000Z"; stored != want {
		t.Fatalf("stored created_at = %q, want %q", stored, want)
	}
	rec := httptest.NewRecorder()
	ch.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?created_at=2026-01-01T00:00:00Z"}))
	if resp := decodeListResponse(t, rec.Body.String()); len(resp.Data) != 1 {
		t.Fatalf("filter on the sent instant = %d rows, want 1: %s", len(resp.Data), rec.Body.String())
	}
}

// A BeforeCreate hook may set a generated column as a time.Time; the
// driver's own time layout is not the stored one.
func TestCreateHookTimeBindsStoredLayout(t *testing.T) {
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		data.(map[string]any)["created_at"] = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		return nil
	})
	ch, db := stampedItems(t, reg)
	if _, err := ch.CreateOne(context.Background(), map[string]any{"id": "b", "title": "y"}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT created_at FROM items WHERE id = 'b'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-01T00:00:00.000000Z"; stored != want {
		t.Fatalf("stored created_at = %q, want %q", stored, want)
	}
}

// The DO NOTHING arm (the body names only the key) re-reads the existing
// row; under a read scope that hides it the upsert failed with no rows
// instead of answering the id, as every other write does.
func TestUpsertNothingHiddenReturnsID(t *testing.T) {
	ch := readBackNotes(t, nil)
	got, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n2"})
	if err != nil {
		t.Fatalf("upsert of a hidden row: %v", err)
	}
	assertIDOnly(t, "DO NOTHING on a draft", got, "n2")
	got, err = ch.UpsertOne(context.Background(), map[string]any{"id": "n1"})
	if err != nil || got["body"] != "pub" {
		t.Fatalf("DO NOTHING on a published row = %v, %v; want the full row", got, err)
	}
}

func stampedItems(t *testing.T, hooks *hook.HookRegistry) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, created_at TEXT, updated_at TEXT)`)
	ent := entity.Define("items", entity.EntityConfig{
		Table:    "items",
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	})
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hooks
	return ch, db
}
