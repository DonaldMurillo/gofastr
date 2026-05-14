package sqlite

import (
	"testing"
)

func TestUnionAll(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (2, 'y')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (3, 'z')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (2, 'y')")

	r := exec(t, e, "SELECT val FROM a UNION ALL SELECT val FROM b ORDER BY val")
	if len(r.Rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "x")
	checkRow(t, r, 1, "y")
	checkRow(t, r, 2, "y")
	checkRow(t, r, 3, "z")
}

func TestUnion(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (2, 'y')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (3, 'z')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (2, 'y')")

	r := exec(t, e, "SELECT val FROM a UNION SELECT val FROM b ORDER BY val")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows (deduped), got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "x")
	checkRow(t, r, 1, "y")
	checkRow(t, r, 2, "z")
}

func TestIntersect(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (2, 'y')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (3, 'z')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (2, 'y')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (3, 'z')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (4, 'w')")

	r := exec(t, e, "SELECT val FROM a INTERSECT SELECT val FROM b ORDER BY val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "y")
	checkRow(t, r, 1, "z")
}

func TestExcept(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (2, 'y')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (3, 'z')")
	exec(t, e, "INSERT INTO b (id, val) VALUES (2, 'y')")

	r := exec(t, e, "SELECT val FROM a EXCEPT SELECT val FROM b ORDER BY val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "x")
	checkRow(t, r, 1, "z")
}

func TestUnionWithLimit(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 30)")

	r := exec(t, e, "SELECT val FROM t WHERE val < 20 UNION ALL SELECT val FROM t WHERE val > 20 ORDER BY val LIMIT 2")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "10")
	checkRow(t, r, 1, "30")
}

func TestUnionWithOrderBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "INSERT INTO t1 (id, v) VALUES (1, 30)")
	exec(t, e, "INSERT INTO t1 (id, v) VALUES (2, 10)")
	exec(t, e, "INSERT INTO t2 (id, v) VALUES (1, 20)")

	r := exec(t, e, "SELECT v FROM t1 UNION ALL SELECT v FROM t2 ORDER BY v")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "10")
	checkRow(t, r, 1, "20")
	checkRow(t, r, 2, "30")
}

func TestUnionEmpty(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "INSERT INTO t1 (id, v) VALUES (1, 10)")

	r := exec(t, e, "SELECT v FROM t1 UNION ALL SELECT v FROM t2")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "10")
}

func TestThreeWayUnion(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "CREATE TABLE t3 (id INTEGER PRIMARY KEY, v INTEGER)")
	exec(t, e, "INSERT INTO t1 (id, v) VALUES (1, 1)")
	exec(t, e, "INSERT INTO t2 (id, v) VALUES (1, 2)")
	exec(t, e, "INSERT INTO t3 (id, v) VALUES (1, 3)")

	r := exec(t, e, "SELECT v FROM t1 UNION ALL SELECT v FROM t2 UNION ALL SELECT v FROM t3 ORDER BY v")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "1")
	checkRow(t, r, 1, "2")
	checkRow(t, r, 2, "3")
}
