package sqlite

import (
	"fmt"
	"testing"
)

// ============================================================================
// INNER JOIN tests
// ============================================================================

func TestJoinBasic(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	mustExec(t, db, "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL)")

	mustExec(t, db, "INSERT INTO users (id, name) VALUES (1, 'alice')")
	mustExec(t, db, "INSERT INTO users (id, name) VALUES (2, 'bob')")
	mustExec(t, db, "INSERT INTO users (id, name) VALUES (3, 'charlie')")

	mustExec(t, db, "INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 10.5)")
	mustExec(t, db, "INSERT INTO orders (id, user_id, amount) VALUES (2, 1, 20.0)")
	mustExec(t, db, "INSERT INTO orders (id, user_id, amount) VALUES (3, 2, 15.0)")
	// charlie has no orders

	rows := mustQuery(t, db, "SELECT users.name, orders.amount FROM users INNER JOIN orders ON users.id = orders.user_id")
	defer rows.Close()

	type result struct {
		name   string
		amount float64
	}
	var results []result
	for rows.Next() {
		var r result
		if err := rows.Scan(&r.name, &r.amount); err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 rows (charlie has no orders), got %d", len(results))
	}

	// alice should appear twice
	aliceCount := 0
	for _, r := range results {
		if r.name == "alice" {
			aliceCount++
		}
	}
	if aliceCount != 2 {
		t.Errorf("alice should appear in 2 rows, got %d", aliceCount)
	}
}

func TestJoinWithWhere(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE departments (id INTEGER PRIMARY KEY, name TEXT)")
	mustExec(t, db, "CREATE TABLE employees (id INTEGER PRIMARY KEY, dept_id INTEGER, name TEXT)")

	mustExec(t, db, "INSERT INTO departments (id, name) VALUES (1, 'eng')")
	mustExec(t, db, "INSERT INTO departments (id, name) VALUES (2, 'sales')")

	mustExec(t, db, "INSERT INTO employees (id, dept_id, name) VALUES (1, 1, 'alice')")
	mustExec(t, db, "INSERT INTO employees (id, dept_id, name) VALUES (2, 1, 'bob')")
	mustExec(t, db, "INSERT INTO employees (id, dept_id, name) VALUES (3, 2, 'charlie')")

	rows := mustQuery(t, db, "SELECT employees.name FROM employees INNER JOIN departments ON employees.dept_id = departments.id WHERE departments.name = 'eng'")
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 eng employees, got %d: %v", len(names), names)
	}
}

func TestJoinQualifiedColumns(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER, val TEXT)")

	mustExec(t, db, "INSERT INTO a (id, val) VALUES (1, 'a1')")
	mustExec(t, db, "INSERT INTO b (id, a_id, val) VALUES (1, 1, 'b1')")

	rows := mustQuery(t, db, "SELECT a.val, b.val FROM a INNER JOIN b ON a.id = b.a_id")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected a row")
	}
	var aVal, bVal string
	rows.Scan(&aVal, &bVal)
	if aVal != "a1" || bVal != "b1" {
		t.Fatalf("got a.val=%q b.val=%q, expected 'a1' 'b1'", aVal, bVal)
	}
}

func TestJoinStarExpansion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE x (id INTEGER PRIMARY KEY, a TEXT)")
	mustExec(t, db, "CREATE TABLE y (id INTEGER PRIMARY KEY, b TEXT)")

	mustExec(t, db, "INSERT INTO x (id, a) VALUES (1, 'xa')")
	mustExec(t, db, "INSERT INTO y (id, b) VALUES (1, 'yb')")

	rows := mustQuery(t, db, "SELECT * FROM x INNER JOIN y ON x.id = y.id")
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 4 {
		t.Fatalf("expected 4 columns from JOIN *, got %d: %v", len(cols), cols)
	}

	var id1 int
	var a string
	var id2 int
	var b string
	if !rows.Next() {
		t.Fatal("expected a row")
	}
	if err := rows.Scan(&id1, &a, &id2, &b); err != nil {
		t.Fatal(err)
	}
	if a != "xa" || b != "yb" {
		t.Fatalf("got a=%q b=%q", a, b)
	}
}

func TestJoinNoMatches(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE p (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "CREATE TABLE q (id INTEGER PRIMARY KEY, p_id INTEGER, val TEXT)")

	mustExec(t, db, "INSERT INTO p (id, val) VALUES (1, 'p1')")
	mustExec(t, db, "INSERT INTO q (id, p_id, val) VALUES (1, 99, 'q1')") // no match

	rows := mustQuery(t, db, "SELECT p.val, q.val FROM p INNER JOIN q ON p.id = q.p_id")
	defer rows.Close()

	if rows.Next() {
		t.Fatal("INNER JOIN should return 0 rows when no matches")
	}
}

