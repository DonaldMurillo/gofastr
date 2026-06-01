package migrate

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func mock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, m
}

// testReg is a minimal entity.Registry for migrate tests.
type testReg map[string]*entity.Entity

func (r testReg) All() map[string]*entity.Entity { return r }
func (r testReg) AllSorted() []*entity.Entity {
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*entity.Entity, 0, len(r))
	for _, n := range names {
		out = append(out, r[n])
	}
	return out
}
func (r testReg) Get(name string) (*entity.Entity, error) {
	if e, ok := r[name]; ok {
		return e, nil
	}
	return nil, errors.New("not found")
}

// expectSQLiteDialect makes DetectDialect resolve to SQLite (version() errors).
func expectSQLiteDialect(m sqlmock.Sqlmock) {
	m.ExpectQuery("SELECT version").WillReturnError(errors.New("no such function"))
}

const ctxBg = "bg"

func ctxB() context.Context { return context.Background() }

// ---- bulk.go ----

func TestBulk_EmptyInputs(t *testing.T) {
	db, _ := mock(t)
	if got, err := ReadLiveColumnsBulk(ctxB(), db, nil, DialectPostgres); err != nil || len(got) != 0 {
		t.Fatalf("empty ReadLiveColumnsBulk: %v %v", got, err)
	}
	if got, err := TableExistsBulk(ctxB(), db, nil, DialectPostgres); err != nil || len(got) != 0 {
		t.Fatalf("empty TableExistsBulk: %v %v", got, err)
	}
}

func TestBulk_PostgresErrors(t *testing.T) {
	db, m := mock(t)
	m.ExpectQuery("information_schema.columns").WillReturnError(errors.New("boom"))
	if _, err := ReadLiveColumnsBulk(ctxB(), db, []string{"t"}, DialectPostgres); err == nil {
		t.Error("expected bulk column read query error")
	}

	db2, m2 := mock(t)
	m2.ExpectQuery("information_schema.columns").
		WillReturnRows(sqlmock.NewRows([]string{"a"}).AddRow("x")) // wrong column count → scan error
	if _, err := ReadLiveColumnsBulk(ctxB(), db2, []string{"t"}, DialectPostgres); err == nil {
		t.Error("expected bulk column scan error")
	}

	db3, m3 := mock(t)
	m3.ExpectQuery("pg_tables").WillReturnError(errors.New("boom"))
	if _, err := TableExistsBulk(ctxB(), db3, []string{"t"}, DialectPostgres); err == nil {
		t.Error("expected table-exists query error")
	}

	db4, m4 := mock(t)
	m4.ExpectQuery("pg_tables").WillReturnRows(sqlmock.NewRows([]string{"a", "b"}).AddRow("x", "y"))
	if _, err := TableExistsBulk(ctxB(), db4, []string{"t"}, DialectPostgres); err == nil {
		t.Error("expected table-exists scan error")
	}
}

func TestBulk_SQLiteErrors(t *testing.T) {
	db, m := mock(t)
	m.ExpectQuery("PRAGMA table_info").WillReturnError(errors.New("boom"))
	if _, err := ReadLiveColumnsBulk(ctxB(), db, []string{"t"}, DialectSQLite); err == nil {
		t.Error("expected sqlite bulk read error")
	}

	db2, m2 := mock(t)
	m2.ExpectQuery("sqlite_master").WillReturnError(errors.New("boom"))
	if _, err := TableExistsBulk(ctxB(), db2, []string{"t"}, DialectSQLite); err == nil {
		t.Error("expected sqlite table-exists error")
	}
}

// ---- schema_diff.go ReadLiveColumns ----

