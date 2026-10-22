package crud

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A composite-keyed entity's tools take one argument per key column,
// named as the write tools name its field, and address the record by its
// own path: a value holding a slash reaches its row, not a sibling.
func TestMCPToolsTakeCompositeKey(t *testing.T) {
	db := setupDB(t, `CREATE TABLE enrollments (student_id TEXT NOT NULL, course_id TEXT NOT NULL, grade TEXT, PRIMARY KEY (student_id, course_id))`)
	ent := entity.Define("enrollments", entity.EntityConfig{
		Table: "enrollments",
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
		},
		PrimaryKey: []string{"student_id", "course_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	r := router.New()
	ch := NewCrudHandler(ent, db)
	RegisterCrudRoutes(r, ch, "/enrollments")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	ctx := handler.SetUser(context.Background(), &testUser{id: "u1"})

	for _, g := range []string{"A", "C"} {
		course := map[string]string{"A": "c9", "C": "c8"}[g]
		if _, err := srv.CallTool(ctx, "enrollments_create", map[string]any{"student_id": "s/1", "course_id": course, "grade": g}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	key := map[string]any{"student_id": "s/1", "course_id": "c9"}
	got, err := srv.CallTool(ctx, "enrollments_get", key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.(map[string]any)["grade"] != "A" {
		t.Fatalf("get = %v, want grade A", got)
	}
	if _, err := srv.CallTool(ctx, "enrollments_get", map[string]any{"id": "s%2F1/c9"}); err == nil {
		t.Fatal("get by a joined id succeeded; a composite key takes one argument per column")
	}
	upd, err := srv.CallTool(ctx, "enrollments_update", map[string]any{"student_id": "s/1", "course_id": "c9", "grade": "B"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.(map[string]any)["grade"] != "B" {
		t.Fatalf("update = %v, want grade B", upd)
	}
	del, err := srv.CallTool(ctx, "enrollments_delete", key)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if m := del.(map[string]any); m["deleted"] != true || m["student_id"] != "s/1" || m["course_id"] != "c9" {
		t.Fatalf("delete = %v", del)
	}
	sib, err := srv.CallTool(ctx, "enrollments_get", map[string]any{"student_id": "s/1", "course_id": "c8"})
	if err != nil || sib.(map[string]any)["grade"] != "C" {
		t.Fatalf("sibling row = %v, %v; want grade C intact", sib, err)
	}

	s := idToolSchema(ch.toolKeyArgs())
	if req := s["required"].([]string); !slices.Equal(req, []string{"student_id", "course_id"}) {
		t.Errorf("get schema required = %v", req)
	}
}
