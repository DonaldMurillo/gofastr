package sqlite

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

func TestDriverCompatCreateInsertSelect(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	_, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)")
	if err != nil {
		t.Fatal(err)
	}

	res, err := db.Exec("INSERT INTO users (name, email) VALUES (?, ?)", "alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if lastID == 0 {
		t.Fatal("expected non-zero last insert id")
	}

	affected, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected, got %d", affected)
	}

	var name, email string
	err = db.QueryRow("SELECT name, email FROM users WHERE id = ?", lastID).Scan(&name, &email)
	if err != nil {
		t.Fatal(err)
	}
	if name != "alice" || email != "alice@example.com" {
		t.Fatalf("unexpected values: %s, %s", name, email)
	}
}

func TestDriverCompatPreparedStmt(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	stmt, err := db.Prepare("INSERT INTO t (val) VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	for i := 0; i < 5; i++ {
		_, err := stmt.Exec(fmt.Sprintf("row_%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query("SELECT val FROM t ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
}

func TestDriverCompatQueryRow(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	db.Exec("INSERT INTO t (val) VALUES (42)")

	var val int
	err := db.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != 42 {
		t.Fatalf("expected 42, got %d", val)
	}

	// Non-existent row
	err = db.QueryRow("SELECT val FROM t WHERE id = 999").Scan(&val)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDriverCompatNullHandling(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO t (id, val) VALUES (1, NULL)")
	db.Exec("INSERT INTO t (id, val) VALUES (2, 'hello')")

	rows, err := db.Query("SELECT val FROM t ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	// First row: NULL
	if !rows.Next() {
		t.Fatal("expected row")
	}
	var val sql.NullString
	if err := rows.Scan(&val); err != nil {
		t.Fatal(err)
	}
	if val.Valid {
		t.Fatal("expected NULL")
	}

	// Second row: "hello"
	if !rows.Next() {
		t.Fatal("expected row")
	}
	if err := rows.Scan(&val); err != nil {
		t.Fatal(err)
	}
	if !val.Valid || val.String != "hello" {
		t.Fatalf("expected 'hello', got %v", val)
	}
}

func TestDriverCompatTransactions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.Exec("INSERT INTO t (val) VALUES (?)", "in_tx")
	if err != nil {
		t.Fatal(err)
	}

	// Read within transaction
	var val string
	err = tx.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "in_tx" {
		t.Fatalf("expected 'in_tx', got %s", val)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Verify after commit
	err = db.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "in_tx" {
		t.Fatalf("expected 'in_tx' after commit, got %s", val)
	}
}

func TestDriverCompatRollback(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	tx.Exec("INSERT INTO t (val) VALUES (?)", "will_rollback")

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var val string
	err = db.QueryRow("SELECT val FROM t").Scan(&val)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after rollback, got %v", err)
	}
}

func TestDriverCompatMultipleRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, num INTEGER)")
	for i := 1; i <= 10; i++ {
		db.Exec("INSERT INTO t (num) VALUES (?)", i*10)
	}

	rows, err := db.Query("SELECT num FROM t ORDER BY num")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	expected := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	i := 0
	for rows.Next() {
		var num int64
		if err := rows.Scan(&num); err != nil {
			t.Fatal(err)
		}
		if num != expected[i] {
			t.Fatalf("row %d: expected %d, got %d", i, expected[i], num)
		}
		i++
	}
	if i != 10 {
		t.Fatalf("expected 10 rows, got %d", i)
	}
}

func TestDriverCompatIntFloatScan(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, ival INTEGER, fval REAL)")
	db.Exec("INSERT INTO t (ival, fval) VALUES (?, ?)", 42, 3.14)

	var ival int64
	var fval float64
	err := db.QueryRow("SELECT ival, fval FROM t").Scan(&ival, &fval)
	if err != nil {
		t.Fatal(err)
	}
	if ival != 42 {
		t.Fatalf("expected 42, got %d", ival)
	}
	if math.Abs(fval-3.14) > 0.001 {
		t.Fatalf("expected 3.14, got %f", fval)
	}
}

func TestDriverCompatBoolArg(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, flag INTEGER)")
	db.Exec("INSERT INTO t (flag) VALUES (?)", true)

	var flag int64
	err := db.QueryRow("SELECT flag FROM t").Scan(&flag)
	if err != nil {
		t.Fatal(err)
	}
	if flag != 1 {
		t.Fatalf("expected 1 from bool true, got %d", flag)
	}
}