func TestJoinWithAggregates(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT)")
	mustExec(t, db, "CREATE TABLE purchases (id INTEGER PRIMARY KEY, cust_id INTEGER, amount REAL)")

	mustExec(t, db, "INSERT INTO customers (id, name) VALUES (1, 'alice')")
	mustExec(t, db, "INSERT INTO customers (id, name) VALUES (2, 'bob')")

	mustExec(t, db, "INSERT INTO purchases (id, cust_id, amount) VALUES (1, 1, 10.0)")
	mustExec(t, db, "INSERT INTO purchases (id, cust_id, amount) VALUES (2, 1, 20.0)")
	mustExec(t, db, "INSERT INTO purchases (id, cust_id, amount) VALUES (3, 2, 30.0)")

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM customers INNER JOIN purchases ON customers.id = purchases.cust_id").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("COUNT(*) = %d, expected 3", count)
	}
}

func TestJoinMultipleRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	mustExec(t, db, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, name TEXT)")

	mustExec(t, db, "INSERT INTO parent (id, name) VALUES (1, 'p1')")
	for i := 1; i <= 10; i++ {
		mustExec(t, db, fmt.Sprintf("INSERT INTO child (id, parent_id, name) VALUES (%d, 1, 'c%d')", i, i))
	}

	rows := mustQuery(t, db, "SELECT child.name FROM parent INNER JOIN child ON parent.id = child.parent_id")
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	if count != 10 {
		t.Fatalf("expected 10 children, got %d", count)
	}
}

func TestJoinWithLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE m (id INTEGER PRIMARY KEY, v TEXT)")
	mustExec(t, db, "CREATE TABLE n (id INTEGER PRIMARY KEY, m_id INTEGER, v TEXT)")

	mustExec(t, db, "INSERT INTO m (id, v) VALUES (1, 'm1')")
	for i := 1; i <= 20; i++ {
		mustExec(t, db, fmt.Sprintf("INSERT INTO n (id, m_id, v) VALUES (%d, 1, 'n%d')", i, i))
	}

	rows := mustQuery(t, db, "SELECT n.v FROM m INNER JOIN n ON m.id = n.m_id LIMIT 5")
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	if count != 5 {
		t.Fatalf("LIMIT 5 should return 5 rows, got %d", count)
	}
}

func TestJoinWithOrderBy(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	mustExec(t, db, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, a TEXT)")
	mustExec(t, db, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, t1_id INTEGER, b TEXT)")

	mustExec(t, db, "INSERT INTO t1 (id, a) VALUES (1, 'z')")
	mustExec(t, db, "INSERT INTO t1 (id, a) VALUES (2, 'a')")
	mustExec(t, db, "INSERT INTO t1 (id, a) VALUES (3, 'm')")

	mustExec(t, db, "INSERT INTO t2 (id, t1_id, b) VALUES (1, 1, 'bz')")
	mustExec(t, db, "INSERT INTO t2 (id, t1_id, b) VALUES (2, 2, 'ba')")
	mustExec(t, db, "INSERT INTO t2 (id, t1_id, b) VALUES (3, 3, 'bm')")

	rows := mustQuery(t, db, "SELECT t1.a FROM t1 INNER JOIN t2 ON t1.id = t2.t1_id ORDER BY t1.a")
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}

	expected := []string{"a", "m", "z"}
	if len(names) != len(expected) {
		t.Fatalf("got %d rows, expected %d", len(names), len(expected))
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("row %d: got %q, expected %q", i, n, expected[i])
		}
	}
}

// ============================================================================
// Engine-level JOIN tests (no database/sql)
// ============================================================================

func TestEngineJoinBasic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER, val TEXT)")

	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'hello')")
	exec(t, e, "INSERT INTO b (id, a_id, val) VALUES (1, 1, 'world')")

	r := exec(t, e, "SELECT a.val, b.val FROM a INNER JOIN b ON a.id = b.a_id")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][0].TextVal != "hello" {
		t.Errorf("a.val = %q, expected 'hello'", r.Rows[0][0].TextVal)
	}
	if r.Rows[0][1].TextVal != "world" {
		t.Errorf("b.val = %q, expected 'world'", r.Rows[0][1].TextVal)
	}
}

