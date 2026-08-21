package migrate

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// legacyDB builds the shape a pre-v0.67 AutoMigrate produced: an entity whose
// owner column is also a relation, so the CREATE TABLE carries a foreign key
// the framework violates on every create.
func legacyDB(t *testing.T, ddl ...string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// One connection, so a PRAGMA set here governs every statement below and
	// every assertion after — with a pool, the pragma and the query land on
	// different connections and the test measures nothing.
	db.SetMaxOpenConns(1)
	// Seed with enforcement OFF, which is how the rows got there: releases
	// before v0.67 left foreign keys off on every SQLite connection, so the
	// contradictory constraint sat in the schema while writes ignored it.
	// sqlite/stdlib now turns them on by default, so the fixture has to opt
	// out to reproduce the database an upgrading app actually has.
	if _, err := db.Exec("PRAGMA foreign_keys=off"); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%v\nwhile running: %s", err, stmt)
		}
	}
	return db
}

func taskEntity(t *testing.T) *entity.Entity {
	t.Helper()
	return entity.Define("tasks", entity.EntityConfig{
		Table: "tasks",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("user", "users", "user_id")},
	}.WithTimestamps(false))
}

// The whole point of the repair: a database carrying the stale key refuses
// every create once foreign keys are enforced, and comes back working after the
// rebuild — with its rows, its indices, and its other constraints intact.
func TestRepairOwnerFKUnblocksCreates(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title   TEXT DEFAULT 'untitled',
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_tasks_title ON tasks(title)`,
		`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','existing')`,
	)
	reg := testReg{"tasks": taskEntity(t)}
	ctx := context.Background()

	stale, err := FindStaleOwnerForeignKeys(ctx, db, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("found %d stale keys, want 1: %+v", len(stale), stale)
	}
	if stale[0].Column != "user_id" || stale[0].References != "users" {
		t.Errorf("finding = %+v, want the user_id → users key", stale[0])
	}

	// Before the repair, enforcement makes the ordinary create fail — this is
	// the symptom an upgraded app actually reports.
	if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','new')`)
	if err == nil {
		t.Fatal("the stale constraint did not refuse a create — the fixture is not reproducing the bug")
	}

	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatalf("repair: %v", err)
	}

	// The create the framework makes on every request now works.
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','new')`); err != nil {
		t.Fatalf("create still refused after the repair: %v", err)
	}
	// Existing rows survived.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("row count = %d, want 2 — the rebuild lost rows", n)
	}
	// The index came back with the table.
	var idx int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_title'`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Error("idx_tasks_title did not survive the rebuild — indices are dropped with the table and must be replayed")
	}
	// The column DEFAULT survived, which is what rebuilding from the entity
	// declaration instead of the stored DDL would have silently dropped.
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "'untitled'") {
		t.Errorf("the column DEFAULT was lost in the rebuild:\n%s", ddl)
	}
	if !strings.Contains(strings.ToUpper(ddl), "NOT NULL") {
		t.Errorf("the NOT NULL constraint was lost in the rebuild:\n%s", ddl)
	}
	// And the scan is now clean, so a second run is a no-op.
	after, err := FindStaleOwnerForeignKeys(ctx, db, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("the key survived the rebuild: %+v", after)
	}
}

// The inline spelling. SQLite reports it through the same pragma, so a repair
// that only understood the table-level form would report success while leaving
// the constraint exactly where it was.
func TestRepairOwnerFKHandlesTheInlineSpelling(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title   TEXT
		)`,
		`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','x')`,
	)
	reg := testReg{"tasks": taskEntity(t)}
	ctx := context.Background()

	stale, err := FindStaleOwnerForeignKeys(ctx, db, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("the inline REFERENCES form was not found: %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','y')`); err != nil {
		t.Fatalf("create still refused: %v", err)
	}
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "NOT NULL") {
		t.Errorf("NOT NULL was stripped along with the REFERENCES clause:\n%s", ddl)
	}
}

