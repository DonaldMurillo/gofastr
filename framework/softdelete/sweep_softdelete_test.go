package softdelete

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func notesDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, deleted_at TIMESTAMP); INSERT INTO notes (id) VALUES ('a'), ('b')`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestHelpersWriteOnlyTheNamedRow(t *testing.T) {
	conn := notesDB(t)
	ctx := context.Background()
	state := func() (deletedA, deletedB bool, rows int) {
		if err := conn.QueryRow(`SELECT COUNT(CASE WHEN id='a' AND deleted_at IS NOT NULL THEN 1 END), COUNT(CASE WHEN id='b' AND deleted_at IS NOT NULL THEN 1 END), COUNT(*) FROM notes`).Scan(&deletedA, &deletedB, &rows); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := SoftDelete(ctx, conn, "notes", "a"); err != nil {
		t.Fatal(err)
	}
	if a, b, n := state(); !a || b || n != 2 {
		t.Fatalf("after SoftDelete(a): a=%v b=%v rows=%d, want only a stamped and both kept", a, b, n)
	}
	if err := Restore(ctx, conn, "notes", "a"); err != nil {
		t.Fatal(err)
	}
	if a, _, _ := state(); a {
		t.Fatal("Restore left deleted_at set")
	}
	if err := ForceDelete(ctx, conn, "notes", "a"); err != nil {
		t.Fatal(err)
	}
	if _, b, n := state(); n != 1 || b {
		t.Fatalf("after ForceDelete(a): rows=%d, want b alone", n)
	}
}

func TestHelpersRefuseAnUnsafeTable(t *testing.T) {
	conn := notesDB(t)
	if _, err := conn.Exec(`CREATE TABLE "odd name" (id TEXT PRIMARY KEY, deleted_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"SoftDelete":  func() error { return SoftDelete(context.Background(), conn, "odd name", "a") },
		"Restore":     func() error { return Restore(context.Background(), conn, "odd name", "a") },
		"ForceDelete": func() error { return ForceDelete(context.Background(), conn, "odd name", "a") },
	} {
		if err := run(); err == nil {
			t.Errorf("%s accepted an unsafe table name", name)
		}
	}
}

func TestTrashedFlagAndFilter(t *testing.T) {
	if !WithTrashed(httptest.NewRequest("GET", "/?trashed=true", nil)) || WithTrashed(httptest.NewRequest("GET", "/?trashed=1", nil)) {
		t.Fatal("WithTrashed must be true for trashed=true only")
	}
	for show, want := range map[bool]bool{false: true, true: false} {
		qb := query.Select("id").From("notes")
		ApplySoftDeleteFilter(qb, show)
		if sqlStr, _ := qb.Build(); strings.Contains(sqlStr, "deleted_at IS NULL") != want {
			t.Errorf("showTrashed=%v built %q", show, sqlStr)
		}
	}
	ent := &entity.Entity{Config: entity.EntityConfig{Scope: &entity.ScopeConfig{}}}
	if !WithSoftDelete(ent).Config.Scope.SoftDelete {
		t.Fatal("WithSoftDelete did not set Scope.SoftDelete")
	}
}

func TestWithSoftDeleteOnAnEntityWithoutScope(t *testing.T) {
	ent := WithSoftDelete(&entity.Entity{Config: entity.EntityConfig{Name: "notes"}})
	if ent.Config.Scope == nil || !ent.Config.Scope.SoftDelete {
		t.Fatal("WithSoftDelete did not create the scope it sets")
	}
}
