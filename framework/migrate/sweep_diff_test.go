package migrate

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSweepDiffStaleRenameHintIgnored(t *testing.T) {
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}}, nil, "id")
	ent.Config.Renames = map[string]string{"title": "heading"}
	changes, err := diffEntityFromLive(ent, nil, DialectSQLite, map[string]string{"id": "TEXT", "title": "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("a rename to an undeclared name produced %v", changes)
	}
}

func TestSweepDiffPostgresRetypeIsDestructive(t *testing.T) {
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}, {Name: "n", Type: schema.Int}}, nil, "id")
	changes, err := diffEntityFromLive(ent, nil, DialectPostgres, map[string]string{"id": "text", "n": "text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Destructive {
		t.Fatalf("a Postgres retype is not one destructive change: %+v", changes)
	}
}

func TestSweepDiffDropDownKeepsType(t *testing.T) {
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	changes, err := diffEntityFromLive(ent, nil, DialectPostgres, map[string]string{"id": "text", "n": "bigint"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.HasSuffix(changes[0].Down, "ADD COLUMN n bigint") {
		t.Fatalf("a dropped column's Down does not re-add its type: %+v", changes)
	}
}

func TestSweepDiffKeepsBothTenantColumns(t *testing.T) {
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	ent.Config.Scope.MultiTenant = true
	ent.Config.Scope.TenantField = "org_id"
	changes, err := diffEntityFromLive(ent, nil, DialectPostgres, map[string]string{"id": "text", "org_id": "text", "tenant_id": "text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("the diff drops a tenant column: %+v", changes)
	}
}

func TestSweepDiffApplyRefusesDestructive(t *testing.T) {
	db := sweepDB(t)
	sweepExec(t, db, `CREATE TABLE w (id TEXT PRIMARY KEY, n TEXT)`)
	changes := []SchemaChange{
		{Summary: "add", SQL: "ALTER TABLE w ADD COLUMN m TEXT"},
		{Summary: "drop", SQL: "ALTER TABLE w DROP COLUMN n", Destructive: true},
	}
	var refused *DestructiveChangeError
	if _, err := ApplySchemaDiff(context.Background(), db, changes); !errors.As(err, &refused) {
		t.Fatalf("ApplySchemaDiff = %v, want a DestructiveChangeError", err)
	}
	if cols := sweepColumns(t, db, "w"); cols["n"] == "" || cols["m"] != "" {
		t.Fatalf("a refused change set ran: %v", cols)
	}
}

func TestSweepDiffForeignKeyIndexNotDoubled(t *testing.T) {
	ent := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}, {Name: "user_id", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToOne, Name: "user", Entity: "users", ForeignKey: []string{"user_id"}}}, "id")
	ent.Config.Indices = []entity.Index{{Name: "posts_by_user", Columns: []string{"user_id"}}}
	if n := len(entityIndices(ent)); n != 1 {
		t.Fatalf("a declared index on the foreign key gained an automatic twin: %d indexes", n)
	}
}

