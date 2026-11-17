package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

const (
	sweepFailFKOnDriver   = "sqlite3-sweep-fail-fk-on"
	sweepFailRenameDriver = "sqlite3-sweep-fail-rename"
)

var errSweepInjected = errors.New("injected by the sweep test driver")

func init() {
	probe, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		panic(err)
	}
	base := probe.Driver()
	_ = probe.Close()
	sql.Register(sweepFailFKOnDriver, sweepFailDriver{base, func(q string) bool {
		return strings.EqualFold(strings.TrimSpace(q), "PRAGMA foreign_keys=on")
	}})
	sql.Register(sweepFailRenameDriver, sweepFailDriver{base, func(q string) bool {
		return strings.HasPrefix(q, "ALTER TABLE") && strings.Contains(q, "__gofastr_repair")
	}})
}

type sweepFailDriver struct {
	driver.Driver
	fail func(string) bool
}

func (d sweepFailDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return sweepFailConn{c, d.fail}, nil
}

type sweepFailConn struct {
	driver.Conn
	fail func(string) bool
}

func (c sweepFailConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.fail(q) {
		return nil, errSweepInjected
	}
	ec, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return ec.ExecContext(ctx, q, args)
}

func sweepStale(t *testing.T, db *sql.DB) []StaleOwnerFK {
	t.Helper()
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": taskEntity(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("owner key not found: %+v", stale)
	}
	return stale
}

func TestSweepSQLiteRepairRetiresConnWhenRestoreFails(t *testing.T) {
	db := legacyDBOn(t, sweepFailFKOnDriver,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	err := RepairStaleOwnerForeignKeys(context.Background(), db, sweepStale(t, db))
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("want the retire error, got %v", err)
	}
	var on int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Error("SECURITY: the connection whose foreign_keys could not be restored went back to the pool")
	}
}

func TestSweepSQLiteRestoreSurvivesCancel(t *testing.T) {
	db := legacyDB(t, `CREATE TABLE users (id TEXT PRIMARY KEY)`)
	ctx, cancel := context.WithCancel(context.Background())
	fnErr := errors.New("fn failed")
	err := withSQLiteRebuildConn(ctx, db, func(*sql.Conn) error {
		cancel()
		return fnErr
	})
	if !errors.Is(err, fnErr) || strings.Contains(err.Error(), "additionally") {
		t.Fatalf("a cancelled rebuild did not restore its pragmas: %v", err)
	}
}

func TestSweepSQLiteRepairRefusesWhenKeyNotInDDL(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, title TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
	)
	err := RepairStaleOwnerForeignKeys(context.Background(), db, []StaleOwnerFK{
		{Entity: "tasks", Table: "tasks", Column: "title", References: "users"},
	})
	if err == nil || !strings.Contains(err.Error(), "could not locate it") {
		t.Fatalf("a rebuild that removes no key reported %v", err)
	}
}

func TestSweepSQLiteRepairKeepsMarkWhenAllRowsDeleted(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT REFERENCES users(id), title TEXT)`,
		`INSERT INTO tasks (user_id, title) VALUES ('u','one')`,
		`INSERT INTO tasks (user_id, title) VALUES ('u','two')`,
		`DELETE FROM tasks`,
	)
	stale, err := FindStaleOwnerForeignKeys(context.Background(), db, testReg{"tasks": autoIncTaskEntity(t)})
	if err != nil || len(stale) != 1 {
		t.Fatalf("stale=%+v err=%v", stale, err)
	}
	if err := RepairStaleOwnerForeignKeys(context.Background(), db, stale); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`INSERT INTO tasks (user_id, title) VALUES ('u','three') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 3 {
		t.Errorf("new row got id %d, want 3: the emptied table lost its AUTOINCREMENT mark", id)
	}
}

func TestSweepSQLiteRepairKeepsTableOptions(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT REFERENCES users(id), title TEXT) WITHOUT ROWID`,
	)
	if err := RepairStaleOwnerForeignKeys(context.Background(), db, sweepStale(t, db)); err != nil {
		t.Fatal(err)
	}
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='tasks'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "WITHOUT ROWID") {
		t.Errorf("the rebuild dropped the table options:\n%s", ddl)
	}
}

func TestSweepSQLiteFailedRebuildLeavesTableIntact(t *testing.T) {
	db := legacyDBOn(t, sweepFailRenameDriver,
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT, title TEXT, FOREIGN KEY (user_id) REFERENCES users(id))`,
		`INSERT INTO tasks VALUES ('t1','u','kept')`,
	)
	if err := RepairStaleOwnerForeignKeys(context.Background(), db, sweepStale(t, db)); !errors.Is(err, errSweepInjected) {
		t.Fatalf("want the injected rename failure, got %v", err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM tasks WHERE id='t1'`).Scan(&title); err != nil || title != "kept" {
		t.Fatalf("a failed rebuild lost the original table: %q %v", title, err)
	}
	var leftovers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '%__gofastr_repair'`).Scan(&leftovers); err != nil {
		t.Fatal(err)
	}
	if leftovers != 0 {
		t.Error("a failed rebuild left its replacement table behind")
	}
}

func TestSweepSQLiteReferencesIsAWholeWord(t *testing.T) {
	for _, def := range []string{
		`user_id my_references REFERENCES users(id)`,
		`user_id references_t REFERENCES users(id)`,
	} {
		got, ok := columnDefWithoutReferences(def, "user_id")
		want := strings.TrimSuffix(def, " REFERENCES users(id)")
		if !ok || got != want {
			t.Errorf("%s: got %q (%v), want %q", def, got, ok, want)
		}
	}
}

func TestSweepSQLiteCommentBeforeReferencesTarget(t *testing.T) {
	got, ok := columnDefWithoutReferences(`user_id TEXT REFERENCES /* auth */ users(id) NOT NULL`, "user_id")
	if !ok || got != "user_id TEXT NOT NULL" {
		t.Errorf("got %q (%v), want %q", got, ok, "user_id TEXT NOT NULL")
	}
}
