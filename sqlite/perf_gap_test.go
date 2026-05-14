package sqlite

import (
	"database/sql"
	"fmt"
	"testing"
)

// Benchmark the two remaining perf gaps vs cgo

func BenchmarkPerfGap_SelectWhere(b *testing.B) {
	db, err := sql.Open("sqlite", b.TempDir()+"/perf_sw.db")
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score INTEGER)")
	for i := 0; i < 500; i++ {
		db.Exec("INSERT INTO t (id, name, score) VALUES (?, ?, ?)", i, fmt.Sprintf("name_%d", i), i*10)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t WHERE score > ?", 2500)
		count := 0
		for rows.Next() {
			var id, score int
			var name string
			rows.Scan(&id, &name, &score)
			count++
		}
		rows.Close()
		_ = count
	}
	db.Close()
}

func BenchmarkPerfGap_OrderByLimit(b *testing.B) {
	db, err := sql.Open("sqlite", b.TempDir()+"/perf_ol.db")
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	for i := 0; i < 500; i++ {
		db.Exec("INSERT INTO t (id, val) VALUES (?, ?)", i, fmt.Sprintf("v_%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t ORDER BY id LIMIT 10 OFFSET 400")
		count := 0
		for rows.Next() {
			var id int
			var val string
			rows.Scan(&id, &val)
			count++
		}
		rows.Close()
		_ = count
	}
	db.Close()
}
