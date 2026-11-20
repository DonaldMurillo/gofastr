package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"

	_ "github.com/lib/pq"
)

func sweepDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "sweep.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sweepPG(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv(pgtest.DSNEnv)
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	name := fmt.Sprintf("sweep_%d", time.Now().UnixNano())
	sweepExec(t, db, "CREATE SCHEMA "+name, "SET search_path TO "+name)
	t.Cleanup(func() {
		_, _ = db.Exec("DROP SCHEMA " + name + " CASCADE")
		_ = db.Close()
	})
	return db, name
}

func sweepExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func sweepColumns(t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()
	cols, err := ReadLiveColumnsSQLite(context.Background(), db, table)
	if err != nil {
		t.Fatal(err)
	}
	return cols
}

func panics(f func()) (did bool) {
	defer func() { did = recover() != nil }()
	f()
	return false
}

func TestSweepCoreAddColumnRereadsUnderLock(t *testing.T) {
	db := sweepDB(t)
	sweepExec(t, db, `CREATE TABLE w (id TEXT PRIMARY KEY, a TEXT)`)
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}, {Name: "a", Type: schema.String}}, nil, "id")
	stale := map[string]string{"id": "TEXT"}
	if err := addMissingColumns(context.Background(), db, ent, nil, DialectSQLite, stale); err != nil {
		t.Fatalf("a column a peer added after the pre-read was added again: %v", err)
	}
}

func TestSweepCoreBootNeverDropsOrRetypes(t *testing.T) {
	db := sweepDB(t)
	sweepExec(t, db, `CREATE TABLE w (id TEXT PRIMARY KEY, n TEXT, extra TEXT)`, `INSERT INTO w VALUES ('a', 'x', 'kept')`)
	ent := rawEnt("w", "w", []schema.Field{{Name: "id", Type: schema.String}, {Name: "n", Type: schema.Int}}, nil, "id")
	if err := AutoMigrate(db, testReg{"w": ent}); err != nil {
		t.Fatal(err)
	}
	cols := sweepColumns(t, db, "w")
	if cols["extra"] == "" || !strings.EqualFold(cols["n"], "TEXT") {
		t.Fatalf("boot dropped or retyped a column: %v", cols)
	}
}

func TestSweepCoreExpressionIndexNeedsName(t *testing.T) {
	if !panics(func() { indexDDL(DialectSQLite, "w", entity.Index{Expression: "lower(a)"}) }) {
		t.Fatal("an expression index without a Name rendered")
	}
}

func TestSweepCoreIndexExpressionRefusesComments(t *testing.T) {
	for _, expr := range []string{"lower(a); DROP TABLE w", "lower(a) -- x", "lower(a) /* x */"} {
		if !panics(func() { sanitizeIndexExpression(expr) }) {
			t.Errorf("index expression %q was accepted", expr)
		}
	}
}

func TestSweepCoreOwnerColumnGetsNoForeignKey(t *testing.T) {
	users := rawEnt("users", "users", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	notes := rawEnt("notes", "notes", []schema.Field{{Name: "id", Type: schema.String}, {Name: "owner_id", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToOne, Name: "owner", Entity: "users", ForeignKey: []string{"owner_id"}, OnDelete: entity.OnDeleteCascade}}, "id")
	notes.Config.Scope.OwnerField = "owner_id"
	all := map[string]*entity.Entity{"users": users, "notes": notes}
	for _, d := range []Dialect{DialectSQLite, DialectPostgres} {
		ddl, err := buildCreateTableSQL(notes, all, d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(ddl, "FOREIGN KEY") {
			t.Errorf("%s: the owner column got a foreign key:\n%s", d, ddl)
		}
	}
	if refs := mutatingFKReferrers(users, all); len(refs) != 0 {
		t.Errorf("an owner-column relation counts as a cascading referrer: %v", refs)
	}
}

func TestSweepCoreUnionDedupesUnnamedIndex(t *testing.T) {
	mk := func(version string) *entity.Entity {
		e := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}}, nil, "id")
		e.Config.Indices = []entity.Index{{Columns: []string{"title"}}}
		e.Version = version
		return e
	}
	merged := UnionEntities(multiVersionRegistry{mk("/api/v1"), mk("/api/v2")})["posts"]
	if n := len(merged.Config.Indices); n != 1 {
		t.Fatalf("two versions declaring one unnamed index merged to %d indexes", n)
	}
}

