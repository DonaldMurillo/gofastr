package sqlite

import (
	"database/sql"
	"fmt"
	"testing"
)

// ============================================================================
// Pager benchmarks
// ============================================================================

func benchPager(b *testing.B, pageSize int) {
	b.Helper()
	for i := 0; i < b.N; i++ {
		mf := NewMemFile()
		p, _ := NewPager(mf, pageSize)
		p.InitNew()
		for j := 0; j < 100; j++ {
			pn, _ := p.AllocatePage()
			data := make([]byte, pageSize)
			data[0] = byte(j)
			p.SetPageData(pn, data)
		}
		p.Flush()
		p.Close()
	}
}

func BenchmarkPager_4096(b *testing.B) { benchPager(b, 4096) }
func BenchmarkPager_1024(b *testing.B) { benchPager(b, 1024) }
func BenchmarkPager_8192(b *testing.B) { benchPager(b, 8192) }

func BenchmarkPagerAlloc(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pn, _ := p.AllocatePage()
		data := make([]byte, 4096)
		data[0] = byte(i)
		p.SetPageData(pn, data)
	}
}

func BenchmarkPagerRead(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	for i := 0; i < 100; i++ {
		pn, _ := p.AllocatePage()
		data := make([]byte, 4096)
		data[0] = byte(i)
		p.SetPageData(pn, data)
	}
	p.Flush()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.GetPageData((i % 100) + 1)
	}
}

func BenchmarkPagerWrite(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	pn, _ := p.AllocatePage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data := make([]byte, 4096)
		data[0] = byte(i)
		p.SetPageData(pn, data)
	}
}

func BenchmarkPagerFlush(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mf := NewMemFile()
		p, _ := NewPager(mf, 4096)
		p.InitNew()
		for j := 0; j < 50; j++ {
			pn, _ := p.AllocatePage()
			data := make([]byte, 4096)
			data[0] = byte(j)
			p.SetPageData(pn, data)
		}
		p.Flush()
	}
}

// ============================================================================
// Varint benchmarks
// ============================================================================

func BenchmarkVarintEncode(b *testing.B) {
	vals := []int64{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455, 1<<31 - 1, 1 << 62}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeVarint(vals[i%len(vals)])
	}
}

func BenchmarkVarintDecode(b *testing.B) {
	vals := []int64{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455, 1<<31 - 1, 1 << 62}
	encoded := make([][]byte, len(vals))
	for i, v := range vals {
		encoded[i] = EncodeVarint(v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeVarint(encoded[i%len(encoded)])
	}
}

// ============================================================================
// Record serialization benchmarks
// ============================================================================

func BenchmarkRecordWrite(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rec := &Record{
			Columns: []Value{
				IntegerValue(int64(i)),
				TextValue("hello world benchmark text"),
				FloatValue(3.14159265358979),
				NullValue,
				IntegerValue(42),
			},
		}
		WriteRecord(rec)
	}
}

func BenchmarkRecordRead(b *testing.B) {
	rec := &Record{
		Columns: []Value{
			IntegerValue(42),
			TextValue("hello world benchmark text"),
			FloatValue(3.14159265358979),
			NullValue,
			IntegerValue(100),
		},
	}
	data := WriteRecord(rec)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReadRecord(data)
	}
}

// ============================================================================
// BTree benchmarks
// ============================================================================

func benchBTreeInsert(b *testing.B, n int) {
	b.Helper()
	for i := 0; i < b.N; i++ {
		mf := NewMemFile()
		p, _ := NewPager(mf, 4096)
		p.InitNew()
		bt := NewBTree(p)
		root, _ := bt.CreateBTree()
		rec := &Record{Columns: []Value{TextValue("benchmark data")}}
		for j := 0; j < n; j++ {
			bt.Insert(root, int64(j), rec)
		}
	}
}

func BenchmarkBTreeInsert_10(b *testing.B)   { benchBTreeInsert(b, 10) }
func BenchmarkBTreeInsert_100(b *testing.B)  { benchBTreeInsert(b, 100) }
func BenchmarkBTreeInsert_1000(b *testing.B) { benchBTreeInsert(b, 1000) }