func TestReadLiveColumns_Errors(t *testing.T) {
	db, m := mock(t)
	m.ExpectQuery("information_schema.columns").WillReturnError(errors.New("boom"))
	if _, err := ReadLiveColumnsPostgres(ctxB(), db, "t"); err == nil {
		t.Error("expected pg read query error")
	}

	db2, m2 := mock(t)
	m2.ExpectQuery("information_schema.columns").WillReturnRows(sqlmock.NewRows([]string{"a"}).AddRow("x"))
	if _, err := ReadLiveColumnsPostgres(ctxB(), db2, "t"); err == nil {
		t.Error("expected pg read scan error")
	}

	db3, m3 := mock(t)
	m3.ExpectQuery("PRAGMA table_info").WillReturnError(errors.New("boom"))
	if _, err := ReadLiveColumnsSQLite(ctxB(), db3, "t"); err == nil {
		t.Error("expected sqlite read query error")
	}

	db4, m4 := mock(t)
	m4.ExpectQuery("PRAGMA table_info").WillReturnRows(sqlmock.NewRows([]string{"a"}).AddRow("x"))
	if _, err := ReadLiveColumnsSQLite(ctxB(), db4, "t"); err == nil {
		t.Error("expected sqlite read scan error")
	}
}

// ---- AutoMigratePlanContext ----

func TestAutoMigratePlan_NilDB(t *testing.T) {
	if err := AutoMigratePlanContext(ctxB(), nil, Plan{}); err != nil {
		t.Fatalf("nil db should be a no-op: %v", err)
	}
}

func TestAutoMigratePlan_BeginError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin().WillReturnError(errors.New("no tx"))
	if err := AutoMigratePlanContext(ctxB(), db, Plan{}); err == nil {
		t.Error("expected begin error")
	}
}

func TestAutoMigratePlan_CommitError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin()
	m.ExpectCommit().WillReturnError(errors.New("commit fail"))
	if err := AutoMigratePlanContext(ctxB(), db, Plan{}); err == nil {
		t.Error("expected commit error")
	}
}

func TestAutoMigratePlan_ViewError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin()
	m.ExpectExec("CREATE VIEW badv").WillReturnError(errors.New("bad view"))
	m.ExpectRollback()
	plan := Plan{Views: []View{{Name: "badv", Select: "SELECT bad"}}}
	if err := AutoMigratePlanContext(ctxB(), db, plan); err == nil {
		t.Error("expected view DDL error")
	}
}

func TestAutoMigratePlan_RoutineError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin()
	m.ExpectExec("CREATE VIEW v").WillReturnError(errors.New("bad routine"))
	m.ExpectRollback()
	plan := Plan{Routines: []Routine{{Name: "v", Up: "CREATE VIEW v AS SELECT 1"}}}
	if err := AutoMigratePlanContext(ctxB(), db, plan); err == nil {
		t.Error("expected routine error")
	}
}

