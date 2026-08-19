package sqlite

import (
	"path/filepath"
	"strings"
	"testing"
)

// FormatExpr renders an expression back to source so that partial-index
// predicates and non-constant column defaults survive a close and reopen.
// Anything it cannot render it returns "" for — and "" is not a neutral
// answer. An empty index predicate is how the schema spells a FULL unique
// index, so a predicate that failed to render came back as a STRONGER
// constraint than the one written, and a foreign key pointing at that column
// was then accepted as if the column were unique.
//
// `IS NOT NULL` — the single most common partial-index predicate there is —
// was one of the unrendered forms.

func TestFormatExprRoundTripsEveryPredicateForm(t *testing.T) {
	for _, src := range []string{
		`code IS NOT NULL`,
		`code IS NULL`,
		`code > 5`,
		`code != 'x'`,
		`code IN (1, 2, 3)`,
		`code NOT IN ('a', 'b')`,
		`code BETWEEN 1 AND 9`,
		`code NOT BETWEEN 1 AND 9`,
		`code LIKE 'a%'`,
		`code NOT LIKE 'a%'`,
		`code GLOB 'a*'`,
		`CAST(code AS INTEGER) > 1`,
		`CASE WHEN code > 1 THEN 1 ELSE 0 END`,
		`rowid > 10`,
		`NOT code`,
		`code > 1 AND code < 9`,
		`(code > 1) OR (code IS NULL)`,
	} {
		p := NewParser(`SELECT * FROM t WHERE ` + src)
		stmt, err := p.Parse()
		if err != nil {
			t.Errorf("%s: parse: %v", src, err)
			continue
		}
		rendered := FormatExpr(stmt.(*SelectStmt).Where)
		if rendered == "" {
			t.Errorf("%s rendered as empty — an unrendered predicate is silently dropped, and a dropped index predicate reads back as a full index", src)
			continue
		}
		// The render has to re-parse, or persisting it just moves the failure
		// to the next open.
		if _, err := ParseExpression(rendered); err != nil {
			t.Errorf("%s rendered as %q, which does not re-parse: %v", src, rendered, err)
		}
	}
}

func TestPartialUniqueIndexSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	// The engine directly rather than through sql.OpenDB: this test reads
	// schema metadata, which the database/sql surface does not expose.
	db, err := newDiskEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, `CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`)
	exec(t, db, `CREATE UNIQUE INDEX p_code ON p (code) WHERE code IS NOT NULL`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := newDiskEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	idxs := db2.schema.IndexesForTable("p")
	if len(idxs) != 1 {
		t.Fatalf("reopened with %d indexes on p, want 1", len(idxs))
	}
	if idxs[0].Where == "" {
		t.Fatal("the partial index came back with no predicate — it is now a FULL unique index, a constraint nobody wrote")
	}
	// And the schema-level consequence, which is what made this reachable from
	// the foreign-key path.
	exec(t, db2, `CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`)
	exec(t, db2, `INSERT INTO p (id, code) VALUES (1, 'a')`)
	if _, err := db2.Execute(`INSERT INTO c (id, pcode) VALUES (10, 'a')`); err == nil {
		t.Error("a foreign key targeting a partially-unique column was accepted after reopen")
	}
}

// The other half of the contract: when a predicate genuinely cannot be
// rendered, the CREATE is refused rather than quietly stored as a full index.
func TestUnrenderablePartialIndexIsRefused(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`)
	exec(t, e, `CREATE TABLE q (x TEXT)`)
	_, err := e.Execute(`CREATE UNIQUE INDEX p_code ON p (code) WHERE code IN (SELECT x FROM q)`)
	if err == nil {
		t.Fatal("an index whose predicate cannot be persisted was created — it would reopen as a full unique index")
	}
	if !strings.Contains(err.Error(), "partial index predicate") {
		t.Errorf("refused with %v, want an unsupported-predicate error", err)
	}
	if len(e.schema.IndexesForTable("p")) != 0 {
		t.Error("the refused index was registered anyway")
	}
}
