package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAutoMigrateSkipsOwnerFKIndex: the owner column carries no foreign key,
// so it gets no automatic belongs_to index either. Another belongs_to on the
// same entity still gets one.
func TestAutoMigrateSkipsOwnerFKIndex(t *testing.T) {
	db := openMigrateSQLite(t)
	users := entity.Define("users", entity.EntityConfig{Table: "users"}.WithTimestamps(false))
	projects := entity.Define("projects", entity.EntityConfig{Table: "projects"}.WithTimestamps(false))
	tasks := entity.Define("tasks", entity.EntityConfig{
		Table: "tasks",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("user", "users", "user_id"),
			entity.BelongsTo("project", "projects", "project_id"),
		},
	}.WithTimestamps(false))
	reg := secTypeReg{"users": users, "projects": projects, "tasks": tasks}
	if err := AutoMigrateContext(context.Background(), db, reg); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	idx := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'tasks'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		idx[n] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !idx["idx_tasks_project_id"] {
		t.Fatalf("idx_tasks_project_id missing (have %v): the FK auto-index did not run", idx)
	}
	if idx["idx_tasks_user_id"] {
		t.Error("idx_tasks_user_id exists: the owner column got a belongs_to index")
	}
}

// TestAutoMigrateRejectsBadOnDelete: an ON DELETE action outside the four the
// dialects share is refused, not spliced into the DDL.
func TestAutoMigrateRejectsBadOnDelete(t *testing.T) {
	db := openMigrateSQLite(t)
	users := entity.Define("users", entity.EntityConfig{Table: "users"}.WithTimestamps(false))
	notes := entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("user", "users", "user_id").OnDeleteAction("CASCADE; DROP TABLE users"),
		},
	}.WithTimestamps(false))
	err := AutoMigrateContext(context.Background(), db, secTypeReg{"users": users, "notes": notes})
	if err == nil || !strings.Contains(err.Error(), "invalid ON DELETE action") {
		t.Fatalf("AutoMigrate error = %v, want an invalid ON DELETE action refusal", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'notes'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("notes was created despite the refused ON DELETE action")
	}
}
