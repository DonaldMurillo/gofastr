package crud

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestUpdateRestampsUpdatedAt pins that doUpdate advances updated_at on every
// UPDATE rather than leaving it frozen at the creation timestamp.
func TestUpdateRestampsUpdatedAt(t *testing.T) {
	installSecurityOwnerExtractor(t)
	// Timestamps default to true ⇒ created_at + updated_at are injected.
	cfg := entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
	}
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, created_at TEXT, updated_at TEXT)`)
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "title": "orig", "created_at": "2020-01-01T00:00:00Z", "updated_at": "2020-01-01T00:00:00Z"},
	})

	req := httptest.NewRequest("PATCH", "/posts/p1", nil)
	err := ch.inTx(req.Context(), func(ctx context.Context, ch *CrudHandler) error {
		_, err := ch.doUpdate(ctx, req, "p1", map[string]any{"title": "changed"})
		return err
	})
	if err != nil {
		t.Fatalf("doUpdate: %v", err)
	}

	var updatedAt string
	if err := db.QueryRow("SELECT updated_at FROM posts WHERE id = $1", "p1").Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if updatedAt == "2020-01-01T00:00:00Z" || updatedAt == "" {
		t.Fatalf("updated_at not restamped on UPDATE (got %q)", updatedAt)
	}
}

// TestUpdateAllRestampsUpdatedAt is the bulk-update analogue: TypedQuery.
// UpdateAll must restamp updated_at too.
func TestUpdateAllRestampsUpdatedAt(t *testing.T) {
	db := setupDB(t, `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, created_at TEXT, updated_at TEXT)`)
	ent := entity.Define("posts", entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
	})
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "title": "orig", "created_at": "2020-01-01T00:00:00Z", "updated_at": "2020-01-01T00:00:00Z"},
	})

	type row struct {
		ID string `json:"id"`
	}
	n, err := NewTypedQuery[row](ch).
		Where(entity.NewStringColumn("title").Eq("orig")).
		UpdateAll(context.Background(), map[string]any{"title": "changed"})
	if err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if n != 1 {
		t.Fatalf("UpdateAll affected %d rows, want 1", n)
	}

	var updatedAt string
	if err := db.QueryRow("SELECT updated_at FROM posts WHERE id = $1", "p1").Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if updatedAt == "2020-01-01T00:00:00Z" || updatedAt == "" {
		t.Fatalf("updated_at not restamped on bulk UpdateAll (got %q)", updatedAt)
	}
}