func TestEngineJoinCrossProduct(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE x (id INTEGER PRIMARY KEY)")
	exec(t, e, "CREATE TABLE y (id INTEGER PRIMARY KEY)")

	// Without a restrictive ON clause, all combinations match
	exec(t, e, "INSERT INTO x (id) VALUES (1)")
	exec(t, e, "INSERT INTO x (id) VALUES (2)")
	exec(t, e, "INSERT INTO y (id) VALUES (10)")
	exec(t, e, "INSERT INTO y (id) VALUES (20)")

	r := exec(t, e, "SELECT x.id, y.id FROM x INNER JOIN y ON 1")
	if len(r.Rows) != 4 {
		t.Fatalf("cross join should produce 4 rows, got %d", len(r.Rows))
	}
}

// openTestDB is defined in driver_test.go

// ============================================================================
// LEFT JOIN tests
// ============================================================================

func TestLeftJoinBasic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL)")

	exec(t, e, "INSERT INTO users (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO users (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO users (id, name) VALUES (3, 'charlie')") // no orders

	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 10.0)")
	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (2, 1, 20.0)")
	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (3, 2, 30.0)")

	r := exec(t, e, "SELECT u.name, o.amount FROM users u LEFT JOIN orders o ON u.id = o.user_id ORDER BY u.name, o.amount")
	if len(r.Rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(r.Rows))
	}

	// alice 10.0, alice 20.0, bob 30.0, charlie NULL
	type pair struct {
		name   string
		isNull bool
		amt    float64
	}
	got := make([]pair, len(r.Rows))
	for i, row := range r.Rows {
		got[i].name = fmt.Sprintf("%v", row[0].TextVal)
		if row[1].Type == DataTypeNull {
			got[i].isNull = true
		} else {
			f, _ := row[1].AsFloat64()
			got[i].amt = f
		}
	}

	expected := []pair{
		{"alice", false, 10.0},
		{"alice", false, 20.0},
		{"bob", false, 30.0},
		{"charlie", true, 0},
	}
	for i, exp := range expected {
		if got[i].name != exp.name {
			t.Errorf("row %d: expected name=%q, got %q", i, exp.name, got[i].name)
		}
		if got[i].isNull != exp.isNull {
			t.Errorf("row %d: expected isNull=%v, got %v", i, exp.isNull, got[i].isNull)
		}
		if !exp.isNull && got[i].amt != exp.amt {
			t.Errorf("row %d: expected amt=%v, got %v", i, exp.amt, got[i].amt)
		}
	}
}

func TestLeftJoinAllUnmatched(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE left_t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE right_t (id INTEGER PRIMARY KEY, left_id INTEGER, val TEXT)")

	exec(t, e, "INSERT INTO left_t (id, val) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO left_t (id, val) VALUES (2, 'b')")

	// right_t is empty - all left rows should have NULLs for right columns
	r := exec(t, e, "SELECT l.val, r.val FROM left_t l LEFT JOIN right_t r ON l.id = r.left_id ORDER BY l.val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	// Both right columns should be NULL
	for i, row := range r.Rows {
		if row[1].Type != DataTypeNull {
			t.Errorf("row %d: expected right.val to be NULL, got %v", i, row[1])
		}
	}
}

func TestLeftJoinAllMatched(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE dept (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE emp (id INTEGER PRIMARY KEY, dept_id INTEGER, name TEXT)")

	exec(t, e, "INSERT INTO dept (id, name) VALUES (1, 'eng')")
	exec(t, e, "INSERT INTO emp (id, dept_id, name) VALUES (1, 1, 'alice')")
	exec(t, e, "INSERT INTO emp (id, dept_id, name) VALUES (2, 1, 'bob')")

	r := exec(t, e, "SELECT d.name, e.name FROM dept d LEFT JOIN emp e ON d.id = e.dept_id ORDER BY e.name")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
}

// ============================================================================
// RIGHT JOIN tests
// ============================================================================

func TestRightJoinBasic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL)")

	exec(t, e, "INSERT INTO users (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO users (id, name) VALUES (2, 'bob')")

	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 10.0)")
	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (2, 1, 20.0)")
	exec(t, e, "INSERT INTO orders (id, user_id, amount) VALUES (3, 99, 30.0)") // orphan order

	r := exec(t, e, "SELECT u.name, o.amount FROM users u RIGHT JOIN orders o ON u.id = o.user_id ORDER BY o.amount")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}

	// First two rows: alice + amount
	// Third row: NULL name + 30.0
	lastRow := r.Rows[2]
	if lastRow[1].Type == DataTypeNull {
		t.Errorf("expected order amount to be 30.0, got NULL")
	}
	if lastRow[0].Type != DataTypeNull {
		t.Errorf("expected user name to be NULL for orphan order, got %v", lastRow[0])
	}
}

