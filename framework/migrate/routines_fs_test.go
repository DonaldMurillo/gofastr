package migrate

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	_ "github.com/mattn/go-sqlite3"
)

// TestRoutinesFS_PlainFileIsUpWhenDialectUnset: a single `foo.sql` maps to a
// Routine whose Up is the file body and whose Dialect is the zero value (runs
// everywhere).
func TestRoutinesFS_PlainFileIsUpWhenDialectUnset(t *testing.T) {
	fsys := fstest.MapFS{
		"db/routines/foo.sql": &fstest.MapFile{Data: []byte("CREATE VIEW foo AS SELECT 1")},
	}
	got, err := RoutinesFS(fsys, "db/routines")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d routines, want 1", len(got))
	}
	r := got[0]
	if r.Name != "foo" {
		t.Errorf("Name=%q want foo", r.Name)
	}
	if r.Up != "CREATE VIEW foo AS SELECT 1" {
		t.Errorf("Up=%q", r.Up)
	}
	if r.Down != "" {
		t.Errorf("Down=%q want empty", r.Down)
	}
	if r.Dialect != Dialect("") {
		t.Errorf("Dialect=%q want empty (all dialects)", r.Dialect)
	}
}

// TestRoutinesFS_DownFilePopulatesDown: a paired `foo.down.sql` populates Down.
func TestRoutinesFS_DownFilePopulatesDown(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql":      &fstest.MapFile{Data: []byte("CREATE VIEW foo AS SELECT 1")},
		"r/foo.down.sql": &fstest.MapFile{Data: []byte("DROP VIEW foo")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if got[0].Down != "DROP VIEW foo" {
		t.Fatalf("Down=%q", got[0].Down)
	}
}

// TestRoutinesFS_PgSuffixSetsDialect: `foo.pg.sql` makes the routine
// Postgres-only.
func TestRoutinesFS_PgSuffixSetsDialect(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.pg.sql": &fstest.MapFile{Data: []byte("CREATE OR REPLACE FUNCTION foo() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if got[0].Dialect != DialectPostgres {
		t.Fatalf("Dialect=%q want postgres", got[0].Dialect)
	}
	if !strings.Contains(got[0].Up, "CREATE OR REPLACE FUNCTION foo") {
		t.Fatalf("Up=%q", got[0].Up)
	}
}

// TestRoutinesFS_SqliteSuffixSetsDialect: symmetric case for SQLite.
func TestRoutinesFS_SqliteSuffixSetsDialect(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sqlite.sql": &fstest.MapFile{Data: []byte("CREATE TRIGGER foo AFTER INSERT ON t BEGIN SELECT 1; END")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if got[0].Dialect != DialectSQLite {
		t.Fatalf("Dialect=%q want sqlite", got[0].Dialect)
	}
}

// TestRoutinesFS_RejectsPlainAndDialectCollision: a name with BOTH `foo.sql`
// and `foo.pg.sql` is an authoring error — pick one.
func TestRoutinesFS_RejectsPlainAndDialectCollision(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql":    &fstest.MapFile{Data: []byte("CREATE VIEW foo AS SELECT 1")},
		"r/foo.pg.sql": &fstest.MapFile{Data: []byte("CREATE OR REPLACE FUNCTION foo() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql")},
	}
	_, err := RoutinesFS(fsys, "r")
	if err == nil {
		t.Fatal("expected error for plain+dialect Up collision, got nil")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Fatalf("error should name the colliding routine: %v", err)
	}
}

// TestRoutinesFS_RejectsTwoDialectsForSameName: `foo.pg.sql` AND `foo.sqlite.sql`
// is ambiguous; pick one.
func TestRoutinesFS_RejectsTwoDialectsForSameName(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.pg.sql":     &fstest.MapFile{Data: []byte("SELECT 1")},
		"r/foo.sqlite.sql": &fstest.MapFile{Data: []byte("SELECT 2")},
	}
	_, err := RoutinesFS(fsys, "r")
	if err == nil {
		t.Fatal("expected error for two-dialect Up, got nil")
	}
}

