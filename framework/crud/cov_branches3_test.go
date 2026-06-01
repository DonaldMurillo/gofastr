package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func TestRequireTenantContext_Present(t *testing.T) {
	db := setupDB(t, `CREATE TABLE mt3 (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mt3", entity.EntityConfig{
		Name: "mt3", Table: "mt3", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := tenant.SetTenantID(context.Background(), "T1")
	if err := ch.requireTenantContext(ctx); err != nil {
		t.Errorf("tenant present should pass: %v", err)
	}
}

func TestPool_OversizedDrop(t *testing.T) {
	// Slice with cap above the pool cap is dropped rather than retained.
	big := make([]map[string]any, 0, maxPooledMapEntries+1)
	returnRowSlice(&big)
	// Ptr slice grow path: borrow larger than initial cap.
	p := borrowPtrSlice(maxPooledMapEntries + 1)
	if len(*p) != maxPooledMapEntries+1 {
		t.Errorf("borrowPtrSlice grow len = %d", len(*p))
	}
	returnPtrSlice(p) // oversized → dropped
}

func TestJsonKeysFor_Mismatch(t *testing.T) {
	ch, _ := covNotesHandler(t) // visible: id,title,body
	// A column subset that differs from the cached visible set → convertedKeys path.
	keys := ch.jsonKeysFor([]string{"id", "title"})
	if len(keys) != 2 {
		t.Errorf("jsonKeysFor subset = %v", keys)
	}
	// Same length, different last col → mismatch branch.
	keys = ch.jsonKeysFor([]string{"id", "title", "other"})
	if len(keys) != 3 {
		t.Errorf("jsonKeysFor mismatch = %v", keys)
	}
}

func TestBuildExistsSubquery_UnknownRelType(t *testing.T) {
	nf := nestedFilter{
		Relation: entity.Relation{Type: entity.RelationType(99), Name: "r", Entity: "x"},
		Field:    "name", Op: filter.OpEq, Value: "v",
	}
	if sql, _ := buildExistsSubquery("posts", "id", nf); sql != "1 = 0" {
		t.Errorf("unknown rel type sql = %q, want '1 = 0'", sql)
	}
}

func TestSplitSegmentFilter_CloseBeforeOpen(t *testing.T) {
	// ")(" → close index < open index → treated as no-filter, raw name.
	name, f := splitSegmentFilter("rel)x(y")
	if f != "" {
		t.Errorf("close-before-open filter = %q, want empty", f)
	}
	_ = name
}

func TestParseScopedFilters_Errors(t *testing.T) {
	// Missing '=' in a scoped filter clause.
	if _, err := parseScopedFilters("nofield", nil, "path"); err == nil {
		t.Error("missing = should error")
	}
	// Empty entries skipped, valid entry parsed.
	out, err := parseScopedFilters(" , status=draft , ", nil, "path")
	if err != nil {
		t.Fatalf("parseScopedFilters: %v", err)
	}
	if len(out) != 1 || out[0].Field != "status" {
		t.Errorf("parseScopedFilters = %+v", out)
	}
	// _in expansion with pipe-separated values.
	out, err = parseScopedFilters("tag_in=a|b|c", nil, "path")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Errorf("_in scoped expansion = %d, want 3", len(out))
	}
	// _in over the cap → error.
	var sb strings.Builder
	sb.WriteString("tag_in=")
	for i := 0; i <= maxScopedINEntries; i++ {
		if i > 0 {
			sb.WriteString("|")
		}
		sb.WriteString("x")
	}
	if _, err := parseScopedFilters(sb.String(), nil, "path"); err == nil {
		t.Error("oversized scoped _in should error")
	}
	// Unknown field rejected when fields are provided.
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	if _, err := parseScopedFilters("ghost=1", fields, "path"); err == nil {
		t.Error("unknown scoped field should error")
	}
}

func TestScopedInclude_INFilter(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	// _in scoped filter on a HasMany include exercises the IN expansion +
	// filterClause path.
	req := httptest.NewRequest("GET", "/posts?include=comments(body_in=nice|ok)", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped _in include = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpsert_NoUpdatableFields_DoNothing(t *testing.T) {
	// An entity whose only column is the auto-generated PK takes the
	// "DO NOTHING" upsert branch (setParts is empty). The first insert
	// succeeds; a conflicting re-upsert hits ON CONFLICT DO NOTHING, whose
	// RETURNING yields no row — the framework surfaces that as an error.
	// We exercise the branch either way; assert only that the first insert
	// works and the conflicting one is handled (no panic).
	db := setupDB(t, `CREATE TABLE only_id (id TEXT PRIMARY KEY)`)
	ent := entity.Define("only_id", entity.EntityConfig{
		Name: "only_id", Table: "only_id",
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "x1"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Conflicting re-upsert exercises the DO NOTHING setParts==0 branch.
	_, _ = ch.UpsertOne(context.Background(), map[string]any{"id": "x1"})
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM only_id").Scan(&n)
	if n != 1 {
		t.Errorf("DO NOTHING upsert created duplicate: %d rows", n)
	}
}
