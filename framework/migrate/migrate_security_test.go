package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Property: every value spliced into generated DDL is escaped for the
// literal context it lands in: for a DEFAULT clause that means a
// single-quoted string literal that cannot be closed from inside.
//
// Surfaces: SQLDefault feeds ColumnDefaultClause, which feeds BOTH
// columnDefs (CREATE TABLE) and diffEntityFromLive (ALTER TABLE ADD
// COLUMN). schema.Field.Default is `any`, and only the `case string`
// arm doubled quotes; the default: arm rendered fmt.Sprintf("'%v'", v)
// raw. A named string type, a fmt.Stringer, or, the reachable one, a
// []any / map[string]any decoded from JSON took that arm.
//
// Reachability: entity.Declaration's Default is `any` and kiln's
// add_entity op accepts a field default straight from an HTTP JSON
// payload, so a JSON array or object arrives as []any and closes the
// literal. A verified payload created and COMMITTED an extra column.
func TestSQLDefaultEscapesEveryType(t *testing.T) {
	type named string
	const payload = `x', shadow TEXT DEFAULT 'y`

	cases := []struct {
		name string
		val  any
	}{
		{"plain string", payload},
		{"named string type", named(payload)},
		{"json array", []any{payload}},
		{"json object", map[string]any{"a": payload}},
		{"stringer", stringerDefault(payload)},
	}

	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		for _, tc := range cases {
			got := SQLDefault(schema.Field{Default: tc.val}, dialect)
			if !isSingleQuotedLiteral(got) {
				t.Errorf("SECURITY: [migrate] SQLDefault(%s, %s) = %s — the literal is closable. "+
					"Attack: a kiln add_entity field default injects DDL that COMMITS "+
					"(an extra column, a dropped table) into CREATE TABLE / ALTER TABLE ADD COLUMN.",
					tc.name, dialect, got)
			}
		}
	}

	// Non-string scalars keep their unquoted native rendering.
	if got := SQLDefault(schema.Field{Default: 42}, DialectSQLite); got != "42" {
		t.Errorf("[migrate] int default regressed: %s", got)
	}
	if got := SQLDefault(schema.Field{Default: true}, DialectPostgres); got != "TRUE" {
		t.Errorf("[migrate] bool default regressed: %s", got)
	}
	if got := SQLDefault(schema.Field{Default: "o'brien"}, DialectSQLite); got != "'o''brien'" {
		t.Errorf("[migrate] string escaping regressed: %s", got)
	}
}

type stringerDefault string

func (s stringerDefault) String() string { return string(s) }

// isSingleQuotedLiteral reports whether s is one SQL string literal:
// wrapped in single quotes with every interior quote doubled, so the
// literal cannot be terminated early.
func isSingleQuotedLiteral(s string) bool {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return false
	}
	inner := s[1 : len(s)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\'' {
			continue
		}
		if i+1 >= len(inner) || inner[i+1] != '\'' {
			return false
		}
		i++ // skip the escaped pair
	}
	return true
}

// TestReadLiveColumnsRejectsBadTable pins that the SQLite live-column
// read validates its identifier like every one of its siblings. It is
// the only identifier site in the package that interpolated without
// SafeIdent, justified by a comment claiming the table name "is taken
// from our own registry, not user input", which is false: kiln's
// add_entity / update_entity ops accept a `table` override over HTTP
// (kiln/journal/replay.go validates only the entity Name), and this
// read runs at AutoMigratePlanContext BEFORE any SafeIdent call.
//
// No injection landed today only because database/sql executes a single
// statement; that is the driver's property, not this package's.
func TestReadLiveColumnsRejectsBadTable(t *testing.T) {
	for _, bad := range []string{
		`foo); DROP TABLE victim; --`,
		`foo" ; DROP TABLE victim; --`,
		"foo\nbar",
	} {
		_, err := ReadLiveColumnsSQLite(context.Background(), nil, bad)
		if err == nil {
			t.Errorf("SECURITY: [migrate] ReadLiveColumnsSQLite accepted table name %q. "+
				"Attack: an agent-supplied table override reaches PRAGMA table_info "+
				"unvalidated, ahead of every SafeIdent call in the plan path.", bad)
			continue
		}
		if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("[migrate] unexpected rejection reason for %q: %v", bad, err)
		}
	}
}

