package sqlite

import (
	"strings"
	"testing"
)

// AutoMigrate emits foreign keys as a TABLE constraint —
// `FOREIGN KEY (col) REFERENCES target(id)` — never as an inline column
// REFERENCES. The parser only ever handled the inline form, so the table
// form fell through to parseColumnDef and was read as a COLUMN literally
// named "FOREIGN" of type "KEY(col)". That phantom column carried the
// REFERENCES constraint, so the engine registered a foreign key against a
// column that does not exist and always holds NULL — which no row can
// violate. Every FK the framework's own migrator writes was unenforceable,
// and the extra column shifted `SELECT *` and positional INSERT.
func TestTableConstraintFKParsesWithoutPhantomColumn(t *testing.T) {
	p := NewParser(`CREATE TABLE items (id TEXT PRIMARY KEY, order_id TEXT, FOREIGN KEY (order_id) REFERENCES orders(id))`)
	stmt, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ct := stmt.(*CreateTableStmt)

	if len(ct.Columns) != 2 {
		names := make([]string, len(ct.Columns))
		for i, c := range ct.Columns {
			names[i] = c.Name
		}
		t.Fatalf("columns = %v, want exactly [id order_id] — a table constraint is not a column", names)
	}

	if len(ct.TableConstraints) != 1 {
		t.Fatalf("table constraints = %d, want 1", len(ct.TableConstraints))
	}
	tc := ct.TableConstraints[0]
	if tc.Type != ConstraintForeignKey {
		t.Errorf("constraint type = %v, want ConstraintForeignKey", tc.Type)
	}
	if len(tc.Columns) != 1 || tc.Columns[0] != "order_id" {
		t.Errorf("constraint columns = %v, want [order_id]", tc.Columns)
	}
	if tc.RefTable != "orders" {
		t.Errorf("RefTable = %q, want orders", tc.RefTable)
	}
	if len(tc.RefCols) != 1 || tc.RefCols[0] != "id" {
		t.Errorf("RefCols = %v, want [id]", tc.RefCols)
	}

	// The constraint has to reach TableInfo pointing at the REAL column, or
	// enforcement checks a column the row never fills.
	info := BuildTableInfo(ct, 1)
	if len(info.ForeignKeys) != 1 {
		t.Fatalf("TableInfo.ForeignKeys = %d, want 1", len(info.ForeignKeys))
	}
	fk := info.ForeignKeys[0]
	if got := info.Columns[fk.FromCol].Name; got != "order_id" {
		t.Errorf("FK points at column %q, want order_id — this is the phantom-column bug", got)
	}
	if fk.ToTable != "orders" {
		t.Errorf("FK.ToTable = %q, want orders", fk.ToTable)
	}
}

// Enforcement is the point of the parse fix: with PRAGMA foreign_keys ON,
// an id that names no parent row must be refused.
func TestTableConstraintFKIsEnforced(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE orders (id TEXT PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE items (id TEXT PRIMARY KEY, order_id TEXT, FOREIGN KEY (order_id) REFERENCES orders(id))`)
	exec(t, e, `INSERT INTO orders (id) VALUES ('o1')`)

	// A real parent resolves.
	exec(t, e, `INSERT INTO items (id, order_id) VALUES ('i1','o1')`)

	// A fabricated one must not.
	if _, err := e.Execute(`INSERT INTO items (id, order_id) VALUES ('i2','nope')`); err == nil {
		t.Error("an orphan FK insert was accepted — the table-constraint foreign key is not enforced")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("orphan insert failed with %v, want a FOREIGN KEY constraint error", err)
	}

	// And SELECT * must not expose a phantom column.
	res, err := e.Execute(`SELECT * FROM items`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(res.Columns) != 2 {
		t.Errorf("SELECT * columns = %v, want [id order_id]", res.Columns)
	}
}

// A composite table-constraint FK cannot be represented by ForeignKeyInfo,
// which holds a single FromCol. Dropping it silently is how the single-column
// form went unenforced for so long, so refuse it at parse time instead.
func TestCompositeTableConstraintFKIsRefused(t *testing.T) {
	// Two independent guards refuse composites: one on the child column list,
	// one on the referenced column list. A single fixture naming both as
	// composite trips both at once, so deleting either one alone leaves the
	// test green — it proves "at least one fired", which is not what it claims.
	// One fixture per guard.
	cases := []struct{ name, ddl string }{
		{"composite child columns", `CREATE TABLE items (a TEXT, b TEXT, FOREIGN KEY (a, b) REFERENCES parent(x))`},
		{"composite referenced columns", `CREATE TABLE items (a TEXT, b TEXT, FOREIGN KEY (a) REFERENCES parent(x, y))`},
		{"composite on both sides", `CREATE TABLE items (a TEXT, b TEXT, FOREIGN KEY (a, b) REFERENCES parent(x, y))`},
	}
	for _, c := range cases {
		if _, err := NewParser(c.ddl).Parse(); err == nil {
			t.Errorf("%s: parsed cleanly — a composite FOREIGN KEY must be refused, not silently dropped\n  %s", c.name, c.ddl)
		}
	}

	// And the single-column form still parses, or the guards are too broad.
	if _, err := NewParser(`CREATE TABLE items (a TEXT, b TEXT, FOREIGN KEY (a) REFERENCES parent(x))`).Parse(); err != nil {
		t.Errorf("a single-column FOREIGN KEY was refused: %v", err)
	}
}

// The parent-delete check collected rows from a cursor that reuses one Record
// buffer per scan, so every collected row aliased the LAST row visited. With a
// single-row parent table — the shape the existing FK tests use — the alias is
// the row being deleted and the check looks correct. Add a second row and it
// checks the wrong one: a referenced parent is deleted and its children are
// left pointing at nothing.
func TestDeleteRefusesAReferencedParentAmongMany(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES parent(id))`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (1, 'alice')`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (2, 'bob')`)
	exec(t, e, `INSERT INTO child (id, parent_id) VALUES (10, 1)`)

	// Deleting the FIRST parent, which is the referenced one. The scan visits
	// row 2 last, so a check reading the stale buffer compares against bob.
	if _, err := e.Execute(`DELETE FROM parent WHERE id = 1`); err == nil {
		t.Error("deleted a parent that a child still references — the check read a different row than the one being deleted")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("delete failed with %v, want a FOREIGN KEY constraint error", err)
	}

	// The unreferenced parent must still be deletable, or the fix is just a
	// blanket refusal.
	if _, err := e.Execute(`DELETE FROM parent WHERE id = 2`); err != nil {
		t.Errorf("deleting an UNreferenced parent was refused: %v", err)
	}
}

