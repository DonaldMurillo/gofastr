package migrate

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

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