func TestSweepCoreUnionKeepsEveryManyToMany(t *testing.T) {
	mk := func(version string, rels ...string) *entity.Entity {
		var rs []entity.Relation
		for _, r := range rels {
			rs = append(rs, entity.Relation{Type: entity.RelManyToMany, Name: r, Entity: r, Through: "posts_" + r})
		}
		e := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}}, rs, "id")
		e.Version = version
		return e
	}
	merged := UnionEntities(multiVersionRegistry{mk("/api/v1", "tags"), mk("/api/v2", "tags", "cats")})["posts"]
	if n := len(merged.Config.Relations); n != 2 {
		t.Fatalf("the union kept %d of two many_to_many relations", n)
	}
}

func TestSweepCoreUnmanagedEntityGetsNoDDL(t *testing.T) {
	db := sweepDB(t)
	ent := rawEnt("v", "v", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	ent.Config.Unmanaged = true
	if err := MigrateEntityDialect(db, ent, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if cols := sweepColumns(t, db, "v"); len(cols) != 0 {
		t.Fatalf("an unmanaged entity got a table: %v", cols)
	}
	// The invariant's other half: an entity with no fields gets no DDL on
	// every entry point, not only at boot.
	ghost := rawEnt("ghost", "ghost", nil, nil, "")
	if err := MigrateEntityDialect(db, ghost, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if cols := sweepColumns(t, db, "ghost"); len(cols) != 0 {
		t.Fatalf("a fields-less entity got a table: %v", cols)
	}
	reg := unsortedRegistry{ghost}
	changes, err := DiffSchema(context.Background(), db, reg)
	if err != nil {
		t.Fatalf("DiffSchema refused a fields-less entity: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("DiffSchema proposed DDL for a fields-less entity: %v", changes)
	}
	previous := rawEnt("ghost", "ghost", []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "label", Type: schema.String, Unique: true},
	}, nil, "id")
	previous.Config.Indices = []entity.Index{{Columns: []string{"id", "label"}}}
	for _, tc := range []struct {
		name string
		prev SchemaSnapshot
	}{
		{"never migrated", SchemaSnapshot{Tables: map[string]map[string]string{}}},
		{"previously migrated", SnapshotFromRegistry(unsortedRegistry{previous}, DialectSQLite)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up, down, next, err := GeneratePlan(Plan{Registry: reg}, tc.prev, DialectSQLite)
			if err != nil {
				t.Fatalf("GeneratePlan refused a fields-less entity: %v", err)
			}
			if up != "" || down != "" {
				t.Fatalf("GeneratePlan proposed DDL for a fields-less entity: up=%q down=%q", up, down)
			}
			if !reflect.DeepEqual(next, tc.prev) {
				t.Fatalf("skipped entity changed the snapshot: got %+v, want %+v", next, tc.prev)
			}
		})
	}
}

func TestSweepCoreThroughSpelledWithOtherSchemaRefused(t *testing.T) {
	posts := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	tags := rawEnt("tags", "tags", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	join := rawEnt("post_tags", "post_tags", []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "tag_id", Type: schema.String}}, nil, "")
	rel := entity.Relation{Type: entity.RelManyToMany, Name: "tags", Entity: "tags", Through: "main.post_tags", LocalKey: "post_id", ForeignKeyTarget: "tag_id"}
	all := map[string]*entity.Entity{"posts": posts, "tags": tags, "post_tags": join}
	if err := checkPivotEntity(posts, rel, all); err == nil {
		t.Fatal("a Through naming the join entity's table with a schema it leaves off was accepted")
	}
}

