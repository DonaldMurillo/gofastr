package sqlite

import (
	"fmt"
	"testing"
)

// BenchmarkEngineOLTP_Cached uses parameterized queries so the
// prepared statement cache can skip re-parsing on every iteration.
func BenchmarkEngineOLTP_Cached(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, score INTEGER)")
	for i := 0; i < 500; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO users (id, name, email, score) VALUES (%d, 'user_%d', 'user_%d@example.com', %d)",
			i, i, i, i*10))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch i % 5 {
		case 0: // Point query (cached)
			e.Execute("SELECT * FROM users WHERE id = ?", IntegerValue(int64(i%500)))
		case 1: // Range query (cached)
			e.Execute("SELECT * FROM users WHERE score > 2500")
		case 2: // Update — needs unique SQL per iteration for cache miss
			e.Execute(fmt.Sprintf("UPDATE users SET score = %d WHERE id = %d", i, i%500))
		case 3: // Aggregate (cached)
			e.Execute("SELECT COUNT(*) FROM users")
		case 4: // Order by + limit (cached)
			e.Execute("SELECT * FROM users ORDER BY score DESC LIMIT 10")
		}
	}
}

// BenchmarkEngineInsert_Cached uses the same SQL string each iteration.
func BenchmarkEngineInsert_Cached(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute("INSERT INTO t (id, name, score) VALUES (?, ?, ?)",
			IntegerValue(int64(i)),
			TextValue("hello"),
			FloatValue(1.5),
		)
	}
}
