package sqlite

import (
	"testing"
)

// ── IN subquery ──

func TestInSubqueryBasic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE orders (id INTEGER PRIMARY KEY, cust_id INTEGER, amount REAL)")
	exec(t, e, "CREATE TABLE vip (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "INSERT INTO vip (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO vip (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (1, 1, 100)")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (2, 2, 200)")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (3, 3, 50)")

	r := exec(t, e, "SELECT id, amount FROM orders WHERE cust_id IN (SELECT id FROM vip) ORDER BY id")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "1", "100")
	checkRow(t, r, 1, "2", "200")
}

func TestNotInSubquery(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE orders (id INTEGER PRIMARY KEY, cust_id INTEGER)")
	exec(t, e, "CREATE TABLE vip (id INTEGER PRIMARY KEY)")
	exec(t, e, "INSERT INTO vip (id) VALUES (1)")
	exec(t, e, "INSERT INTO orders (id, cust_id) VALUES (1, 1)")
	exec(t, e, "INSERT INTO orders (id, cust_id) VALUES (2, 2)")
	exec(t, e, "INSERT INTO orders (id, cust_id) VALUES (3, 3)")

	r := exec(t, e, "SELECT id FROM orders WHERE cust_id NOT IN (SELECT id FROM vip) ORDER BY id")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "2")
	checkRow(t, r, 1, "3")
}

func TestInSubqueryEmpty(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "CREATE TABLE empty (id INTEGER PRIMARY KEY)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")

	r := exec(t, e, "SELECT id FROM t WHERE val IN (SELECT id FROM empty)")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(r.Rows))
	}
}

// ── Scalar subquery ──

func TestScalarSubquery(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 30)")

	r := exec(t, e, "SELECT val FROM t WHERE val > (SELECT AVG(val) FROM t) ORDER BY val")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 30 {
		t.Fatalf("expected 30, got %d", r.Rows[0][0].IntVal)
	}
}

func TestScalarSubqueryInSelect(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")

	r := exec(t, e, "SELECT val, (SELECT MAX(val) FROM t) FROM t ORDER BY val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	// Both rows should have MAX(val)=20 in the subquery column
	if r.Rows[0][1].IntVal != 20 {
		t.Fatalf("expected subquery result 20, got %d", r.Rows[0][1].IntVal)
	}
	if r.Rows[1][1].IntVal != 20 {
		t.Fatalf("expected subquery result 20, got %d", r.Rows[1][1].IntVal)
	}
}
