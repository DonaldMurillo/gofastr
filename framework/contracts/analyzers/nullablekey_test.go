package analyzers_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func hits1418(ds []contracts.Diagnostic) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == contracts.RuleNullableKey {
			out = append(out, d)
		}
	}
	return out
}

func nkFixture(t *testing.T, name, body string) []contracts.Diagnostic {
	t.Helper()
	marker := ""
	if rest, ok := strings.CutPrefix(body, "//"); ok {
		marker, body, _ = strings.Cut("//"+rest, "\n")
	}
	src := "package store\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n\nfunc ddl(table, col, typ string) string {\n\t" + marker + "\n\treturn " + body + "\n}\n"
	return hits1418(fixture(t, map[string]string{name: src}))
}

// The rule fires exactly where SQLite stores a NULL key: each statement
// is read by the rule and also run, with a NULL-key insert, on SQLite.
func TestNullableKeyRuleMatchesSQLite(t *testing.T) {
	cases := []struct {
		name, ddl, insert, key string
		nullable               bool
		names                  string
	}{
		{"text key", `CREATE TABLE IF NOT EXISTS t (id TEXT PRIMARY KEY, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", true, "column id "},
		{"text key says not null", `CREATE TABLE IF NOT EXISTS t (id TEXT PRIMARY KEY NOT NULL, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", false, ""},
		{"not null before the key", `CREATE TABLE t (id TEXT NOT NULL PRIMARY KEY, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", false, ""},
		{"key with a reference", `CREATE TABLE t (id TEXT PRIMARY KEY REFERENCES t(id), v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", true, "column id "},
		{"bigint key is no rowid", `CREATE TABLE t (id BIGINT PRIMARY KEY, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", true, "column id "},
		{"integer key is the rowid", `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", false, ""},
		{"inline descending key is not the rowid", `CREATE TABLE t (id INTEGER PRIMARY KEY DESC, v TEXT)`, `INSERT INTO t (id) VALUES (NULL)`, "id", true, "column id "},
		{"table-level integer key is the rowid", `CREATE TABLE t (id INTEGER, v TEXT, PRIMARY KEY (id))`, `INSERT INTO t (id) VALUES (NULL)`, "id", false, ""},
		{"table-level descending key is the rowid", `CREATE TABLE t (id INTEGER, PRIMARY KEY (id DESC))`, `INSERT INTO t (id) VALUES (NULL)`, "id", false, ""},
		{"sized integer key is not the rowid", `CREATE TABLE t (id INTEGER(8) PRIMARY KEY)`, `INSERT INTO t (id) VALUES (NULL)`, "id", true, "column id "},
		{"without rowid rejects null keys", `CREATE TABLE t (id INTEGER PRIMARY KEY DESC) WITHOUT ROWID`, `INSERT INTO t (id) VALUES (NULL)`, "id", false, ""},
		{"strict tables reject null keys", `CREATE TABLE t (id TEXT PRIMARY KEY) STRICT`, `INSERT INTO t (id) VALUES (NULL)`, "id", false, ""},
		{"autoincrement key", `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", false, ""},
		{"checked integer key", `CREATE TABLE t (id INTEGER PRIMARY KEY CHECK (id = 1), v TEXT)`, `INSERT INTO t (id, v) VALUES (NULL, 'a')`, "id", false, ""},
		{"composite key, one column open", `CREATE TABLE t (a TEXT NOT NULL, b TEXT, v TEXT, PRIMARY KEY (a, b))`, `INSERT INTO t (a, b) VALUES ('x', NULL)`, "b", true, "column b "},
		{"composite integer key", `CREATE TABLE t (a INTEGER, b INTEGER NOT NULL, PRIMARY KEY (a, b))`, `INSERT INTO t (a, b) VALUES (NULL, 1)`, "a", true, "column a "},
		{"composite key, all not null", `CREATE TABLE t (a TEXT NOT NULL, b TEXT NOT NULL, PRIMARY KEY (a, b))`, `INSERT INTO t (a, b) VALUES ('x', NULL)`, "b", false, ""},
		{"named key constraint", `CREATE TABLE t (a TEXT, v TEXT, CONSTRAINT t_pk PRIMARY KEY (a))`, `INSERT INTO t (a) VALUES (NULL)`, "a", true, "column a "},
		{"quoted key column", `CREATE TABLE t ("Key" TEXT, v TEXT, PRIMARY KEY ("Key"))`, `INSERT INTO t ("Key") VALUES (NULL)`, `"Key"`, true, "column key "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := nkFixture(t, "store/ddl.go", "`"+c.ddl+"`")
			if fired := len(hs) == 1; fired != c.nullable {
				t.Fatalf("rule fired=%v, want %v: %v", fired, c.nullable, hs)
			}
			if c.nullable && !strings.Contains(hs[0].Message, c.names) {
				t.Errorf("message should name %q: %s", c.names, hs[0].Message)
			}
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if _, err := db.Exec(c.ddl); err != nil {
				t.Fatal(err)
			}
			_, insertErr := db.Exec(c.insert)
			var stored int
			if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE ` + c.key + ` IS NULL`).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if (stored > 0) != c.nullable {
				t.Fatalf("SQLite stored %d NULL keys (insert error %v), rule said nullable=%v", stored, insertErr, c.nullable)
			}
		})
	}
}

// Statements only Postgres runs, and names spelled through a verb or an
// operand, which the rule reads as the expression behind them.
func TestNullableKeySpellings(t *testing.T) {
	cases := []struct {
		name, file, body string
		want             string // a fragment of the one finding; "" is quiet
	}{
		{"serial key", "store/ddl.go", "`CREATE TABLE t (id BIGSERIAL PRIMARY KEY, v TEXT)`", ""},
		{"table through a verb", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, v TEXT)`, table)", "column id "},
		{"column through a verb", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %s (%s TEXT PRIMARY KEY, v %s)`, table, col, typ)", "column col "},
		{"type through a verb", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY)`, table, typ)", "column id "},
		{"verb column, not null", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %s (%s TEXT PRIMARY KEY NOT NULL)`, table, col)", ""},
		{"composite through verbs", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %s (%s TEXT NOT NULL, %s TEXT, PRIMARY KEY (%s, %s))`, table, col, typ, col, typ)", "column typ "},
		{"composite through verbs, not null", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %s (%s TEXT NOT NULL, %s TEXT NOT NULL, PRIMARY KEY (%s, %s))`, table, col, typ, col, typ)", ""},
		{"indexed verbs", "store/ddl.go", "fmt.Sprintf(`CREATE TABLE %[1]s (%[2]s TEXT, PRIMARY KEY (%[2]s))`, table, col)", "column col "},
		{"concatenation", "store/ddl.go", "`CREATE TABLE ` + table + ` (` + col + ` TEXT, PRIMARY KEY (` + col + `))`", "column col "},
		{"concatenation, not null", "store/ddl.go", "`CREATE TABLE ` + table + ` (` + col + ` TEXT NOT NULL, PRIMARY KEY (` + col + `))`", ""},
		{"key names no declared column", "store/ddl.go", "`CREATE TABLE t (a TEXT NOT NULL, PRIMARY KEY (b))`", "no column of that name"},
		{"test file", "store/ddl_test.go", "`CREATE TABLE t (id TEXT PRIMARY KEY)`", ""},
		{"allowed with a reason", "store/ddl.go", "//gofastr:allow(GOFASTR1418) a trigger fills the key before the row lands\n`CREATE TABLE t (id TEXT PRIMARY KEY)`", ""},
		{"prose, not a statement", "store/ddl.go", "`run CREATE TABLE once per boot (id TEXT PRIMARY KEY is typical)`", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := nkFixture(t, c.file, c.body)
			if c.want == "" {
				if len(hs) != 0 {
					t.Fatalf("want quiet, got %v", hs)
				}
				return
			}
			if len(hs) != 1 || !strings.Contains(hs[0].Message, c.want) {
				t.Fatalf("want one finding naming %q, got %v", c.want, hs)
			}
		})
	}
}