// ============================================================================
// Property: SQL text embedded verbatim into the `-- +migrate` directive
// format must not be able to synthesize a directive line. The runner's
// parser (core/migrate parseMigration behind RegisterFromReader) is bufio
// line-based and quote-blind: any line whose trimmed text starts with
// `-- +migrate` flips the section, even a line inside an open string
// literal. The generator (RenderMigrationFile) embeds entity SQL verbatim,
// and quoteSQLLiteral doubles quotes but preserves newlines.
//
// Surfaces: SQLDefault case-string -> ColumnDefaultClause -> columnDefs ->
// GeneratePlan -> RenderMigrationFile -> GenerateMigrationFile -> the
// runner's parseMigration. Reachability: kiln add_entity accepts field
// defaults over HTTP (only the entity Name is validated, see
// TestReadLiveColumnsRejectsBadTable above), so a multi-line string default
// reaches the generator verbatim.
//
// RED (2026-08-30 pass): a default containing "\n-- +migrate Down\nDROP
// TABLE victims;-- " is committed into the .sql file across three lines.
// The parsed Up section ends mid-literal (apply fails loudly, and the
// snapshot already advanced past a migration that never applied), and the
// parsed Down section carries the attacker's bytes, which would execute
// verbatim on rollback of that version. Either the generator must
// refuse/neutralize directive-synthesizing SQL or the runner must scan
// directives quote-aware.
// ============================================================================

