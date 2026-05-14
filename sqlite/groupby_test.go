package sqlite

import (
	"fmt"
	"testing"
)

// ── GROUP BY basics ──

func TestGroupBySingleColumn(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'b', 30)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (4, 'b', 40)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (5, 'c', 50)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "30")
	checkRow(t, r, 1, "b", "70")
	checkRow(t, r, 2, "c", "50")
}

func TestGroupByCountStar(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT)")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (2, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (3, 'b')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (4, 'b')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (5, 'b')")

	r := exec(t, e, "SELECT cat, COUNT(*) FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "2")
	checkRow(t, r, 1, "b", "3")
}

func TestGroupByAvg(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val REAL)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'x', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'x', 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'x', 30)")

	r := exec(t, e, "SELECT cat, AVG(val) FROM t GROUP BY cat")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][1].FloatVal != 20.0 {
		t.Fatalf("expected AVG=20.0, got %f", r.Rows[0][1].FloatVal)
	}
}

func TestGroupByMinMax(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 5)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'a', 15)")

	r := exec(t, e, "SELECT cat, MIN(val), MAX(val) FROM t GROUP BY cat")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][1].IntVal != 5 {
		t.Fatalf("expected MIN=5, got %d", r.Rows[0][1].IntVal)
	}
	if r.Rows[0][2].IntVal != 15 {
		t.Fatalf("expected MAX=15, got %d", r.Rows[0][2].IntVal)
	}
}

func TestGroupByMultipleColumns(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b, val) VALUES (1, 'x', 'm', 10)")
	exec(t, e, "INSERT INTO t (id, a, b, val) VALUES (2, 'x', 'm', 20)")
	exec(t, e, "INSERT INTO t (id, a, b, val) VALUES (3, 'x', 'n', 30)")
	exec(t, e, "INSERT INTO t (id, a, b, val) VALUES (4, 'y', 'm', 40)")

	r := exec(t, e, "SELECT a, b, SUM(val) FROM t GROUP BY a, b ORDER BY a, b")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "x", "m", "30")
	checkRow(t, r, 1, "x", "n", "30")
	checkRow(t, r, 2, "y", "m", "40")
}

// ── GROUP BY with no aggregates ──

func TestGroupByNoAggregates(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT)")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (2, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (3, 'b')")

	r := exec(t, e, "SELECT cat FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a")
	checkRow(t, r, 1, "b")
}

// ── GROUP BY with empty result ──

func TestGroupByEmptyTable(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")

	r := exec(t, e, "SELECT cat, COUNT(*) FROM t GROUP BY cat")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows for empty table, got %d", len(r.Rows))
	}
}

// ── Aggregate without GROUP BY (all rows as one group) ──

func TestAggregateNoGroupBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 30)")

	r := exec(t, e, "SELECT COUNT(*), SUM(val), AVG(val), MIN(val), MAX(val) FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 3 {
		t.Fatalf("expected COUNT=3, got %d", r.Rows[0][0].IntVal)
	}
	if r.Rows[0][1].FloatVal != 60.0 {
		t.Fatalf("expected SUM=60, got %f", r.Rows[0][1].FloatVal)
	}
	if r.Rows[0][2].FloatVal != 20.0 {
		t.Fatalf("expected AVG=20, got %f", r.Rows[0][2].FloatVal)
	}
	if r.Rows[0][3].IntVal != 10 {
		t.Fatalf("expected MIN=10, got %d", r.Rows[0][3].IntVal)
	}
	if r.Rows[0][4].IntVal != 30 {
		t.Fatalf("expected MAX=30, got %d", r.Rows[0][4].IntVal)
	}
}

func TestAggregateNoGroupByEmptyTable(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")

	r := exec(t, e, "SELECT COUNT(*), SUM(val) FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 0 {
		t.Fatalf("expected COUNT=0, got %d", r.Rows[0][0].IntVal)
	}
	if r.Rows[0][1].FloatVal != 0.0 {
		t.Fatalf("expected SUM=0, got %f", r.Rows[0][1].FloatVal)
	}
}

