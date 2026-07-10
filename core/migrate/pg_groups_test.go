package migrate_test

// Real-Postgres integration tests for migration groups. These prove the
// group-aware path actually works on Postgres: selective apply, the legacy
// single-column-PK → composite-key upgrade via ALTER, cross-group version
// collision after upgrade, and advisory-lock serialization of concurrent
// deployers that each carry groups.
//
// Skips automatically when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func pgPKColumns(t *testing.T, db *sql.DB, tableName string) []string {
	t.Helper()
	q := `SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`
	rows, err := db.Query(q, `"`+tableName+`"`)
	if err != nil {
		t.Fatalf("pg PK columns: %v", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, c)
	}
	return cols
}

// TestPG_GroupsApplySelectively proves groups apply in (version, group) order
// and that selecting a single group leaves the others pending.
func TestPG_GroupsApplySelectively(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	if err := m.Register(migrate.Migration{Version: 1, Name: "core", Up: "CREATE TABLE pg_core (id INT)", Down: "DROP TABLE pg_core"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(migrate.Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE pg_kb (id INT)", Down: "DROP TABLE pg_kb"}); err != nil {
		t.Fatal(err)
	}

	// Apply only knowledge → core stays pending.
	if err := m.Up(ctx, "knowledge"); err != nil {
		t.Fatalf("Up(knowledge): %v", err)
	}
	if !tableExists(t, db, "pg_kb") {
		t.Fatal("knowledge table not created")
	}
	if tableExists(t, db, "pg_core") {
		t.Fatal("core table should NOT exist when only knowledge was selected")
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 1 || st.Applied[0].Group != "knowledge" {
		t.Fatalf("expected only knowledge applied, got %+v", st.Applied)
	}

	// Now apply everything → core runs too.
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up all: %v", err)
	}
	if !tableExists(t, db, "pg_core") {
		t.Fatal("core table not created after full Up")
	}
}

// TestPG_LegacyTableUpgradedToGroups proves a Postgres tracking table created
// with the legacy single-column (version) PK is upgraded to the composite
// (group_name, version) key when groups come into play, and that existing
// default-group rows survive the upgrade.
func TestPG_LegacyTableUpgradedToGroups(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	if err := m.Register(migrate.Migration{Version: 1, Name: "core", Up: "CREATE TABLE ltu_core (id INT)", Down: "DROP TABLE ltu_core"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("legacy Up: %v", err)
	}
	cols := pgPKColumns(t, db, "_migrations")
	if len(cols) != 1 || cols[0] != "version" {
		t.Fatalf("legacy PK = %v, want [version]", cols)
	}

	// Add a group and Up → ALTER upgrades the PK.
	if err := m.Register(migrate.Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE ltu_kb (id INT)", Down: "DROP TABLE ltu_kb"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with groups after legacy: %v", err)
	}
	cols = pgPKColumns(t, db, "_migrations")
	if len(cols) != 2 || cols[0] != "group_name" || cols[1] != "version" {
		t.Fatalf("upgraded PK = %v, want [group_name version]", cols)
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 2 {
		t.Fatalf("expected 2 applied after upgrade, got %d", len(st.Applied))
	}
}

// TestPG_CollisionAllowedAfterUpgrade proves the uniqueness property the
// composite key exists for: after the upgrade, two groups each own a version 1
// in the same tracking table without corruption.
func TestPG_CollisionAllowedAfterUpgrade(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	if err := m.Register(migrate.Migration{Version: 1, Name: "core", Up: "CREATE TABLE coll_core (id INT)", Down: "DROP TABLE coll_core"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up default: %v", err)
	}
	if err := m.Register(migrate.Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE coll_kb (id INT)", Down: "DROP TABLE coll_kb"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with colliding version: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = 1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows with version=1 (different groups), got %d", n)
	}
}

// TestPG_GroupConcurrentBoot models two rolling-deploy replicas that each carry
// default + knowledge groups racing to migrate the same fresh database. The
// advisory lock must serialize them so migrations apply exactly once.
func TestPG_GroupConcurrentBoot(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	open := func() *sql.DB {
		d, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		d.SetMaxOpenConns(1)
		return d
	}
	dbA, dbB := open(), open()
	defer dbA.Close()
	defer dbB.Close()

	migs := []migrate.Migration{
		{Version: 1, Name: "core", Up: "CREATE TABLE gc_core (id INT)", Down: "DROP TABLE gc_core"},
		{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE gc_kb (id INT)", Down: "DROP TABLE gc_kb"},
		{Group: "knowledge", Version: 2, Name: "kb2", Up: "CREATE TABLE gc_kb2 (id INT)", Down: "DROP TABLE gc_kb2"},
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, d := range []*sql.DB{dbA, dbB} {
		wg.Add(1)
		go func(i int, d *sql.DB) {
			defer wg.Done()
			m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
			for _, mg := range migs {
				if err := m.Register(mg); err != nil {
					errs[i] = err
					return
				}
			}
			errs[i] = m.Up(context.Background())
		}(i, d)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("deployer %d: %v", i, e)
		}
	}
	var n int
	if err := dbA.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("_migrations has %d rows, want exactly 3 (each migration applied once)", n)
	}
}

// TestPG_MixedCaseTableNameWithGroups proves F2's fix: a mixed-case
// WithTableName works when the PK upgrade queries the table via $1::regclass.
// Before the fix the raw (unquoted) table name was passed, regclass folded it
// to lowercase, and Postgres reported "relation ... does not exist".
func TestPG_MixedCaseTableNameWithGroups(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres), migrate.WithTableName("MyMigrations"))
	if err := m.Register(migrate.Migration{Version: 1, Name: "core", Up: "CREATE TABLE mc_core (id INT)", Down: "DROP TABLE mc_core"}); err != nil {
		t.Fatal(err)
	}
	// Apply default only — creates the legacy table "MyMigrations".
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up default: %v", err)
	}

	// Add a group → triggers ensureCompositeKey → primaryKeyColumns → $1::regclass.
	if err := m.Register(migrate.Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE mc_kb (id INT)", Down: "DROP TABLE mc_kb"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with groups on mixed-case table: %v", err)
	}

	// Verify the PK was upgraded to composite.
	cols := pgPKColumns(t, db, "MyMigrations")
	if len(cols) != 2 || cols[0] != "group_name" || cols[1] != "version" {
		t.Fatalf("PK = %v, want [group_name version]", cols)
	}
}