func TestAutoMigratePlan_MigrateEntityBranches(t *testing.T) {
	// No-fields entity → skipped.
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin()
	m.ExpectCommit()
	reg := testReg{"e": rawEnt("e", "e", nil, nil, "")}
	if err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: reg}); err != nil {
		t.Fatalf("no-fields entity: %v", err)
	}

	// Invalid table name → SafeIdent error.
	db2, m2 := mock(t)
	expectSQLiteDialect(m2)
	m2.ExpectBegin()
	m2.ExpectRollback()
	reg2 := testReg{"e": rawEnt("e", "bad table", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	if err := AutoMigratePlanContext(ctxB(), db2, Plan{Registry: reg2}); err == nil {
		t.Error("expected invalid-table-name error")
	}

	// Empty index → skipped (no index exec).
	db3, m3 := mock(t)
	expectSQLiteDialect(m3)
	m3.ExpectBegin()
	m3.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m3.ExpectCommit()
	e3 := rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")
	e3.Config.Indices = []Index{{}} // empty → skipped
	if err := AutoMigratePlanContext(ctxB(), db3, Plan{Registry: testReg{"e": e3}}); err != nil {
		t.Fatalf("empty index: %v", err)
	}

	// Index create error.
	db4, m4 := mock(t)
	expectSQLiteDialect(m4)
	m4.ExpectBegin()
	m4.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m4.ExpectExec("CREATE INDEX").WillReturnError(errors.New("idx fail"))
	m4.ExpectRollback()
	e4 := rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")
	e4.Config.Indices = []Index{{Name: "ix", Columns: []string{"x"}}}
	if err := AutoMigratePlanContext(ctxB(), db4, Plan{Registry: testReg{"e": e4}}); err == nil {
		t.Error("expected index create error")
	}
}

// ---- ApplySchemaDiffWithOptions ----

func TestApplySchemaDiff_EmptyBeginCommitErrors(t *testing.T) {
	db, _ := mock(t)
	if n, err := ApplySchemaDiff(ctxB(), db, nil); n != 0 || err != nil {
		t.Fatalf("empty apply: %d %v", n, err)
	}

	db2, m2 := mock(t)
	m2.ExpectBegin().WillReturnError(errors.New("no tx"))
	if _, err := ApplySchemaDiff(ctxB(), db2, []SchemaChange{{SQL: "X"}}); err == nil {
		t.Error("expected begin error")
	}

	db3, m3 := mock(t)
	m3.ExpectBegin()
	m3.ExpectExec("X").WillReturnResult(sqlmock.NewResult(0, 0))
	m3.ExpectCommit().WillReturnError(errors.New("commit fail"))
	if _, err := ApplySchemaDiff(ctxB(), db3, []SchemaChange{{SQL: "X"}}); err == nil {
		t.Error("expected commit error")
	}
}

// ---- DiffSchema ----

func TestDiffSchema_Errors(t *testing.T) {
	// ReadLiveColumnsBulk error (Postgres path).
	db, m := mock(t)
	m.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("PostgreSQL 16"))
	m.ExpectQuery("information_schema.columns").WillReturnError(errors.New("boom"))
	reg := testReg{"e": rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	if _, err := DiffSchema(ctxB(), db, reg); err == nil {
		t.Error("expected bulk-read error in DiffSchema")
	}

	// topoSort error (BelongsTo unknown).
	db2, m2 := mock(t)
	expectSQLiteDialect(m2)
	bad := rawEnt("p", "p", []schema.Field{{Name: "x", Type: schema.String}}, []entity.Relation{{Type: entity.RelManyToOne, Entity: "ghost", ForeignKey: "g_id"}}, "id")
	if _, err := DiffSchema(ctxB(), db2, testReg{"p": bad}); err == nil {
		t.Error("expected topo-sort error in DiffSchema")
	}
}

// ---- seed.go ----

func TestWithSeedLogger_Nil(t *testing.T) {
	ctx := ctxB()
	if got := WithSeedLogger(ctx, nil); got != ctx {
		t.Error("WithSeedLogger(nil) should return the same context")
	}
}

func TestEnsureSeedLedger_ExecError(t *testing.T) {
	db, m := mock(t)
	m.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("boom"))
	if err := ensureSeedLedger(ctxB(), db, DialectSQLite); err == nil {
		t.Error("expected ensureSeedLedger exec error")
	}
}

func TestReadSeededSet_Errors(t *testing.T) {
	db, m := mock(t)
	m.ExpectQuery("SELECT entity_name").WillReturnError(errors.New("boom"))
	if _, err := readSeededSet(ctxB(), db); err == nil {
		t.Error("expected readSeededSet query error")
	}

	db2, m2 := mock(t)
	m2.ExpectQuery("SELECT entity_name").
		WillReturnRows(sqlmock.NewRows([]string{"a", "b"}).AddRow("x", "y")) // scan mismatch
	if _, err := readSeededSet(ctxB(), db2); err == nil {
		t.Error("expected readSeededSet scan error")
	}
}