func TestRightJoinAllUnmatched(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE left_t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE right_t (id INTEGER PRIMARY KEY, val TEXT)")

	exec(t, e, "INSERT INTO right_t (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO right_t (id, val) VALUES (2, 'y')")

	// left_t is empty - all right rows should have NULLs for left columns
	r := exec(t, e, "SELECT l.val, r.val FROM left_t l RIGHT JOIN right_t r ON l.id = r.id ORDER BY r.val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	for i, row := range r.Rows {
		if row[0].Type != DataTypeNull {
			t.Errorf("row %d: expected left.val to be NULL, got %v", i, row[0])
		}
	}
}

// ============================================================================
// FULL OUTER JOIN tests
// ============================================================================

func TestFullJoinBasic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER, val TEXT)")

	exec(t, e, "INSERT INTO a (id, val) VALUES (1, 'a1')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (2, 'a2')")
	exec(t, e, "INSERT INTO a (id, val) VALUES (3, 'a3')") // no match in b

	exec(t, e, "INSERT INTO b (id, a_id, val) VALUES (1, 1, 'b1')")
	exec(t, e, "INSERT INTO b (id, a_id, val) VALUES (2, 2, 'b2')")
	exec(t, e, "INSERT INTO b (id, a_id, val) VALUES (3, 99, 'b3')") // no match in a

	r := exec(t, e, "SELECT a.val, b.val FROM a FULL OUTER JOIN b ON a.id = b.a_id ORDER BY a.val, b.val")
	if len(r.Rows) != 4 {
		t.Fatalf("expected 4 rows, got %d: %+v", len(r.Rows), r.Rows)
	}

	// Expected:
	// a1, b1   (matched)
	// a2, b2   (matched)
	// a3, NULL (left unmatched)
	// NULL, b3 (right unmatched)
	type pair struct {
		a string // "NULL" for null
		b string
	}
	got := make([]pair, len(r.Rows))
	for i, row := range r.Rows {
		if row[0].Type == DataTypeNull {
			got[i].a = "NULL"
		} else {
			got[i].a = fmt.Sprintf("%v", row[0].TextVal)
		}
		if row[1].Type == DataTypeNull {
			got[i].b = "NULL"
		} else {
			got[i].b = fmt.Sprintf("%v", row[1].TextVal)
		}
	}

	expected := []pair{
		{"NULL", "b3"},
		{"a1", "b1"},
		{"a2", "b2"},
		{"a3", "NULL"},
	}
	for i, e := range expected {
		if got[i].a != e.a || got[i].b != e.b {
			t.Errorf("row %d: expected (%s, %s), got (%s, %s)", i, e.a, e.b, got[i].a, got[i].b)
		}
	}
}

func TestFullJoinAllMatched(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE x (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE TABLE y (id INTEGER PRIMARY KEY, x_id INTEGER, v TEXT)")

	exec(t, e, "INSERT INTO x (id, v) VALUES (1, 'x1')")
	exec(t, e, "INSERT INTO y (id, x_id, v) VALUES (1, 1, 'y1')")

	r := exec(t, e, "SELECT x.v, y.v FROM x FULL OUTER JOIN y ON x.id = y.x_id")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
}

func TestFullJoinBothEmpty(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE x (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE TABLE y (id INTEGER PRIMARY KEY, v TEXT)")

	r := exec(t, e, "SELECT x.v, y.v FROM x FULL OUTER JOIN y ON x.id = y.id")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(r.Rows))
	}
}

func TestLeftJoinMultipleJoins(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE a (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER, v TEXT)")
	exec(t, e, "CREATE TABLE c (id INTEGER PRIMARY KEY, b_id INTEGER, v TEXT)")

	exec(t, e, "INSERT INTO a (id, v) VALUES (1, 'a1')")
	exec(t, e, "INSERT INTO b (id, a_id, v) VALUES (1, 1, 'b1')")
	// c is empty

	r := exec(t, e, "SELECT a.v, b.v, c.v FROM a LEFT JOIN b ON a.id = b.a_id LEFT JOIN c ON b.id = c.b_id")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	// c.v should be NULL
	if r.Rows[0][2].Type != DataTypeNull {
		t.Errorf("expected c.v to be NULL, got %v", r.Rows[0][2])
	}
}
