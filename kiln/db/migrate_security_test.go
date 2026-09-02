package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Property: a schema.Field.Default of ANY Go type must render as ONE
// SQL string literal that cannot terminate early. This is the exact
// property the framework's own SQLDefault fixed and pinned
// (framework/migrate/migrate.go:1077-1113: "the old unescaped
// fmt.Sprintf(\"'%v'\", v) fallback let a kiln add_entity payload close
// the literal and commit an extra column"). kiln/db keeps a private
// mirror (sqlDefault) whose default arm still splices %v raw between
// unescaped quotes.
//
// Reachability note (verified against this worktree): alignColumns is
// today the only caller, and its only divergence from the framework's
// case-insensitive diff is a case-only column rename, which SQLite
// rejects as "duplicate column name" before any injected tail could
// execute. The sink itself is fully capable: the repo's sqlite driver
// (modernc.org/sqlite via sqlite/stdlib) executes multi-statement
// strings once the first statement parses, so the moment any path lets
// a genuinely-new column reach alignColumns with a hostile non-scalar
// default (or the diff condition changes), the DEFAULT clause executes
// arbitrary DDL. The framework deleted this arm after a verified
// payload; the mirror must not keep it.
//
// isSingleQuotedLiteral mirrors the framework's test helper: s is one
// SQL string literal with every interior quote doubled.
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

type stringerDefault string

func (s stringerDefault) String() string { return string(s) }

func TestSQLDefaultRendersOneLiteral(t *testing.T) {
	hostile := []struct {
		name string
		f    schema.Field
	}{
		{"json map default", schema.Field{Name: "meta", Type: schema.JSON,
			Default: map[string]any{"a": "'; DROP TABLE victims; --"}}},
		{"json slice default", schema.Field{Name: "tags", Type: schema.JSON,
			Default: []any{"x'; ATTACH DATABASE '/tmp/kiln-pwn.db' AS q; --"}}},
		{"stringer default", schema.Field{Name: "flag", Type: schema.String,
			Default: stringerDefault("y'; DROP TABLE victims; --")}},
	}
	for _, tc := range hostile {
		if got := sqlDefault(tc.f); !isSingleQuotedLiteral(got) {
			t.Errorf("SECURITY: [kiln/db] sqlDefault(%s) = %s: the default arm splices the "+
				"%%v rendering raw between unescaped quotes, so one quote in the value "+
				"closes the literal and appends arbitrary DDL to ALTER TABLE ... ADD COLUMN. "+
				"The framework deleted this exact arm after a verified kiln payload "+
				"(framework/migrate/migrate.go:1077-1113); the mirror must render, then "+
				"quote-double, never splice.", tc.name, got)
		}
	}
	// Controls: the scalar arms must keep their exact renderings so the
	// fix cannot regress them.
	if got := sqlDefault(schema.Field{Default: "o'brien"}); got != "'o''brien'" {
		t.Errorf("[kiln/db] string escaping regressed: %s", got)
	}
	if got := sqlDefault(schema.Field{Default: 42}); got != "42" {
		t.Errorf("[kiln/db] int default regressed: %s", got)
	}
	if got := sqlDefault(schema.Field{Default: true}); got != "1" {
		t.Errorf("[kiln/db] bool default regressed: %s", got)
	}
}