func TestRecordSeeded_ExecErrorAndPostgresPlaceholder(t *testing.T) {
	db, m := mock(t)
	m.ExpectExec("INSERT INTO").WillReturnError(errors.New("boom"))
	if err := recordSeeded(ctxB(), db, DialectSQLite, "x"); err == nil {
		t.Error("expected recordSeeded exec error")
	}

	db2, m2 := mock(t)
	m2.ExpectExec(`VALUES \(\$1\)`).WithArgs("x").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := recordSeeded(ctxB(), db2, DialectPostgres, "x"); err != nil {
		t.Fatalf("postgres recordSeeded: %v", err)
	}
}

func seededEntity(name string, seed func(context.Context, *sql.DB) error) *entity.Entity {
	e := rawEnt(name, name, []schema.Field{{Name: "x", Type: schema.String}}, nil, "id")
	e.Config.Seed = seed
	return e
}

func TestRunSeeds_Branches(t *testing.T) {
	okSeed := func(context.Context, *sql.DB) error { return nil }

	// nil db.
	if err := RunSeeds(ctxB(), nil, testReg{}); err != nil {
		t.Fatalf("nil db: %v", err)
	}
	// No entity has a Seed → early return.
	db0, _ := mock(t)
	if err := RunSeeds(ctxB(), db0, testReg{"e": rawEnt("e", "e", nil, nil, "")}); err != nil {
		t.Fatalf("no-seed registry: %v", err)
	}

	// ensureSeedLedger error.
	db1, m1 := mock(t)
	expectSQLiteDialect(m1)
	m1.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("boom"))
	if err := RunSeeds(ctxB(), db1, testReg{"e": seededEntity("e", okSeed)}); err == nil {
		t.Error("expected ensure-ledger error")
	}

	// readSeededSet error.
	db2, m2 := mock(t)
	expectSQLiteDialect(m2)
	m2.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m2.ExpectQuery("SELECT entity_name").WillReturnError(errors.New("boom"))
	if err := RunSeeds(ctxB(), db2, testReg{"e": seededEntity("e", okSeed)}); err == nil {
		t.Error("expected ledger-read error")
	}

	// topoSort error (a seeded entity that BelongsTo an unknown one).
	db3, m3 := mock(t)
	expectSQLiteDialect(m3)
	m3.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m3.ExpectQuery("SELECT entity_name").WillReturnRows(sqlmock.NewRows([]string{"entity_name"}))
	badSeed := seededEntity("p", okSeed)
	badSeed.Config.Relations = []entity.Relation{{Type: entity.RelManyToOne, Entity: "ghost", ForeignKey: "g_id"}}
	if err := RunSeeds(ctxB(), db3, testReg{"p": badSeed}); err == nil {
		t.Error("expected topo-sort error")
	}

	// record error after a successful seed; plus a sibling with no Seed (skip).
	db4, m4 := mock(t)
	expectSQLiteDialect(m4)
	m4.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m4.ExpectQuery("SELECT entity_name").WillReturnRows(sqlmock.NewRows([]string{"entity_name"}))
	m4.ExpectExec("INSERT INTO").WillReturnError(errors.New("record fail"))
	reg4 := testReg{
		"aaa": seededEntity("aaa", okSeed),
		"zzz": rawEnt("zzz", "zzz", nil, nil, ""), // no Seed → skipped in loop
	}
	if err := RunSeeds(ctxB(), db4, reg4); err == nil {
		t.Error("expected record-ledger error")
	}
}

// ---- additional branch coverage ----

func TestAutoMigratePlan_TableExistsBulkError(t *testing.T) {
	db, m := mock(t)
	m.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("PostgreSQL 16"))
	m.ExpectQuery("pg_tables").WillReturnError(errors.New("boom"))
	reg := testReg{"e": rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	if err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: reg}); err == nil {
		t.Error("expected TableExistsBulk error")
	}
}