// ── HAVING ──

func TestGroupByHaving(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'b', 5)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (4, 'c', 100)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat HAVING SUM(val) > 15 ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "30")
	checkRow(t, r, 1, "c", "100")
}

func TestGroupByHavingCount(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, dept TEXT)")
	exec(t, e, "INSERT INTO t (id, dept) VALUES (1, 'eng')")
	exec(t, e, "INSERT INTO t (id, dept) VALUES (2, 'eng')")
	exec(t, e, "INSERT INTO t (id, dept) VALUES (3, 'eng')")
	exec(t, e, "INSERT INTO t (id, dept) VALUES (4, 'sales')")
	exec(t, e, "INSERT INTO t (id, dept) VALUES (5, 'hr')")

	r := exec(t, e, "SELECT dept, COUNT(*) FROM t GROUP BY dept HAVING COUNT(*) >= 2 ORDER BY dept")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "eng", "3")
}

func TestGroupByHavingFiltersAllGroups(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 1)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'b', 2)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat HAVING SUM(val) > 100")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows when HAVING filters all, got %d", len(r.Rows))
	}
}

// ── GROUP BY with ORDER BY ──

func TestGroupByWithOrderBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 30)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'b', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'c', 20)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat ORDER BY SUM(val) DESC")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "30")
	checkRow(t, r, 1, "c", "20")
	checkRow(t, r, 2, "b", "10")
}

// ── GROUP BY with LIMIT ──

func TestGroupByWithLimit(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	for i := 0; i < 9; i++ {
		cat := string(rune('a' + i%3))
		exec(t, e, "INSERT INTO t (cat, val) VALUES (?, ?)", TextValue(cat), IntegerValue(int64((i+1)*10)))
	}

	r := exec(t, e, "SELECT cat, COUNT(*) FROM t GROUP BY cat ORDER BY cat LIMIT 2")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows with LIMIT, got %d", len(r.Rows))
	}
}

// ── GROUP BY with WHERE ──

func TestGroupByWithWhere(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'b', 30)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (4, 'b', 5)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t WHERE val > 10 GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "20")
	checkRow(t, r, 1, "b", "30")
}

// ── GROUP BY with JOIN ──

func TestGroupByWithJoin(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE orders (id INTEGER PRIMARY KEY, cust_id INTEGER, amount REAL)")
	exec(t, e, "CREATE TABLE cust (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "INSERT INTO cust (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO cust (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (1, 1, 10)")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (2, 1, 20)")
	exec(t, e, "INSERT INTO orders (id, cust_id, amount) VALUES (3, 2, 50)")

	r := exec(t, e, "SELECT cust.name, SUM(orders.amount) FROM orders JOIN cust ON orders.cust_id = cust.id GROUP BY cust.name ORDER BY cust.name")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "alice", "30")
	checkRow(t, r, 1, "bob", "50")
}

// ── GROUP BY with NULL values ──

func TestGroupByWithNulls(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, NULL, 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, NULL, 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'a', 30)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
	// NULLs sort first
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL group first, got %v", r.Rows[0][0])
	}
	if r.Rows[0][1].FloatVal != 30.0 {
		t.Fatalf("expected NULL group SUM=30, got %f", r.Rows[0][1].FloatVal)
	}
	if r.Rows[1][0].TextVal != "a" {
		t.Fatalf("expected 'a' group second, got %v", r.Rows[1][0])
	}
	if r.Rows[1][1].FloatVal != 30.0 {
		t.Fatalf("expected 'a' group SUM=30, got %f", r.Rows[1][1].FloatVal)
	}
}

// ── GROUP BY single row per group ──

func TestGroupBySingleRowPerGroup(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'b', 20)")

	r := exec(t, e, "SELECT cat, COUNT(*), SUM(val) FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "1", "10")
	checkRow(t, r, 1, "b", "1", "20")
}