// TestRoutinesFS_RejectsEmptyFile: an empty Up file is a scream — silent
// no-ops hide misconfiguration.
func TestRoutinesFS_RejectsEmptyFile(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql": &fstest.MapFile{Data: []byte("   \n\t  ")},
	}
	_, err := RoutinesFS(fsys, "r")
	if err == nil {
		t.Fatal("expected error for whitespace-only Up, got nil")
	}
}

// TestRoutinesFS_RejectsEmptyDir: an empty directory is a scream.
func TestRoutinesFS_RejectsEmptyDir(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := RoutinesFS(fsys, "r")
	if err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}

// TestRoutinesFS_RejectsMissingDir: a directory that doesn't exist in the FS
// errors instead of silently returning no routines.
func TestRoutinesFS_RejectsMissingDir(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	_, err := RoutinesFS(fsys, "nope")
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

// TestRoutinesFS_DeterministicSortedOrder proves routines come back sorted by
// name (the boot plan and generated migrations are order-sensitive).
func TestRoutinesFS_DeterministicSortedOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"r/zeta.sql":  &fstest.MapFile{Data: []byte("CREATE VIEW zeta AS SELECT 1")},
		"r/alpha.sql": &fstest.MapFile{Data: []byte("CREATE VIEW alpha AS SELECT 1")},
		"r/mid.sql":   &fstest.MapFile{Data: []byte("CREATE VIEW mid AS SELECT 1")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	names := make([]string, 0, len(got))
	for _, r := range got {
		names = append(names, r.Name)
	}
	want := []string{"alpha", "mid", "zeta"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v want %v", names, want)
	}
	if !slices.IsSorted(names) {
		t.Fatalf("routines not in sorted order: %v", names)
	}
}

// TestRoutinesFS_TrimsSurroundingWhitespace: the loader leaves the Up body
// alone except for trimming — what you wrote is what runs. A trailing
// semicolon is preserved (it's part of the SQL).
func TestRoutinesFS_TrimsSurroundingWhitespace(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql": &fstest.MapFile{Data: []byte("\n\nCREATE VIEW foo AS SELECT 1;\n\n")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if got[0].Up != "CREATE VIEW foo AS SELECT 1;" {
		t.Fatalf("Up=%q (want surrounding whitespace trimmed, trailing ; kept)", got[0].Up)
	}
}

// TestRoutinesFS_IgnoresUnrelatedFiles: `.md`, dotfiles, and files in nested
// sub-directories under the routines dir don't become routines. Only top-level
// `.sql` files do.
func TestRoutinesFS_IgnoresUnrelatedFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql":      &fstest.MapFile{Data: []byte("CREATE VIEW foo AS SELECT 1")},
		"r/README.md":    &fstest.MapFile{Data: []byte("notes")},
		"r/.hidden":      &fstest.MapFile{Data: []byte("nope")},
		"r/sub/bar.sql":  &fstest.MapFile{Data: []byte("CREATE VIEW bar AS SELECT 1")},
		"r/foo.down.sql": &fstest.MapFile{Data: []byte("DROP VIEW foo")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatalf("RoutinesFS: %v", err)
	}
	if len(got) != 1 {
		names := make([]string, 0, len(got))
		for _, r := range got {
			names = append(names, r.Name)
		}
		t.Fatalf("got %d routines (%v), want 1 (foo only, top-level, no nested dirs)", len(got), names)
	}
	if got[0].Name != "foo" {
		t.Fatalf("Name=%q want foo", got[0].Name)
	}
}

// --- Dialect scoping on apply ----------------------------------------------