func TestAutoMigratePlan_MigrateEntityFKError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectBegin()
	// "users" (the FK target) is topo-sorted first and created successfully;
	// then "p"'s foreignKeyClauses fails on its invalid FK column name.
	m.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectRollback()
	users := rawEnt("users", "users", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	bad := rawEnt("p", "p", []schema.Field{{Name: "id", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToOne, Entity: "users", ForeignKey: "bad col"}}, "id")
	if err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: testReg{"users": users, "p": bad}}); err == nil {
		t.Error("expected migrateEntity FK error")
	}
}

func TestDiffSchema_DiffEntityError(t *testing.T) {
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectQuery("PRAGMA table_info").WillReturnRows(sqlmock.NewRows([]string{"cid", "name", "type", "notnull", "dflt_value", "pk"}))
	// No-fields entity → diffEntityFromLive returns a buildCreateTableSQL error.
	if _, err := DiffSchema(ctxB(), db, testReg{"e": rawEnt("e", "e", nil, nil, "")}); err == nil {
		t.Error("expected DiffSchema diffEntity error")
	}
}

func TestEnsureSeedLedger_Postgres(t *testing.T) {
	db, m := mock(t)
	m.ExpectExec("NOW\\(\\)").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := ensureSeedLedger(ctxB(), db, DialectPostgres); err != nil {
		t.Fatalf("postgres ensureSeedLedger: %v", err)
	}
}

func TestGeneratePlan_Errors(t *testing.T) {
	// topoSort error.
	bad := rawEnt("p", "p", []schema.Field{{Name: "x", Type: schema.String}},
		[]entity.Relation{{Type: entity.RelManyToOne, Entity: "ghost", ForeignKey: "g_id"}}, "id")
	if _, _, _, err := GeneratePlan(Plan{Registry: testReg{"p": bad}}, SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite); err == nil {
		t.Error("expected GeneratePlan topo-sort error")
	}
	// diffEntityFromLive error (no-fields entity, table absent from snapshot).
	if _, _, _, err := GeneratePlan(Plan{Registry: testReg{"e": rawEnt("e", "e", nil, nil, "")}}, SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite); err == nil {
		t.Error("expected GeneratePlan diffEntity error")
	}
}

func TestRunSeeds_CancelledLoopAndSeedNilSkip(t *testing.T) {
	okSeed := func(context.Context, *sql.DB) error { return nil }

	// errOnlyCtx never fires Done() (so the DB calls in ensure/read succeed) but
	// reports a non-nil Err(), so the loop's explicit ctx.Err() check trips —
	// exercising the "honour cancellation between seeds" branch deterministically.
	db, m := mock(t)
	expectSQLiteDialect(m)
	m.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT entity_name").WillReturnRows(sqlmock.NewRows([]string{"entity_name"}))
	if err := RunSeeds(errOnlyCtx{context.Background()}, db, testReg{"e": seededEntity("e", okSeed)}); err == nil {
		t.Error("expected cancelled-context error in the seed loop")
	}

	// Seed==nil skip: a no-seed entity that sorts first is skipped, then the
	// seeded one runs and records.
	db2, m2 := mock(t)
	expectSQLiteDialect(m2)
	m2.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	m2.ExpectQuery("SELECT entity_name").WillReturnRows(sqlmock.NewRows([]string{"entity_name"}))
	m2.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(0, 1))
	reg := testReg{
		"aaa": rawEnt("aaa", "aaa", nil, nil, ""), // no Seed, sorts first → skipped
		"zzz": seededEntity("zzz", okSeed),
	}
	if err := RunSeeds(context.Background(), db2, reg); err != nil {
		t.Fatalf("seed-nil-skip path: %v", err)
	}
}

// errOnlyCtx reports a non-nil Err() while never closing Done(), so DB calls
// that select on Done() proceed but explicit Err() checks see "cancelled".
type errOnlyCtx struct{ context.Context }

func (errOnlyCtx) Err() error { return errors.New("cancelled") }

var _ = ctxBg