func BenchmarkBTreeInsert_LargeRecord(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	root, _ := bt.CreateBTree()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := fmt.Sprintf("benchmark record data with some padding value %d", i)
		rec := &Record{Columns: []Value{TextValue(text), IntegerValue(int64(i))}}
		bt.Insert(root, int64(i), rec)
	}
}

func BenchmarkBTreeSearch(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	root, _ := bt.CreateBTree()
	rec := &Record{Columns: []Value{TextValue("data")}}
	for i := 0; i < 1000; i++ {
		bt.Insert(root, int64(i), rec)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bt.Search(root, int64(i%1000))
	}
}

func BenchmarkBTreeSearch_Miss(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	root, _ := bt.CreateBTree()
	rec := &Record{Columns: []Value{TextValue("data")}}
	for i := 0; i < 1000; i++ {
		bt.Insert(root, int64(i), rec)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bt.Search(root, int64(1000+i))
	}
}

func BenchmarkBTreeScan(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	root, _ := bt.CreateBTree()
	rec := &Record{Columns: []Value{TextValue("data"), IntegerValue(42)}}
	for i := 0; i < 1000; i++ {
		bt.Insert(root, int64(i), rec)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cur, _ := bt.Scan(root)
		count := 0
		for cur.Next() {
			count++
		}
		_ = count
	}
}

func BenchmarkBTreeDelete(b *testing.B) {
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	root, _ := bt.CreateBTree()
	rec := &Record{Columns: []Value{TextValue("data")}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bt.Insert(root, int64(i), rec)
		bt.Delete(root, int64(i))
	}
}

// ============================================================================
// Lexer benchmarks
// ============================================================================

func BenchmarkLexer(b *testing.B) {
	sql := "SELECT id, name, UPPER(email) FROM users WHERE age > 18 AND status = 'active' ORDER BY name LIMIT 10 OFFSET 20"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := NewLexer(sql)
		for {
			tok := l.Next()
			if tok.Type == TokenEOF || tok.Type == TokenError {
				break
			}
		}
	}
}

func BenchmarkLexerLong(b *testing.B) {
	sql := "CREATE TABLE benchmark (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE, age INTEGER DEFAULT 0, score REAL, bio TEXT, created INTEGER)"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := NewLexer(sql)
		for {
			tok := l.Next()
			if tok.Type == TokenEOF || tok.Type == TokenError {
				break
			}
		}
	}
}

// ============================================================================
// Parser benchmarks
// ============================================================================

func BenchmarkParserCreateTable(b *testing.B) {
	sql := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER DEFAULT 0)"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewParser(sql)
		p.Parse()
	}
}

func BenchmarkParserInsert(b *testing.B) {
	sql := "INSERT INTO users (id, name, email, age) VALUES (1, 'Alice', 'alice@example.com', 30)"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewParser(sql)
		p.Parse()
	}
}

func BenchmarkParserSelect(b *testing.B) {
	sql := "SELECT u.id, u.name, COUNT(*) FROM users u WHERE u.age > 18 AND u.status = 'active' GROUP BY u.id ORDER BY u.name LIMIT 10"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewParser(sql)
		p.Parse()
	}
}