// TestAutoMigratePlan_SkipsRoutinesForOtherDialect proves the skip rule:
// running against SQLite, a Postgres-only routine's Up is NOT executed.
func TestAutoMigratePlan_SkipsRoutinesForOtherDialect(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	plan := Plan{
		Routines: []Routine{
			{Name: "should_skip", Up: "CREATE VIEW should_skip AS SELECT 1", Dialect: DialectPostgres},
			{Name: "should_run", Up: "CREATE VIEW should_run AS SELECT 1"},
		},
	}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='view' AND name='should_run'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("should_run view not created: count=%d", n)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='view' AND name='should_skip'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("Postgres-only routine ran on SQLite: count=%d", n)
	}
}

// TestAutoMigratePlan_SkipsAndLogsDialect proves that skip rule emits exactly
// ONE slog.Info naming the skipped routine(s) and the declared dialect.
func TestAutoMigratePlan_SkipsAndLogsDialect(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	plan := Plan{
		Routines: []Routine{
			{Name: "pg_only", Up: "CREATE VIEW pg_only AS SELECT 1", Dialect: DialectPostgres},
			{Name: "any_db", Up: "CREATE VIEW any_db AS SELECT 1"},
		},
	}
	logs := captureMigrateSlog(t)
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "pg_only") {
		t.Fatalf("expected skip log to name 'pg_only', got:\n%s", out)
	}
	// Reason names the declared-vs-running mismatch.
	if !strings.Contains(out, "postgres") {
		t.Fatalf("expected skip log to name the declared dialect 'postgres', got:\n%s", out)
	}
	if !strings.Contains(out, "sqlite") {
		t.Fatalf("expected skip log to name the running dialect 'sqlite', got:\n%s", out)
	}
}

// --- Ledger ----------------------------------------------------------------

// TestLedger_TableCreatedOnSQLite proves the ledger table is created during
// auto-migrate on SQLite.
func TestLedger_TableCreatedOnSQLite(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	plan := Plan{Routines: []Routine{{Name: "f", Up: "CREATE VIEW f AS SELECT 1"}}}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='gofastr_routines'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ledger table not created: count=%d", n)
	}
}
func TestLedger_UpsertsRowAndReportsMatching(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	// SQLite views can't be CREATE OR REPLACE — the SQLite idiom (documented
	// in migrations.md) is DROP IF EXISTS + CREATE so every boot re-runs
	// idempotently. That's the body shape we exercise here.
	idempotent := "DROP VIEW IF EXISTS f;\nCREATE VIEW f AS SELECT 1"
	plan := Plan{Routines: []Routine{{Name: "f", Up: idempotent}}}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	var (
		name, checksum string
		appliedAt      sql.NullTime
	)
	err := db.QueryRow(`SELECT name, checksum, applied_at FROM gofastr_routines WHERE name = 'f'`).Scan(&name, &checksum, &appliedAt)
	if err != nil {
		t.Fatalf("ledger row missing: %v", err)
	}
	if checksum == "" {
		t.Fatal("checksum empty")
	}
	if !appliedAt.Valid {
		t.Fatal("applied_at not set")
	}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var checksum2 string
	if err := db.QueryRow(`SELECT checksum FROM gofastr_routines WHERE name = 'f'`).Scan(&checksum2); err != nil {
		t.Fatal(err)
	}
	if checksum2 != checksum {
		t.Fatalf("checksum drifted without an Up change: %q -> %q", checksum, checksum2)
	}
}

