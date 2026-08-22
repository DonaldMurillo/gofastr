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
	return legacyDBOn(t, "sqlite3", ddl...)
}

// legacyDBOn is legacyDB on a named driver, so a test can seat a fault
// injector under the same fixture.
func legacyDBOn(t *testing.T, driverName string, ddl ...string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open(driverName, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
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

// SQLite stores the CREATE TABLE text verbatim in sqlite_master, comments and
// all, and a hand-written legacy migration is exactly where a comment turns up.
// A comma inside one is not a column separator, and reading it as one split a
// definition in two: the following column was absorbed as type words of a
// phantom column, and the result was DDL SQLite ACCEPTS. That is the dangerous
// shape, because the rebuild then reports success on a mangled table.
func TestRepairSurvivesSQLCommentsInStoredDDL(t *testing.T) {
	cases := map[string]string{
		"line comment with a comma": `CREATE TABLE tasks (
			id      TEXT PRIMARY KEY, -- the pk, obviously
			user_id TEXT NOT NULL,
			title   TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		"comment above the owner column": `CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			-- the owner, stamped from the session
			user_id TEXT NOT NULL REFERENCES users(id),
			title   TEXT
		)`,
		"block comment holding a comma and a keyword": `CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			/* user_id, title: FOREIGN KEY (title) REFERENCES nope(id) */
			user_id TEXT NOT NULL,
			title   TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		"comment naming a column that does not exist": `CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL, -- references users(id), historically
			title   TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
	}
	for name, ddl := range cases {
		t.Run(name, func(t *testing.T) {
			db := legacyDB(t, `CREATE TABLE users (id TEXT PRIMARY KEY)`, ddl,
				`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','kept')`)
			ctx := context.Background()
			stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": taskEntity(t)})
			if err != nil {
				t.Fatal(err)
			}
			if len(stale) != 1 {
				t.Fatalf("the owner key was not found: %+v", stale)
			}
			if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
				t.Fatalf("repair: %v", err)
			}

			// Every column survives. The phantom-column split loses one.
			cols := map[string]bool{}
			rows, err := db.Query(`SELECT name FROM pragma_table_info('tasks')`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				cols[n] = true
			}
			rows.Close()
			for _, want := range []string{"id", "user_id", "title"} {
				if !cols[want] {
					t.Errorf("column %q did not survive the rebuild; columns = %v", want, cols)
				}
			}
			if len(cols) != 3 {
				t.Errorf("rebuild produced %d columns, want 3 — a comment was read as a column definition: %v", len(cols), cols)
			}
			// The row is intact and creates work again.
			var title string
			if err := db.QueryRow(`SELECT title FROM tasks WHERE id='t1'`).Scan(&title); err != nil || title != "kept" {
				t.Errorf("row lost in the rebuild: title=%q err=%v", title, err)
			}
			if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','new')`); err != nil {
				t.Errorf("create still refused after the repair: %v", err)
			}
		})
	}
}

// The rewriter must hand back everything that is not the foreign key, byte for
// byte. Tokenizing the remainder to strip ON DELETE / DEFERRABLE and rejoining
// it collapsed whitespace inside whatever followed, so a quoted DEFAULT came
// out of the rebuild holding a different string than the one declared. A
// repair that quietly edits a stored value is worse than the constraint it
// removes.
func TestRepairPreservesLiteralsAfterAnInlineForeignKey(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT REFERENCES users(id) ON DELETE CASCADE DEFAULT 'guest  account',
			note    TEXT DEFAULT 'two  spaces' CHECK (note <> 'a  b')
		)`,
	)
	ctx := context.Background()
	stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("owner key not found: %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatalf("repair: %v", err)
	}

	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"'guest  account'", "'two  spaces'", "'a  b'"} {
		if !strings.Contains(ddl, want) {
			t.Errorf("the rebuild altered the literal %q:\n%s", want, ddl)
		}
	}
	// And the stored value really is the declared one.
	if _, err := db.Exec(`INSERT INTO tasks (id) VALUES ('t1')`); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT user_id FROM tasks WHERE id='t1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "guest  account" {
		t.Errorf("default stored as %q, want %q — the rewrite collapsed whitespace inside a quoted literal", got, "guest  account")
	}
}

// A composite key that merely INCLUDES the owner column is not the stale
// shape. The framework never emitted one, and it is doing real work on its
// other columns. Reporting it made AutoMigrate warn that "every create fails"
// about a constraint that is working, and made `--apply` abort on a healthy
// schema telling the operator to rebuild the table by hand. The scanner and
// the rewriter have to agree on what is stale, or the report describes tables
// the repair then refuses.
func TestFindStaleOwnerFKIgnoresACompositeKey(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE memberships (user_id TEXT, org TEXT, PRIMARY KEY (user_id, org))`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, org TEXT,
			FOREIGN KEY (user_id, org) REFERENCES memberships(user_id, org))`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("a composite key including the owner column was reported as stale: %+v", stale)
	}
}

// A single-column key on the owner column IS the stale shape, alongside a
// composite one on the same table. Without this the fix above could simply
// stop reporting anything and still pass the test before it.
func TestFindStaleOwnerFKStillFindsTheSingleColumnKey(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE memberships (user_id TEXT, org TEXT, PRIMARY KEY (user_id, org))`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, org TEXT,
			FOREIGN KEY (user_id, org) REFERENCES memberships(user_id, org),
			FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("want exactly the single-column owner key, got %+v", stale)
	}
	if stale[0].References != "users" {
		t.Errorf("reported the composite key's target %q instead of the stale key's", stale[0].References)
	}
}