// ── GROUP BY preserves insertion order ──

func TestGroupByPreservesOrder(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT)")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (1, 'z')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (2, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (3, 'm')")

	r := exec(t, e, "SELECT cat, COUNT(*) FROM t GROUP BY cat")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(r.Rows))
	}
	if r.Rows[0][0].TextVal != "z" || r.Rows[1][0].TextVal != "a" || r.Rows[2][0].TextVal != "m" {
		t.Fatalf("expected insertion order z/a/m, got %v/%v/%v",
			r.Rows[0][0], r.Rows[1][0], r.Rows[2][0])
	}
}

// ── HAVING with complex expression ──

func TestGroupByHavingComplex(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 20)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (3, 'b', 100)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (4, 'c', 1)")

	r := exec(t, e, "SELECT cat, SUM(val) FROM t GROUP BY cat HAVING SUM(val) > 15 AND COUNT(*) > 1 ORDER BY cat")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "a", "30")
}

// ── GROUP BY with column alias ──

func TestGroupByColumnAlias(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (1, 'a', 10)")
	exec(t, e, "INSERT INTO t (id, cat, val) VALUES (2, 'a', 20)")

	r := exec(t, e, "SELECT cat AS category, SUM(val) AS total FROM t GROUP BY cat")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Columns[0] != "category" {
		t.Fatalf("expected column alias 'category', got '%s'", r.Columns[0])
	}
	if r.Columns[1] != "total" {
		t.Fatalf("expected column alias 'total', got '%s'", r.Columns[1])
	}
}

// ── GROUP BY large dataset ──

func TestGroupByLargeDataset(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT, val INTEGER)")
	for i := 0; i < 100; i++ {
		cat := string(rune('a' + i%5))
		exec(t, e, "INSERT INTO t (cat, val) VALUES (?, ?)", TextValue(cat), IntegerValue(int64(i+1)))
	}

	r := exec(t, e, "SELECT cat, COUNT(*), SUM(val) FROM t GROUP BY cat ORDER BY cat")
	if len(r.Rows) != 5 {
		t.Fatalf("expected 5 groups, got %d", len(r.Rows))
	}
	// Each group should have 20 rows
	for i := 0; i < 5; i++ {
		if r.Rows[i][1].IntVal != 20 {
			t.Fatalf("group %s: expected COUNT=20, got %d", r.Rows[i][0].TextVal, r.Rows[i][1].IntVal)
		}
	}
}

// ── GROUP BY with OFFSET ──

func TestGroupByWithOffset(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, cat TEXT)")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (2, 'b')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (3, 'c')")
	exec(t, e, "INSERT INTO t (id, cat) VALUES (4, 'd')")

	r := exec(t, e, "SELECT cat FROM t GROUP BY cat ORDER BY cat LIMIT 2 OFFSET 1")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	if r.Rows[0][0].TextVal != "b" {
		t.Fatalf("expected first row 'b', got %s", r.Rows[0][0].TextVal)
	}
	if r.Rows[1][0].TextVal != "c" {
		t.Fatalf("expected second row 'c', got %s", r.Rows[1][0].TextVal)
	}
}

// ── helper ──

func checkRow(t *testing.T, r *Result, idx int, expected ...string) {
	t.Helper()
	if idx >= len(r.Rows) {
		t.Fatalf("row %d: no such row (only %d rows)", idx, len(r.Rows))
	}
	row := r.Rows[idx]
	for i, exp := range expected {
		if i >= len(row) {
			t.Fatalf("row %d col %d: missing column", idx, i)
		}
		var got string
		if row[i].IsNull() {
			got = "NULL"
		} else if row[i].Type == DataTypeFloat {
			got = fmt.Sprintf("%g", row[i].FloatVal)
		} else {
			got = row[i].String()
		}
		if got != exp {
			t.Fatalf("row %d col %d: expected %q, got %q", idx, i, exp, got)
		}
	}
}