// DELETE with no WHERE takes a fast path that rebuilds the table btree without
// consulting foreign keys at all, so a bulk delete of a referenced parent
// table left every child dangling. Real SQLite performs an implicit row-wise
// delete and refuses.
func TestBareDeleteRefusesAReferencedParentTable(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES parent(id))`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (1, 'alice')`)
	exec(t, e, `INSERT INTO child (id, parent_id) VALUES (10, 1)`)

	if _, err := e.Execute(`DELETE FROM parent`); err == nil {
		t.Error("a bare DELETE emptied a table whose rows are still referenced")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("bare delete failed with %v, want a FOREIGN KEY constraint error", err)
	}

	// And a bare delete of an unreferenced table still works.
	exec(t, e, `DELETE FROM child`)
	if _, err := e.Execute(`DELETE FROM parent`); err != nil {
		t.Errorf("bare delete refused after the children were removed: %v", err)
	}
}

// A refused bare DELETE must leave the table exactly as it was.
//
// What this pins is the ROW state: all rows survive a refusal. It does not pin
// the two-pass ordering itself — a one-pass variant that drops index entries
// before refusing passes this test and every index-shaped assertion too,
// because UNIQUE enforcement and lookups consult the table rather than the
// index alone. See the comment at the delete path for why the ordering is kept
// regardless.
func TestRefusedBareDeleteMutatesNothing(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES parent(id))`)
	// Three parents; only the LAST is referenced, so the first two clear the
	// check before the refusal lands.
	exec(t, e, `INSERT INTO parent (id, name) VALUES (1, 'a')`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (2, 'b')`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (3, 'c')`)
	exec(t, e, `INSERT INTO child (id, parent_id) VALUES (10, 3)`)

	if _, err := e.Execute(`DELETE FROM parent`); err == nil {
		t.Fatal("the bare delete was accepted despite a referenced row")
	}

	res := exec(t, e, `SELECT id FROM parent`)
	if len(res.Rows) != 3 {
		t.Errorf("after a refused DELETE the table holds %d rows, want all 3 — the statement mutated the table it reported failing on", len(res.Rows))
	}
}

// Only the child side was checked on UPDATE. Moving a parent's referenced key
// out from under its children left them dangling — real SQLite refuses.
func TestUpdateRefusesMovingAReferencedParentKey(t *testing.T) {
	e := newTestEngine(t)
	// The referenced column is `code`, not the rowid primary key: the engine
	// refuses to update an INTEGER PRIMARY KEY at all, so a fixture keyed on
	// it would be refused for a reason that has nothing to do with foreign
	// keys and would pass whether or not this check exists.
	//
	// `code` must be UNIQUE. Without it this fixture is a foreign key real
	// SQLite refuses to process at all ("foreign key mismatch"), so the test
	// pinned an enforcement decision no other engine makes — it passed only
	// because this engine used to answer a question SQLite declines to ask.
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, code TEXT UNIQUE, name TEXT)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_code TEXT, FOREIGN KEY (parent_code) REFERENCES parent(code))`)
	exec(t, e, `INSERT INTO parent (id, code, name) VALUES (1, 'A', 'alice')`)
	exec(t, e, `INSERT INTO parent (id, code, name) VALUES (2, 'B', 'bob')`)
	exec(t, e, `INSERT INTO child (id, parent_code) VALUES (10, 'A')`)

	if _, err := e.Execute(`UPDATE parent SET code = 'Z' WHERE code = 'A'`); err == nil {
		t.Error("moved a parent's referenced key out from under a child that points at it")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("update failed with %v, want a FOREIGN KEY constraint error", err)
	}

	// A non-referenced column on the same row is nobody's business.
	if _, err := e.Execute(`UPDATE parent SET name = 'alice2' WHERE code = 'A'`); err != nil {
		t.Errorf("updating a non-referenced column was refused: %v", err)
	}
	// And an unreferenced parent's key still moves, or this is a blanket refusal.
	if _, err := e.Execute(`UPDATE parent SET code = 'Y' WHERE code = 'B'`); err != nil {
		t.Errorf("moving an UNreferenced parent's key was refused: %v", err)
	}
}

// A FOREIGN KEY naming a column the table does not have was dropped in
// silence, which is the posture the composite refusal in this same parser
// explicitly rejects: a wrong-but-quiet database is worse than one that will
// not open.
func TestForeignKeyNamingAnUnknownColumnIsRefused(t *testing.T) {
	p := NewParser(`CREATE TABLE c (id TEXT PRIMARY KEY, FOREIGN KEY (ghost) REFERENCES p(id))`)
	_, err := p.Parse()
	if err == nil {
		t.Fatal("a FOREIGN KEY naming a column that does not exist was accepted and silently dropped")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("the refusal should name the unknown column, got %v", err)
	}

	// The valid spelling must still parse. Constraint-LAST, because real SQLite
	// requires a table constraint to follow a comma after the column list —
	// `CREATE TABLE c (FOREIGN KEY …, id …)` is a syntax error there, and
	// pinning it here would enshrine SQL the shipped driver rejects. This
	// parser is more lenient about placement; the test asserts the spelling
	// that works everywhere.
	ok := NewParser(`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id))`)
	if _, err := ok.Parse(); err != nil {
		t.Errorf("a table-constraint FOREIGN KEY after its column was refused: %v", err)
	}
}