// The pragma restore has to run on EVERY exit from the repair, not just the
// happy one. The dangerous window is after enforcement is turned off and
// before it is turned back on: sql.Conn.Close returns a connection to the pool
// rather than destroying it, so leaving that window with the pragma off hands
// the next writer an unenforced connection, and with MaxOpenConns(1) the next
// writer is guaranteed to get it.
//
// Driven through a rebuild that fails after the pragma is off, by naming a
// table that does not exist. Deterministic, where cancelling mid-copy is not.
func TestRepairRestoresEnforcementOnTheFailurePath(t *testing.T) {
	db := legacyDB(t, `CREATE TABLE users (id TEXT PRIMARY KEY)`)
	err := RepairStaleOwnerForeignKeys(context.Background(), db, []StaleOwnerFK{
		{Entity: "ghosts", Table: "ghosts", Column: "user_id", References: "users"},
	})
	if err == nil {
		t.Fatal("repairing a table that does not exist reported success")
	}

	// MaxOpenConns(1), so this is the connection the repair ran on. Asserted
	// through the pragma rather than a write, because the fixture deliberately
	// seeds with enforcement off.
	var on int
	if qerr := db.QueryRow("PRAGMA foreign_keys").Scan(&on); qerr != nil {
		t.Fatal(qerr)
	}
	if on != 1 {
		t.Error("SECURITY: a failed repair returned a connection to the pool with foreign key enforcement off")
	}
}

// A view over the rebuilt table stopped the repair dead. Modern ALTER TABLE
// RENAME re-resolves every object naming the table, and by the rename the
// original has been dropped, so SQLite fails the transaction with
// `error in view <name>: no such table`. The table was left intact, so this
// was a refusal rather than corruption, but a repair that cannot run on a
// schema with a view is not a repair path.
func TestRepairSurvivesADependentView(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, title TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id))`,
		`CREATE VIEW open_tasks AS SELECT id, title FROM tasks`,
		`CREATE INDEX idx_tasks_title ON tasks(title)`,
		`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','kept')`,
	)
	ctx := context.Background()
	stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("owner key not found: %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatalf("a dependent view blocked the repair: %v", err)
	}

	// The view still resolves against the replacement table.
	var title string
	if err := db.QueryRow(`SELECT title FROM open_tasks WHERE id='t1'`).Scan(&title); err != nil {
		t.Fatalf("the view no longer resolves after the rebuild: %v", err)
	}
	if title != "kept" {
		t.Errorf("view returned %q, want kept", title)
	}
	// And the repair did its job.
	if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','new')`); err != nil {
		t.Errorf("create still refused after the repair: %v", err)
	}
}

// legacy_alter_table changes how every later ALTER on the connection behaves,
// and the connection goes back to the pool, so it has to be restored on the
// way out just like foreign_keys. Leaving it on is a quieter defect than an
// unenforced key: nothing fails until someone renames a table.
func TestRepairRestoresLegacyAlterTable(t *testing.T) {
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
	var legacy int
	if err := db.QueryRow("PRAGMA legacy_alter_table").Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != 0 {
		t.Error("legacy_alter_table was left on for the pooled connection; a later rename will not update its references")
	}
}

