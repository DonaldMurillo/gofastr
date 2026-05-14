package sqlite

import (
	"database/sql"
	"fmt"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) sql.Result {
	t.Helper()
	r, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("Exec(%q): %v", query, err)
	}
	return r
}

func mustQuery(t *testing.T, db *sql.DB, query string, args ...interface{}) *sql.Rows {
	t.Helper()
	r, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("Query(%q): %v", query, err)
	}
	return r
}

// ============================================================================
// Basic Open/Close
// ============================================================================

func TestDriverOpen(t *testing.T) {
	db := openTestDB(t)
	if db == nil {
		t.Fatal("db is nil")
	}
}

func TestDriverPing(t *testing.T) {
	db := openTestDB(t)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}

// ============================================================================
// CREATE TABLE
// ============================================================================

func TestDriverCreateTable(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
}

func TestDriverCreateTableDuplicate(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	_, err := db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	if err == nil {
		t.Fatal("expected error for duplicate table")
	}
}

// ============================================================================
// INSERT
// ============================================================================

func TestDriverInsert(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	r := mustExec(t, db, "INSERT INTO t (id, val) VALUES (1, 'hello')")
	n, err := r.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows affected = %d, expected 1", n)
	}
	lastID, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if lastID != 1 {
		t.Fatalf("last insert id = %d, expected 1", lastID)
	}
}

func TestDriverInsertMultiple(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")

	for i := 1; i <= 10; i++ {
		mustExec(t, db, "INSERT INTO t (id, name) VALUES (?, ?)", i, fmt.Sprintf("name_%d", i))
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("count = %d, expected 10", count)
	}
}

// ============================================================================
// SELECT
// ============================================================================

func TestDriverSelectBasic(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (1, 'alice')")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (2, 'bob')")

	rows := mustQuery(t, db, "SELECT id, name FROM t ORDER BY id")
	defer rows.Close()

	var results []struct {
		id   int
		name string
	}
	for rows.Next() {
		var r struct {
			id   int
			name string
		}
		if err := rows.Scan(&r.id, &r.name); err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("rows = %d, expected 2", len(results))
	}
	if results[0].id != 1 || results[0].name != "alice" {
		t.Fatalf("row 0: %d %q", results[0].id, results[0].name)
	}
	if results[1].id != 2 || results[1].name != "bob" {
		t.Fatalf("row 1: %d %q", results[1].id, results[1].name)
	}
}

func TestDriverSelectStar(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (a INTEGER, b TEXT, c REAL)")
	mustExec(t, db, "INSERT INTO t (a, b, c) VALUES (1, 'x', 3.14)")

	rows := mustQuery(t, db, "SELECT * FROM t")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	var a int
	var b string
	var c float64
	if err := rows.Scan(&a, &b, &c); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != "x" || c != 3.14 {
		t.Fatalf("got %d %q %f", a, b, c)
	}
	if rows.Next() {
		t.Fatal("expected no more rows")
	}
}

// ============================================================================
// WHERE
// ============================================================================

func TestDriverWhereEquals(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, val TEXT)")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (1, 'a')")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (2, 'b')")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (3, 'c')")

	rows := mustQuery(t, db, "SELECT val FROM t WHERE id = 2")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	var val string
	if err := rows.Scan(&val); err != nil {
		t.Fatal(err)
	}
	if val != "b" {
		t.Fatalf("val = %q, expected 'b'", val)
	}
}

func TestDriverWhereComparison(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, score INTEGER)")
	for i := 1; i <= 5; i++ {
		mustExec(t, db, "INSERT INTO t (id, score) VALUES (?, ?)", i, i*10)
	}

	rows := mustQuery(t, db, "SELECT COUNT(*) FROM t WHERE score > 20")
	defer rows.Close()
	var count int
	if !rows.Next() {
		t.Fatal("expected row")
	}
	rows.Scan(&count)
	if count != 3 {
		t.Fatalf("count = %d, expected 3", count)
	}
}

// ============================================================================
// UPDATE
// ============================================================================

func TestDriverUpdate(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (1, 'old')")

	r := mustExec(t, db, "UPDATE t SET val = 'new' WHERE id = 1")
	n, _ := r.RowsAffected()
	if n != 1 {
		t.Fatalf("affected = %d, expected 1", n)
	}

	var val string
	err := db.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "new" {
		t.Fatalf("val = %q, expected 'new'", val)
	}
}

// ============================================================================
// DELETE
// ============================================================================

func TestDriverDelete(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (1, 'a')")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (2, 'b')")

	r := mustExec(t, db, "DELETE FROM t WHERE id = 1")
	n, _ := r.RowsAffected()
	if n != 1 {
		t.Fatalf("affected = %d, expected 1", n)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count)
	if count != 1 {
		t.Fatalf("count = %d, expected 1", count)
	}
}

// ============================================================================
// Transactions
// ============================================================================

func TestDriverTransaction(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.Exec("INSERT INTO t (id, val) VALUES (1, 'hello')")
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var val string
	err = db.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "hello" {
		t.Fatalf("val = %q", val)
	}
}

func TestDriverTransactionRollback(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx.Exec("INSERT INTO t (id, val) VALUES (1, 'hello')")

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count)
	if count != 0 {
		t.Fatalf("count = %d after rollback, expected 0", count)
	}
}

// ============================================================================
// NULL handling
// ============================================================================

func TestDriverNull(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "INSERT INTO t (id) VALUES (1)")

	var val sql.NullString
	err := db.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val.Valid {
		t.Fatalf("expected NULL, got %q", val.String)
	}
}

