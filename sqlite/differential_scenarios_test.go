package sqlite

// The scenario table for the differential harness (differential_test.go).
//
// Every scenario is a script both engines execute statement for statement.
// Adding a case costs one entry; that cheapness is the point, since the
// failures this catches are the ones nobody thought to look for.

var fkDiffScenarios = []diffScenario{
	// ---- the child side -------------------------------------------------
	{
		name: "child insert without a parent is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`},
	},
	{
		name: "a NULL child key references nothing and is accepted",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO c (id, pid) VALUES (10, NULL)`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},
	{
		name: "updating a child onto a missing parent is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`UPDATE c SET pid = 999 WHERE id = 10`,
			`UPDATE c SET pid = NULL WHERE id = 10`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},

	// ---- the parent side ------------------------------------------------
	{
		name: "deleting a referenced parent is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1), (2)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`DELETE FROM p WHERE id = 1`,
			`DELETE FROM p WHERE id = 2`,
		},
		probes: []string{`SELECT id FROM p ORDER BY id`},
	},
	{
		name: "a bare DELETE that would orphan anything mutates nothing",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1), (2), (3)`,
			`INSERT INTO c (id, pid) VALUES (10, 3)`,
			`DELETE FROM p`,
		},
		probes: []string{`SELECT id FROM p ORDER BY id`, `SELECT id, pid FROM c`},
	},
	{
		name: "moving a referenced parent key is refused, moving anything else is not",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`UPDATE p SET name = 'renamed' WHERE id = 1`,
			`UPDATE p SET id = 7 WHERE id = 1`,
		},
		probes: []string{`SELECT id, name FROM p`},
	},
	{
		name: "dropping a referenced parent table is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`DROP TABLE p`,
		},
		probes: []string{`SELECT id FROM p`, `SELECT id, pid FROM c`},
	},
	{
		name: "dropping an unreferenced parent table is accepted",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`DROP TABLE p`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},
	{
		name: "dropping the child first frees the parent",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`DROP TABLE c`,
			`DROP TABLE p`,
		},
	},

	// ---- statement-end semantics ---------------------------------------
	{
		name: "clearing the children then the parents is accepted",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1), (2)`,
			`INSERT INTO c (id, pid) VALUES (10, 1), (11, 2)`,
			`DELETE FROM c`,
			`DELETE FROM p`,
		},
		probes: []string{`SELECT id FROM p`, `SELECT id FROM c`},
	},
	{
		name: "a self-reference does not block deleting its own row",
		steps: []string{
			`CREATE TABLE n (id INTEGER PRIMARY KEY, parent INTEGER, FOREIGN KEY (parent) REFERENCES n(id))`,
			`INSERT INTO n (id, parent) VALUES (1, NULL)`,
			`INSERT INTO n (id, parent) VALUES (2, 1)`,
			`DELETE FROM n WHERE id = 2`,
			`DELETE FROM n WHERE id = 1`,
		},
		probes: []string{`SELECT id, parent FROM n ORDER BY id`},
	},
	{
		name: "clearing a self-referencing table wholesale is accepted",
		steps: []string{
			`CREATE TABLE n (id INTEGER PRIMARY KEY, parent INTEGER, FOREIGN KEY (parent) REFERENCES n(id))`,
			`INSERT INTO n (id, parent) VALUES (1, NULL)`,
			`INSERT INTO n (id, parent) VALUES (2, 1)`,
			`DELETE FROM n`,
		},
		probes: []string{`SELECT id FROM n`},
	},

	// ---- the write paths that are not INSERT/UPDATE/DELETE ---------------
	{
		name: "REPLACE that would delete a referenced parent is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT OR REPLACE INTO p (id, name) VALUES (1, 'replaced')`,
		},
		probes: []string{`SELECT id, name FROM p`, `SELECT id, pid FROM c`},
	},
	{
		name: "REPLACE of a child still checks its own parent",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT OR REPLACE INTO c (id, pid) VALUES (10, 1)`,
			`INSERT OR REPLACE INTO c (id, pid) VALUES (10, 999)`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},
	{
		name: "upsert DO UPDATE that moves a referenced key is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO p (id, name) VALUES (1, 'x') ON CONFLICT (id) DO UPDATE SET id = 7`,
			`INSERT INTO p (id, name) VALUES (1, 'x') ON CONFLICT (id) DO UPDATE SET name = 'touched'`,
		},
		probes: []string{`SELECT id, name FROM p ORDER BY id`},
	},
	{
		name: "INSERT ... SELECT is checked row by row",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE src (pid INTEGER)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO src (pid) VALUES (1), (999)`,
			`INSERT INTO c (pid) SELECT pid FROM src`,
		},
		probes: []string{`SELECT pid FROM c ORDER BY pid`},
	},

	// ---- schema changes carry their constraints -------------------------
	{
		name: "ALTER TABLE ADD COLUMN carries its REFERENCES",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY)`,
			`INSERT INTO p (id) VALUES (1)`,
			`ALTER TABLE c ADD COLUMN pid INTEGER REFERENCES p(id)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`, `SELECT id FROM p`},
	},
	{
		name: "renaming the parent table keeps the children enforced",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`ALTER TABLE p RENAME TO p2`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
			`DELETE FROM p2 WHERE id = 1`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`, `SELECT id FROM p2`},
	},
	{
		name: "renaming the referenced column keeps the children enforced",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`ALTER TABLE p RENAME COLUMN code TO code2`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (11, 'zzz')`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`},
	},

	// ---- the pragma -----------------------------------------------------
	{
		// The one divergence in this file that is chosen. modernc scopes
		// `PRAGMA foreign_keys` to the connection; the in-house driver builds
		// one engine per sql.Open and shares it across the pool, so the same
		// statement would disable enforcement for every other connection —
		// silently, and for a cross-check engine whose entire job is to keep
		// checking. It therefore accepts the ON direction and refuses the OFF
		// direction rather than honouring a scope it cannot provide. A test
		// that genuinely needs enforcement off drives the Engine directly,
		// where there is exactly one owner and no side channel.
		name: "turning enforcement off is refused on a shared pool",
		wantDiff: "PRAGMA foreign_keys is pool-wide in the in-house driver, so " +
			"the OFF direction is refused instead of silently disabling every connection",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`PRAGMA foreign_keys = OFF`,
			`INSERT INTO c (id, pid) VALUES (10, 999)`,
			`PRAGMA foreign_keys = ON`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
		},
		probes: []string{`PRAGMA foreign_keys`, `SELECT id, pid FROM c ORDER BY id`},
	},
	{
		name: "turning enforcement on is accepted and changes nothing",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`PRAGMA foreign_keys = ON`,
			`INSERT INTO c (id, pid) VALUES (10, 999)`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (11, 1)`,
		},
		probes: []string{`PRAGMA foreign_keys`, `SELECT id, pid FROM c`},
	},
	{
		name: "the pragma is a no-op inside a transaction",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`BEGIN`,
			`PRAGMA foreign_keys = OFF`,
			`ROLLBACK`,
			`INSERT INTO c (id, pid) VALUES (10, 999)`,
		},
		probes: []string{`PRAGMA foreign_keys`, `SELECT id, pid FROM c`},
	},

	// ---- key shapes -----------------------------------------------------
	{
		name: "a bare REFERENCES targets the primary key, not the first column",
		steps: []string{
			`CREATE TABLE p (name TEXT, id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p)`,
			`INSERT INTO p (name, id) VALUES ('one', 5)`,
			`INSERT INTO c (id, pid) VALUES (10, 5)`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
			`DELETE FROM p WHERE id = 5`,
			`UPDATE p SET id = 6 WHERE id = 5`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`, `SELECT name, id FROM p`},
	},
	{
		name: "a TEXT parent key does not merge with its numeric spelling",
		steps: []string{
			`CREATE TABLE p (code TEXT PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (code) VALUES ('007')`,
			`INSERT INTO c (id, pcode) VALUES (10, 7)`,
			`INSERT INTO c (id, pcode) VALUES (11, '007')`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`},
	},
	{
		name: "the parent key's affinity is applied to the child value",
		steps: []string{
			`CREATE TABLE p (code TEXT PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (code) VALUES ('7')`,
			`INSERT INTO c (id, pcode) VALUES (10, 7)`,
		},
		probes: []string{`SELECT id, pcode FROM c`},
	},
	{
		name: "a UNIQUE parent column is a legal foreign-key target",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (11, 'zzz')`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`},
	},

	// ---- declarations SQLite refuses to process at all --------------------
	//
	// "foreign key mismatch" is a different failure from "constraint failed":
	// the key cannot be evaluated, so every statement that would have to
	// evaluate it fails, whether or not any row actually violates anything.
	// An engine that instead enforces such a key against the wrong column is
	// not being lenient — it is answering a question SQLite declines to ask.
	{
		name: "a non-unique parent column is not a legal target",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (11, NULL)`,
			`DELETE FROM p WHERE id = 1`,
			`UPDATE p SET code = 'b' WHERE id = 1`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`, `SELECT id, code FROM p`},
	},
	{
		name: "a partial unique index does not make a column a legal target",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
			`CREATE UNIQUE INDEX p_code ON p (code) WHERE code IS NOT NULL`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
		},
		probes: []string{`SELECT id, pcode FROM c`},
	},
	{
		name: "a full unique index does make a column a legal target",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
			`CREATE UNIQUE INDEX p_code ON p (code)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (11, 'zzz')`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`, `SELECT id FROM p`},
	},
	{
		name: "a bare REFERENCES to a table with no primary key is a mismatch",
		steps: []string{
			`CREATE TABLE p (a TEXT, b TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pa TEXT REFERENCES p)`,
			`INSERT INTO p (a, b) VALUES ('x', 'y')`,
			`INSERT INTO c (id, pa) VALUES (10, 'x')`,
			`DELETE FROM p`,
		},
		probes: []string{`SELECT id, pa FROM c`},
	},
	{
		name: "a REFERENCES naming a column the parent does not have is a mismatch",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(ghost))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},
	{
		name: "a REFERENCES to a table that does not exist is a mismatch",
		steps: []string{
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES ghost(id))`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO c (id, pid) VALUES (11, NULL)`,
		},
		probes: []string{`SELECT id, pid FROM c`},
	},
	{
		name: "a mismatched key is only raised when the key is used",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`SELECT * FROM c`,
		},
		probes: []string{`SELECT id, code FROM p`},
	},

	// ---- REPLACE and the statement-end rule ------------------------------
	{
		name: "REPLACE that keeps the referenced key is accepted",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT OR REPLACE INTO p (id, name) VALUES (1, 'replaced')`,
		},
		probes: []string{`SELECT id, name FROM p`, `SELECT id, pid FROM c`},
	},
	{
		name: "REPLACE that moves the referenced key away is refused",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT OR REPLACE INTO p (id, code) VALUES (2, 'a')`,
		},
		probes: []string{`SELECT id, code FROM p ORDER BY id`, `SELECT id, pid FROM c`},
	},

	// ---- transactions ---------------------------------------------------
	{
		name: "a refused statement leaves an open transaction usable",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`BEGIN`,
			`INSERT INTO c (id, pid) VALUES (10, 999)`,
			`INSERT INTO c (id, pid) VALUES (11, 1)`,
			`COMMIT`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`},
	},
	{
		name: "rolling back a refused parent delete restores nothing extra",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1), (2)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`BEGIN`,
			`DELETE FROM p WHERE id = 2`,
			`DELETE FROM p WHERE id = 1`,
			`ROLLBACK`,
		},
		probes: []string{`SELECT id FROM p ORDER BY id`},
	},
	{
		name: "deleting a parent and re-inserting it in one transaction",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`BEGIN`,
			`DELETE FROM p WHERE id = 1`,
			`INSERT INTO p (id) VALUES (1)`,
			`COMMIT`,
		},
		probes: []string{`SELECT id FROM p`, `SELECT id, pid FROM c`},
	},

	// ---- conflict clauses do not suppress a foreign key -------------------
	{
		name: "INSERT OR IGNORE does not silently drop a foreign-key violation",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT OR IGNORE INTO c (id, pid) VALUES (10, 999)`,
			`INSERT OR IGNORE INTO c (id, pid) VALUES (11, 1)`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`},
	},
	{
		name: "upsert DO NOTHING on a parent leaves its children alone",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`INSERT INTO p (id, name) VALUES (1, 'x') ON CONFLICT (id) DO NOTHING`,
		},
		probes: []string{`SELECT id, name FROM p`, `SELECT id, pid FROM c`},
	},

	// ---- several children, several keys -----------------------------------
	{
		name: "two child tables both hold the parent",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c1 (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`CREATE TABLE c2 (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c2 (id, pid) VALUES (20, 1)`,
			`DELETE FROM c1`,
			`DELETE FROM p WHERE id = 1`,
			`DELETE FROM c2`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id FROM p`},
	},
	{
		name: "one child row with two foreign keys",
		steps: []string{
			`CREATE TABLE a (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE b (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, aid INTEGER, bid INTEGER, FOREIGN KEY (aid) REFERENCES a(id), FOREIGN KEY (bid) REFERENCES b(id))`,
			`INSERT INTO a (id) VALUES (1)`,
			`INSERT INTO b (id) VALUES (2)`,
			`INSERT INTO c (id, aid, bid) VALUES (10, 1, 2)`,
			`INSERT INTO c (id, aid, bid) VALUES (11, 1, 999)`,
			`DELETE FROM a WHERE id = 1`,
			`DELETE FROM b WHERE id = 2`,
		},
		probes: []string{`SELECT id, aid, bid FROM c`},
	},

	// ---- divergences that are chosen, and must stay observable -------------
	{
		// The engine's foreign-key metadata holds a single column, so a
		// composite key cannot be represented at all. Storing it partially
		// would enforce half a constraint, which is worse than refusing the
		// schema outright.
		name:     "a composite foreign key is refused at declaration",
		wantDiff: "the engine's foreign-key metadata holds one column, so a composite key is refused rather than half-enforced",
		steps: []string{
			`CREATE TABLE p (a INTEGER, b INTEGER, PRIMARY KEY (a, b))`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pa INTEGER, pb INTEGER, FOREIGN KEY (pa, pb) REFERENCES p(a, b))`,
			`INSERT INTO p (a, b) VALUES (1, 2)`,
			`INSERT INTO c (id, pa, pb) VALUES (10, 1, 2)`,
		},
		probes: []string{`SELECT id FROM c`},
	},
	{
		// ON DELETE CASCADE parses — refusing it would make an ORM-generated
		// or `sqlite3 .dump` schema unopenable — but nothing cascades. The
		// engine refuses the delete real SQLite would propagate, which is
		// stricter, never looser.
		name:     "ON DELETE CASCADE is parsed and not performed",
		wantDiff: "referential actions parse but do not run; the engine refuses the delete SQLite would cascade",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p(id) ON DELETE CASCADE)`,
			`INSERT INTO p (id) VALUES (1)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id FROM p`, `SELECT id, pid FROM c`},
	},

	// ---- more schema motion ----------------------------------------------
	{
		name: "renaming the child table keeps its own key enforced",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`ALTER TABLE c RENAME TO c2`,
			`INSERT INTO c2 (id, pid) VALUES (10, 1)`,
			`INSERT INTO c2 (id, pid) VALUES (11, 999)`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, pid FROM c2 ORDER BY id`, `SELECT id FROM p`},
	},
	{
		name: "renaming the child key column keeps it enforced",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER, FOREIGN KEY (pid) REFERENCES p(id))`,
			`INSERT INTO p (id) VALUES (1)`,
			`ALTER TABLE c RENAME COLUMN pid TO parent_id`,
			`INSERT INTO c (id, parent_id) VALUES (10, 1)`,
			`INSERT INTO c (id, parent_id) VALUES (11, 999)`,
			`DELETE FROM p WHERE id = 1`,
		},
		probes: []string{`SELECT id, parent_id FROM c ORDER BY id`, `SELECT id FROM p`},
	},
	{
		name: "ADD COLUMN with a bare REFERENCES targets the primary key",
		steps: []string{
			`CREATE TABLE p (name TEXT, id INTEGER PRIMARY KEY)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY)`,
			`INSERT INTO p (name, id) VALUES ('one', 5)`,
			`ALTER TABLE c ADD COLUMN pid INTEGER REFERENCES p`,
			`INSERT INTO c (id, pid) VALUES (10, 5)`,
			`INSERT INTO c (id, pid) VALUES (11, 999)`,
		},
		probes: []string{`SELECT id, pid FROM c ORDER BY id`},
	},
	{
		name: "a key added by ADD COLUMN is enforced on the parent side too",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY)`,
			`INSERT INTO p (id, name) VALUES (1, 'one')`,
			`ALTER TABLE c ADD COLUMN pid INTEGER REFERENCES p(id)`,
			`INSERT INTO c (id, pid) VALUES (10, 1)`,
			`UPDATE p SET id = 7 WHERE id = 1`,
			`DROP TABLE p`,
		},
		probes: []string{`SELECT id FROM p`, `SELECT id, pid FROM c`},
	},
	{
		name: "a unique index dropped after the key was declared unmakes the target",
		steps: []string{
			`CREATE TABLE p (id INTEGER PRIMARY KEY, code TEXT)`,
			`CREATE UNIQUE INDEX p_code ON p (code)`,
			`CREATE TABLE c (id INTEGER PRIMARY KEY, pcode TEXT, FOREIGN KEY (pcode) REFERENCES p(code))`,
			`INSERT INTO p (id, code) VALUES (1, 'a')`,
			`INSERT INTO c (id, pcode) VALUES (10, 'a')`,
			`DROP INDEX p_code`,
			`INSERT INTO c (id, pcode) VALUES (11, 'a')`,
		},
		probes: []string{`SELECT id, pcode FROM c ORDER BY id`},
	},
}
