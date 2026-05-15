package sqlite

import (
	"fmt"
	"testing"
)

// Compare cached vs uncached with identical work
func BenchmarkCache_UncachedInsert(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each SQL string is unique — no cache hit
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name) VALUES (%d, 'hello')", i))
	}
}

func BenchmarkCache_CachedInsert(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Same SQL each time — cache hit after first iteration
		e.Execute("INSERT INTO t (id, name) VALUES (?, 'hello')", IntegerValue(int64(i)))
	}
}

func BenchmarkCache_UncachedSelect(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	for i := 0; i < 100; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name) VALUES (%d, 'hello')", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute(fmt.Sprintf("SELECT * FROM t WHERE id = %d", i%100))
	}
}

func BenchmarkCache_CachedSelect(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	for i := 0; i < 100; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name) VALUES (%d, 'hello')", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute("SELECT * FROM t WHERE id = ?", IntegerValue(int64(i%100)))
	}
}
