//go:build cgo

package sqlite

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openCGO() *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}
	return db
}

func BenchmarkCGO_CreateTable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openCGO()
		db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
		db.Close()
	}
}

func BenchmarkCGO_Insert(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Exec("INSERT INTO t(name, score) VALUES('hello', 1.5)")
	}
	db.Close()
}

func BenchmarkCGO_InsertPrepared(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	stmt, _ := db.Prepare("INSERT INTO t(name, score) VALUES(?, ?)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stmt.Exec("hello", 1.5)
	}
	stmt.Close()
	db.Close()
}

func BenchmarkCGO_SelectAll(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	for i := 0; i < 1000; i++ {
		db.Exec("INSERT INTO t(name, score) VALUES('hello', 1.5)")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t")
		for rows.Next() {
			var id int
			var name string
			var score float64
			rows.Scan(&id, &name, &score)
		}
		rows.Close()
	}
	db.Close()
}

func BenchmarkCGO_SelectWhere(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	for i := 0; i < 1000; i++ {
		name := "user_a"
		if i%2 == 0 {
			name = "user_b"
		}
		db.Exec("INSERT INTO t(name, score) VALUES(?, 1.5)", name)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t WHERE name = 'user_a'")
		for rows.Next() {
			var id int
			var name string
			var score float64
			rows.Scan(&id, &name, &score)
		}
		rows.Close()
	}
	db.Close()
}

func BenchmarkCGO_Delete(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openCGO()
		db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT)")
		db.Exec("INSERT INTO t(name) VALUES('hello')")
		b.StopTimer()
		// reset for next iter
		db.Close()
		db = openCGO()
		db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT)")
		db.Exec("INSERT INTO t(name) VALUES('hello')")
		b.StartTimer()
		db.Exec("DELETE FROM t WHERE id = 1")
		db.Close()
	}
}

func BenchmarkCGO_Transaction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openCGO()
		db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
		tx, _ := db.Begin()
		for j := 0; j < 50; j++ {
			tx.Exec("INSERT INTO t(name, score) VALUES('hello', 1.5)")
		}
		tx.Commit()
		db.Close()
	}
}

func BenchmarkCGO_OLTP(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	db.Exec("INSERT INTO t(name, score) VALUES('hello', 1.5)")
	stmt, _ := db.Prepare("SELECT * FROM t WHERE id = ?")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := stmt.Query(1)
		rows.Next()
		var id int
		var name string
		var score float64
		rows.Scan(&id, &name, &score)
		rows.Close()
	}
	stmt.Close()
	db.Close()
}

func BenchmarkCGO_OrderByLimit(b *testing.B) {
	db := openCGO()
	db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT)")
	for i := 0; i < 100; i++ {
		db.Exec("INSERT INTO t(name) VALUES('hello')")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t ORDER BY id LIMIT 10")
		for rows.Next() {
			var id int
			var name string
			rows.Scan(&id, &name)
		}
		rows.Close()
	}
	db.Close()
}

func BenchmarkCGO_DriverTransaction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openCGO()
		db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)")
		tx, _ := db.Begin()
		stmt, _ := tx.Prepare("INSERT INTO t(name, score) VALUES(?, ?)")
		for j := 0; j < 50; j++ {
			stmt.Exec("hello", 1.5)
		}
		stmt.Close()
		tx.Commit()
		db.Close()
	}
}