func seededEnt(name string, seed func(context.Context, *sql.DB) error) *entity.Entity {
	e := rawEnt(name, name, []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	e.Config.Seed = seed
	return e
}

func TestSweepCoreSeedLedgerRecordTwice(t *testing.T) {
	db := sweepDB(t)
	ctx := context.Background()
	if err := ensureSeedLedger(ctx, db, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := recordSeeded(ctx, db, DialectSQLite, "posts"); err != nil {
			t.Fatalf("a second record of one seed failed: %v", err)
		}
	}
}

func TestSweepCoreSeedOnUnversionedBase(t *testing.T) {
	db := sweepDB(t)
	ran := false
	v1 := seededEnt("posts", func(context.Context, *sql.DB) error { ran = true; return nil })
	v2 := seededEnt("posts", nil)
	v2.Version = "/api/v2"
	if err := RunSeeds(context.Background(), db, multiVersionRegistry{v1, v2}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("a seed declared on one version of two never ran")
	}
}

func TestSweepCoreSeedProbeFailsClosed(t *testing.T) {
	db, m := mock(t)
	for range 3 {
		m.ExpectQuery("SELECT version").WillReturnError(errors.New("connection reset by peer"))
	}
	reg := testReg{"posts": seededEnt("posts", func(context.Context, *sql.DB) error { return nil })}
	if err := RunSeeds(context.Background(), db, reg); err == nil || !strings.Contains(err.Error(), "dialect") {
		t.Fatalf("RunSeeds on an unreachable database = %v, want a dialect error", err)
	}
}

func TestSweepCoreSeedlessRegistrySkipsProbe(t *testing.T) {
	db, m := mock(t)
	if err := RunSeeds(context.Background(), db, testReg{"posts": seededEnt("posts", nil)}); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSweepCoreSeedStopsOnCancel(t *testing.T) {
	db := sweepDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second := false
	reg := testReg{
		"a": seededEnt("a", func(context.Context, *sql.DB) error { cancel(); return nil }),
		"b": seededEnt("b", func(context.Context, *sql.DB) error { second = true; return nil }),
	}
	if err := RunSeeds(ctx, db, reg); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSeeds after a cancel = %v", err)
	}
	if second {
		t.Fatal("a seed ran after the context was cancelled")
	}
}

func TestSweepCoreSeedLockIsNotTheMigrationLock(t *testing.T) {
	db, _ := sweepPG(t)
	db.SetMaxOpenConns(4)
	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", coremig.AdvisoryLockKey); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = holder.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", coremig.AdvisoryLockKey)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg := testReg{"posts": seededEnt("posts", func(context.Context, *sql.DB) error { return nil })}
	if err := RunSeeds(ctx, db, reg); err != nil {
		t.Fatalf("seeds waited on the migration lock: %v", err)
	}
}

func TestSweepCoreSeedOnOneConnectionPool(t *testing.T) {
	db, _ := sweepPG(t)
	buf := captureMigrateSlog(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := testReg{"posts": seededEnt("posts", func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, "SELECT 1")
		return err
	})}
	if err := RunSeeds(ctx, db, reg); err != nil {
		t.Fatalf("seeds on a one-connection pool: %v", err)
	}
	if !strings.Contains(buf.String(), "seed advisory lock skipped") {
		t.Fatalf("no warning that the seed lock was skipped:\n%s", buf.String())
	}
}

func TestSweepCoreRawTableIsOnlyItsColumns(t *testing.T) {
	ent := Table{Name: "pairs", Columns: []Column{
		{Name: "store", Type: schema.String, PrimaryKey: true},
		{Name: "no", Type: schema.Int, PrimaryKey: true},
	}}.ToEntity()
	if got := ent.KeyColumns(); strings.Join(got, ",") != "store,no" {
		t.Fatalf("a raw Table's key is %v, want store,no", got)
	}
	cfg := ent.Config
	if cfg.Timestamps == nil || *cfg.Timestamps || cfg.Exposure.CRUD == nil || *cfg.Exposure.CRUD || len(ent.GetFields()) != 2 {
		t.Fatalf("a raw Table gained managed columns or CRUD: %+v", cfg)
	}
}

func TestSweepCorePivotIndexNameIsClaimed(t *testing.T) {
	posts := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToMany, Name: "tags", Entity: "tags", Through: "post_tags", LocalKey: "post_id", ForeignKeyTarget: "tag_id"}}, "id")
	tags := rawEnt("tags", "tags", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	other := rawEnt("other", "other", []schema.Field{{Name: "id", Type: schema.String}, {Name: "x", Type: schema.String}}, nil, "id")
	other.Config.Indices = []entity.Index{{Name: defaultIndexName("post_tags", []string{"tag_id"}), Columns: []string{"x"}}}
	all := map[string]*entity.Entity{"posts": posts, "tags": tags, "other": other}
	ordered, err := topoSortEntities(all)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkIndexNames(ordered, all); err == nil {
		t.Fatal("an index named like a generated pivot's index was accepted")
	}
}