// TestAlterAddColumnEscapesHostileDefault executes the package's own
// statement builder against a real SQLite database and pins that the
// emitted ALTER cannot run injected SQL past its DEFAULT clause. The
// modernc driver executes multi-statement strings, so an unescaped
// DEFAULT turns one ADD COLUMN into arbitrary DDL/DML plus ATTACH-based
// file creation at attacker-chosen paths.
func TestAlterAddColumnEscapesHostileDefault(t *testing.T) {
	dir := t.TempDir()
	attach := filepath.ToSlash(filepath.Join(dir, "kiln-pwn.db"))
	d, cleanup, err := EphemeralSQLite("kiln-red")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}

	stmt := alterAddColumn("posts", schema.Field{Name: "meta", Type: schema.JSON,
		Default: map[string]any{"a": "'; ATTACH DATABASE '" + attach + "' AS q; --"}})
	if _, err := d.Exec(stmt); err != nil {
		t.Fatalf("ALTER with hostile default errored (acceptable fix shape, but see literal pin): %v\nstatement: %s", err, stmt)
	}
	if _, err := os.Stat(attach); err == nil {
		t.Fatalf("SECURITY: [kiln/db] the ALTER emitted by alterAddColumn executed injected "+
			"SQL: the ATTACH in the DEFAULT clause created %s.\nstatement: %s\n"+
			"One quote in a JSON map default closed the literal; the repo's sqlite "+
			"driver runs the tail as further statements.", attach, stmt)
	}
	// The stored default must be the WHOLE rendering as one literal, not
	// truncated at the attacker's quote.
	rows, err := d.Query(`SELECT dflt_value FROM pragma_table_info('posts') WHERE name = 'meta'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("meta column missing after ALTER\nstatement: %s", stmt)
	}
	var stored any
	if err := rows.Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == nil || !strings.Contains(stored.(string), "ATTACH DATABASE") {
		t.Errorf("SECURITY: [kiln/db] stored DEFAULT for meta = %v: the literal was truncated "+
			"at the attacker's quote, so the tail executed as separate statements instead of "+
			"being stored; the whole rendering must survive as one quoted literal.\nstatement: %s",
			stored, stmt)
	}
}

// TestAlterTableQuotesHostileIdentifiers pins the same one-statement
// property on the IDENTIFIER fields of the package's own builders:
// alterAddColumn interpolates the table and column names raw
// (migrate.go: `ALTER TABLE %s ADD COLUMN %s`) and tableColumns
// interpolates the table raw into `PRAGMA table_info(...)`. Both derive
// from agent-authored world IR (entity name / table override / field
// name over the unauthenticated add_entity/add_field surface), and the
// modernc driver executes multi-statement strings — the exact primitive
// the DEFAULT fix above closed. A field or table name carrying a `; --`
// tail must not become a second statement.
func TestAlterTableQuotesHostileIdentifiers(t *testing.T) {
	dir := t.TempDir()
	attachAt := func(n int) string {
		return filepath.ToSlash(filepath.Join(dir, fmt.Sprintf("kiln-pwn-%d.db", n)))
	}
	d, cleanup, err := EphemeralSQLite("kiln-red")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	// Surface 1: hostile COLUMN identifier.
	attach := attachAt(1)
	stmt := alterAddColumn("t", schema.Field{
		Name: `meta; ATTACH DATABASE '` + attach + `' AS q; --`, Type: schema.Text,
	})
	_, _ = d.Exec(stmt) // error or not, no second statement may run
	if _, err := os.Stat(attach); err == nil {
		t.Errorf("SECURITY: [kiln/db] the hostile COLUMN name in alterAddColumn executed as a "+
			"second statement: ATTACH created %s.\nstatement: %s\nIdentifiers must be quoted "+
			"(query.QuoteIdent) exactly like the DEFAULT literal is.", attach, stmt)
	}

	// Surface 2: hostile TABLE identifier.
	attach = attachAt(2)
	stmt = alterAddColumn(`t; ATTACH DATABASE '`+attach+`' AS q; --`, schema.Field{
		Name: "meta", Type: schema.Text,
	})
	_, _ = d.Exec(stmt)
	if _, err := os.Stat(attach); err == nil {
		t.Errorf("SECURITY: [kiln/db] the hostile TABLE name in alterAddColumn executed as a "+
			"second statement: ATTACH created %s.\nstatement: %s", attach, stmt)
	}

	// Surface 3: hostile TABLE identifier in the PRAGMA probe.
	attach = attachAt(3)
	_, _ = tableColumns(d, `t) ; ATTACH DATABASE '`+attach+`' AS q; --`)
	if _, err := os.Stat(attach); err == nil {
		t.Errorf("SECURITY: [kiln/db] tableColumns interpolated the table raw into "+
			"PRAGMA table_info(...): the attacker tail executed and created %s.", attach)
	}

	// The witness table must survive every attempt above.
	if _, err := d.Query(`SELECT id FROM posts LIMIT 1`); err != nil {
		t.Fatalf("posts table did not survive the hostile identifiers: %v", err)
	}
}