func BenchmarkParserComplex(b *testing.B) {
	sql := "SELECT a.id, a.name, b.total FROM customers a INNER JOIN orders b ON a.id = b.customer_id WHERE a.region IN ('US', 'EU') AND b.total BETWEEN 100 AND 500 ORDER BY b.total DESC LIMIT 50 OFFSET 100"
	b.SetBytes(int64(len(sql)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewParser(sql)
		p.Parse()
	}
}

// ============================================================================
// Expression evaluator benchmarks
// ============================================================================

func BenchmarkExprEvalArithmetic(b *testing.B) {
	eval := &ExprEval{
		Row:       []Value{IntegerValue(10), IntegerValue(20)},
		ColumnMap: map[string]int{"a": 0, "b": 1},
	}
	expr := BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpAdd, Right: ColumnRef{Column: "b"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval.Eval(expr)
	}
}

func BenchmarkExprEvalComparison(b *testing.B) {
	eval := &ExprEval{
		Row:       []Value{IntegerValue(10), IntegerValue(20)},
		ColumnMap: map[string]int{"a": 0, "b": 1},
	}
	expr := BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpLt, Right: ColumnRef{Column: "b"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval.Eval(expr)
	}
}

func BenchmarkExprEvalLike(b *testing.B) {
	eval := &ExprEval{
		Row:       []Value{TextValue("alice")},
		ColumnMap: map[string]int{"name": 0},
	}
	expr := LikeExpr{
		Expr:    ColumnRef{Column: "name"},
		Pattern: LiteralExpr{Type: DataTypeText, TextVal: "a%"},
		Op:      LikeLike,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval.Eval(expr)
	}
}

func BenchmarkExprEvalFuncCall(b *testing.B) {
	eval := &ExprEval{
		Row:       []Value{TextValue("hello world")},
		ColumnMap: map[string]int{"s": 0},
	}
	expr := FunctionCall{Name: "UPPER", Args: []Expr{ColumnRef{Column: "s"}}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval.Eval(expr)
	}
}

// ============================================================================
// Engine benchmarks (end-to-end SQL)
// ============================================================================

func newBenchEngine(b *testing.B) *Engine {
	b.Helper()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()
	bt := NewBTree(p)
	return NewEngine(p, bt)
}

func BenchmarkEngineCreateTable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newBenchEngine(b)
		e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	}
}

func BenchmarkEngineInsert(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name, score) VALUES (%d, 'user_%d', %f)", i, i, float64(i)*1.1))
	}
}

func BenchmarkEngineInsert_WithParams(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute("INSERT INTO t (id, name, score) VALUES (?, ?, ?)",
			IntegerValue(int64(i)),
			TextValue(fmt.Sprintf("user_%d", i)),
			FloatValue(float64(i)*1.1),
		)
	}
}

func BenchmarkEngineSelectAll(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name) VALUES (%d, 'name_%d')", i, i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT * FROM t")
		_ = r
	}
}

func BenchmarkEngineSelectWhere(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score INTEGER)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, name, score) VALUES (%d, 'name_%d', %d)", i, i, i*10))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT * FROM t WHERE score > 500")
		_ = r
	}
}

func BenchmarkEngineSelectCount(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id) VALUES (%d)", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT COUNT(*) FROM t")
		_ = r
	}
}

func BenchmarkEngineUpdate(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'old_%d')", i, i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute(fmt.Sprintf("UPDATE t SET val = 'new_%d' WHERE id = %d", i, i%1000))
	}
}

func BenchmarkEngineDelete(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'data')", i))
		e.Execute(fmt.Sprintf("DELETE FROM t WHERE id = %d", i))
	}
}

func BenchmarkEngineOrderBy(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, score INTEGER)")
	for i := 0; i < 100; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, score) VALUES (%d, %d)", i, 100-i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT * FROM t ORDER BY score DESC")
		_ = r
	}
}

func BenchmarkEngineLimitOffset(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id) VALUES (%d)", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT * FROM t ORDER BY id LIMIT 10 OFFSET 500")
		_ = r
	}
}

func BenchmarkEngineLimitOffset_NoOrder(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id) VALUES (%d)", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _ := e.Execute("SELECT * FROM t LIMIT 10 OFFSET 500")
		_ = r
	}
}

func BenchmarkEngineTransaction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newBenchEngine(b)
		e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
		e.Execute("BEGIN")
		for j := 0; j < 100; j++ {
			e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'data_%d')", j, j))
		}
		e.Execute("COMMIT")
	}
}

func BenchmarkEngineRollback(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newBenchEngine(b)
		e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
		e.Execute("BEGIN")
		for j := 0; j < 100; j++ {
			e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'data_%d')", j, j))
		}
		e.Execute("ROLLBACK")
	}
}

// ============================================================================
// Mixed OLTP benchmark (realistic workload)
// ============================================================================