func TestSweepCoreForeignKeyJoinsIntegerWidths(t *testing.T) {
	orders := rawEnt("orders", "orders", []schema.Field{{Name: "store", Type: schema.String}, {Name: "no", Type: schema.Int}}, nil, "")
	orders.Config.PrimaryKey = []string{"store", "no"}
	lines := rawEnt("lines", "lines", []schema.Field{{Name: "id", Type: schema.String}, {Name: "order_store", Type: schema.String}, {Name: "order_no", Type: schema.Int, RawType: "INTEGER"}},
		[]entity.Relation{{Type: entity.RelManyToOne, Name: "order", Entity: "orders", ForeignKey: []string{"order_store", "order_no"}}}, "id")
	all := map[string]*entity.Entity{"orders": orders, "lines": lines}
	if err := checkForeignKeyTypes(lines, lines.Config.Relations[0], all); err != nil {
		t.Fatalf("an INTEGER column referencing a BIGINT key was refused: %v", err)
	}
}

func TestSweepCoreKeyShapeCheckedForEveryTable(t *testing.T) {
	parent := rawEnt("parent", "parent", []schema.Field{{Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}}, nil, "")
	parent.Config.PrimaryKey = []string{"a", "b"}
	child := rawEnt("child", "child", []schema.Field{{Name: "id", Type: schema.String}, {Name: "parent_id", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToOne, Name: "parent", Entity: "parent", ForeignKey: []string{"parent_id"}}}, "id")
	if _, err := topoSortEntities(map[string]*entity.Entity{"parent": parent, "child": child}); err == nil {
		t.Fatal("a one-column foreign key onto a two-column key was accepted")
	}
}

type unsortedRegistry []*entity.Entity

func (r unsortedRegistry) All() map[string]*entity.Entity { return multiVersionRegistry(r).All() }
func (r unsortedRegistry) AllSorted() []*entity.Entity    { return r }
func (r unsortedRegistry) Get(name string) (*entity.Entity, error) {
	return multiVersionRegistry(r).Get(name)
}

func TestSweepCoreUnionBaseIsUnversioned(t *testing.T) {
	v1 := rawEnt("posts", "posts_v1", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	v1.Version = "/api/v1"
	plain := rawEnt("posts", "posts", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	if got := UnionEntities(unsortedRegistry{v1, plain})["posts"].GetTable(); got != "posts" {
		t.Fatalf("the union's base is the table %q, want the unversioned posts", got)
	}
}

func TestSweepCoreSkippedRoutineIsNoOrphan(t *testing.T) {
	db := sweepDB(t)
	ctx := context.Background()
	if err := ensureRoutineLedger(ctx, db, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if err := upsertRoutineLedger(ctx, db, DialectSQLite, "pg_fn", "x"); err != nil {
		t.Fatal(err)
	}
	buf := captureMigrateSlog(t)
	plan := Plan{Routines: []Routine{
		{Name: "pg_fn", Up: "CREATE FUNCTION pg_fn() RETURNS int AS 'SELECT 1' LANGUAGE sql", Dialect: DialectPostgres},
		{Name: "v", Up: "CREATE VIEW IF NOT EXISTS v AS SELECT 1 AS x"},
	}}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "no longer registered") {
		t.Fatalf("a routine skipped for its dialect was reported as an orphan:\n%s", buf.String())
	}
}

func TestSweepCoreTableExistsRefusesBadName(t *testing.T) {
	db := sweepDB(t)
	if _, err := TableExistsBulk(context.Background(), db, []string{"x; DROP TABLE y"}, DialectSQLite); err == nil {
		t.Fatal("TableExistsBulk accepted an invalid table name")
	}
}

func TestSweepCoreIndexNamesCompareCut(t *testing.T) {
	long := strings.Repeat("n", 63)
	a := rawEnt("a", "a", []schema.Field{{Name: "id", Type: schema.String}, {Name: "x", Type: schema.String}}, nil, "id")
	a.Config.Indices = []entity.Index{{Name: long + "_one", Columns: []string{"x"}}}
	b := rawEnt("b", "b", []schema.Field{{Name: "id", Type: schema.String}, {Name: "x", Type: schema.String}}, nil, "id")
	b.Config.Indices = []entity.Index{{Name: long + "_two", Columns: []string{"x"}}}
	all := map[string]*entity.Entity{"a": a, "b": b}
	if err := checkIndexNames([]*entity.Entity{a, b}, all); err == nil {
		t.Fatal("two index names alike in their first 63 bytes were accepted")
	}
}
