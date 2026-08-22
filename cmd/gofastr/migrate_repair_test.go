package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// repairT_setup scaffolds the blueprint the repair subcommand reads (one
// owner-scoped entity) and returns it plus the path of a not-yet-created
// database, chdir'd into a scratch dir.
func repairT_setup(t *testing.T) (bp string, dbPath string) {
	t.Helper()
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp = filepath.Join(dir, "gofastr.yml")
	yml := `app:
  name: testapp
entities:
  - name: tasks
    table: tasks
    owner_field: user_id
    fields:
      - name: title
        type: string
`
	if err := os.WriteFile(bp, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return bp, filepath.Join(dir, "app.db")
}

// repairT_legacyDB builds the database an app upgraded from a pre-v0.67
// release has: tasks still carries the owner-column foreign key, seeded while
// enforcement was off. One pooled connection, so the pragma that disables
// enforcement governs the seeding below (the same fixture discipline as
// framework/migrate's legacyDB).
func repairT_legacyDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=off"); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title   TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','existing')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%v\nwhile running: %s", err, stmt)
		}
	}
}

// repairT_run runs the subcommand in-process and returns its output and the
// exit code it chose (osExit is captured, not fatal; -1 means it returned
// normally). stdout must wrap the exit capture, because the sentinel panic
// that aborts an exiting run unwinds past an inner capture before it can
// read its buffer back.
func repairT_run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runMigrate(args) })
	})
	return out, code
}

func TestMigrateRepairCleanDBExitsZero(t *testing.T) {
	bp, dbPath := repairT_setup(t)
	out, code := repairT_run(t, "repair", "--from="+bp, "--db-url="+dbPath)
	if code != -1 {
		t.Fatalf("clean database exited %d instead of returning:\n%s", code, out)
	}
	if !strings.Contains(out, "No stale owner-column foreign keys") {
		t.Fatalf("clean run did not say so:\n%s", out)
	}
}

func TestMigrateRepairReportsLegacyDB(t *testing.T) {
	bp, dbPath := repairT_setup(t)
	repairT_legacyDB(t, dbPath)
	out, code := repairT_run(t, "repair", "--from="+bp, "--db-url="+dbPath)
	if code != 1 {
		t.Fatalf("report-only run exited %d with findings, want 1 — nothing gates on it:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks.user_id") {
		t.Fatalf("report does not name the finding:\n%s", out)
	}
	if !strings.Contains(out, "--apply") {
		t.Fatalf("report does not point at --apply:\n%s", out)
	}
	// Report only: the table was not rewritten, so the key is still there.
	db, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list('tasks')`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if fks != 1 {
		t.Fatalf("report-only run rewrote the schema (foreign keys on tasks = %d, want 1)", fks)
	}
}

func TestMigrateRepairAppliesAndIsClean(t *testing.T) {
	bp, dbPath := repairT_setup(t)
	repairT_legacyDB(t, dbPath)

	out, code := repairT_run(t, "repair", "--from="+bp, "--db-url="+dbPath, "--apply")
	if code != -1 {
		t.Fatalf("--apply exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "rewriting tasks") {
		t.Fatalf("apply run did not name the table it rewrote BEFORE rewriting it:\n%s", out)
	}
	// The row survived and the key is gone.
	db, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fks, rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list('tasks')`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if fks != 0 {
		t.Error("the stale key survived the repair")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("rows after repair = %d, want 1 — the rebuild lost data", rows)
	}

	// A re-run reports clean.
	out, code = repairT_run(t, "repair", "--from="+bp, "--db-url="+dbPath)
	if code != -1 || !strings.Contains(out, "No stale owner-column foreign keys") {
		t.Fatalf("re-run after repair: exit %d\n%s", code, out)
	}
}

func TestMigrateRepairNeedsBlueprint(t *testing.T) {
	_, dbPath := repairT_setup(t) // also chdirs into the scratch dir
	out, code := repairT_run(t, "repair", "--db-url="+dbPath)
	if code != 1 {
		t.Fatalf("missing --from exited %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "--from") {
		t.Fatalf("missing --from error does not name the flag:\n%s", out)
	}
}