func BenchmarkEngineOLTP(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, score INTEGER)")
	for i := 0; i < 500; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO users (id, name, email, score) VALUES (%d, 'user_%d', 'user_%d@example.com', %d)",
			i, i, i, i*10))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch i % 5 {
		case 0: // Point query
			e.Execute("SELECT * FROM users WHERE id = ?", IntegerValue(int64(i%500)))
		case 1: // Range query
			e.Execute("SELECT * FROM users WHERE score > 2500")
		case 2: // Update
			e.Execute(fmt.Sprintf("UPDATE users SET score = %d WHERE id = %d", i, i%500))
		case 3: // Aggregate
			e.Execute("SELECT COUNT(*) FROM users")
		case 4: // Order by + limit
			e.Execute("SELECT * FROM users ORDER BY score DESC LIMIT 10")
		}
	}
}

// ============================================================================
// database/sql driver benchmarks
// ============================================================================

func openBenchDB(b *testing.B) *sql.DB {
	b.Helper()
	db, err := Open()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

func BenchmarkDriverInsert(b *testing.B) {
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Exec("INSERT INTO t (id, name, score) VALUES (?, ?, ?)", i, fmt.Sprintf("user_%d", i), float64(i)*1.1)
	}
}

func BenchmarkDriverSelect(b *testing.B) {
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	for i := 0; i < 1000; i++ {
		db.Exec("INSERT INTO t (id, name) VALUES (?, ?)", i, fmt.Sprintf("name_%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t")
		for rows.Next() {
			var id int
			var name string
			rows.Scan(&id, &name)
		}
		rows.Close()
	}
}

func BenchmarkDriverSelectWhere(b *testing.B) {
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score INTEGER)")
	for i := 0; i < 1000; i++ {
		db.Exec("INSERT INTO t (id, name, score) VALUES (?, ?, ?)", i, fmt.Sprintf("name_%d", i), i*10)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM t WHERE score > ?", 500)
		for rows.Next() {
			var id, score int
			var name string
			rows.Scan(&id, &name, &score)
		}
		rows.Close()
	}
}

func BenchmarkDriverPreparedInsert(b *testing.B) {
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	stmt, err := db.Prepare("INSERT INTO t (id, name) VALUES (?, ?)")
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stmt.Exec(i, fmt.Sprintf("name_%d", i))
	}
}

func BenchmarkDriverTransaction(b *testing.B) {
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, _ := db.Begin()
		for j := 0; j < 50; j++ {
			tx.Exec("INSERT INTO t (id, val) VALUES (?, ?)", i*50+j, fmt.Sprintf("v_%d", i*50+j))
		}
		tx.Commit()
	}
}

func BenchmarkDriverRoundTrip(b *testing.B) {
	// Measures full round-trip: prepare -> exec -> query -> scan
	db := openBenchDB(b)
	db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO t (id, val) VALUES (1, 'hello')")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var val string
		db.QueryRow("SELECT val FROM t WHERE id = ?", 1).Scan(&val)
	}
}

// ============================================================================
// Schema benchmarks
// ============================================================================

func BenchmarkBuildTableInfo(b *testing.B) {
	sql := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE, age INTEGER DEFAULT 0, score REAL, bio TEXT)"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewParser(sql)
		stmt, _ := p.Parse()
		if ct, ok := stmt.(*CreateTableStmt); ok {
			BuildTableInfo(ct, 2)
		}
	}
}

// ============================================================================
// Full pipeline benchmark
// ============================================================================

func BenchmarkFullPipeline(b *testing.B) {
	// Simulates: create -> insert N -> select all -> update -> delete
	for i := 0; i < b.N; i++ {
		e := newBenchEngine(b)
		e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
		for j := 0; j < 50; j++ {
			e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'val_%d')", j, j))
		}
		e.Execute("SELECT * FROM t")
		e.Execute("UPDATE t SET val = 'updated' WHERE id = 1")
		e.Execute("DELETE FROM t WHERE id = 2")
		e.Execute("SELECT COUNT(*) FROM t")
	}
}