func TestPG_SiblingSchemaDoesNotFalsePositive(t *testing.T) {
	// hasGroupColumn must resolve the tracking table via the search_path
	// (::regclass), NOT information_schema.columns across all schemas: a
	// sibling schema's group-aware "_migrations" of the same name would
	// otherwise flip a plain legacy app onto the group-aware read path and
	// hard-fail it with `column "group_name" does not exist`.
	db := pgtest.DB(t)
	ctx := context.Background()

	if _, err := db.Exec(`CREATE SCHEMA zz_sibling`); err != nil {
		t.Fatalf("create sibling schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE zz_sibling._migrations (
		group_name TEXT NOT NULL DEFAULT '',
		version    BIGINT NOT NULL,
		name       TEXT NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT NOW(),
		checksum   TEXT NOT NULL DEFAULT '',
		dirty      BOOLEAN NOT NULL DEFAULT FALSE,
		PRIMARY KEY (group_name, version)
	)`); err != nil {
		t.Fatalf("create sibling table: %v", err)
	}

	// Plain legacy usage in the test's own schema: no groups anywhere.
	m := pgMigrator(t, db)
	if err := m.Register(migrate.Migration{Version: 1, Name: "core", Up: "CREATE TABLE zz_sib_core (id INT)", Down: "DROP TABLE zz_sib_core"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("legacy Up with a group-aware sibling-schema table: %v", err)
	}
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st.Applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(st.Applied))
	}
}