// Foreign keys that are NOT on the owner column are the ones doing real work,
// and the repair must leave every one of them enforcing.
func TestRepairOwnerFKKeepsOtherForeignKeys(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			project_id TEXT NOT NULL,
			title      TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (project_id) REFERENCES projects(id)
		)`,
		`INSERT INTO projects (id) VALUES ('p1')`,
		`INSERT INTO tasks (id, user_id, project_id) VALUES ('t1','auth-user-1','p1')`,
	)
	ent := entity.Define("tasks", entity.EntityConfig{
		Table: "tasks",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "user_id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("user", "users", "user_id"),
			entity.BelongsTo("project", "projects", "project_id"),
		},
	}.WithTimestamps(false))
	ctx := context.Background()
	stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": ent})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("want exactly the owner key, got %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
		t.Fatal(err)
	}
	// The owner column takes any value now.
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, project_id) VALUES ('t2','auth-user-2','p1')`); err != nil {
		t.Fatalf("owner key still enforcing: %v", err)
	}
	// The project key still refuses a dangling reference. Dropping every key
	// instead of the one on the owner column is the plausible wrong fix, and it
	// would pass every assertion above.
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, project_id) VALUES ('t3','auth-user-3','nope')`); err == nil {
		t.Error("the repair dropped the project_id foreign key too — only the owner key is stale")
	}
}

// Enforcement must be back on when the repair returns. The rebuild runs with it
// off by necessity, on a pooled connection that goes on to serve ordinary
// writes; leaving it off there would silently disable the feature the whole
// v0.67 change exists to turn on.
func TestRepairRestoresForeignKeyEnforcement(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	ctx := context.Background()
	stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatal(err)
	}
	// MaxOpenConns(1), so this is the very connection the rebuild ran on.
	var on int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Error("foreign key enforcement was left off on the pooled connection the rebuild used")
	}
}

// A clean database has nothing to report and nothing to rebuild.
func TestFindStaleOwnerFKIsQuietOnACleanSchema(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("a clean schema reported %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(context.Background(), db, nil); err != nil {
		t.Errorf("repairing nothing returned %v", err)
	}
}

// The DDL rewriter is where a mistake silently corrupts a table, so its edge
// cases get direct coverage rather than only being exercised through a rebuild.
func TestDDLWithoutForeignKeyOnEdgeCases(t *testing.T) {
	cases := []struct {
		name      string
		ddl       string
		want      bool
		mustKeep  []string
		mustDrop  []string
		wantError bool
	}{
		{
			name:     "table-level key",
			ddl:      `CREATE TABLE tasks (id TEXT, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
			want:     true,
			mustKeep: []string{"user_id TEXT"},
			mustDrop: []string{"FOREIGN KEY"},
		},
		{
			name:     "named constraint",
			ddl:      `CREATE TABLE tasks (id TEXT, user_id TEXT, CONSTRAINT fk_owner FOREIGN KEY (user_id) REFERENCES users(id))`,
			want:     true,
			mustDrop: []string{"fk_owner", "FOREIGN KEY"},
		},
		{
			name:     "quoted column in the key",
			ddl:      `CREATE TABLE tasks (id TEXT, "user_id" TEXT, FOREIGN KEY ("user_id") REFERENCES users(id))`,
			want:     true,
			mustDrop: []string{"FOREIGN KEY"},
		},
		{
			// A composite key is doing work on the other column too. Dropping it
			// would remove a constraint nobody complained about.
			name: "composite key is left alone",
			ddl:  `CREATE TABLE tasks (id TEXT, user_id TEXT, org TEXT, FOREIGN KEY (user_id, org) REFERENCES memberships(user_id, org))`,
			want: false,
		},
		{
			// A CHECK constraint holds a comma inside parens and a DEFAULT can
			// hold the literal text of a constraint. Splitting on a bare comma
			// mangles both.
			name:     "commas inside a CHECK and a string default",
			ddl:      `CREATE TABLE tasks (id TEXT, kind TEXT DEFAULT 'a,b' CHECK (kind IN ('a,b','c')), user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
			want:     true,
			mustKeep: []string{"'a,b'", "CHECK (kind IN ('a,b','c'))"},
			mustDrop: []string{"FOREIGN KEY"},
		},
		{
			name:     "inline reference with an action",
			ddl:      `CREATE TABLE tasks (id TEXT, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE SET NULL, title TEXT)`,
			want:     true,
			mustKeep: []string{"user_id TEXT NOT NULL", "title TEXT"},
			mustDrop: []string{"REFERENCES", "ON DELETE"},
		},
		{
			// A column whose NAME contains the keyword must not be mistaken for
			// the keyword itself.
			name:     "a column named self_references",
			ddl:      `CREATE TABLE tasks (id TEXT, self_references TEXT, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
			want:     true,
			mustKeep: []string{"self_references TEXT"},
		},
		{
			name: "no key on the owner column",
			ddl:  `CREATE TABLE tasks (id TEXT, user_id TEXT, project_id TEXT, FOREIGN KEY (project_id) REFERENCES projects(id))`,
			want: false,
		},
		{
			name:      "not a column list at all",
			ddl:       `CREATE TABLE tasks`,
			wantError: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, removed, err := ddlWithoutForeignKeyOn(tc.ddl, "user_id", "tasks__new")
			if tc.wantError {
				if err == nil {
					t.Fatal("want an error for unparseable DDL")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if removed != tc.want {
				t.Fatalf("removed = %v, want %v (result: %s)", removed, tc.want, got)
			}
			if !removed {
				return
			}
			for _, keep := range tc.mustKeep {
				if !strings.Contains(got, keep) {
					t.Errorf("the rebuild lost %q:\n%s", keep, got)
				}
			}
			for _, drop := range tc.mustDrop {
				if strings.Contains(strings.ToUpper(got), strings.ToUpper(drop)) {
					t.Errorf("%q survived the rewrite:\n%s", drop, got)
				}
			}
			if !strings.Contains(got, "tasks__new") {
				t.Errorf("the replacement was not renamed:\n%s", got)
			}
		})
	}
}

// Every rewritten DDL has to be something SQLite will actually accept. A
// rewriter that produces plausible-looking text is worth nothing.
func TestRewrittenDDLIsValidSQLite(t *testing.T) {
	ddls := []string{
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT DEFAULT 'x', FOREIGN KEY (user_id) REFERENCES users(id))`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT REFERENCES users(id) ON UPDATE CASCADE ON DELETE NO ACTION, n INTEGER)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, kind TEXT CHECK (kind IN ('a,b','c')), user_id TEXT, CONSTRAINT fk FOREIGN KEY (user_id) REFERENCES users(id)) WITHOUT ROWID`,
	}
	db := legacyDB(t, `CREATE TABLE users (id TEXT PRIMARY KEY)`)
	for i, ddl := range ddls {
		rewritten, removed, err := ddlWithoutForeignKeyOn(ddl, "user_id", "probe")
		if err != nil || !removed {
			t.Fatalf("case %d: removed=%v err=%v", i, removed, err)
		}
		if _, err := db.Exec(rewritten); err != nil {
			t.Errorf("case %d: SQLite rejected the rewritten DDL: %v\n%s", i, err, rewritten)
			continue
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list('probe') WHERE "from"='user_id'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("case %d: the key is still there after the rewrite", i)
		}
		if _, err := db.Exec(`DROP TABLE probe`); err != nil {
			t.Fatal(err)
		}
	}
}