// Real-world DDL — sqlite3 .dump, Django, SQLAlchemy — writes referential
// actions and named constraints. The engine has no cascade machinery, but
// failing to PARSE these means such a schema cannot be opened at all, which is
// a much bigger problem than ignoring the action.
func TestForeignKeyClauseVariantsParse(t *testing.T) {
	cases := []string{
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) ON DELETE CASCADE)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) ON DELETE SET NULL)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) ON UPDATE CASCADE ON DELETE RESTRICT)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) ON DELETE NO ACTION)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) ON DELETE SET DEFAULT)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) MATCH SIMPLE)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, FOREIGN KEY (pid) REFERENCES p(id) NOT DEFERRABLE)`,
		// The named-constraint spelling most ORMs emit. Without it the
		// CONSTRAINT token falls through to the column parser — the same
		// phantom-column bug this change set fixed for the bare spelling.
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, CONSTRAINT fk_p FOREIGN KEY (pid) REFERENCES p(id))`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT, CONSTRAINT fk_p FOREIGN KEY (pid) REFERENCES p(id) ON DELETE CASCADE)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT UNIQUE, CONSTRAINT uq_pid UNIQUE (pid))`,
		// Inline column REFERENCES carries the same trailing clauses.
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT REFERENCES p(id) ON DELETE CASCADE)`,
	}
	for _, ddl := range cases {
		p := NewParser(ddl)
		stmt, err := p.Parse()
		if err != nil {
			t.Errorf("parse failed: %v\n  %s", err, ddl)
			continue
		}
		ct, ok := stmt.(*CreateTableStmt)
		if !ok {
			t.Errorf("not a CreateTableStmt: %T\n  %s", stmt, ddl)
			continue
		}
		// The phantom-column symptom: a table element read as a column.
		for _, col := range ct.Columns {
			if strings.EqualFold(col.Name, "FOREIGN") || strings.EqualFold(col.Name, "CONSTRAINT") {
				t.Errorf("a table constraint was read as a column %q\n  %s", col.Name, ddl)
			}
		}
	}
}

// Ignoring the action is a deliberate limitation, so it must not be mistaken
// for support: a declared CASCADE still refuses the delete rather than
// cascading it, which is the safe direction.
//
// This is the one place the engine deliberately DIVERGES from the shipped
// driver rather than matching it: modernc with foreign_keys(1) cascades and
// removes the children. Refusing is stricter, never looser, so no row survives
// here that the driver would have deleted — but a suite comparing the two
// engines should expect this case to differ.
func TestOnDeleteCascadeIsParsedButNotHonoured(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES parent(id) ON DELETE CASCADE)`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (1, 'alice')`)
	exec(t, e, `INSERT INTO parent (id, name) VALUES (2, 'bob')`)
	exec(t, e, `INSERT INTO child (id, parent_id) VALUES (10, 1)`)

	if _, err := e.Execute(`DELETE FROM parent WHERE id = 1`); err == nil {
		t.Error("ON DELETE CASCADE was treated as supported — the engine has no cascade machinery, so it must refuse rather than silently orphan or delete rows")
	}
}

// SQLite applies the PARENT key's affinity to the child value before comparing
// (docs §4.2), so a TEXT child column referencing an INTEGER parent key
// matches. The engine compared raw types and rejected the pair, which means
// the pure-engine suites validated behavior real SQLite does not have — the
// opposite of what a cross-check engine is for.
func TestForeignKeyComparisonAppliesParentAffinity(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE u (id INTEGER PRIMARY KEY, name TEXT)`)
	exec(t, e, `CREATE TABLE p (id TEXT PRIMARY KEY, uid TEXT, FOREIGN KEY (uid) REFERENCES u(id))`)
	exec(t, e, `INSERT INTO u (id, name) VALUES (7, 'alice')`)

	// TEXT '7' against an INTEGER parent key 7.
	if _, err := e.Execute(`INSERT INTO p (id, uid) VALUES ('x', '7')`); err != nil {
		t.Errorf("a TEXT child value referencing an INTEGER parent key was rejected: %v", err)
	}
	// An integer literal into the same TEXT column.
	if _, err := e.Execute(`INSERT INTO p (id, uid) VALUES ('y', 7)`); err != nil {
		t.Errorf("an INTEGER child value referencing an INTEGER parent key was rejected: %v", err)
	}
	// A genuinely absent parent must still fail, or this is just leniency.
	if _, err := e.Execute(`INSERT INTO p (id, uid) VALUES ('z', '8')`); err == nil {
		t.Error("a child value with no matching parent was accepted — the comparison is now too loose")
	}

	// And the parent-delete direction sees the same match.
	if _, err := e.Execute(`DELETE FROM u WHERE id = 7`); err == nil {
		t.Error("deleted a parent still referenced by a cross-affinity child value")
	}
}

// The CONSTRAINT-name branch consumes `CONSTRAINT <name>` then re-dispatches,
// but parseTableElements has no CHECK arm — so a named CHECK constraint went
// from "parsed wrongly as a phantom column, but openable" to "parse error".
// That is the outcome the trailer/CONSTRAINT work exists to prevent, and
// Django and SQLAlchemy both emit named CHECK constraints.
func TestNamedCheckConstraintParses(t *testing.T) {
	cases := []string{
		`CREATE TABLE c (id TEXT PRIMARY KEY, a INTEGER, CONSTRAINT ck_a CHECK (a > 0))`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, a INTEGER, CHECK (a > 0))`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, a INTEGER, CONSTRAINT ck_a CHECK (a > 0 AND a < (10 + 1)))`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, a INTEGER, CONSTRAINT ck_a CHECK (a > 0), CONSTRAINT uq_a UNIQUE (a))`,
		// A column literally named "constraint" must still work — the branch
		// keys on the token, so a column of that name could be swallowed.
		`CREATE TABLE c (id TEXT PRIMARY KEY, "constraint" TEXT)`,
	}
	for _, ddl := range cases {
		stmt, err := NewParser(ddl).Parse()
		if err != nil {
			t.Errorf("parse failed: %v\n  %s", err, ddl)
			continue
		}
		ct, ok := stmt.(*CreateTableStmt)
		if !ok {
			t.Errorf("not a CreateTableStmt: %T\n  %s", stmt, ddl)
			continue
		}
		for _, col := range ct.Columns {
			if strings.EqualFold(col.Name, "CHECK") || strings.EqualFold(col.Name, "CONSTRAINT") && !strings.Contains(ddl, `"constraint"`) {
				t.Errorf("a table constraint was read as column %q\n  %s", col.Name, ddl)
			}
		}
	}
}

