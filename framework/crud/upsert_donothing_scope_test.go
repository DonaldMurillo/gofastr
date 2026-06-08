package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestUpsertDoNothingSkipsSoftDeleted pins that an UpsertOne whose update
// set is empty (only PK + auto fields) and lands on a soft-deleted row does
// NOT surface that row through the DO-NOTHING fallback SELECT.
func TestUpsertDoNothingSkipsSoftDeleted(t *testing.T) {
	installSecurityOwnerExtractor(t)
	// Entity whose only non-auto column is the PK ⇒ ON CONFLICT DO NOTHING.
	cfg := makeEntityConfig("tags", "tags", "", []schema.Field{
		{Name: "id", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true })
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, deleted_at TEXT)`)
	seedRows(t, db, "tags", []map[string]any{
		{"id": "t1", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	row, err := ch.UpsertOne(context.Background(), map[string]any{"id": "t1"})
	if err == nil {
		t.Fatalf("upsert surfaced soft-deleted row via DO-NOTHING fallback: %v", row)
	}
	if !errors.Is(err, errSoftDeletedResurrection) {
		t.Fatalf("want errSoftDeletedResurrection, got %v", err)
	}
}
