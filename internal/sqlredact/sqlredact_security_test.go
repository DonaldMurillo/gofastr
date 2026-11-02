package sqlredact

import (
	"strings"
	"testing"
)

// TestTextMasksEachDialectsLiterals holds every literal spelling each
// engine reads out of the masked text, and Unknown to the union of all
// of them. framework/slowquery's first masker read every statement as
// PostgreSQL and leaked the SQLite "double-quoted" string fallback,
// MySQL's backslash escape, and PostgreSQL's own nested block comment;
// core/tracing's leaked the nested comment, MySQL's # comment and a
// $é$ dollar tag.
func TestTextMasksEachDialectsLiterals(t *testing.T) {
	cases := []struct {
		in, secret string
		dialects   []Dialect
	}{
		{`SELECT * FROM t WHERE token = "SECRET"`, "SECRET", []Dialect{SQLite, MySQL, Unknown}},
		{`SELECT * FROM t WHERE a = 'a\'SECRET' AND b = 1`, "SECRET", []Dialect{MySQL, Unknown}},
		{`SELECT E'a\'SECRET' FROM t`, "SECRET", []Dialect{Postgres, Unknown}},
		{`/* a /* b */ it's */ SELECT 'SECRET'`, "SECRET", []Dialect{Postgres, Unknown}},
		{"SELECT 1 # it's\nFROM t WHERE a = 'SECRET'", "SECRET", []Dialect{MySQL, Unknown}},
		{`SELECT $é$SECRET$é$`, "SECRET", []Dialect{Postgres, SQLite, MySQL, Unknown}},
		{`SELECT a /* SECRET */ FROM t`, "SECRET", []Dialect{Postgres, SQLite, MySQL, Unknown}},
		{`SELECT 'C:\' || 'SECRET' FROM t`, "SECRET", []Dialect{Postgres, SQLite, Unknown}},
	}
	for _, c := range cases {
		for _, d := range c.dialects {
			if got := Text(c.in, d); strings.Contains(got, c.secret) {
				t.Errorf("Text(%q, %d) = %q: a literal survived", c.in, d, got)
			}
		}
	}
}

// TestTextKeepsShape: masking is for values. Placeholders and plain
// identifiers stay under every dialect, and a PostgreSQL quoted
// identifier stays where the engine is named.
func TestTextKeepsShape(t *testing.T) {
	const q = `SELECT name FROM posts WHERE id = $1 AND x = ? AND y = :n`
	for _, d := range []Dialect{Unknown, Postgres, SQLite, MySQL} {
		if got := Text(q, d); got != q {
			t.Errorf("Text(%q, %d) = %q, want it unchanged", q, d, got)
		}
	}
	if got := Text(`SELECT "Name" FROM posts`, Postgres); got != `SELECT "Name" FROM posts` {
		t.Errorf("Postgres quoted identifier = %q", got)
	}
	for in, want := range map[string]Dialect{"postgres": Postgres, "PostgreSQL": Postgres, "sqlite3": SQLite, "mariadb": MySQL, "sqlserver": Unknown, "": Unknown} {
		if got := ForSystem(in); got != want {
			t.Errorf("ForSystem(%q) = %d, want %d", in, got, want)
		}
	}
}