// SQLite's grammar allows a bare NULL column constraint (`ccons ::= NULL`), and
// it appears after REFERENCES in generated DDL. The element loop ended on it.
func TestBareNullColumnConstraintParses(t *testing.T) {
	for _, ddl := range []string{
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT REFERENCES p(id) NULL)`,
		`CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT NULL)`,
	} {
		if _, err := NewParser(ddl).Parse(); err != nil {
			t.Errorf("parse failed: %v\n  %s", err, ddl)
		}
	}
}

// The parent-side UPDATE check was added to executeUpdate, but two other
// statements move or remove parent rows and neither went through it.
// `ON CONFLICT DO UPDATE` moves a referenced key exactly as a plain UPDATE
// does; `INSERT OR REPLACE` deletes conflicting rows outright, and when the
// conflict is on a secondary UNIQUE column the deleted row's primary key can be
// the one children reference.
func TestUpsertAndReplaceRespectForeignKeys(t *testing.T) {
	t.Run("ON CONFLICT DO UPDATE cannot move a referenced key", func(t *testing.T) {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`)
		exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_code TEXT, FOREIGN KEY (parent_code) REFERENCES parent(code))`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (1, 'A')`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (2, 'B')`)
		exec(t, e, `INSERT INTO child (id, parent_code) VALUES (10, 'A')`)

		if _, err := e.Execute(`INSERT INTO parent (id, code) VALUES (1, 'A') ON CONFLICT (id) DO UPDATE SET code = 'Z'`); err == nil {
			t.Error("an upsert moved a referenced parent key, orphaning its child")
		} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			t.Errorf("upsert failed with %v, want a FOREIGN KEY constraint error", err)
		}

		// The same statement against an UNreferenced row must still work —
		// and must actually WRITE. Asserting only "no error" let a mutant
		// that returns (nil, nil) from every upsert pass: the statement
		// silently did nothing and reported success.
		res, err := e.Execute(`INSERT INTO parent (id, code) VALUES (2, 'B') ON CONFLICT (id) DO UPDATE SET code = 'Y'`)
		if err != nil {
			t.Fatalf("an upsert on an unreferenced parent was refused: %v", err)
		}
		if res == nil {
			t.Fatal("the upsert returned a nil Result with a nil error — a caller reading RowsAffected would panic")
		}
		if got := exec(t, e, `SELECT code FROM parent WHERE id = 2`); len(got.Rows) != 1 || cellText(got.Rows[0][0]) != "Y" {
			t.Errorf("after the upsert, parent 2 holds %v, want Y — the statement reported success without writing", got.Rows)
		}
	})

	// The child-side check on the same rewrite path. Its two siblings in this
	// switch arm were tested and this one was not, which is the "twin had four
	// tests" shape: DO UPDATE can point the child's own FK column at nothing.
	t.Run("ON CONFLICT DO UPDATE cannot orphan the row it updates", func(t *testing.T) {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`)
		exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_code TEXT, FOREIGN KEY (parent_code) REFERENCES parent(code))`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (1, 'A')`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (2, 'B')`)
		exec(t, e, `INSERT INTO child (id, parent_code) VALUES (10, 'A')`)

		if _, err := e.Execute(`INSERT INTO child (id, parent_code) VALUES (10, 'A') ON CONFLICT (id) DO UPDATE SET parent_code = 'GHOST'`); err == nil {
			t.Error("an upsert pointed the child's foreign key at a value no parent has")
		} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			t.Errorf("upsert failed with %v, want a FOREIGN KEY constraint error", err)
		}

		// Repointing at a REAL parent must still work, and must land.
		res, err := e.Execute(`INSERT INTO child (id, parent_code) VALUES (10, 'A') ON CONFLICT (id) DO UPDATE SET parent_code = 'B'`)
		if err != nil {
			t.Fatalf("an upsert repointing the child at an existing parent was refused: %v", err)
		}
		if res == nil {
			t.Fatal("the upsert returned a nil Result with a nil error")
		}
		if got := exec(t, e, `SELECT parent_code FROM child WHERE id = 10`); len(got.Rows) != 1 || cellText(got.Rows[0][0]) != "B" {
			t.Errorf("after the upsert, child 10 points at %v, want B — the statement reported success without writing", got.Rows)
		}
	})

	t.Run("INSERT OR REPLACE cannot delete a referenced row", func(t *testing.T) {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`)
		exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES parent(id))`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (1, 'A')`)
		exec(t, e, `INSERT INTO parent (id, code) VALUES (2, 'B')`)
		exec(t, e, `INSERT INTO child (id, parent_id) VALUES (10, 2)`)

		// Conflicts on code='B', which would delete parent id=2 — the row
		// child 10 points at — and insert id=99 in its place.
		if _, err := e.Execute(`INSERT OR REPLACE INTO parent (id, code) VALUES (99, 'B')`); err == nil {
			t.Error("INSERT OR REPLACE deleted a referenced parent row")
		} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			t.Errorf("replace failed with %v, want a FOREIGN KEY constraint error", err)
		}
		// Nothing may have changed.
		res := exec(t, e, `SELECT id FROM parent ORDER BY id`)
		if len(res.Rows) != 2 {
			t.Errorf("after a refused REPLACE the parent table holds %d rows, want 2", len(res.Rows))
		}

		// Replacing an UNreferenced row still works, and must land.
		if _, err := e.Execute(`INSERT OR REPLACE INTO parent (id, code) VALUES (98, 'A')`); err != nil {
			t.Fatalf("REPLACE of an unreferenced row was refused: %v", err)
		}
		if got := exec(t, e, `SELECT id FROM parent WHERE code = 'A'`); len(got.Rows) != 1 || cellText(got.Rows[0][0]) != "98" {
			t.Errorf("after REPLACE, code 'A' belongs to %v, want 98 — the statement reported success without writing", got.Rows)
		}
	})
}