// TestSQLDefaultCannotSynthesizeDirective pins both halves of the property:
// a benign multi-line DEFAULT is legitimate SQL and must keep applying (the
// fix cannot be "reject every newline"), and a directive-looking line
// inside a DEFAULT must never flip the runner's Up/Down sections.
func TestSQLDefaultCannotSynthesizeDirective(t *testing.T) {
	t.Run("benign multiline default applies", func(t *testing.T) {
		content := generateFileWithDefault(t, "first\nsecond")
		db := openMigrateSQLite(t)
		m := coremig.New(db, coremig.WithDialect(coremig.DialectSQLite))
		if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
			t.Fatalf("parse generated file: %v\nfile:\n%s", err, content)
		}
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("Up failed on a benign multi-line DEFAULT (legitimate SQL must keep applying): %v\nfile:\n%s", err, content)
		}
	})

	t.Run("directive line in default cannot flip sections", func(t *testing.T) {
		const poison = "x\n-- +migrate Down\nDROP TABLE victims;-- "

		path, genErr := GenerateMigrationFile(
			Plan{Registry: defaultTestReg(t, poison)}, "poison_default",
			MigrationFileOptions{MigrationsDir: t.TempDir(), Dialect: DialectSQLite})
		if genErr != nil {
			// Acceptable fix shape: the generator refuses to embed
			// directive-synthesizing SQL. Nothing was committed, so there is
			// nothing to round-trip.
			return
		}
		if path == "" {
			t.Fatal("expected a migration to be generated for a new table")
		}
		content := readGeneratedFile(t, path)

		db := openMigrateSQLite(t)
		if _, err := db.Exec(`CREATE TABLE victims (id TEXT PRIMARY KEY)`); err != nil {
			t.Fatalf("seed victims: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO victims (id) VALUES ('v1')`); err != nil {
			t.Fatalf("seed victims row: %v", err)
		}
		m := coremig.New(db, coremig.WithDialect(coremig.DialectSQLite))
		if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
			t.Fatalf("parse generated file: %v\nfile:\n%s", err, content)
		}
		// Desired: the committed file applies cleanly. The multi-line
		// DEFAULT is one literal and no line inside it may be read as a
		// directive by the runner.
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("SECURITY: [migrate] the committed migration does not apply: a line inside the string DEFAULT synthesized a %q directive, so the runner truncated the Up section mid-literal and parked the attacker's bytes in Down: %v\ncommitted file:\n%s",
				"-- +migrate Down", err, content)
		}
		// Desired: rollback runs only the generator's own Down. The
		// attacker's DROP TABLE victims must never execute.
		if err := m.Down(context.Background(), 1); err != nil {
			t.Fatalf("Down: %v\ncommitted file:\n%s", err, content)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM victims`).Scan(&n); err != nil {
			t.Fatalf("count victims: %v", err)
		}
		if n != 1 {
			t.Fatalf("SECURITY: [migrate] rollback destroyed the victims row: attacker SQL from the field default executed as Down\ncommitted file:\n%s", content)
		}
	})
}

// defaultTestReg builds a one-entity registry whose "note" field carries def
// as its DEFAULT - the kiln add_entity shape (schema.Field.Default is `any`,
// decoded straight from an HTTP JSON payload).
func defaultTestReg(t *testing.T, def string) testReg {
	t.Helper()
	return testReg{"notes": rawEnt("notes", "notes", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "note", Type: schema.String, Default: def},
	}, nil, "id")}
}

// generateFileWithDefault runs the real offline generator for an entity whose
// note field carries def, and returns the committed file's content.
func generateFileWithDefault(t *testing.T, def string) string {
	t.Helper()
	path, err := GenerateMigrationFile(
		Plan{Registry: defaultTestReg(t, def)}, "multiline_default",
		MigrationFileOptions{MigrationsDir: t.TempDir(), Dialect: DialectSQLite})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if path == "" {
		t.Fatal("expected a migration to be generated for a new table")
	}
	return readGeneratedFile(t, path)
}

func readGeneratedFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	return string(b)
}

// openMigrateSQLite opens a fresh single-connection in-memory SQLite database,
// the shape every SQLite-touching test in this package uses.
func openMigrateSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// ============================================================================
// Routine surfaces (wave 2). Routines are the plan's verbatim-SQL members:
// their Up runs inside AutoMigrate's transaction and their Name is the
// ledger's key. The properties below cover ledger key binding, set
// atomicity, and the directive guard's width on routine bodies.
// ============================================================================

// Property: a routine's NAME is data, never SQL. Names key the gofastr_routines
// ledger (and the orphan WARN + app_routines introspection that read it back),
// and they can carry arbitrary bytes from a filename (RoutinesFS) or a plan
// literal. Every ledger statement must bind the name as a parameter, on both
// dialect placeholder forms.
//
// Surfaces: upsertRoutineLedger's SQLite ("?") and Postgres ("$1") forms
// (sqlmock bound-arg matching for the Postgres form), plus the end-to-end
// boot on a real SQLite database: round-trip through readRoutineLedger must
// return the hostile names verbatim, and the schema must be otherwise
// untouched (an injected DROP inside a name cannot execute).
func TestRoutineLedgerBindsHostileNames(t *testing.T) {
	ctx := context.Background()
	names := []string{
		`x'); DROP TABLE gofastr_routines;--`,
		`x"); DROP TABLE victims;--`,
		`r; TRUNCATE gofastr_routines`,
		`рутины--комментарий`,
		"tab\tname",
	}

	// Surface 1: the Postgres placeholder form binds name + checksum.
	pgDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer pgDB.Close()
	for _, n := range names {
		mock.ExpectExec("INSERT INTO .gofastr_routines.*ON CONFLICT").
			WithArgs(n, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	for _, n := range names {
		if err := upsertRoutineLedger(ctx, pgDB, DialectPostgres, n, "deadbeef"); err != nil {
			t.Fatalf("pg upsert %q: %v", n, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("pg-form upsert did not bind the name as a parameter: %v", err)
	}

	// Surface 2: end-to-end boot on real SQLite round-trips the names
	// verbatim and leaves the schema untouched.
	db := openMigrateSQLite(t)
	routines := make([]Routine, len(names))
	for i, n := range names {
		routines[i] = Routine{Name: n, Up: fmt.Sprintf("DROP VIEW IF EXISTS hv%d; CREATE VIEW hv%d AS SELECT %d", i, i, i)}
	}
	if err := AutoMigratePlanContext(ctx, db, Plan{Routines: routines}); err != nil {
		t.Fatalf("boot: %v", err)
	}
	ledger, err := readRoutineLedger(ctx, db)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(ledger) != len(names) {
		t.Fatalf("ledger has %d rows, want %d", len(ledger), len(names))
	}
	for _, n := range names {
		if _, ok := ledger[n]; !ok {
			t.Errorf("routine name %q did not round-trip verbatim", n)
		}
	}
	if sqliteHasObject(t, db, "victims") {
		t.Error("SQL smuggled inside a routine name executed (victims table exists)")
	}
}

// Property: a failing routine rolls back the WHOLE boot DDL set — entities,
// views, earlier routines, and the ledger rows — so a partially-applied
// routine set never persists. The ledger lives in the same transaction as
// the routine bodies precisely so bookkeeping cannot diverge from what
// landed; this pins that contract at every member of the set.
//
// Surfaces (all asserted absent after the failure): the entity table, the
// view, the healthy routine's object, the failing routine's object, and the
// gofastr_routines ledger table itself (created earlier in the same tx).
func TestRoutineSetRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	reg := testReg{"acct": rawEnt("acct", "acct", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")}
	plan := Plan{
		Registry: reg,
		Views:    []View{{Name: "v_bal", Select: "SELECT id FROM acct"}},
		Routines: []Routine{
			{Name: "r_ok", Up: "CREATE VIEW rv_ok AS SELECT 1"},
			{Name: "r_bad", Up: "NOT VALID SQL"},
		},
	}
	err := AutoMigratePlanContext(ctx, db, plan)
	if err == nil {
		t.Fatal("expected the failing routine to fail the boot")
	}
	if !strings.Contains(err.Error(), "r_bad") {
		t.Errorf("error must name the failing routine, got: %v", err)
	}
	for _, obj := range []string{"acct", "v_bal", "rv_ok", "gofastr_routines"} {
		if sqliteHasObject(t, db, obj) {
			t.Errorf("%q survived a failed boot: the routine set is not atomic", obj)
		}
	}

	// Recovery: the next boot with the routine fixed converges in full.
	plan.Routines[1] = Routine{Name: "r_bad", Up: "CREATE VIEW rv_fixed AS SELECT 1"}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("recovery boot: %v", err)
	}
	for _, obj := range []string{"acct", "v_bal", "rv_ok", "rv_fixed", "gofastr_routines"} {
		if !sqliteHasObject(t, db, obj) {
			t.Errorf("%q missing after the recovery boot", obj)
		}
	}
}

// Property: no SQL body the generator embeds may synthesize a `-- +migrate`
// directive line — the runner's parser is quote-blind, so a directive-looking
// line inside ANY verbatim body flips the Up/Down sections of the committed
// file. This EXTENDS TestSQLDefaultCannotSynthesizeDirective (same wave-1
// root: generator embeds verbatim, parser is line-based) from column
// DEFAULTs to ROUTINE bodies, the other verbatim channel: a routine's own Up
// and its Down (which also lands verbatim in the generated Down section).
// Root counted in wave 1; surfaces extended here.
func TestGenerateRefusesDirectiveInRoutines(t *testing.T) {
	const poison = "\n-- +migrate Down\nDROP TABLE victims;-- "
	shapes := []struct {
		name    string
		routine Routine
	}{
		{"hostile routine Up", Routine{Name: "evil_up", Up: "CREATE VIEW ev AS SELECT 1" + poison, Down: "DROP VIEW ev"}},
		{"hostile routine Down", Routine{Name: "evil_down", Up: "CREATE VIEW ev2 AS SELECT 1", Down: "DROP VIEW ev2" + poison}},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := GenerateMigrationFile(
				Plan{Routines: []Routine{sh.routine}}, "add_routine",
				MigrationFileOptions{MigrationsDir: dir, Dialect: DialectSQLite})
			if !errors.Is(err, ErrDirectiveInSQL) {
				t.Fatalf("expected ErrDirectiveInSQL, got %v (path %q)", err, path)
			}
			if path != "" {
				t.Errorf("a poisoned migration file was committed: %s", path)
			}
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatalf("read dir: %v", readErr)
			}
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".sql") || strings.HasSuffix(e.Name(), ".json") {
					t.Errorf("generator left %s behind after refusing the render", e.Name())
				}
			}
		})
	}
}

