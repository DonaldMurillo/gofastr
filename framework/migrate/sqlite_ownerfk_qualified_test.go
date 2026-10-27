package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// qualifiedTaskEntity is taskEntity on table.
func qualifiedTaskEntity(t *testing.T, table string) testReg {
	ent := taskEntity(t)
	ent.Config.Table = table
	return testReg{"tasks": ent}
}

func countRow(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// The repair scan quoted a qualified main.tasks as the one identifier
// "main.tasks", which names no table, so it found nothing to repair; and
// the rebuild it feeds looked the table up, renamed it back and replayed
// its indexes and triggers by that same spelling.
func TestRepairOwnerFKQualifiedTable(t *testing.T) {
	ctx := context.Background()
	t.Run("main", func(t *testing.T) {
		db := legacyDB(t,
			`CREATE TABLE users (id TEXT PRIMARY KEY)`,
			`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, title TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
			`CREATE INDEX idx_tasks_title ON tasks(title)`,
			`INSERT INTO tasks VALUES ('t1', 'auth-user-1', 'x')`,
		)
		stale, err := FindStaleOwnerForeignKeys(ctx, db, qualifiedTaskEntity(t, "main.tasks"))
		if err != nil || len(stale) != 1 {
			t.Fatalf("stale = %+v, %v; want the user_id key", stale, err)
		}
		if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
			t.Fatalf("repair: %v", err)
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM pragma_foreign_key_list('tasks')`); n != 0 {
			t.Fatalf("tasks still holds %d foreign key(s)", n)
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_title'`); n != 1 {
			t.Fatal("the index was not replayed")
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM tasks`); n != 1 {
			t.Fatalf("tasks holds %d rows after the rebuild, want 1", n)
		}
	})
	t.Run("attached", func(t *testing.T) {
		db := legacyDB(t,
			`ATTACH DATABASE ':memory:' AS aux`,
			// main gets a sqlite_sequence of its own, so an unqualified
			// lookup finds main's and misses aux's high-water mark.
			`CREATE TABLE main.counter (id INTEGER PRIMARY KEY AUTOINCREMENT)`,
			`INSERT INTO main.counter DEFAULT VALUES`,
			`CREATE TABLE aux.users (id TEXT PRIMARY KEY)`,
			`CREATE TABLE aux.tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT, title TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
			`CREATE INDEX aux.idx_tasks_title ON tasks(title)`,
			`CREATE TABLE aux.log (n INTEGER)`,
			`CREATE TRIGGER aux.trg_tasks AFTER INSERT ON tasks BEGIN INSERT INTO log VALUES (NEW.id); END`,
			`INSERT INTO aux.tasks (user_id, title) VALUES ('auth-user-1', 'a'), ('auth-user-1', 'b'), ('auth-user-1', 'c')`,
			`DELETE FROM aux.tasks WHERE id = 3`,
		)
		stale, err := FindStaleOwnerForeignKeys(ctx, db, qualifiedTaskEntity(t, "aux.Tasks"))
		if err != nil || len(stale) != 1 {
			t.Fatalf("stale = %+v, %v; want the user_id key", stale, err)
		}
		if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
			t.Fatalf("repair: %v", err)
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM pragma_foreign_key_list('tasks', 'aux')`); n != 0 {
			t.Fatalf("aux.tasks still holds %d foreign key(s)", n)
		}
		for _, name := range []string{"idx_tasks_title", "trg_tasks"} {
			if n := countRow(t, db, `SELECT COUNT(*) FROM aux.sqlite_master WHERE name = ?`, name); n != 1 {
				t.Errorf("%s was not replayed into aux", name)
			}
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM main.sqlite_master WHERE type IN ('table', 'index', 'trigger') AND name NOT IN ('counter', 'sqlite_sequence')`); n != 0 {
			t.Errorf("the rebuild left %d object(s) in main", n)
		}
		if _, err := db.Exec(`INSERT INTO aux.tasks (user_id, title) VALUES ('auth-user-2', 'd')`); err != nil {
			t.Fatal(err)
		}
		if n := countRow(t, db, `SELECT MAX(id) FROM aux.tasks`); n != 4 {
			t.Errorf("the next AUTOINCREMENT id = %d, want 4: the high-water mark was lost", n)
		}
		if n := countRow(t, db, `SELECT COUNT(*) FROM aux.log`); n != 4 {
			t.Errorf("aux.log holds %d row(s), want 4: three before the repair, one from the insert after it", n)
		}
	})
	t.Run("attached violation is reported", func(t *testing.T) {
		db := legacyDB(t,
			`ATTACH DATABASE ':memory:' AS aux`,
			`CREATE TABLE aux.users (id TEXT PRIMARY KEY)`,
			`CREATE TABLE aux.projects (id TEXT PRIMARY KEY)`,
			`CREATE TABLE aux.tasks (id TEXT PRIMARY KEY, user_id TEXT, project_id TEXT,
				FOREIGN KEY (user_id) REFERENCES users(id), FOREIGN KEY (project_id) REFERENCES projects(id))`,
			`INSERT INTO aux.tasks VALUES ('t1', 'auth-user-1', 'no-such-project')`,
		)
		stale, err := FindStaleOwnerForeignKeys(ctx, db, qualifiedTaskEntity(t, "aux.tasks"))
		if err != nil || len(stale) != 1 {
			t.Fatalf("stale = %+v, %v; want the user_id key", stale, err)
		}
		err = RepairStaleOwnerForeignKeys(ctx, db, stale)
		if err == nil || !strings.Contains(err.Error(), "violation") {
			t.Fatalf("repair = %v, want the dangling project_id reported", err)
		}
	})
}