// A FOREIGN KEY whose REFERENCES names a column the parent does not have
// cannot be validated at parse time — the parent table may not exist yet — so
// it has to be caught at DML time. It was not: the delete-side check returned
// nil when the column missed, and the insert-side check silently fell back to
// the parent's primary key. Both directions failed OPEN, which is the wrong
// direction for a typo in a schema.
func TestForeignKeyNamingAnUnknownParentColumnFailsClosed(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY, other TEXT)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(ghost))`)
	exec(t, e, `INSERT INTO p (id, other) VALUES (1, 'x')`)

	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (10, 1)`); err == nil {
		t.Error("an insert against a foreign key naming a nonexistent parent column was accepted")
	} else if !strings.Contains(strings.ToLower(err.Error()), "mismatch") {
		t.Errorf("insert failed with %v, want a foreign key mismatch error", err)
	}

	if _, err := e.Execute(`DELETE FROM p WHERE id = 1`); err == nil {
		t.Error("a parent delete against a foreign key naming a nonexistent parent column was accepted")
	} else if !strings.Contains(strings.ToLower(err.Error()), "mismatch") {
		t.Errorf("delete failed with %v, want a foreign key mismatch error", err)
	}

	// A correctly-named reference is unaffected.
	e2 := newTestEngine(t)
	exec(t, e2, `CREATE TABLE p (id INTEGER PRIMARY KEY, other TEXT)`)
	exec(t, e2, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)
	exec(t, e2, `INSERT INTO p (id, other) VALUES (1, 'x')`)
	exec(t, e2, `INSERT INTO c (id, pid) VALUES (10, 1)`)
}

// SQLite checks immediate foreign keys at STATEMENT end, not per row. When the
// rows doing the referencing are removed by the same statement, there is no
// violation to report. The bare-DELETE check was written per-row, so emptying a
// self-referencing table — something `sqlite3 .dump` output routinely does —
// was refused even though nothing dangles afterwards.
func TestBareDeleteAllowsSelfReferencingRows(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE emp (id INTEGER PRIMARY KEY, boss INTEGER, FOREIGN KEY (boss) REFERENCES emp(id))`)
	exec(t, e, `INSERT INTO emp (id, boss) VALUES (1, NULL)`)
	exec(t, e, `INSERT INTO emp (id, boss) VALUES (2, 1)`)
	exec(t, e, `INSERT INTO emp (id, boss) VALUES (3, 2)`)

	// Every referencing row is deleted by this same statement, so the table is
	// consistent when it ends.
	if _, err := e.Execute(`DELETE FROM emp`); err != nil {
		t.Errorf("emptying a self-referencing table was refused: %v — every referencing row is removed by the same statement", err)
	}
	if res := exec(t, e, `SELECT id FROM emp`); len(res.Rows) != 0 {
		t.Errorf("table not empty after DELETE: %d rows", len(res.Rows))
	}
}

// The relaxation must not reach across tables: a child in a DIFFERENT table
// still dangles when its parent table is emptied.
func TestBareDeleteStillRefusesAnotherTablesChildren(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE parent (id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE child (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES parent(id))`)
	exec(t, e, `INSERT INTO parent (id) VALUES (1)`)
	exec(t, e, `INSERT INTO child (id, pid) VALUES (10, 1)`)

	if _, err := e.Execute(`DELETE FROM parent`); err == nil {
		t.Error("emptied a parent table while another table's rows still reference it")
	}
}

// SQLite applies the parent key's affinity to the child value before comparing,
// and its INTEGER affinity accepts more text shapes than a strict int64 parse:
// a decimal, a leading space, a plus sign. Its NONE affinity converts nothing,
// so text in an untyped or BLOB-declared column stays text. Both gaps made the
// engine refuse rows real SQLite accepts.
//
// Every case here was cross-checked against the shipped modernc driver.
func TestForeignKeyAffinityMatchesRealSQLite(t *testing.T) {
	accepted := []struct{ childType, value, why string }{
		{"TEXT", `'7'`, "plain text against an integer key"},
		{"TEXT", `7`, "integer literal into a text column"},
		{"TEXT", `7.0`, "float literal into a text column"},
		{"TEXT", `' 7'`, "leading space"},
		{"TEXT", `'+7'`, "plus sign"},
		{"TEXT", `'7.0'`, "decimal text"},
		{"BLOB", `'7'`, "text into a BLOB-affinity column: NONE affinity converts nothing"},
		{"", `'7'`, "text into an untyped column"},
	}
	for _, c := range accepted {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE u (id INTEGER PRIMARY KEY, name TEXT)`)
		exec(t, e, `CREATE TABLE p (id TEXT PRIMARY KEY, uid `+c.childType+`, FOREIGN KEY (uid) REFERENCES u(id))`)
		exec(t, e, `INSERT INTO u (id, name) VALUES (7, 'alice')`)
		if _, err := e.Execute(`INSERT INTO p (id, uid) VALUES ('x', ` + c.value + `)`); err != nil {
			t.Errorf("child %q value %s refused (%s): %v", c.childType, c.value, c.why, err)
		}
	}

	// And the refusals that must survive, or the relaxation is just leniency.
	refused := []struct{ setup, insert, why string }{
		{`CREATE TABLE u (id INTEGER PRIMARY KEY)`, `'8'`, "no parent row with that value"},
		{`CREATE TABLE u (id INTEGER PRIMARY KEY)`, `'abc'`, "non-numeric text"},
		{`CREATE TABLE u (id INTEGER PRIMARY KEY)`, `''`, "empty string is not 0"},
		{`CREATE TABLE u (id INTEGER PRIMARY KEY)`, `-7`, "wrong sign"},
	}
	for _, c := range refused {
		e := newTestEngine(t)
		exec(t, e, c.setup)
		exec(t, e, `CREATE TABLE p (id TEXT PRIMARY KEY, uid TEXT, FOREIGN KEY (uid) REFERENCES u(id))`)
		exec(t, e, `INSERT INTO u (id) VALUES (7)`)
		if _, err := e.Execute(`INSERT INTO p (id, uid) VALUES ('x', ` + c.insert + `)`); err == nil {
			t.Errorf("child value %s was accepted (%s) — the comparison is too loose", c.insert, c.why)
		}
	}

	// A TEXT parent key must compare as TEXT: '007' and 7 are different keys,
	// and a numeric fallback that ignored the parent's type would merge them.
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE u (code TEXT PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE p (id TEXT PRIMARY KEY, ucode TEXT, FOREIGN KEY (ucode) REFERENCES u(code))`)
	exec(t, e, `INSERT INTO u (code) VALUES ('007')`)
	if _, err := e.Execute(`INSERT INTO p (id, ucode) VALUES ('x', 7)`); err == nil {
		t.Error("integer 7 matched a TEXT parent key '007' — a numeric fallback must not apply when the parent key is text")
	}

	// The one arm that isolates the affinity COERCION from the numeric
	// fallback. The child column is UNTYPED, so NONE affinity leaves the
	// integer as an integer — verified against the shipped driver, which
	// stores typeof(ucode) = integer and accepts the reference. The parent key
	// is TEXT '7', so the comparison is the first and only place the parent's
	// affinity can apply: coercion turns the child 7 into '7' and it matches,
	// while the numeric fallback cannot fire because the parent value is text.
	//
	// A TEXT child column would NOT discriminate: storage affinity converts
	// the integer to text on insert, so the coercion has nothing left to do
	// and skipping it still passes.
	e2 := newTestEngine(t)
	exec(t, e2, `CREATE TABLE u (code TEXT PRIMARY KEY)`)
	exec(t, e2, `CREATE TABLE p (id TEXT PRIMARY KEY, ucode, FOREIGN KEY (ucode) REFERENCES u(code))`)
	exec(t, e2, `INSERT INTO u (code) VALUES ('7')`)
	if _, err := e2.Execute(`INSERT INTO p (id, ucode) VALUES ('x', 7)`); err != nil {
		t.Errorf("integer child 7 did not match TEXT parent key '7': %v — the parent's TEXT affinity must be applied to the child value", err)
	}
}