// Property: the directive guard must be exactly as wide as the runner's
// parser. parseMigration trims each line before matching the "-- +migrate"
// prefix, so a directive indented by spaces or a tab, or followed by a tab,
// still flips a section. If the guard ever matched only the untrimmed
// prefix, an indented poison line would sail through RenderMigrationFileChecked
// and still flip the parser.
//
// Surfaces: RenderMigrationFileChecked must refuse each shape, AND the real
// parser (RegisterFromReader → Up → Down on a live SQLite db) must be shown
// fooled by it — proving the guard's width is load-bearing, not decorative.
func TestDirectiveGuardMatchesParserTrim(t *testing.T) {
	for _, line := range []string{
		"-- +migrate Down",
		"   -- +migrate Down",
		"\t-- +migrate Down",
		"-- +migrate\tDown",
	} {
		up := "SELECT 1\n" + line + "\nCREATE TABLE pwnd (id INTEGER);-- "
		if _, err := RenderMigrationFileChecked(1, "n", up, ""); !errors.Is(err, ErrDirectiveInSQL) {
			t.Errorf("guard accepted a directive line it must refuse: %q", line)
		}

		// The parser really flips on it: after Up+Down, the attacker's
		// CREATE TABLE (parked in the parsed Down section) has executed.
		db := openMigrateSQLite(t)
		m := coremig.New(db, coremig.WithDialect(coremig.DialectSQLite))
		rendered := RenderMigrationFile(1, "n", up, "")
		if err := m.RegisterFromReader(strings.NewReader(rendered)); err != nil {
			t.Fatalf("parse: %v", err)
		}
		ctx := context.Background()
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		if err := m.Down(ctx, 1); err != nil {
			t.Fatalf("Down: %v", err)
		}
		if !sqliteHasObject(t, db, "pwnd") {
			t.Errorf("parser did NOT flip on %q — if true, the guard pin above is unanchored for this shape", line)
		}
	}
}

