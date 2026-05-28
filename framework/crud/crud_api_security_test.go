package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func setupOwnerCreateInProcHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT
	)`)
	ent := entity.Define("notes", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
}

func setupTenantInProcHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE docs (
		id TEXT PRIMARY KEY,
		tenant_id TEXT,
		title TEXT
	)`)
	ent := entity.Define("docs", entity.EntityConfig{
		Table: "docs",
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "docs", []map[string]any{
		{"id": "doc-a", "tenant_id": "tenant-a", "title": "Alpha"},
		{"id": "doc-b", "tenant_id": "tenant-b", "title": "Beta"},
	})
	return ch, db
}

// TestCrud_AnonymousOwnerCreateRejected pins the contract that in-proc
// Create / BatchCreate against an OwnerField entity refuses anonymous
// callers. The HTTP path has middleware-level enforcement; in-process
// callers (typed repos, jobs, seed scripts) bypass that path and would
// otherwise write orphan rows.
func TestCrud_AnonymousOwnerCreateRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch := setupOwnerCreateInProcHandler(t)

	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "hi"}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("CreateOne err=%v, want errOwnerRequired", err)
	}
	if _, err := ch.BatchCreateMany(context.Background(), []map[string]any{{"title": "hi"}}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("BatchCreateMany err=%v, want errOwnerRequired", err)
	}
}

// TestCrud_MissingTenantContextRejects pins fail-closed behaviour on
// every in-proc CRUD method touching a MultiTenant entity. The HTTP
// path uses tenant middleware to refuse; in-proc callers bypass that
// path and ApplyTenantScope alone is fail-OPEN (no tenant ⇒ no WHERE).
func TestCrud_MissingTenantContextRejects(t *testing.T) {
	ch, db := setupTenantInProcHandler(t)
	ctx := context.Background()

	if _, err := ch.GetOne(ctx, "doc-a", nil); err == nil {
		t.Fatalf("GetOne without tenant context returned no error")
	}
	if rows, err := ch.ListAll(ctx, ListOptions{}); err == nil && len(rows) > 0 {
		t.Fatalf("ListAll without tenant context returned rows: %+v", rows)
	}
	if n, err := ch.CountAll(ctx, ListOptions{}); err == nil && n != 0 {
		t.Fatalf("CountAll without tenant context returned %d", n)
	}
	if _, err := ch.UpdateOne(ctx, "doc-a", map[string]any{"title": "tampered"}); err == nil {
		t.Fatalf("UpdateOne without tenant context returned no error")
	}
	if err := ch.DeleteOne(ctx, "doc-a"); err == nil {
		t.Fatalf("DeleteOne without tenant context returned no error")
	}
	if _, err := ch.BatchUpdateMany(ctx, []string{"doc-a"}, []map[string]any{{"title": "x"}}); err == nil {
		t.Fatalf("BatchUpdateMany without tenant context returned no error")
	}
	if _, err := ch.BatchDeleteMany(ctx, []string{"doc-a"}); err == nil {
		t.Fatalf("BatchDeleteMany without tenant context returned no error")
	}

	// Sanity: the rows themselves were not mutated.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM docs").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("rejected operations still mutated rows; count=%d", n)
	}
}

// TestParseScopedFilters_CapsInListSize keeps a defensible cap on
// `field_in=a|b|...` so a single request can't blow up a JOIN with
// thousands of bind parameters.
func TestParseScopedFilters_CapsInListSize(t *testing.T) {
	values := ""
	for i := 0; i < maxScopedINEntries+1; i++ {
		if i > 0 {
			values += "|"
		}
		values += "v"
	}
	if _, err := parseScopedFilters("id_in="+values, nil, "comments"); err == nil {
		t.Fatalf("parseScopedFilters accepted IN list of %d entries (cap %d)", maxScopedINEntries+1, maxScopedINEntries)
	}
}