// cellText renders a result cell for comparison, so an allow-arm assertion can
// check what a statement actually wrote rather than only that it returned no
// error.
func cellText(v Value) string {
	switch v.Type {
	case DataTypeText:
		return v.TextVal
	case DataTypeInteger:
		return formatInt64(v.IntVal)
	case DataTypeFloat:
		return formatFloat64(v.FloatVal)
	}
	return ""
}

// SQLite performs an implicit DELETE FROM when a table is dropped, so dropping
// a parent whose rows are still referenced fails. The engine dropped it and
// left the children pointing at a table that no longer exists — permanently,
// since nothing revisits them. Enforcement was wired one statement at a time
// and DROP was never one of the statements anyone reported.
func TestDropTableRefusesAReferencedParent(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)
	exec(t, e, `INSERT INTO p (id) VALUES (1)`)
	exec(t, e, `INSERT INTO c (id, pid) VALUES (10, 1)`)

	if _, err := e.Execute(`DROP TABLE p`); err == nil {
		t.Error("dropped a parent table whose rows are still referenced — the children dangle forever")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("drop failed with %v, want a FOREIGN KEY constraint error", err)
	}

	// Once the children are gone the drop is fine, or this is a blanket refusal.
	exec(t, e, `DELETE FROM c`)
	if _, err := e.Execute(`DROP TABLE p`); err != nil {
		t.Errorf("dropping an unreferenced parent was refused: %v", err)
	}

	// And a table nobody references drops freely.
	exec(t, e, `CREATE TABLE lone (id INTEGER PRIMARY KEY)`)
	if _, err := e.Execute(`DROP TABLE lone`); err != nil {
		t.Errorf("dropping an unreferenced table was refused: %v", err)
	}

	// A SELF-referencing table drops freely: its own rows go with it, so no
	// reference can survive. Without the self-skip in the reference scan, the
	// table would count as "referenced" by itself and never be droppable.
	e2 := newTestEngine(t)
	exec(t, e2, `CREATE TABLE emp (id INTEGER PRIMARY KEY, boss INTEGER, FOREIGN KEY (boss) REFERENCES emp(id))`)
	exec(t, e2, `INSERT INTO emp (id, boss) VALUES (1, NULL)`)
	exec(t, e2, `INSERT INTO emp (id, boss) VALUES (2, 1)`)
	if _, err := e2.Execute(`DROP TABLE emp`); err != nil {
		t.Errorf("dropping a self-referencing table was refused: %v — its own rows go with it", err)
	}

	// But a self-reference must not excuse ANOTHER table's children.
	e3 := newTestEngine(t)
	exec(t, e3, `CREATE TABLE node (id INTEGER PRIMARY KEY, parent INTEGER, FOREIGN KEY (parent) REFERENCES node(id))`)
	exec(t, e3, `CREATE TABLE tag (id INTEGER PRIMARY KEY, nid INTEGER, FOREIGN KEY (nid) REFERENCES node(id))`)
	exec(t, e3, `INSERT INTO node (id, parent) VALUES (1, NULL)`)
	exec(t, e3, `INSERT INTO tag (id, nid) VALUES (10, 1)`)
	if _, err := e3.Execute(`DROP TABLE node`); err == nil {
		t.Error("dropped a self-referencing table whose rows ANOTHER table still references")
	}

	// DROP TABLE IF EXISTS on a table that is not there must stay a no-op:
	// the reference check runs only for a table that exists, and reaching it
	// with no table would scan nothing at best and panic at worst.
	if _, err := e.Execute(`DROP TABLE IF EXISTS never_existed`); err != nil {
		t.Errorf("DROP TABLE IF EXISTS on a missing table returned %v, want a silent no-op", err)
	}
}

// The parser reads `REFERENCES` on an added column; the ALTER path then read
// only NOT NULL and DEFAULT off the new column and dropped the foreign key on
// the floor. The constraint existed in the schema and enforced nothing.
func TestAlterAddColumnCarriesItsForeignKey(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY)`)
	exec(t, e, `INSERT INTO p (id) VALUES (1)`)
	exec(t, e, `ALTER TABLE c ADD COLUMN pid INTEGER REFERENCES p(id)`)

	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (10, 999)`); err == nil {
		t.Error("an added REFERENCES column accepted a value naming no parent row — the constraint was parsed and discarded")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("insert failed with %v, want a FOREIGN KEY constraint error", err)
	}
	// A real parent still works, and NULL still passes.
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (11, 1)`); err != nil {
		t.Errorf("a valid reference through an added column was refused: %v", err)
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (12, NULL)`); err != nil {
		t.Errorf("a NULL in an added REFERENCES column was refused: %v", err)
	}
	// And the parent side sees it too.
	if _, err := e.Execute(`DELETE FROM p WHERE id = 1`); err == nil {
		t.Error("deleted a parent still referenced through an added column")
	}
}

