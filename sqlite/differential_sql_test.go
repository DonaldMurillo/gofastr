package sqlite

import "testing"

// The foreign-key scenarios have a companion here: the same harness aimed at
// the SQL the framework actually emits. A cross-check engine is only worth
// running if the two engines agree about ordinary queries, and "ordinary" is
// exactly where a divergence hides longest — nobody writes a test for whether
// ORDER BY puts NULLs first, they just rely on it.

func TestEnginesAgreeOnQuerySemantics(t *testing.T) {
	for _, sc := range sqlDiffScenarios {
		t.Run(sc.name, func(t *testing.T) {
			house := runDiff(t, openDiffDB(t, "gofastr-sqlite"), sc)
			real := runDiff(t, openDiffDB(t, "sqlite3"), sc)
			assertScenarioRan(t, sc, real)
			compareTranscripts(t, sc, house, real)
		})
	}
}

var sqlDiffScenarios = []diffScenario{
	{
		name: "NULLs sort first ascending and last descending",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER)`,
			`INSERT INTO t (id, v) VALUES (1, 3), (2, NULL), (3, 1)`,
		},
		probes: []string{`SELECT id, v FROM t ORDER BY v`, `SELECT id, v FROM t ORDER BY v DESC`},
	},
	{
		name: "= NULL matches nothing and IS NULL matches the NULLs",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, 'a'), (2, NULL)`,
		},
		probes: []string{`SELECT id FROM t WHERE v = NULL`, `SELECT id FROM t WHERE v IS NULL`, `SELECT id FROM t WHERE v IS NOT NULL`},
	},
	{
		name: "aggregates skip NULLs but COUNT(*) does not",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER)`,
			`INSERT INTO t (id, v) VALUES (1, 10), (2, NULL), (3, 20)`,
		},
		probes: []string{`SELECT COUNT(*), COUNT(v), SUM(v), AVG(v), MIN(v), MAX(v) FROM t`},
	},
	{
		name: "aggregates over no rows",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER)`,
		},
		probes: []string{`SELECT COUNT(*), SUM(v), AVG(v) FROM t`},
	},
	{
		name: "GROUP BY with a HAVING clause",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, g TEXT, v INTEGER)`,
			`INSERT INTO t (id, g, v) VALUES (1, 'a', 1), (2, 'a', 2), (3, 'b', 5), (4, NULL, 9)`,
		},
		probes: []string{
			`SELECT g, COUNT(*), SUM(v) FROM t GROUP BY g ORDER BY g`,
			`SELECT g, COUNT(*) FROM t GROUP BY g HAVING COUNT(*) > 1 ORDER BY g`,
		},
	},
	{
		name: "LIMIT and OFFSET",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY)`,
			`INSERT INTO t (id) VALUES (1), (2), (3), (4), (5)`,
		},
		probes: []string{
			`SELECT id FROM t ORDER BY id LIMIT 2`,
			`SELECT id FROM t ORDER BY id LIMIT 2 OFFSET 3`,
			`SELECT id FROM t ORDER BY id LIMIT 99 OFFSET 99`,
		},
	},
	{
		// SQLite reads a double-quoted name that matches no column as a string
		// literal — a compatibility misfeature its own documentation calls a
		// bug it cannot remove. The consequence is that a typo'd column name
		// becomes a silent constant: `WHERE "usr_id" = 1` compares the string
		// "usr_id" to 1, matches nothing, and reports no error. This engine
		// raises "unknown column" instead. Refusing is the safe direction: a
		// query that names a column that does not exist is a bug in every
		// case, and the alternative is a filter that silently stops filtering.
		name:     "a double-quoted string that is not an identifier",
		wantDiff: "a double-quoted unknown identifier is an error here, not a silent string literal",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, 'a')`,
		},
		probes: []string{`SELECT id FROM t WHERE v = "a"`},
	},
	{
		name: "LIKE is case-insensitive for ASCII and GLOB is not",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, 'Alpha'), (2, 'alpha'), (3, 'BETA')`,
		},
		probes: []string{
			`SELECT id FROM t WHERE v LIKE 'alpha' ORDER BY id`,
			`SELECT id FROM t WHERE v GLOB 'alpha' ORDER BY id`,
			`SELECT id FROM t WHERE v LIKE 'a%' ORDER BY id`,
		},
	},
	{
		name: "LIKE with an escaped wildcard",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, '100%'), (2, '1000')`,
		},
		probes: []string{`SELECT id FROM t WHERE v LIKE '100\%' ESCAPE '\' ORDER BY id`},
	},
	{
		// SQLite applies the comparison's affinity before comparing: an
		// INTEGER-affinity column compared against a text literal converts the
		// literal to a number, so `v = '7'` matches a stored 7. This engine
		// compares storage classes directly and matches nothing.
		//
		// It is not fixed here because it cannot be fixed correctly without
		// the column's declared affinity reaching the expression evaluator,
		// and the evaluator is constructed at 29 sites. A value-shaped
		// approximation — "if one side is numeric and the other is text,
		// compare numerically" — gets the opposite case wrong: a TEXT column
		// holding '007' compared against 7 would start matching, where SQLite
		// applies TEXT affinity to the literal and keeps them distinct. That
		// error direction adds rows to a filter's result, which is the wrong
		// way for a cross-check engine to be wrong. Matching nothing is
		// stricter and safe; matching too much is not.
		name:     "comparison affinity is not applied to a literal",
		wantDiff: "a text literal is not coerced to the column's numeric affinity; the engine matches nothing rather than risk matching too much",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER, tx TEXT)`,
			`INSERT INTO t (id, v, tx) VALUES (1, 7, '007')`,
		},
		probes: []string{
			`SELECT id FROM t WHERE v = 7.0`,
			`SELECT id FROM t WHERE v = '7'`,
			`SELECT id FROM t WHERE tx = 7`,
			`SELECT typeof(v), typeof(tx) FROM t`,
		},
	},
	{
		name: "a text column takes affinity on insert",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT, u)`,
			`INSERT INTO t (id, v, u) VALUES (1, 7, 7)`,
		},
		probes: []string{`SELECT typeof(v), typeof(u), v, u FROM t`},
	},
	{
		name: "UNIQUE and NOT NULL are enforced",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, code TEXT UNIQUE, name TEXT NOT NULL)`,
			`INSERT INTO t (id, code, name) VALUES (1, 'a', 'x')`,
			`INSERT INTO t (id, code, name) VALUES (2, 'a', 'y')`,
			`INSERT INTO t (id, code, name) VALUES (3, 'b', NULL)`,
			`INSERT INTO t (id, code, name) VALUES (4, NULL, 'z')`,
			`INSERT INTO t (id, code, name) VALUES (5, NULL, 'w')`,
		},
		// Two NULLs do not collide under UNIQUE: NULL is not equal to NULL.
		probes: []string{`SELECT id, code, name FROM t ORDER BY id`},
	},
	{
		name: "DEFAULT fills an omitted column",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT DEFAULT 'fallback', n INTEGER DEFAULT 0)`,
			`INSERT INTO t (id) VALUES (1)`,
			`INSERT INTO t (id, v) VALUES (2, NULL)`,
		},
		probes: []string{`SELECT id, v, n FROM t ORDER BY id`},
	},
	{
		// SQLite hands a new row the largest rowid in the table plus one, so
		// deleting the last row frees its id for the next insert. Only
		// AUTOINCREMENT prevents that. This engine keeps a counter that never
		// goes back, so it behaves as if every table were AUTOINCREMENT.
		//
		// The divergence is deliberate on the safe side: a reused rowid means
		// a reference held to a deleted row silently resolves to a DIFFERENT
		// row later, which is the kind of bug a cross-check engine exists to
		// make loud rather than to reproduce. Matching SQLite would mean
		// finding the maximum rowid on every insert.
		name:     "a deleted rowid is not handed out again",
		wantDiff: "rowids are never reused; SQLite reuses the largest one after a delete unless the table is AUTOINCREMENT",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (v) VALUES ('a'), ('b')`,
			`DELETE FROM t WHERE id = 2`,
			`INSERT INTO t (v) VALUES ('c')`,
		},
		probes: []string{`SELECT id, v FROM t ORDER BY id`},
	},
	{
		name: "LEFT JOIN keeps unmatched rows",
		steps: []string{
			`CREATE TABLE a (id INTEGER PRIMARY KEY, name TEXT)`,
			`CREATE TABLE b (id INTEGER PRIMARY KEY, aid INTEGER, tag TEXT)`,
			`INSERT INTO a (id, name) VALUES (1, 'one'), (2, 'two')`,
			`INSERT INTO b (id, aid, tag) VALUES (10, 1, 'x')`,
		},
		probes: []string{
			`SELECT a.id, b.tag FROM a LEFT JOIN b ON b.aid = a.id ORDER BY a.id`,
			`SELECT a.id, b.tag FROM a JOIN b ON b.aid = a.id ORDER BY a.id`,
		},
	},
	{
		name: "IN with a subquery and NOT IN with a NULL",
		steps: []string{
			`CREATE TABLE a (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE b (aid INTEGER)`,
			`INSERT INTO a (id) VALUES (1), (2), (3)`,
			`INSERT INTO b (aid) VALUES (1), (NULL)`,
		},
		// NOT IN against a set containing NULL matches nothing — the classic
		// three-valued-logic trap.
		probes: []string{
			`SELECT id FROM a WHERE id IN (SELECT aid FROM b) ORDER BY id`,
			`SELECT id FROM a WHERE id NOT IN (SELECT aid FROM b) ORDER BY id`,
		},
	},
	{
		name: "DISTINCT collapses duplicates including NULL",
		steps: []string{
			`CREATE TABLE t (v TEXT)`,
			`INSERT INTO t (v) VALUES ('a'), ('a'), (NULL), (NULL), ('b')`,
		},
		probes: []string{`SELECT DISTINCT v FROM t ORDER BY v`, `SELECT COUNT(DISTINCT v) FROM t`},
	},
	{
		name: "UNION dedupes and UNION ALL does not",
		steps: []string{
			`CREATE TABLE a (v INTEGER)`,
			`CREATE TABLE b (v INTEGER)`,
			`INSERT INTO a (v) VALUES (1), (2)`,
			`INSERT INTO b (v) VALUES (2), (3)`,
		},
		probes: []string{
			`SELECT v FROM a UNION SELECT v FROM b ORDER BY v`,
			`SELECT v FROM a UNION ALL SELECT v FROM b ORDER BY v`,
		},
	},
	{
		name: "COALESCE, NULLIF and CASE",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, NULL), (2, 'set')`,
		},
		probes: []string{
			`SELECT id, COALESCE(v, 'none'), NULLIF(v, 'set'), CASE WHEN v IS NULL THEN 'empty' ELSE v END FROM t ORDER BY id`,
		},
	},
	{
		name: "integer division truncates and modulo keeps the sign",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY)`,
			`INSERT INTO t (id) VALUES (1)`,
		},
		probes: []string{`SELECT 7 / 2, -7 / 2, 7 % 3, -7 % 3, 7.0 / 2 FROM t`},
	},
	{
		name: "concatenation with a NULL yields NULL",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, NULL), (2, 'x')`,
		},
		probes: []string{`SELECT id, 'a' || v FROM t ORDER BY id`},
	},
	{
		name: "an UPDATE with no matching rows is not an error",
		steps: []string{
			`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`,
			`INSERT INTO t (id, v) VALUES (1, 'a')`,
			`UPDATE t SET v = 'b' WHERE id = 99`,
			`DELETE FROM t WHERE id = 99`,
		},
		probes: []string{`SELECT id, v FROM t`},
	},
}