// Three defects introduced by the previous round's own fixes, each caught by
// re-review rather than by the tests that shipped with them.
func TestRepairHandlesAwkwardButLegalDDL(t *testing.T) {
	t.Run("quoted owner column with an inline key", func(t *testing.T) {
		// stripLeadingComments used skipInert, which treats a QUOTED run as
		// inert too. At offset zero the first token is the column name, so
		// `"user_id" TEXT REFERENCES …` was stripped to ` TEXT REFERENCES …`,
		// the matcher compared TEXT against the owner column, and the repair
		// refused a table it can rebuild.
		db := legacyDB(t,
			`CREATE TABLE users (id TEXT PRIMARY KEY)`,
			`CREATE TABLE tasks (
				"id"      TEXT PRIMARY KEY,
				"user_id" TEXT NOT NULL REFERENCES users(id),
				"title"   TEXT
			)`,
			`INSERT INTO tasks ("id", "user_id", "title") VALUES ('t1','auth-user-1','kept')`,
		)
		repairAndAssertCreatable(t, db)
	})

	t.Run("modifier wrapped across lines", func(t *testing.T) {
		// The modifier match required a literal single space, so a wrapped
		// `ON DELETE\n CASCADE` consumed nothing and left the action stranded
		// without its REFERENCES clause, which SQLite rejects.
		db := legacyDB(t,
			`CREATE TABLE users (id TEXT PRIMARY KEY)`,
			"CREATE TABLE tasks (\n\tid TEXT PRIMARY KEY,\n\tuser_id TEXT REFERENCES users(id)\n\t\tON DELETE\n\t\tCASCADE,\n\ttitle TEXT\n)",
			`INSERT INTO tasks (id, user_id, title) VALUES ('t1','auth-user-1','kept')`,
		)
		repairAndAssertCreatable(t, db)
	})

}

// repairAndAssertCreatable runs the repair and checks the thing that matters:
// the rows survived and the create the framework makes on every request works.
func repairAndAssertCreatable(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	stale, err := FindStaleOwnerForeignKeys(ctx, db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("the owner key was not found: %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(ctx, db, stale); err != nil {
		t.Fatalf("repair refused a table it can rebuild: %v", err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM tasks WHERE id='t1'`).Scan(&title); err != nil || title != "kept" {
		t.Errorf("row lost in the rebuild: %q %v", title, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=on"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, user_id, title) VALUES ('t2','auth-user-2','new')`); err != nil {
		t.Errorf("create still refused after the repair: %v", err)
	}
}

// Enforcement must be restored on EVERY early return, including the one
// between turning foreign keys off and the rebuild. That window was reachable:
// if enabling legacy_alter_table failed the function returned before `restore`
// existed, and the deferred cleanup only closed the connection, which returns
// it to the pool.
//
// Proved by failing that exact statement in the driver. Asserting the pragmas
// after a SUCCESSFUL repair, which is what this test used to do, never enters
// the window at all: it passes just as happily against the broken version.
func TestRepairRestoresEnforcementWhenTheSecondPragmaFails(t *testing.T) {
	db := legacyDBOn(t, failLegacyPragmaDriver,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	// Without this the whole test is vacuous: RepairStaleOwnerForeignKeys
	// returns immediately on an empty slice, so it never sets the pragmas and
	// the assertions below would pass on a scanner that stopped reporting.
	if len(stale) != 1 {
		t.Fatalf("the owner key was not found, so the repair never ran: %+v", stale)
	}

	err = RepairStaleOwnerForeignKeys(context.Background(), db, stale)
	if err == nil {
		t.Fatal("the repair reported success while legacy_alter_table=ON was failing")
	}
	if !strings.Contains(err.Error(), errLegacyPragmaInjected.Error()) {
		t.Fatalf("a different failure than the injected one: %v", err)
	}

	// MaxOpenConns(1), so this is the connection the repair ran on.
	for _, p := range []string{"foreign_keys", "legacy_alter_table"} {
		var v int
		if qerr := db.QueryRow("PRAGMA " + p).Scan(&v); qerr != nil {
			t.Fatal(qerr)
		}
		want := 1
		if p == "legacy_alter_table" {
			want = 0
		}
		if v != want {
			t.Errorf("SECURITY: PRAGMA %s = %d on the pooled connection after the early return, want %d", p, v, want)
		}
	}
}

// The pragma still has to be back after a repair that SUCCEEDS. Separate test,
// because a single one asserting both would pass on either path alone.
func TestRepairRestoresEnforcementAfterASuccessfulRun(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("the owner key was not found, so the repair never ran: %+v", stale)
	}
	if err := RepairStaleOwnerForeignKeys(context.Background(), db, stale); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"foreign_keys", "legacy_alter_table"} {
		var v int
		if err := db.QueryRow("PRAGMA " + p).Scan(&v); err != nil {
			t.Fatal(err)
		}
		want := 1
		if p == "legacy_alter_table" {
			want = 0
		}
		if v != want {
			t.Errorf("PRAGMA %s = %d after the repair, want %d", p, v, want)
		}
	}
}