func TestDriverCompatUpdateRowsAffected(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	db.Exec("INSERT INTO t (val) VALUES (1)")
	db.Exec("INSERT INTO t (val) VALUES (2)")
	db.Exec("INSERT INTO t (val) VALUES (3)")

	res, err := db.Exec("UPDATE t SET val = val * 10 WHERE val > 1")
	if err != nil {
		t.Fatal(err)
	}
	affected, _ := res.RowsAffected()
	if affected != 2 {
		t.Fatalf("expected 2 rows affected, got %d", affected)
	}
}

func TestDriverCompatDeleteRowsAffected(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	db.Exec("INSERT INTO t (val) VALUES (1)")
	db.Exec("INSERT INTO t (val) VALUES (2)")

	res, err := db.Exec("DELETE FROM t WHERE val = ?", 1)
	if err != nil {
		t.Fatal(err)
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		t.Fatalf("expected 1 row affected, got %d", affected)
	}
}

func TestDriverCompatAggregateFunctions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	for i := 1; i <= 5; i++ {
		db.Exec("INSERT INTO t (val) VALUES (?)", i*10)
	}

	var count, sum, mn, mx int64
	err := db.QueryRow("SELECT COUNT(*), SUM(val), MIN(val), MAX(val) FROM t").Scan(&count, &sum, &mn, &mx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("COUNT expected 5, got %d", count)
	}
	if sum != 150 {
		t.Fatalf("SUM expected 150, got %d", sum)
	}
	if mn != 10 {
		t.Fatalf("MIN expected 10, got %d", mn)
	}
	if mx != 50 {
		t.Fatalf("MAX expected 50, got %d", mx)
	}
}

func TestDriverCompatConcurrentReads(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	for i := 0; i < 100; i++ {
		db.Exec("INSERT INTO t (val) VALUES (?)", fmt.Sprintf("row_%d", i))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				var val string
				err := db.QueryRow("SELECT val FROM t WHERE id = ?", (g*10+i)%100+1).Scan(&val)
				if err != nil {
					errCh <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

func TestDriverCompatEmptyResult(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	rows, err := db.Query("SELECT * FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if rows.Next() {
		t.Fatal("expected no rows")
	}
}

func TestDriverCompatLike(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	db.Exec("INSERT INTO t (name) VALUES ('alice')")
	db.Exec("INSERT INTO t (name) VALUES ('bob')")
	db.Exec("INSERT INTO t (name) VALUES ('alex')")

	rows, err := db.Query("SELECT name FROM t WHERE name LIKE ?", "al%")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names matching 'al%%', got %d: %v", len(names), names)
	}
}

func TestDriverCompatBetween(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	for i := 1; i <= 10; i++ {
		db.Exec("INSERT INTO t (val) VALUES (?)", i)
	}

	rows, err := db.Query("SELECT val FROM t WHERE val BETWEEN ? AND ? ORDER BY val", 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var val int
		rows.Scan(&val)
		count++
	}
	if count != 5 {
		t.Fatalf("expected 5 rows between 3 and 7, got %d", count)
	}
}

func TestDriverCompatPragma(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var mode string
	err := db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "memory" {
		t.Fatalf("expected 'memory', got %s", mode)
	}
}

func TestDriverCompatLimitOffset(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	for i := 1; i <= 10; i++ {
		db.Exec("INSERT INTO t (val) VALUES (?)", i)
	}

	rows, err := db.Query("SELECT val FROM t ORDER BY val LIMIT 3 OFFSET 2")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	expected := []int64{3, 4, 5}
	i := 0
	for rows.Next() {
		var val int64
		rows.Scan(&val)
		if val != expected[i] {
			t.Fatalf("row %d: expected %d, got %d", i, expected[i], val)
		}
		i++
	}
}

func TestDriverCompatSyntaxError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	_, err := db.Exec("INVALID SQL STATEMENT")
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "unexpected token") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestDriverCompatDiskPersistence(t *testing.T) {
	path := t.TempDir() + "/test.db"

	db1, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db1.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	db1.Exec("INSERT INTO t (val) VALUES ('persistent')")
	db1.Close()

	db2, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var val string
	err = db2.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "persistent" {
		t.Fatalf("expected 'persistent', got %s", val)
	}
}