func TestDriverInsertNull(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db, "INSERT INTO t (id, val) VALUES (1, NULL)")

	rows := mustQuery(t, db, "SELECT val FROM t")
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected row")
	}
	var val sql.NullString
	rows.Scan(&val)
	if val.Valid {
		t.Fatalf("expected NULL, got %q", val.String)
	}
}

// ============================================================================
// Multiple tables
// ============================================================================

func TestDriverMultipleTables(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, a TEXT)")
	mustExec(t, db, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, b TEXT)")

	mustExec(t, db, "INSERT INTO t1 (id, a) VALUES (1, 'from_t1')")
	mustExec(t, db, "INSERT INTO t2 (id, b) VALUES (1, 'from_t2')")

	var a, b string
	db.QueryRow("SELECT a FROM t1 WHERE id = 1").Scan(&a)
	db.QueryRow("SELECT b FROM t2 WHERE id = 1").Scan(&b)
	if a != "from_t1" {
		t.Fatalf("a = %q", a)
	}
	if b != "from_t2" {
		t.Fatalf("b = %q", b)
	}
}

// ============================================================================
// Error cases
// ============================================================================

func TestDriverSelectNonexistent(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Query("SELECT * FROM nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDriverInsertNonexistent(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("INSERT INTO nonexistent (a) VALUES (1)")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ============================================================================
// Column types
// ============================================================================

func TestDriverColumnTypes(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (a INTEGER, b TEXT, c REAL)")
	mustExec(t, db, "INSERT INTO t (a, b, c) VALUES (1, 'hello', 3.14)")

	rows := mustQuery(t, db, "SELECT a, b, c FROM t")
	defer rows.Close()

	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 3 {
		t.Fatalf("column types = %d, expected 3", len(types))
	}

	names := make([]string, 3)
	for i, ct := range types {
		names[i] = ct.Name()
	}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Fatalf("column names = %v", names)
	}
}

// ============================================================================
// Aggregate functions
// ============================================================================

func TestDriverCount(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := 1; i <= 5; i++ {
		mustExec(t, db, "INSERT INTO t (id) VALUES (?)", i)
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("count = %d, expected 5", count)
	}
}

// ============================================================================
// DROP TABLE
// ============================================================================

func TestDriverDropTable(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER)")
	mustExec(t, db, "DROP TABLE t")
	_, err := db.Query("SELECT * FROM t")
	if err == nil {
		t.Fatal("expected error after drop")
	}
}

// ============================================================================
// Scalar functions in SELECT
// ============================================================================

func TestDriverScalarFuncs(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, name TEXT)")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (1, 'hello')")

	var upper, lower string
	var length int
	err := db.QueryRow("SELECT UPPER(name), LOWER(name), LENGTH(name) FROM t").Scan(&upper, &lower, &length)
	if err != nil {
		t.Fatal(err)
	}
	if upper != "HELLO" {
		t.Fatalf("UPPER = %q", upper)
	}
	if lower != "hello" {
		t.Fatalf("LOWER = %q", lower)
	}
	if length != 5 {
		t.Fatalf("LENGTH = %d", length)
	}
}

// ============================================================================
// Stress: many inserts
// ============================================================================

func TestDriverStressManyInserts(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	for i := 1; i <= 100; i++ {
		mustExec(t, db, "INSERT INTO t (id, val) VALUES (?, ?)", i, fmt.Sprintf("val_%d", i))
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count)
	if count != 100 {
		t.Fatalf("count = %d, expected 100", count)
	}
}

// ============================================================================
// LIKE via driver
// ============================================================================

func TestDriverWhereLike(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, name TEXT)")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (1, 'alice')")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (2, 'bob')")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (3, 'anna')")

	rows := mustQuery(t, db, "SELECT name FROM t WHERE name LIKE 'a%' ORDER BY name")
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	if len(names) != 2 {
		t.Fatalf("LIKE: %d names, expected 2", len(names))
	}
	if names[0] != "alice" || names[1] != "anna" {
		t.Fatalf("names = %v", names)
	}
}

// ============================================================================
// IN via driver
// ============================================================================

func TestDriverWhereIn(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, name TEXT)")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (1, 'a')")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (2, 'b')")
	mustExec(t, db, "INSERT INTO t (id, name) VALUES (3, 'c')")

	rows := mustQuery(t, db, "SELECT name FROM t WHERE name IN ('a', 'c') ORDER BY name")
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	if len(names) != 2 {
		t.Fatalf("IN: %d names, expected 2", len(names))
	}
}

// ============================================================================
// LIMIT/OFFSET via driver
// ============================================================================

func TestDriverLimitOffset(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := 1; i <= 10; i++ {
		mustExec(t, db, "INSERT INTO t (id) VALUES (?)", i)
	}

	rows := mustQuery(t, db, "SELECT id FROM t ORDER BY id LIMIT 3 OFFSET 5")
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	if len(ids) != 3 {
		t.Fatalf("LIMIT/OFFSET: %d rows, expected 3", len(ids))
	}
	if ids[0] != 6 {
		t.Fatalf("first id = %d, expected 6", ids[0])
	}
}

// ============================================================================
// BETWEEN via driver
// ============================================================================

func TestDriverWhereBetween(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, "CREATE TABLE t (id INTEGER, val INTEGER)")
	for i := 1; i <= 5; i++ {
		mustExec(t, db, "INSERT INTO t (id, val) VALUES (?, ?)", i, i*10)
	}

	rows := mustQuery(t, db, "SELECT val FROM t WHERE val BETWEEN 20 AND 40 ORDER BY val")
	defer rows.Close()

	var vals []int
	for rows.Next() {
		var v int
		rows.Scan(&v)
		vals = append(vals, v)
	}
	if len(vals) != 3 {
		t.Fatalf("BETWEEN: %d vals, expected 3", len(vals))
	}
}