// Property: the ledger is REPORTING-ONLY. It never gates application: every
// matching routine's Up runs on every boot, so out-of-band DB-side drift
// (someone dropped the object by hand) is self-healed even when the ledger
// row's checksum matches. A checksum-matched skip would turn the ledger into
// a silent-drift amplifier.
func TestRoutineUpRunsDespiteLedgerMatch(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	// SQLite has no CREATE OR REPLACE VIEW; the documented idiom is
	// DROP IF EXISTS + CREATE, which is also what makes re-runs observable.
	plan := Plan{Routines: []Routine{{
		Name: "heal",
		Up:   "DROP VIEW IF EXISTS heal_v; CREATE VIEW heal_v AS SELECT 1",
	}}}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if !sqliteHasObject(t, db, "heal_v") {
		t.Fatal("setup: heal_v missing after first boot")
	}
	// Out-of-band drift the ledger cannot see.
	if _, err := db.Exec("DROP VIEW heal_v"); err != nil {
		t.Fatalf("drift: %v", err)
	}
	if err := AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	if !sqliteHasObject(t, db, "heal_v") {
		t.Error("matching ledger checksum skipped the routine's Up: DB-side drift was not self-healed (the ledger must never gate application)")
	}
	ledger, err := readRoutineLedger(ctx, db)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(ledger) != 1 {
		t.Errorf("ledger rows = %d, want 1", len(ledger))
	}
}

// Property: the ledger (and its summary/orphan logging) applies ONLY to plans
// that carry routines. A routine-less plan must not conjure a phantom
// gofastr_routines table on an app that never opted into routines — an
// unnoticed new table on every boot is schema bloat and a false introspection
// surface.
func TestNoLedgerTableWithoutRoutines(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	reg := testReg{"solo": rawEnt("solo", "solo", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")}
	if err := AutoMigratePlanContext(ctx, db, Plan{Registry: reg}); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if !sqliteHasObject(t, db, "solo") {
		t.Fatal("entity table missing")
	}
	if sqliteHasObject(t, db, "gofastr_routines") {
		t.Error("gofastr_routines created for a plan with no routines")
	}
}

// sqliteHasObject reports whether name exists in sqlite_master (table, view,
// index, or trigger).
func sqliteHasObject(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", name).Scan(&n); err != nil {
		t.Fatalf("sqlite_master lookup for %q: %v", name, err)
	}
	return n > 0
}