// PRAGMA foreign_keys reported 0 on a connection that refused every dangling
// write, and its SET form was a silent no-op. Both halves lie: code that gates
// on the pragma reads "off" from an engine enforcing unconditionally, and code
// that turns enforcement off gets no error and no effect.
func TestPragmaForeignKeysReportsAndControlsEnforcement(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)

	// Default: on, and it says so.
	if res := exec(t, e, `PRAGMA foreign_keys`); len(res.Rows) != 1 || cellText(res.Rows[0][0]) != "1" {
		t.Fatalf("PRAGMA foreign_keys = %v, want 1 — the engine enforces, so reporting 0 contradicts every write", res.Rows)
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (1, 999)`); err == nil {
		t.Error("a dangling insert was accepted while the pragma reports 1")
	}

	// Turning it off must actually turn it off.
	exec(t, e, `PRAGMA foreign_keys = OFF`)
	if res := exec(t, e, `PRAGMA foreign_keys`); len(res.Rows) != 1 || cellText(res.Rows[0][0]) != "0" {
		t.Fatalf("after PRAGMA foreign_keys = OFF the pragma reads %v, want 0", res.Rows)
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (2, 999)`); err != nil {
		t.Errorf("enforcement is still on after PRAGMA foreign_keys = OFF: %v", err)
	}

	// And back on.
	exec(t, e, `PRAGMA foreign_keys = ON`)
	if res := exec(t, e, `PRAGMA foreign_keys`); len(res.Rows) != 1 || cellText(res.Rows[0][0]) != "1" {
		t.Fatalf("after PRAGMA foreign_keys = ON the pragma reads %v, want 1", res.Rows)
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (3, 999)`); err == nil {
		t.Error("enforcement did not resume after PRAGMA foreign_keys = ON")
	}
	// The parent side follows the same switch.
	exec(t, e, `INSERT INTO p (id) VALUES (1)`)
	exec(t, e, `INSERT INTO c (id, pid) VALUES (4, 1)`)
	if _, err := e.Execute(`DELETE FROM p WHERE id = 1`); err == nil {
		t.Error("a referenced parent was deletable while the pragma reports 1")
	}
	exec(t, e, `PRAGMA foreign_keys = OFF`)
	if _, err := e.Execute(`DELETE FROM p WHERE id = 1`); err != nil {
		t.Errorf("the parent side still enforces after the pragma was turned off: %v", err)
	}

	// The parent-UPDATE check needs its own pragma-off arm: it is a separate
	// guard from the delete side, and a mutation removing its pragma check
	// survived the whole suite because only the delete side was covered.
	e2 := newTestEngine(t)
	exec(t, e2, `CREATE TABLE p2 (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`)
	exec(t, e2, `CREATE TABLE c2 (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p2(code))`)
	exec(t, e2, `INSERT INTO p2 (id, code) VALUES (1, 'A')`)
	exec(t, e2, `INSERT INTO c2 (id, pcode) VALUES (10, 'A')`)
	if _, err := e2.Execute(`UPDATE p2 SET code = 'Z' WHERE id = 1`); err == nil {
		t.Fatal("moving a referenced key was allowed while the pragma reports 1")
	}
	exec(t, e2, `PRAGMA foreign_keys = OFF`)
	if _, err := e2.Execute(`UPDATE p2 SET code = 'Z' WHERE id = 1`); err != nil {
		t.Errorf("the parent-UPDATE check still enforces after the pragma was turned off: %v", err)
	}
}

// Renaming a table or a referenced column left every child's foreign key
// pointing at the old name, so a rename SQLite treats as routine made the
// schema permanently unwritable in the child direction. Fail-closed, but the
// failure is unrecoverable without hand-editing the schema.
func TestRenameRewritesChildForeignKeys(t *testing.T) {
	t.Run("renaming the parent table", func(t *testing.T) {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
		exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)
		exec(t, e, `INSERT INTO p (id) VALUES (1)`)
		exec(t, e, `ALTER TABLE p RENAME TO p2`)

		if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (10, 1)`); err != nil {
			t.Errorf("a valid child insert failed after the parent was renamed: %v — the FK still names the old table", err)
		}
		// Enforcement must follow the rename, not merely stop failing.
		if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (11, 999)`); err == nil {
			t.Error("a dangling child insert was accepted after the rename — enforcement was lost, not moved")
		}
		if _, err := e.Execute(`DELETE FROM p2 WHERE id = 1`); err == nil {
			t.Error("the renamed parent's referenced row was deletable")
		}
	})

	t.Run("renaming the referenced column", func(t *testing.T) {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`)
		exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`)
		exec(t, e, `INSERT INTO p (id, code) VALUES (1, 'A')`)
		exec(t, e, `ALTER TABLE p RENAME COLUMN code TO code2`)

		if _, err := e.Execute(`INSERT INTO c (id, pcode) VALUES (10, 'A')`); err != nil {
			t.Errorf("a valid child insert failed after the referenced column was renamed: %v", err)
		}
		if _, err := e.Execute(`INSERT INTO c (id, pcode) VALUES (11, 'ZZ')`); err == nil {
			t.Error("a dangling child insert was accepted after the column rename — enforcement was lost")
		}
	})
}

// `DELETE FROM t` and `DELETE FROM t WHERE 1=1` do the same thing and must give
// the same answer. The bare form got the statement-end rule — a reference from
// a row this same statement removes cannot dangle afterwards — and the WHERE
// form did not, so emptying a self-referencing table succeeded one way and
// failed the other. SQLite accepts both.
func TestSelfReferentialDeleteAgreesWithAndWithoutWhere(t *testing.T) {
	build := func(t *testing.T) *Engine {
		e := newTestEngine(t)
		exec(t, e, `CREATE TABLE emp (id INTEGER PRIMARY KEY, boss INTEGER, FOREIGN KEY (boss) REFERENCES emp(id))`)
		exec(t, e, `INSERT INTO emp (id, boss) VALUES (1, NULL)`)
		exec(t, e, `INSERT INTO emp (id, boss) VALUES (2, 1)`)
		exec(t, e, `INSERT INTO emp (id, boss) VALUES (3, 2)`)
		return e
	}

	// Baseline: the bare form is already allowed.
	if _, err := build(t).Execute(`DELETE FROM emp`); err != nil {
		t.Fatalf("bare DELETE was refused: %v — this test's premise is wrong", err)
	}
	// The same statement written with a tautological WHERE.
	if _, err := build(t).Execute(`DELETE FROM emp WHERE 1=1`); err != nil {
		t.Errorf("DELETE ... WHERE 1=1 was refused while bare DELETE is allowed: %v — the same operation gives two answers", err)
	}
	// And a WHERE that removes every referencing row along with its referent.
	if _, err := build(t).Execute(`DELETE FROM emp WHERE id IN (2, 3)`); err != nil {
		t.Errorf("deleting a referencing row together with its referent was refused: %v", err)
	}

	// The rule must stay narrow: a row that SURVIVES the statement and still
	// references a deleted one is a real violation.
	e := build(t)
	if _, err := e.Execute(`DELETE FROM emp WHERE id = 1`); err == nil {
		t.Error("deleted a row that a SURVIVING row still references")
	}
	// And another table's children are still protected.
	e2 := newTestEngine(t)
	exec(t, e2, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	exec(t, e2, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)
	exec(t, e2, `INSERT INTO p (id) VALUES (1)`)
	exec(t, e2, `INSERT INTO c (id, pid) VALUES (10, 1)`)
	if _, err := e2.Execute(`DELETE FROM p WHERE 1=1`); err == nil {
		t.Error("emptied a parent table while another table's rows still reference it")
	}

	// The doomed-row escape is keyed on rowid, so it MUST apply only to the
	// table being deleted from. Here the surviving child's rowid (7) is also a
	// rowid being deleted in the parent — if the escape were applied to every
	// referencing table, that collision alone would excuse a real violation.
	e3 := newTestEngine(t)
	exec(t, e3, `CREATE TABLE pp (id INTEGER PRIMARY KEY)`)
	exec(t, e3, `CREATE TABLE cc (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES pp(id))`)
	exec(t, e3, `INSERT INTO pp (id) VALUES (7)`)
	exec(t, e3, `INSERT INTO cc (id, pid) VALUES (7, 7)`)
	if _, err := e3.Execute(`DELETE FROM pp WHERE id = 7`); err == nil {
		t.Error("a child row was excused because its rowid matched a deleted parent's rowid — the same-statement escape must be scoped to one table")
	}
}

// SQLite makes `PRAGMA foreign_keys` a no-op inside a transaction, precisely so
// enforcement cannot be turned off in a way that outlives the transaction that
// did it. Making the setter work — which fixed a real lie, the pragma reporting
// a state it did not have — opened this: OFF inside a transaction took effect,
// survived ROLLBACK, and left the shared engine accepting dangling writes with
// no error anywhere.
func TestPragmaForeignKeysIsANoOpInsideATransaction(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`)

	exec(t, e, `BEGIN`)
	exec(t, e, `PRAGMA foreign_keys = OFF`)
	if res := exec(t, e, `PRAGMA foreign_keys`); cellText(res.Rows[0][0]) != "1" {
		t.Errorf("the pragma changed inside a transaction (reads %v) — SQLite makes this a no-op", res.Rows[0][0])
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (1, 999)`); err == nil {
		t.Error("a dangling insert was accepted inside a transaction after PRAGMA OFF")
	}
	exec(t, e, `ROLLBACK`)

	if res := exec(t, e, `PRAGMA foreign_keys`); cellText(res.Rows[0][0]) != "1" {
		t.Fatalf("enforcement is off after ROLLBACK (reads %v) — a rolled-back statement changed durable state", res.Rows[0][0])
	}
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (2, 999)`); err == nil {
		t.Error("a dangling insert was accepted after ROLLBACK — the pragma escaped the transaction that set it")
	}

	// COMMIT must not launder it either.
	exec(t, e, `BEGIN`)
	exec(t, e, `PRAGMA foreign_keys = OFF`)
	exec(t, e, `COMMIT`)
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (3, 999)`); err == nil {
		t.Error("a dangling insert was accepted after COMMIT of a transaction containing PRAGMA OFF")
	}

	// Outside a transaction the setter still works, or the fix is a blanket
	// refusal rather than SQLite's rule.
	exec(t, e, `PRAGMA foreign_keys = OFF`)
	if _, err := e.Execute(`INSERT INTO c (id, pid) VALUES (4, 999)`); err != nil {
		t.Errorf("the setter stopped working outside a transaction: %v", err)
	}
}

// A `REFERENCES p` with no column list targets the parent's primary key. Every
// check defaulted the referenced column index to 0 — the FIRST column — so a
// table whose primary key is not first enforced against the wrong column: valid
// child rows were refused, and the parent side let referenced rows be deleted
// and updated freely.
func TestBareReferencesTargetsThePrimaryKey(t *testing.T) {
	e := newTestEngine(t)
	// `id` is deliberately NOT the first column.
	exec(t, e, `CREATE TABLE p (name TEXT, id INTEGER PRIMARY KEY)`)
	exec(t, e, `CREATE TABLE c (cid INTEGER PRIMARY KEY, pid INTEGER REFERENCES p)`)
	exec(t, e, `INSERT INTO p (name, id) VALUES ('alice', 5)`)

	if _, err := e.Execute(`INSERT INTO c (cid, pid) VALUES (1, 5)`); err != nil {
		t.Errorf("a child referencing the parent's primary key was refused: %v — the check used column 0 (name) instead", err)
	}
	if _, err := e.Execute(`INSERT INTO c (cid, pid) VALUES (2, 999)`); err == nil {
		t.Error("a child naming no parent row was accepted")
	}
	// Parent side: both directions must see the same column.
	if _, err := e.Execute(`DELETE FROM p WHERE id = 5`); err == nil {
		t.Error("deleted a parent row a child still references through a bare REFERENCES")
	}
	if _, err := e.Execute(`UPDATE p SET id = 6 WHERE id = 5`); err == nil {
		t.Error("moved a referenced primary key out from under a child through a bare REFERENCES")
	}
}
