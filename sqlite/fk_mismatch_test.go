package sqlite

import (
	"database/sql"
	"strings"
	"testing"
)

// SQLite draws a line this engine did not: a foreign key can fail because a
// row violates it ("FOREIGN KEY constraint failed"), or because the key cannot
// be evaluated at all ("foreign key mismatch"). The second is a property of
// the SCHEMA, not of any row — the parent column is missing, or is not unique,
// so "which row does this point at" has no answer. SQLite refuses every
// statement that would have to ask.
//
// This engine used to answer anyway, against whichever column the declaration
// named, which made a malformed key look enforced. That is worse than either
// alternative: the schema is wrong, the engine says nothing, and the writes
// that a real database refuses outright are accepted here — so the cross-check
// suites validated schemas production cannot run.

func TestForeignKeyToANonUniqueColumnIsAMismatch(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`)
	exec(t, e, `INSERT INTO p (id, code) VALUES (1, 'a')`)

	// Every statement that has to evaluate the key fails, including the ones
	// that would SATISFY it. A row that matches an unusable key is still a row
	// written through an unusable key.
	for _, stmt := range []string{
		`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
		`INSERT INTO c (id, pcode) VALUES (11, 'nope')`,
		`DELETE FROM p WHERE id = 1`,
		`UPDATE p SET code = 'b' WHERE id = 1`,
		`DROP TABLE p`,
	} {
		err := execErr(e, stmt)
		if err == nil {
			t.Errorf("%s was accepted through a key with a non-unique parent column", stmt)
			continue
		}
		if !strings.Contains(err.Error(), "mismatch") {
			t.Errorf("%s failed with %v, want a foreign key mismatch", stmt, err)
		}
	}

	// A NULL child value does not escape it either. NULL satisfies a WORKING
	// foreign key; it cannot satisfy one the engine has already decided it
	// cannot evaluate, and checking the value first is how the mismatch got
	// skipped for exactly the rows that carry no key.
	if err := execErr(e, `INSERT INTO c (id, pcode) VALUES (12, NULL)`); err == nil {
		t.Error("a NULL child value was written through an unusable foreign key")
	}
}

func TestForeignKeyMismatchShapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		schema  []string
		stmt    string
		wantErr bool
	}{
		{
			name: "unique constraint makes the column a legal target",
			schema: []string{
				`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
				`INSERT INTO p (id, code) VALUES (1, 'a')`,
			},
			stmt: `INSERT INTO c (id, pcode) VALUES (10, 'a')`,
		},
		{
			name: "a full unique index makes the column a legal target",
			schema: []string{
				`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
				`CREATE UNIQUE INDEX p_code ON p (code)`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
				`INSERT INTO p (id, code) VALUES (1, 'a')`,
			},
			stmt: `INSERT INTO c (id, pcode) VALUES (10, 'a')`,
		},
		{
			// A partial index constrains only the rows its predicate selects,
			// so keys outside it can still repeat and the column is not unique.
			name: "a partial unique index does not",
			schema: []string{
				`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
				`CREATE UNIQUE INDEX p_code ON p (code) WHERE code IS NOT NULL`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
				`INSERT INTO p (id, code) VALUES (1, 'a')`,
			},
			stmt:    `INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			wantErr: true,
		},
		{
			// A non-integer PRIMARY KEY is recorded on the column rather than
			// positionally, so resolving a bare REFERENCES by looking only at
			// the rowid alias would miss it.
			name: "a TEXT primary key is a legal bare target",
			schema: []string{
				`CREATE TABLE p (code TEXT PRIMARY KEY, name TEXT)`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT REFERENCES p)`,
				`INSERT INTO p (code, name) VALUES ('a', 'alice')`,
			},
			stmt: `INSERT INTO c (id, pcode) VALUES (10, 'a')`,
		},
		{
			name: "a bare REFERENCES to a table with no primary key is not",
			schema: []string{
				`CREATE TABLE p (a TEXT, b TEXT)`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pa TEXT REFERENCES p)`,
				`INSERT INTO p (a, b) VALUES ('x', 'y')`,
			},
			stmt:    `INSERT INTO c (id, pa) VALUES (10, 'x')`,
			wantErr: true,
		},
		{
			name: "a REFERENCES naming a column the parent does not have is not",
			schema: []string{
				`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
				`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(ghost))`,
				`INSERT INTO p (id) VALUES (1)`,
			},
			stmt:    `INSERT INTO c (id, pid) VALUES (10, 1)`,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			for _, stmt := range tc.schema {
				exec(t, e, stmt)
			}
			err := execErr(e, tc.stmt)
			if tc.wantErr && err == nil {
				t.Fatalf("%s was accepted; the key is not one SQLite can evaluate", tc.stmt)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s was refused: %v", tc.stmt, err)
			}
		})
	}
}

// A pooled sql.DB shares one engine across every connection, so the pragma
// cannot be scoped the way SQLite scopes it. The OFF direction is refused
// there rather than silently disabling enforcement for connections that never
// asked; the ON direction is accepted because strengthening a shared setting
// harms nobody.
func TestPragmaForeignKeysCannotBeDisabledOnAPool(t *testing.T) {
	db, err := sql.Open("gofastr-sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExecDB(t, db, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	mustExecDB(t, db, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)

	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err == nil {
		t.Fatal("PRAGMA foreign_keys = OFF was accepted on a pooled handle — one connection can disable enforcement for all of them")
	}
	if _, err := db.Exec(`INSERT INTO c (id, pid) VALUES (10, 999)`); err == nil {
		t.Error("a dangling child was written after a refused PRAGMA — the refusal did not hold")
	}
	// The refusal is one-directional, or every test that reasserts the default
	// would break.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Errorf("PRAGMA foreign_keys = ON was refused: %v", err)
	}
	var v int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&v); err != nil {
		t.Fatalf("reading the pragma: %v", err)
	}
	if v != 1 {
		t.Errorf("PRAGMA foreign_keys reads %d, want 1", v)
	}
}

func execErr(e *Engine, sql string) error {
	_, err := e.Execute(sql)
	return err
}

func mustExecDB(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// A database file written before `unique_constraints` was serialized loads
// with a primary key and no unique constraints — `omitempty` drops the field
// entirely. The primary-key branch of fkTargetIsUnique is what keeps foreign
// keys into such a file working; without it every one of them becomes a
// mismatch on open. Constructed directly because no SQL statement can produce
// this shape: BuildTableInfo always records the primary key in both places.
func TestPrimaryKeyIsAUniqueTargetWithoutTheConstraintList(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`)
	ti, ok := e.schema.GetTable("p")
	if !ok {
		t.Fatal("table p missing")
	}
	if ti.PrimaryKey != 0 {
		t.Fatalf("PrimaryKey = %d, want 0 — the fixture depends on it", ti.PrimaryKey)
	}
	// The state a legacy file loads in.
	ti.UniqueConstraints = nil
	if !e.fkTargetIsUnique(ti, ti.PrimaryKey) {
		t.Error("the primary key stopped being a legal foreign-key target once the unique-constraint list was empty — every foreign key in a legacy database file would become a mismatch")
	}
	// And the negative direction, or this only proves the function returns true.
	if e.fkTargetIsUnique(ti, 1) {
		t.Error("a plain column was reported as a unique target")
	}
}