func TestSweepDiffRebuildCopiesAddedKeyColumn(t *testing.T) {
	db := sweepDB(t)
	sweepExec(t, db, `CREATE TABLE w (id TEXT PRIMARY KEY)`, `INSERT INTO w VALUES ('a')`)
	ent := rawEnt("w", "w", []schema.Field{{Name: "code", Type: schema.String, Default: "k"}}, nil, "code")
	reg := testReg{"w": ent}
	changes, err := DiffSchema(context.Background(), db, reg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplySchemaDiffWithOptions(context.Background(), db, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM w WHERE code = 'k'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the rebuild onto an added key column kept %d rows (%v)", n, err)
	}
}

func TestSweepDiffColumnReadersAgree(t *testing.T) {
	db, name := sweepPG(t)
	other := name + "_app"
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA IF EXISTS " + other + " CASCADE") })
	sweepExec(t, db,
		`CREATE TABLE mixedaccount (id TEXT, "Title" TEXT, title VARCHAR(20))`,
		`CREATE TABLE "MixedAccount" (other TEXT)`,
		"CREATE SCHEMA "+other,
		"CREATE TABLE "+other+".widgets (id BIGINT)",
		`CREATE TABLE widgets (name TEXT)`)
	ctx := context.Background()
	tables := []string{"MixedAccount", other + ".widgets", "absent"}
	bulk, err := ReadLiveColumnsBulk(ctx, db, tables, DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		one, err := readEntityColumnsQ(ctx, db, table, DialectPostgres)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(one, bulk[table]) {
			t.Errorf("%s: bulk read %v, single read %v", table, bulk[table], one)
		}
	}
	if got := bulk["MixedAccount"]; len(got) != 2 || got["title"] != "character varying(20)" {
		t.Errorf("the folded table read as %v", got)
	}
}

func TestSweepDiffSkipsOtherDialectRoutines(t *testing.T) {
	plan := Plan{Routines: []Routine{
		{Name: "pg_only", Up: "CREATE FUNCTION pg_only() RETURNS int AS 'SELECT 1' LANGUAGE sql", Down: "DROP FUNCTION pg_only()", Dialect: DialectPostgres},
		{Name: "a", Up: "CREATE VIEW a AS SELECT 1 AS x", Down: "DROP VIEW a"},
		{Name: "b", Up: "CREATE VIEW b AS SELECT 2 AS x", Down: "DROP VIEW b"},
		{Name: "a", Up: "CREATE VIEW a AS SELECT 1 AS x", Down: "DROP VIEW a"},
	}}
	up, _, next, err := GeneratePlan(plan, SchemaSnapshot{}, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(up, "pg_only") {
		t.Errorf("a Postgres routine was generated for SQLite:\n%s", up)
	}
	if _, ok := next.Routines["pg_only"]; ok {
		t.Error("the SQLite snapshot recorded a Postgres routine")
	}
	if next.Routines["a"].Order != 1 || next.Routines["b"].Order != 2 {
		t.Errorf("recorded orders a=%d b=%d, want 1 and 2", next.Routines["a"].Order, next.Routines["b"].Order)
	}
}

func TestSweepDiffRepeatedRoutineKeepsLastBody(t *testing.T) {
	plan := Plan{Routines: []Routine{
		{Name: "v", Up: "CREATE VIEW v AS SELECT 1 AS first_body", Down: "DROP VIEW v"},
		{Name: "v", Up: "CREATE VIEW v AS SELECT 2 AS last_body", Down: "DROP VIEW v"},
	}}
	up, _, _, err := GeneratePlan(plan, SchemaSnapshot{}, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(up, "first_body") || !strings.Contains(up, "last_body") {
		t.Fatalf("a repeated routine did not keep its last definition:\n%s", up)
	}
}

func TestSweepDiffNextVersionIsPastTheHighest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"10_b.sql", "9_a.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- +migrate Up\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if v := nextMigrationVersion(dir); v != 11 {
		t.Fatalf("next version = %d, want 11", v)
	}
}

func TestSweepDiffRemovedIndexIsDestructive(t *testing.T) {
	changes := diffTableIndices("w", "w", nil, []string{"CREATE INDEX IF NOT EXISTS ix_w_a ON w (a)"})
	if len(changes) != 1 || !changes[0].Destructive {
		t.Fatalf("a removed index is not one destructive change: %+v", changes)
	}
}

func TestSweepDiffWidthComparedOnlyWhenBothNameOne(t *testing.T) {
	if !typesEquivalent("VARCHAR(50)", "character varying", DialectPostgres) {
		t.Fatal("a live VARCHAR read without a width diffed against a declared width")
	}
}

func TestSweepDiffRebuildKeepsColumnsBeforeCompositeKey(t *testing.T) {
	db := sweepDB(t)
	sweepExec(t, db, `CREATE TABLE w (a TEXT, b TEXT, extra TEXT, PRIMARY KEY (a, b))`, `INSERT INTO w VALUES ('x', 'y', 'kept')`)
	ent := rawEnt("w", "w", []schema.Field{{Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}}, nil, "")
	ent.Config.PrimaryKey = []string{"a", "b"}
	c, err := sqliteRebuildChange(ent, nil, DialectSQLite, map[string]string{"a": "TEXT", "b": "TEXT", "extra": "TEXT"}, nil, "rebuild", true)
	if err != nil {
		t.Fatal(err)
	}
	sweepExec(t, db, strings.Split(c.SQL, ";\n")...)
	var extra string
	if err := db.QueryRow(`SELECT extra FROM w`).Scan(&extra); err != nil || extra != "kept" {
		t.Fatalf("the rebuild lost its kept column: %q %v", extra, err)
	}
}

func TestSweepDiffSharedKeyChangeStaysIrreversible(t *testing.T) {
	keeps := rawEnt("a", "w", []schema.Field{{Name: "id", Type: schema.String}, {Name: "code", Type: schema.String}}, nil, "code")
	drops := rawEnt("b", "w", []schema.Field{{Name: "code", Type: schema.String}}, nil, "code")
	prev := SchemaSnapshot{
		Tables:      map[string]map[string]string{"w": {"id": "TEXT", "code": "TEXT"}},
		PrimaryKeys: map[string][]string{"w": {"id"}},
	}
	up, down, _, err := GeneratePlan(Plan{Registry: testReg{"a": keeps, "b": drops}}, prev, DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if up == "" || down != "" {
		t.Fatalf("a key change off a dropped column shipped a Down:\nup:\n%s\ndown:\n%s", up, down)
	}
}

func sweepGenerated(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, e.Name())
		}
	}
	return out
}

func sweepPending(t *testing.T) (Plan, string, string) {
	t.Helper()
	plan := Plan{Registry: testReg{"w": rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")}}
	up, down, _, err := GeneratePlan(plan, SchemaSnapshot{}, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	return plan, up, down
}

func TestSweepDiffReconcileNeedsTheSameDown(t *testing.T) {
	dir := t.TempDir()
	plan, up, _ := sweepPending(t)
	if err := os.WriteFile(filepath.Join(dir, "0001_old.sql"), []byte(renderMigrationFile(1, "old", up, "SELECT 'another down'")), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateMigrationFile(plan, "next", MigrationFileOptions{MigrationsDir: dir, Dialect: DialectSQLite})
	if err != nil || path == "" {
		t.Fatalf("a migration with another Down was taken as this delta: path=%q err=%v", path, err)
	}
}

func TestSweepDiffReconcileReadsTheHighestVersion(t *testing.T) {
	dir := t.TempDir()
	plan, up, down := sweepPending(t)
	files := map[string]string{
		"9_same.sql":   renderMigrationFile(9, "same", up, down),
		"10_other.sql": renderMigrationFile(10, "other", "SELECT 1;", ""),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path, err := GenerateMigrationFile(plan, "next", MigrationFileOptions{MigrationsDir: dir, Dialect: DialectSQLite})
	if err != nil || filepath.Base(path) != "0011_next.sql" {
		t.Fatalf("generate after 9 and 10 wrote %q (%v), files %v", path, err, sweepGenerated(t, dir))
	}
}

func TestSweepDiffMigrationNameIsOneSlug(t *testing.T) {
	if got := sanitizeMigrationName("  Add  User--Email! "); got != "add_user_email" {
		t.Fatalf("slug = %q", got)
	}
	if got := sanitizeMigrationName("!!"); got != "migration" {
		t.Fatalf("an empty slug = %q", got)
	}
}
