package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type widget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

func setupWidgetApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE widgets (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT 'draft',
		created_at TEXT,
		updated_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, title, state) VALUES
		('w1','published widget','published'),
		('w2','draft widget','draft')`); err != nil {
		t.Fatal(err)
	}

	app := NewApp(WithDB(db))
	app.Entity("widgets", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "state", Type: schema.String},
		},
	})
	return app, db
}

// TestOnBeforeList_TypedReceivesPayload pins the typed-hook API: callers
// register a typed BeforeList handler and receive *hook.ListPayload
// directly, parallel to OnBeforeCreate / OnBeforeUpdate.
func TestOnBeforeList_TypedReceivesPayload(t *testing.T) {
	app, _ := setupWidgetApp(t)

	var captured *hook.ListPayload
	OnBeforeList(app, "widgets", func(ctx context.Context, p *hook.ListPayload) error {
		captured = p
		p.AddWhere("state = $1", "published")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if captured == nil {
		t.Fatal("OnBeforeList typed hook did not fire")
	}
	if captured.Request == nil {
		t.Errorf("typed hook received nil Request")
	}

	var resp struct {
		Total int `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Errorf("typed BeforeList filter ignored: total=%d, want 1 (only published)", resp.Total)
	}
}

// TestOnBeforeGet_TypedScopesLookup pins the BeforeGet typed wrapper.
func TestOnBeforeGet_TypedScopesLookup(t *testing.T) {
	app, _ := setupWidgetApp(t)

	OnBeforeGet(app, "widgets", func(ctx context.Context, p *hook.GetPayload) error {
		// Only allow 'published' widgets through.
		p.AddWhere("state = $1", "published")
		return nil
	})

	// w1 (published) → 200
	pub := httptest.NewRequest(http.MethodGet, "/widgets/w1", nil)
	pubRec := httptest.NewRecorder()
	app.Router().ServeHTTP(pubRec, pub)
	if pubRec.Code != http.StatusOK {
		t.Errorf("published widget should be visible: status=%d body=%s", pubRec.Code, pubRec.Body.String())
	}

	// w2 (draft) → 404
	draft := httptest.NewRequest(http.MethodGet, "/widgets/w2", nil)
	draftRec := httptest.NewRecorder()
	app.Router().ServeHTTP(draftRec, draft)
	if draftRec.Code != http.StatusNotFound {
		t.Errorf("draft widget should be hidden: status=%d body=%s", draftRec.Code, draftRec.Body.String())
	}
}

// TestOnAfterList_TypedCanMutateResults pins AfterList's typed wrapper.
func TestOnAfterList_TypedCanMutateResults(t *testing.T) {
	app, _ := setupWidgetApp(t)

	OnAfterList(app, "widgets", func(ctx context.Context, p *hook.ListPayload) error {
		for _, row := range p.Results {
			row["title"] = "REDACTED"
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, row := range resp.Data {
		if row["title"] != "REDACTED" {
			t.Errorf("AfterList typed mutation lost on row: %+v", row)
		}
	}
}

// TestOnAfterGet_TypedCanMutateResult pins AfterGet's typed wrapper.
func TestOnAfterGet_TypedCanMutateResult(t *testing.T) {
	app, _ := setupWidgetApp(t)

	OnAfterGet(app, "widgets", func(ctx context.Context, p *hook.GetPayload) error {
		if p.Result != nil {
			p.Result["title"] = "REDACTED"
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets/w1", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["title"] != "REDACTED" {
		t.Errorf("AfterGet typed mutation lost: %+v", got)
	}
}