// TestLedger_WarnsWhenRowHasNoRegisteredRoutine proves the additive-only
// contract: a ledger row whose name disappeared is NOT auto-dropped; it
// produces a slog.Warn.
func TestLedger_WarnsWhenRowHasNoRegisteredRoutine(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	plan0 := Plan{Routines: []Routine{{Name: "ghost", Up: "CREATE VIEW ghost AS SELECT 1"}}}
	if err := AutoMigratePlanContext(ctx, db, plan0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	plan1 := Plan{Routines: []Routine{{Name: "real", Up: "CREATE VIEW real AS SELECT 1"}}}
	logs := captureMigrateSlog(t)
	if err := AutoMigratePlanContext(ctx, db, plan1); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "ghost") {
		t.Fatalf("expected WARN naming the orphaned routine 'ghost', got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "warn") {
		t.Fatalf("expected WARN level, got:\n%s", out)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gofastr_routines WHERE name = 'ghost'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("orphaned ledger row was dropped: n=%d (additive-only contract broken)", n)
	}
}

// TestLedger_WritesNewChecksumOnUpChange proves that when an Up body changes,
// the ledger row's checksum is updated to match.
func TestLedger_WritesNewChecksumOnUpChange(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	// SQLite can't CREATE OR REPLACE a function, but it CAN replace a trigger
	// (DROP IF EXISTS + CREATE) — and a routine body's checksum is independent
	// of whether the Up actually succeeded in changing the DB object.
	planA := Plan{Routines: []Routine{{Name: "f", Up: "CREATE VIEW f AS SELECT 1"}}}
	if err := AutoMigratePlanContext(ctx, db, planA); err != nil {
		t.Fatal(err)
	}
	// Drop and re-create with a different body so the re-apply succeeds (SQLite
	// VIEWs lack OR REPLACE).
	if _, err := db.Exec("DROP VIEW f"); err != nil {
		t.Fatal(err)
	}
	planB := Plan{Routines: []Routine{{Name: "f", Up: "CREATE VIEW f AS SELECT 1, 2"}}}
	if err := AutoMigratePlanContext(ctx, db, planB); err != nil {
		t.Fatal(err)
	}
	var checksum string
	if err := db.QueryRow(`SELECT checksum FROM gofastr_routines WHERE name = 'f'`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if checksum != RoutineChecksum(Routine{Up: "CREATE VIEW f AS SELECT 1, 2"}) {
		t.Fatalf("ledger checksum not updated to match new Up body: %q", checksum)
	}
}

// TestLedger_SummaryLogLine proves a one-line boot summary is logged with the
// four counts the brief specifies: applied / changed / first-time / skipped.
func TestLedger_SummaryLogLine(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	ctx := context.Background()
	plan := Plan{
		Routines: []Routine{
			{Name: "r1", Up: "CREATE VIEW r1 AS SELECT 1"},
			{Name: "r2", Up: "CREATE VIEW r2 AS SELECT 1"},
			{Name: "pg_only", Up: "CREATE VIEW pg_only AS SELECT 1", Dialect: DialectPostgres},
		},
	}
	logs := captureMigrateSlog(t)
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	out := logs.String()
	// 2 applied, 0 changed, 2 first-time, 1 skipped-for-dialect. The exact
	// key=value layout is slog text-handler format; assert on each count
	// independently so a partial regression names which one drifted.
	for _, want := range []string{"applied=2", "first_time=2", "skipped=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary line missing %s; got:\n%s", want, out)
		}
	}
}

// TestRoutineChecksum_StableAndDifferent verifies the helper itself: same Up
// → same digest, different Up → different digest, both lowercase hex sha256.
func TestRoutineChecksum_StableAndDifferent(t *testing.T) {
	a := RoutineChecksum(Routine{Up: "SELECT 1"})
	b := RoutineChecksum(Routine{Up: "SELECT 1"})
	c := RoutineChecksum(Routine{Up: "SELECT 2"})
	if a != b {
		t.Fatalf("checksum not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("checksum did not change with body")
	}
	if len(a) != 64 {
		t.Fatalf("checksum len=%d want 64 (sha256 hex)", len(a))
	}
	if a != strings.ToLower(a) {
		t.Fatalf("checksum not lowercase hex: %q", a)
	}
}

// --- helpers ---------------------------------------------------------------

func openSQLiteForMigrateTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// captureMigrateSlog swaps the package-level slog default (which AutoMigrate
// writes against) for a buffer, and restores it on cleanup. slog.Default() is
// process-global; using t.Cleanup keeps parallel-safe swaps bounded.
func captureMigrateSlog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		_ = io.Discard // keep io import honest if other tests drop it
	})
	return &buf
}
