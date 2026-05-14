package sqlite

import (
	"testing"
)

// ============================================================================
// Helper functions
// ============================================================================

func parseMust(t *testing.T, sql string) Statement {
	t.Helper()
	p := NewParser(sql)
	stmt, err := p.Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", sql, err)
	}
	return stmt
}

func parseMustErr(t *testing.T, sql string) {
	t.Helper()
	p := NewParser(sql)
	_, err := p.Parse()
	if err == nil {
		t.Fatalf("expected error for %q, got nil", sql)
	}
}

func exprIsLiteral(e Expr, typ DataType) bool {
	l, ok := e.(LiteralExpr)
	if !ok {
		return false
	}
	return l.Type == typ
}

func literalInt(e Expr) (int64, bool) {
	l, ok := e.(LiteralExpr)
	if !ok || l.Type != DataTypeInteger {
		return 0, false
	}
	return l.IntVal, true
}

func literalFloat(e Expr) (float64, bool) {
	l, ok := e.(LiteralExpr)
	if !ok || l.Type != DataTypeFloat {
		return 0, false
	}
	return l.FloatVal, true
}

func literalText(e Expr) (string, bool) {
	l, ok := e.(LiteralExpr)
	if !ok || l.Type != DataTypeText {
		return "", false
	}
	return l.TextVal, true
}

func literalNull(e Expr) bool {
	l, ok := e.(LiteralExpr)
	return ok && l.Type == DataTypeNull
}

func isColumnRef(e Expr, table, col string) bool {
	c, ok := e.(ColumnRef)
	return ok && c.Table == table && c.Column == col
}

func isBinaryOp(e Expr, op BinaryOp) (BinaryExpr, bool) {
	b, ok := e.(BinaryExpr)
	return b, ok && b.Op == op
}

// ============================================================================
// SELECT tests
// ============================================================================

func TestSelectStar(t *testing.T) {
	stmt := parseMust(t, "SELECT * FROM t")
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(sel.Columns))
	}
	if _, ok := sel.Columns[0].Expr.(StarColumn); !ok {
		t.Fatalf("expected StarColumn, got %T", sel.Columns[0].Expr)
	}
	if sel.From == nil || sel.From.Table == nil || sel.From.Table.Name != "t" {
		t.Fatal("expected FROM t")
	}
}

func TestSelectColumns(t *testing.T) {
	stmt := parseMust(t, "SELECT a, b, c FROM users")
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(sel.Columns))
	}
	if !isColumnRef(sel.Columns[0].Expr, "", "a") {
		t.Fatalf("expected column ref a, got %T", sel.Columns[0].Expr)
	}
	if !isColumnRef(sel.Columns[1].Expr, "", "b") {
		t.Fatalf("expected column ref b, got %T", sel.Columns[1].Expr)
	}
	if !isColumnRef(sel.Columns[2].Expr, "", "c") {
		t.Fatalf("expected column ref c, got %T", sel.Columns[2].Expr)
	}
}

func TestSelectQualifiedColumns(t *testing.T) {
	stmt := parseMust(t, "SELECT t.a, t.b FROM t")
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sel.Columns))
	}
	if !isColumnRef(sel.Columns[0].Expr, "t", "a") {
		t.Fatalf("expected t.a, got %T: %v", sel.Columns[0].Expr, sel.Columns[0].Expr)
	}
	if !isColumnRef(sel.Columns[1].Expr, "t", "b") {
		t.Fatalf("expected t.b, got %T", sel.Columns[1].Expr)
	}
}

func TestSelectTableStar(t *testing.T) {
	stmt := parseMust(t, "SELECT t.* FROM t")
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(sel.Columns))
	}
	cr, ok := sel.Columns[0].Expr.(ColumnRef)
	if !ok || cr.Table != "t" || cr.Column != "*" {
		t.Fatalf("expected t.*, got %T: %v", sel.Columns[0].Expr, sel.Columns[0].Expr)
	}
}

func TestSelectWithAlias(t *testing.T) {
	stmt := parseMust(t, "SELECT a AS x, b y FROM t")
	sel := stmt.(*SelectStmt)
	if sel.Columns[0].As != "x" {
		t.Fatalf("expected alias x, got %q", sel.Columns[0].As)
	}
	if sel.Columns[1].As != "y" {
		t.Fatalf("expected alias y, got %q", sel.Columns[1].As)
	}
}

func TestSelectDistinct(t *testing.T) {
	stmt := parseMust(t, "SELECT DISTINCT a FROM t")
	sel := stmt.(*SelectStmt)
	if !sel.Distinct {
		t.Fatal("expected DISTINCT")
	}
}

func TestSelectAll(t *testing.T) {
	stmt := parseMust(t, "SELECT ALL a FROM t")
	sel := stmt.(*SelectStmt)
	if sel.Distinct {
		t.Fatal("should not be DISTINCT with ALL")
	}
}

func TestSelectWhere(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE x = 1")
	sel := stmt.(*SelectStmt)
	if sel.Where == nil {
		t.Fatal("expected WHERE clause")
	}
	bin, ok := sel.Where.(BinaryExpr)
	if !ok || bin.Op != OpEq {
		t.Fatalf("expected BinaryExpr with OpEq, got %T", sel.Where)
	}
}

func TestSelectGroupBy(t *testing.T) {
	stmt := parseMust(t, "SELECT a, COUNT(*) FROM t GROUP BY a")
	sel := stmt.(*SelectStmt)
	if len(sel.GroupBy) != 1 {
		t.Fatalf("expected 1 GROUP BY expr, got %d", len(sel.GroupBy))
	}
}

func TestSelectHaving(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t GROUP BY a HAVING COUNT(*) > 5")
	sel := stmt.(*SelectStmt)
	if sel.Having == nil {
		t.Fatal("expected HAVING clause")
	}
}

func TestSelectOrderBy(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t ORDER BY a ASC, b DESC")
	sel := stmt.(*SelectStmt)
	if len(sel.OrderBy) != 2 {
		t.Fatalf("expected 2 ORDER BY items, got %d", len(sel.OrderBy))
	}
	if sel.OrderBy[0].Desc {
		t.Fatal("first ORDER BY should be ASC")
	}
	if !sel.OrderBy[1].Desc {
		t.Fatal("second ORDER BY should be DESC")
	}
}

func TestSelectLimit(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t LIMIT 10")
	sel := stmt.(*SelectStmt)
	n, ok := literalInt(sel.Limit)
	if !ok || n != 10 {
		t.Fatalf("expected LIMIT 10, got %v", sel.Limit)
	}
}

func TestSelectLimitOffset(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t LIMIT 10 OFFSET 5")
	sel := stmt.(*SelectStmt)
	n, ok := literalInt(sel.Limit)
	if !ok || n != 10 {
		t.Fatalf("expected LIMIT 10, got %v", sel.Limit)
	}
	o, ok := literalInt(sel.Offset)
	if !ok || o != 5 {
		t.Fatalf("expected OFFSET 5, got %v", sel.Offset)
	}
}

func TestSelectLimitOffsetComma(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t LIMIT 5, 10")
	sel := stmt.(*SelectStmt)
	n, ok := literalInt(sel.Limit)
	if !ok || n != 5 {
		t.Fatalf("expected LIMIT 5, got %v", sel.Limit)
	}
	o, ok := literalInt(sel.Offset)
	if !ok || o != 10 {
		t.Fatalf("expected OFFSET 10, got %v", sel.Offset)
	}
}

func TestSelectNoFrom(t *testing.T) {
	stmt := parseMust(t, "SELECT 1")
	sel := stmt.(*SelectStmt)
	if sel.From != nil {
		t.Fatal("expected no FROM clause")
	}
	if len(sel.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(sel.Columns))
	}
	n, ok := literalInt(sel.Columns[0].Expr)
	if !ok || n != 1 {
		t.Fatalf("expected literal 1, got %v", sel.Columns[0].Expr)
	}
}

func TestSelectExpression(t *testing.T) {
	stmt := parseMust(t, "SELECT a + b * 2 FROM t")
	sel := stmt.(*SelectStmt)
	// Should be a + (b * 2), not (a + b) * 2
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpAdd {
		t.Fatalf("expected + at top, got %T", sel.Columns[0].Expr)
	}
	right, ok := bin.Right.(BinaryExpr)
	if !ok || right.Op != OpMul {
		t.Fatalf("expected * on right side, got %T", bin.Right)
	}
}

func TestSelectCountStar(t *testing.T) {
	stmt := parseMust(t, "SELECT COUNT(*) FROM t")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if !fc.Star {
		t.Fatal("expected Star=true")
	}
	if fc.Name != "COUNT" {
		t.Fatalf("expected COUNT, got %s", fc.Name)
	}
}

func TestSelectFunctionDistinct(t *testing.T) {
	stmt := parseMust(t, "SELECT COUNT(DISTINCT a) FROM t")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if !fc.Distinct {
		t.Fatal("expected Distinct=true")
	}
	if len(fc.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(fc.Args))
	}
}

// ============================================================================
// JOIN tests
// ============================================================================

func TestSelectInnerJoin(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t1 INNER JOIN t2 ON t1.id = t2.id")
	sel := stmt.(*SelectStmt)
	if sel.From == nil || len(sel.From.Joins) != 1 {
		t.Fatal("expected 1 join")
	}
	if sel.From.Joins[0].Type != JoinInner {
		t.Fatal("expected INNER JOIN")
	}
	if sel.From.Joins[0].Table.Name != "t2" {
		t.Fatalf("expected t2, got %s", sel.From.Joins[0].Table.Name)
	}
}

func TestSelectLeftJoin(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t1 LEFT JOIN t2 ON t1.id = t2.id")
	sel := stmt.(*SelectStmt)
	if len(sel.From.Joins) != 1 {
		t.Fatal("expected 1 join")
	}
	if sel.From.Joins[0].Type != JoinLeft {
		t.Fatal("expected LEFT JOIN")
	}
}

func TestSelectLeftOuterJoin(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id")
	sel := stmt.(*SelectStmt)
	if len(sel.From.Joins) != 1 {
		t.Fatal("expected 1 join")
	}
	if sel.From.Joins[0].Type != JoinLeft {
		t.Fatalf("expected LEFT JOIN, got %d", sel.From.Joins[0].Type)
	}
}

func TestSelectCrossJoin(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t1 CROSS JOIN t2")
	sel := stmt.(*SelectStmt)
	if len(sel.From.Joins) != 1 {
		t.Fatal("expected 1 join")
	}
	if sel.From.Joins[0].Type != JoinCross {
		t.Fatalf("expected CROSS JOIN, got %d", sel.From.Joins[0].Type)
	}
}

func TestSelectMultipleJoins(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t1 JOIN t2 ON t1.id = t2.id JOIN t3 ON t2.id = t3.id")
	sel := stmt.(*SelectStmt)
	if len(sel.From.Joins) != 2 {
		t.Fatalf("expected 2 joins, got %d", len(sel.From.Joins))
	}
}

// ============================================================================
// INSERT tests
// ============================================================================

func TestInsertBasic(t *testing.T) {
	stmt := parseMust(t, "INSERT INTO t VALUES (1, 'hello')")
	ins := stmt.(*InsertStmt)
	if ins.Table.Name != "t" {
		t.Fatalf("expected table t, got %s", ins.Table.Name)
	}
	if len(ins.Values) != 1 {
		t.Fatalf("expected 1 row, got %d", len(ins.Values))
	}
	if len(ins.Values[0]) != 2 {
		t.Fatalf("expected 2 values, got %d", len(ins.Values[0]))
	}
	n, ok := literalInt(ins.Values[0][0])
	if !ok || n != 1 {
		t.Fatalf("expected 1, got %v", ins.Values[0][0])
	}
	s, ok := literalText(ins.Values[0][1])
	if !ok || s != "hello" {
		t.Fatalf("expected 'hello', got %v", ins.Values[0][1])
	}
}

func TestInsertWithColumns(t *testing.T) {
	stmt := parseMust(t, "INSERT INTO t (a, b) VALUES (1, 2)")
	ins := stmt.(*InsertStmt)
	if len(ins.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ins.Columns))
	}
	if ins.Columns[0] != "a" || ins.Columns[1] != "b" {
		t.Fatalf("expected [a, b], got %v", ins.Columns)
	}
}

func TestInsertMultipleRows(t *testing.T) {
	stmt := parseMust(t, "INSERT INTO t VALUES (1, 2), (3, 4), (5, 6)")
	ins := stmt.(*InsertStmt)
	if len(ins.Values) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ins.Values))
	}
	n, _ := literalInt(ins.Values[0][0])
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
	n, _ = literalInt(ins.Values[2][0])
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestInsertSelect(t *testing.T) {
	stmt := parseMust(t, "INSERT INTO t SELECT a, b FROM t2")
	ins := stmt.(*InsertStmt)
	if ins.Select == nil {
		t.Fatal("expected SELECT in INSERT")
	}
}

func TestInsertNull(t *testing.T) {
	stmt := parseMust(t, "INSERT INTO t VALUES (NULL, 1)")
	ins := stmt.(*InsertStmt)
	if !literalNull(ins.Values[0][0]) {
		t.Fatalf("expected NULL, got %v", ins.Values[0][0])
	}
}

// ============================================================================
// UPDATE tests
// ============================================================================

func TestUpdateBasic(t *testing.T) {
	stmt := parseMust(t, "UPDATE t SET a = 1, b = 2 WHERE c = 3")
	upd := stmt.(*UpdateStmt)
	if upd.Table.Name != "t" {
		t.Fatalf("expected table t, got %s", upd.Table.Name)
	}
	if len(upd.Sets) != 2 {
		t.Fatalf("expected 2 SET clauses, got %d", len(upd.Sets))
	}
	if upd.Sets[0].Column != "a" {
		t.Fatalf("expected column a, got %s", upd.Sets[0].Column)
	}
	if upd.Where == nil {
		t.Fatal("expected WHERE clause")
	}
}

func TestUpdateNoWhere(t *testing.T) {
	stmt := parseMust(t, "UPDATE t SET a = 1")
	upd := stmt.(*UpdateStmt)
	if upd.Where != nil {
		t.Fatal("expected no WHERE clause")
	}
}

func TestUpdateExpression(t *testing.T) {
	stmt := parseMust(t, "UPDATE t SET a = a + 1")
	upd := stmt.(*UpdateStmt)
	if len(upd.Sets) != 1 {
		t.Fatalf("expected 1 SET clause, got %d", len(upd.Sets))
	}
	bin, ok := upd.Sets[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpAdd {
		t.Fatalf("expected + expression, got %T", upd.Sets[0].Expr)
	}
}

// ============================================================================
// DELETE tests
// ============================================================================

func TestDeleteBasic(t *testing.T) {
	stmt := parseMust(t, "DELETE FROM t WHERE a = 1")
	del := stmt.(*DeleteStmt)
	if del.Table.Name != "t" {
		t.Fatalf("expected table t, got %s", del.Table.Name)
	}
	if del.Where == nil {
		t.Fatal("expected WHERE clause")
	}
}

func TestDeleteNoWhere(t *testing.T) {
	stmt := parseMust(t, "DELETE FROM t")
	del := stmt.(*DeleteStmt)
	if del.Where != nil {
		t.Fatal("expected no WHERE clause")
	}
}

// ============================================================================
// CREATE TABLE tests
// ============================================================================

func TestCreateTableBasic(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a INTEGER, b TEXT, c REAL)")
	ct := stmt.(*CreateTableStmt)
	if ct.Name != "t" {
		t.Fatalf("expected table t, got %s", ct.Name)
	}
	if len(ct.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ct.Columns))
	}
	if ct.Columns[0].Name != "a" {
		t.Fatalf("expected column a, got %s", ct.Columns[0].Name)
	}
	if ct.Columns[0].Type != "INTEGER" {
		t.Fatalf("expected type INTEGER, got %s", ct.Columns[0].Type)
	}
	if ct.Columns[1].Type != "TEXT" {
		t.Fatalf("expected type TEXT, got %s", ct.Columns[1].Type)
	}
	if ct.Columns[2].Type != "REAL" {
		t.Fatalf("expected type REAL, got %s", ct.Columns[2].Type)
	}
}

func TestCreateTableIfNotExists(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE IF NOT EXISTS t (a INTEGER)")
	ct := stmt.(*CreateTableStmt)
	if !ct.IfNotExists {
		t.Fatal("expected IF NOT EXISTS")
	}
}

func TestCreateTablePrimaryKey(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	ct := stmt.(*CreateTableStmt)
	if len(ct.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ct.Columns))
	}
	hasPK := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintPrimaryKey {
			hasPK = true
		}
	}
	if !hasPK {
		t.Fatal("expected PRIMARY KEY constraint")
	}
}

func TestCreateTableNotNull(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a INTEGER NOT NULL)")
	ct := stmt.(*CreateTableStmt)
	hasNN := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintNotNull {
			hasNN = true
		}
	}
	if !hasNN {
		t.Fatal("expected NOT NULL constraint")
	}
}

func TestCreateTableUnique(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a TEXT UNIQUE)")
	ct := stmt.(*CreateTableStmt)
	hasU := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintUnique {
			hasU = true
		}
	}
	if !hasU {
		t.Fatal("expected UNIQUE constraint")
	}
}

func TestCreateTableDefault(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a INTEGER DEFAULT 0, b TEXT DEFAULT 'hello')")
	ct := stmt.(*CreateTableStmt)
	if len(ct.Columns[0].Constraints) == 0 {
		t.Fatal("expected constraints on column a")
	}
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintDefault {
			n, ok := literalInt(con.Value)
			if !ok || n != 0 {
				t.Fatalf("expected DEFAULT 0, got %v", con.Value)
			}
		}
	}
	for _, con := range ct.Columns[1].Constraints {
		if con.Type == ConstraintDefault {
			s, ok := literalText(con.Value)
			if !ok || s != "hello" {
				t.Fatalf("expected DEFAULT 'hello', got %v", con.Value)
			}
		}
	}
}

func TestCreateTableReferences(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a INTEGER REFERENCES other(id))")
	ct := stmt.(*CreateTableStmt)
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintForeignKey {
			if con.RefTable != "other" {
				t.Fatalf("expected references other, got %s", con.RefTable)
			}
			if len(con.RefCols) != 1 || con.RefCols[0] != "id" {
				t.Fatalf("expected ref col id, got %v", con.RefCols)
			}
			return
		}
	}
	t.Fatal("expected FOREIGN KEY constraint")
}

func TestCreateTableAutoIncrement(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT)")
	ct := stmt.(*CreateTableStmt)
	// Should have PRIMARY KEY constraint
	hasPK := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintPrimaryKey {
			hasPK = true
		}
	}
	if !hasPK {
		t.Fatal("expected PRIMARY KEY constraint")
	}
}

func TestCreateTableMultipleConstraints(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (id INTEGER PRIMARY KEY NOT NULL, name TEXT NOT NULL UNIQUE)")
	ct := stmt.(*CreateTableStmt)
	if len(ct.Columns[0].Constraints) < 2 {
		t.Fatalf("expected at least 2 constraints on id, got %d", len(ct.Columns[0].Constraints))
	}
	if len(ct.Columns[1].Constraints) < 2 {
		t.Fatalf("expected at least 2 constraints on name, got %d", len(ct.Columns[1].Constraints))
	}
}

// ============================================================================
// CREATE INDEX tests
// ============================================================================

func TestCreateIndexBasic(t *testing.T) {
	stmt := parseMust(t, "CREATE INDEX idx ON t(a)")
	ci := stmt.(*CreateIndexStmt)
	if ci.Name != "idx" {
		t.Fatalf("expected idx, got %s", ci.Name)
	}
	if ci.Table != "t" {
		t.Fatalf("expected table t, got %s", ci.Table)
	}
	if len(ci.Columns) != 1 || ci.Columns[0].Name != "a" {
		t.Fatalf("expected column a, got %v", ci.Columns)
	}
}

func TestCreateUniqueIndex(t *testing.T) {
	stmt := parseMust(t, "CREATE UNIQUE INDEX idx ON t(a)")
	ci := stmt.(*CreateIndexStmt)
	if !ci.Unique {
		t.Fatal("expected UNIQUE")
	}
}

func TestCreateIndexIfNotExists(t *testing.T) {
	stmt := parseMust(t, "CREATE INDEX IF NOT EXISTS idx ON t(a)")
	ci := stmt.(*CreateIndexStmt)
	if !ci.IfNotExists {
		t.Fatal("expected IF NOT EXISTS")
	}
}

func TestCreateIndexMultiColumn(t *testing.T) {
	stmt := parseMust(t, "CREATE INDEX idx ON t(a ASC, b DESC)")
	ci := stmt.(*CreateIndexStmt)
	if len(ci.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ci.Columns))
	}
	if ci.Columns[0].Desc {
		t.Fatal("first should be ASC")
	}
	if !ci.Columns[1].Desc {
		t.Fatal("second should be DESC")
	}
}

func TestCreateIndexCollate(t *testing.T) {
	stmt := parseMust(t, "CREATE INDEX idx ON t(a COLLATE NOCASE)")
	ci := stmt.(*CreateIndexStmt)
	if ci.Columns[0].Collate != "NOCASE" {
		t.Fatalf("expected NOCASE, got %s", ci.Columns[0].Collate)
	}
}

// ============================================================================
// DROP TABLE / DROP INDEX tests
// ============================================================================

func TestDropTable(t *testing.T) {
	stmt := parseMust(t, "DROP TABLE t")
	dt := stmt.(*DropTableStmt)
	if dt.Name != "t" {
		t.Fatalf("expected t, got %s", dt.Name)
	}
	if dt.IfExists {
		t.Fatal("should not have IF EXISTS")
	}
}

func TestDropTableIfExists(t *testing.T) {
	stmt := parseMust(t, "DROP TABLE IF EXISTS t")
	dt := stmt.(*DropTableStmt)
	if !dt.IfExists {
		t.Fatal("expected IF EXISTS")
	}
}

func TestDropIndex(t *testing.T) {
	stmt := parseMust(t, "DROP INDEX idx")
	di := stmt.(*DropIndexStmt)
	if di.Name != "idx" {
		t.Fatalf("expected idx, got %s", di.Name)
	}
}

func TestDropIndexIfExists(t *testing.T) {
	stmt := parseMust(t, "DROP INDEX IF EXISTS idx")
	di := stmt.(*DropIndexStmt)
	if !di.IfExists {
		t.Fatal("expected IF EXISTS")
	}
}

// ============================================================================
// Transaction tests
// ============================================================================

func TestBegin(t *testing.T) {
	stmt := parseMust(t, "BEGIN")
	if _, ok := stmt.(*BeginStmt); !ok {
		t.Fatalf("expected BeginStmt, got %T", stmt)
	}
}

func TestBeginTransaction(t *testing.T) {
	stmt := parseMust(t, "BEGIN TRANSACTION")
	if _, ok := stmt.(*BeginStmt); !ok {
		t.Fatalf("expected BeginStmt, got %T", stmt)
	}
}

func TestCommit(t *testing.T) {
	stmt := parseMust(t, "COMMIT")
	if _, ok := stmt.(*CommitStmt); !ok {
		t.Fatalf("expected CommitStmt, got %T", stmt)
	}
}

func TestCommitTransaction(t *testing.T) {
	stmt := parseMust(t, "COMMIT TRANSACTION")
	if _, ok := stmt.(*CommitStmt); !ok {
		t.Fatalf("expected CommitStmt, got %T", stmt)
	}
}

func TestRollback(t *testing.T) {
	stmt := parseMust(t, "ROLLBACK")
	if _, ok := stmt.(*RollbackStmt); !ok {
		t.Fatalf("expected RollbackStmt, got %T", stmt)
	}
}

func TestRollbackTransaction(t *testing.T) {
	stmt := parseMust(t, "ROLLBACK TRANSACTION")
	if _, ok := stmt.(*RollbackStmt); !ok {
		t.Fatalf("expected RollbackStmt, got %T", stmt)
	}
}

// ============================================================================
// Literal expression tests
// ============================================================================

func TestLiteralInteger(t *testing.T) {
	stmt := parseMust(t, "SELECT 42")
	sel := stmt.(*SelectStmt)
	n, ok := literalInt(sel.Columns[0].Expr)
	if !ok || n != 42 {
		t.Fatalf("expected 42, got %v", sel.Columns[0].Expr)
	}
}

func TestLiteralNegativeInteger(t *testing.T) {
	stmt := parseMust(t, "SELECT -42")
	sel := stmt.(*SelectStmt)
	u, ok := sel.Columns[0].Expr.(UnaryExpr)
	if !ok || u.Op != OpNegate {
		t.Fatalf("expected UnaryExpr OpNegate, got %T", sel.Columns[0].Expr)
	}
	n, ok := literalInt(u.Expr)
	if !ok || n != 42 {
		t.Fatalf("expected 42, got %v", u.Expr)
	}
}

func TestLiteralFloat(t *testing.T) {
	stmt := parseMust(t, "SELECT 3.14")
	sel := stmt.(*SelectStmt)
	f, ok := literalFloat(sel.Columns[0].Expr)
	if !ok {
		t.Fatalf("expected float, got %T", sel.Columns[0].Expr)
	}
	if f != 3.14 {
		t.Fatalf("expected 3.14, got %f", f)
	}
}

func TestLiteralString(t *testing.T) {
	stmt := parseMust(t, "SELECT 'hello world'")
	sel := stmt.(*SelectStmt)
	s, ok := literalText(sel.Columns[0].Expr)
	if !ok || s != "hello world" {
		t.Fatalf("expected 'hello world', got %v", sel.Columns[0].Expr)
	}
}

func TestLiteralStringEscaped(t *testing.T) {
	// 'it''s' → it's
	stmt := parseMust(t, "SELECT 'it''s'")
	sel := stmt.(*SelectStmt)
	s, ok := literalText(sel.Columns[0].Expr)
	if !ok || s != "it's" {
		t.Fatalf("expected \"it's\", got %q", s)
	}
}

func TestLiteralNull(t *testing.T) {
	stmt := parseMust(t, "SELECT NULL")
	sel := stmt.(*SelectStmt)
	if !literalNull(sel.Columns[0].Expr) {
		t.Fatalf("expected NULL, got %v", sel.Columns[0].Expr)
	}
}

func TestLiteralBlob(t *testing.T) {
	stmt := parseMust(t, "SELECT X'DEADBEEF'")
	sel := stmt.(*SelectStmt)
	lit, ok := sel.Columns[0].Expr.(LiteralExpr)
	if !ok || lit.Type != DataTypeBlob {
		t.Fatalf("expected BLOB, got %T", sel.Columns[0].Expr)
	}
	if len(lit.BlobVal) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(lit.BlobVal))
	}
	if lit.BlobVal[0] != 0xDE || lit.BlobVal[1] != 0xAD {
		t.Fatalf("expected DEAD, got %X", lit.BlobVal[:2])
	}
}

func TestLiteralHexInteger(t *testing.T) {
	stmt := parseMust(t, "SELECT 0xFF")
	sel := stmt.(*SelectStmt)
	n, ok := literalInt(sel.Columns[0].Expr)
	if !ok || n != 255 {
		t.Fatalf("expected 255, got %v", sel.Columns[0].Expr)
	}
}

// ============================================================================
// Binary operator tests
// ============================================================================

func TestBinaryAdd(t *testing.T) {
	stmt := parseMust(t, "SELECT 1 + 2")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpAdd {
		t.Fatalf("expected +, got %T", sel.Columns[0].Expr)
	}
}

func TestBinarySub(t *testing.T) {
	stmt := parseMust(t, "SELECT 3 - 1")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpSub {
		t.Fatalf("expected -, got %T", sel.Columns[0].Expr)
	}
}

func TestBinaryMul(t *testing.T) {
	stmt := parseMust(t, "SELECT 3 * 4")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpMul {
		t.Fatalf("expected *, got %T", sel.Columns[0].Expr)
	}
}

func TestBinaryDiv(t *testing.T) {
	stmt := parseMust(t, "SELECT 10 / 3")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpDiv {
		t.Fatalf("expected /, got %T", sel.Columns[0].Expr)
	}
}

func TestBinaryMod(t *testing.T) {
	stmt := parseMust(t, "SELECT 10 % 3")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpMod {
		t.Fatalf("expected %%, got %T", sel.Columns[0].Expr)
	}
}

func TestBinaryConcat(t *testing.T) {
	stmt := parseMust(t, "SELECT 'hello' || ' world'")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpConcat {
		t.Fatalf("expected ||, got %T", sel.Columns[0].Expr)
	}
}

func TestBinaryComparisonOps(t *testing.T) {
	tests := []struct {
		sql string
		op  BinaryOp
	}{
		{"SELECT a WHERE x = 1", OpEq},
		{"SELECT a WHERE x <> 1", OpNe},
		{"SELECT a WHERE x != 1", OpNe},
		{"SELECT a WHERE x < 1", OpLt},
		{"SELECT a WHERE x <= 1", OpLe},
		{"SELECT a WHERE x > 1", OpGt},
		{"SELECT a WHERE x >= 1", OpGe},
	}
	for _, tt := range tests {
		stmt := parseMust(t, tt.sql)
		sel := stmt.(*SelectStmt)
		bin, ok := sel.Where.(BinaryExpr)
		if !ok {
			t.Fatalf("%s: expected BinaryExpr, got %T", tt.sql, sel.Where)
		}
		if bin.Op != tt.op {
			t.Fatalf("%s: expected op %d, got %d", tt.sql, tt.op, bin.Op)
		}
	}
}

func TestBinaryAnd(t *testing.T) {
	stmt := parseMust(t, "SELECT a WHERE x = 1 AND y = 2")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Where.(BinaryExpr)
	if !ok || bin.Op != OpAnd {
		t.Fatalf("expected AND, got %T", sel.Where)
	}
}

func TestBinaryOr(t *testing.T) {
	stmt := parseMust(t, "SELECT a WHERE x = 1 OR y = 2")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Where.(BinaryExpr)
	if !ok || bin.Op != OpOr {
		t.Fatalf("expected OR, got %T", sel.Where)
	}
}

// ============================================================================
// Unary operator tests
// ============================================================================

func TestUnaryNegate(t *testing.T) {
	stmt := parseMust(t, "SELECT -5")
	sel := stmt.(*SelectStmt)
	u, ok := sel.Columns[0].Expr.(UnaryExpr)
	if !ok || u.Op != OpNegate {
		t.Fatalf("expected OpNegate, got %T", sel.Columns[0].Expr)
	}
}

func TestUnaryBitNot(t *testing.T) {
	stmt := parseMust(t, "SELECT ~5")
	sel := stmt.(*SelectStmt)
	u, ok := sel.Columns[0].Expr.(UnaryExpr)
	if !ok || u.Op != OpBitNot {
		t.Fatalf("expected OpBitNot, got %T", sel.Columns[0].Expr)
	}
}

func TestUnaryNot(t *testing.T) {
	stmt := parseMust(t, "SELECT NOT 1")
	sel := stmt.(*SelectStmt)
	u, ok := sel.Columns[0].Expr.(UnaryExpr)
	if !ok || u.Op != OpNot {
		t.Fatalf("expected OpNot, got %T", sel.Columns[0].Expr)
	}
}

func TestUnaryPlus(t *testing.T) {
	stmt := parseMust(t, "SELECT +5")
	sel := stmt.(*SelectStmt)
	// Unary plus should just pass through
	n, ok := literalInt(sel.Columns[0].Expr)
	if !ok || n != 5 {
		t.Fatalf("expected 5, got %T: %v", sel.Columns[0].Expr, sel.Columns[0].Expr)
	}
}

// ============================================================================
// IS NULL / IS NOT NULL tests
// ============================================================================

func TestIsNull(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a IS NULL")
	sel := stmt.(*SelectStmt)
	is, ok := sel.Where.(IsNullExpr)
	if !ok {
		t.Fatalf("expected IsNullExpr, got %T", sel.Where)
	}
	if is.Negate {
		t.Fatal("should not be negated")
	}
}

func TestIsNotNull(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a IS NOT NULL")
	sel := stmt.(*SelectStmt)
	is, ok := sel.Where.(IsNullExpr)
	if !ok {
		t.Fatalf("expected IsNullExpr, got %T", sel.Where)
	}
	if !is.Negate {
		t.Fatal("should be negated")
	}
}

// ============================================================================
// BETWEEN tests
// ============================================================================

func TestBetween(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a BETWEEN 1 AND 10")
	sel := stmt.(*SelectStmt)
	be, ok := sel.Where.(BetweenExpr)
	if !ok {
		t.Fatalf("expected BetweenExpr, got %T", sel.Where)
	}
	if be.Negate {
		t.Fatal("should not be negated")
	}
	lo, _ := literalInt(be.Low)
	hi, _ := literalInt(be.High)
	if lo != 1 || hi != 10 {
		t.Fatalf("expected BETWEEN 1 AND 10, got %d AND %d", lo, hi)
	}
}

func TestNotBetween(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a NOT BETWEEN 1 AND 10")
	sel := stmt.(*SelectStmt)
	be, ok := sel.Where.(BetweenExpr)
	if !ok {
		t.Fatalf("expected BetweenExpr, got %T", sel.Where)
	}
	if !be.Negate {
		t.Fatal("should be negated")
	}
}

// ============================================================================
// IN tests
// ============================================================================

func TestInList(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a IN (1, 2, 3)")
	sel := stmt.(*SelectStmt)
	in, ok := sel.Where.(InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", sel.Where)
	}
	if len(in.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(in.Values))
	}
	if in.Negate {
		t.Fatal("should not be negated")
	}
}

func TestNotIn(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a NOT IN (1, 2, 3)")
	sel := stmt.(*SelectStmt)
	in, ok := sel.Where.(InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", sel.Where)
	}
	if !in.Negate {
		t.Fatal("should be negated")
	}
}

func TestInSubquery(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a IN (SELECT id FROM t2)")
	sel := stmt.(*SelectStmt)
	in, ok := sel.Where.(InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", sel.Where)
	}
	if in.Select == nil {
		t.Fatal("expected subquery")
	}
}

// ============================================================================
// LIKE / GLOB tests
// ============================================================================

func TestLike(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a LIKE '%test%'")
	sel := stmt.(*SelectStmt)
	like, ok := sel.Where.(LikeExpr)
	if !ok {
		t.Fatalf("expected LikeExpr, got %T", sel.Where)
	}
	if like.Op != LikeLike {
		t.Fatal("expected LIKE")
	}
	if like.Negate {
		t.Fatal("should not be negated")
	}
}

func TestNotLike(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a NOT LIKE '%test%'")
	sel := stmt.(*SelectStmt)
	like, ok := sel.Where.(LikeExpr)
	if !ok {
		t.Fatalf("expected LikeExpr, got %T", sel.Where)
	}
	if !like.Negate {
		t.Fatal("should be negated")
	}
}

func TestGlob(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a GLOB '*.txt'")
	sel := stmt.(*SelectStmt)
	like, ok := sel.Where.(LikeExpr)
	if !ok {
		t.Fatalf("expected LikeExpr, got %T", sel.Where)
	}
	if like.Op != LikeGlob {
		t.Fatal("expected GLOB")
	}
}

func TestNotGlob(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a NOT GLOB '*.txt'")
	sel := stmt.(*SelectStmt)
	like, ok := sel.Where.(LikeExpr)
	if !ok {
		t.Fatalf("expected LikeExpr, got %T", sel.Where)
	}
	if !like.Negate {
		t.Fatal("should be negated")
	}
}

// ============================================================================
// CASE expression tests
// ============================================================================

func TestCaseWhen(t *testing.T) {
	stmt := parseMust(t, "SELECT CASE WHEN a = 1 THEN 'one' WHEN a = 2 THEN 'two' ELSE 'other' END FROM t")
	sel := stmt.(*SelectStmt)
	ce, ok := sel.Columns[0].Expr.(CaseExpr)
	if !ok {
		t.Fatalf("expected CaseExpr, got %T", sel.Columns[0].Expr)
	}
	if len(ce.Whens) != 2 {
		t.Fatalf("expected 2 WHENs, got %d", len(ce.Whens))
	}
	if ce.Else == nil {
		t.Fatal("expected ELSE")
	}
	if ce.Operand != nil {
		t.Fatal("expected no operand (simple CASE)")
	}
}

func TestCaseOperand(t *testing.T) {
	stmt := parseMust(t, "SELECT CASE a WHEN 1 THEN 'one' WHEN 2 THEN 'two' END FROM t")
	sel := stmt.(*SelectStmt)
	ce, ok := sel.Columns[0].Expr.(CaseExpr)
	if !ok {
		t.Fatalf("expected CaseExpr, got %T", sel.Columns[0].Expr)
	}
	if ce.Operand == nil {
		t.Fatal("expected operand")
	}
	if len(ce.Whens) != 2 {
		t.Fatalf("expected 2 WHENs, got %d", len(ce.Whens))
	}
	if ce.Else != nil {
		t.Fatal("expected no ELSE")
	}
}

// ============================================================================
// CAST expression tests
// ============================================================================

func TestCast(t *testing.T) {
	stmt := parseMust(t, "SELECT CAST(a AS INTEGER)")
	sel := stmt.(*SelectStmt)
	ce, ok := sel.Columns[0].Expr.(CastExpr)
	if !ok {
		t.Fatalf("expected CastExpr, got %T", sel.Columns[0].Expr)
	}
	if ce.Type != "INTEGER" {
		t.Fatalf("expected type INTEGER, got %s", ce.Type)
	}
}

func TestCastText(t *testing.T) {
	stmt := parseMust(t, "SELECT CAST(42 AS TEXT)")
	sel := stmt.(*SelectStmt)
	ce, ok := sel.Columns[0].Expr.(CastExpr)
	if !ok {
		t.Fatalf("expected CastExpr, got %T", sel.Columns[0].Expr)
	}
	if ce.Type != "TEXT" {
		t.Fatalf("expected type TEXT, got %s", ce.Type)
	}
}

// ============================================================================
// Function call tests
// ============================================================================

func TestFunctionCall(t *testing.T) {
	stmt := parseMust(t, "SELECT UPPER(a) FROM t")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if fc.Name != "UPPER" {
		t.Fatalf("expected UPPER, got %s", fc.Name)
	}
	if len(fc.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(fc.Args))
	}
}

func TestFunctionCallNoArgs(t *testing.T) {
	stmt := parseMust(t, "SELECT random()")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if len(fc.Args) != 0 && !fc.Star {
		t.Fatalf("expected 0 args or star, got %d args", len(fc.Args))
	}
}

func TestFunctionCallMultiArgs(t *testing.T) {
	stmt := parseMust(t, "SELECT COALESCE(a, b, c) FROM t")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if len(fc.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(fc.Args))
	}
}

// ============================================================================
// Parenthesized expression tests
// ============================================================================

func TestParenExpr(t *testing.T) {
	stmt := parseMust(t, "SELECT (a + b) * c FROM t")
	sel := stmt.(*SelectStmt)
	// Should be ((a + b) * c)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpMul {
		t.Fatalf("expected * at top level, got %T", sel.Columns[0].Expr)
	}
	left, ok := bin.Left.(ParenExpr)
	if !ok {
		t.Fatalf("expected ParenExpr on left, got %T", bin.Left)
	}
	inner, ok := left.Expr.(BinaryExpr)
	if !ok || inner.Op != OpAdd {
		t.Fatalf("expected + inside parens, got %T", left.Expr)
	}
}

// ============================================================================
// Operator precedence tests
// ============================================================================

func TestPrecedenceMulOverAdd(t *testing.T) {
	// a + b * c → a + (b * c)
	stmt := parseMust(t, "SELECT a + b * c FROM t")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpAdd {
		t.Fatalf("expected + at top, got %T", sel.Columns[0].Expr)
	}
	_, ok = bin.Right.(BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr on right, got %T", bin.Right)
	}
}

func TestPrecedenceAddOverConcat(t *testing.T) {
	// a || b + c → a || (b + c)
	stmt := parseMust(t, "SELECT a || b + c FROM t")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpConcat {
		t.Fatalf("expected || at top, got %T", sel.Columns[0].Expr)
	}
	right, ok := bin.Right.(BinaryExpr)
	if !ok || right.Op != OpAdd {
		t.Fatalf("expected + on right, got %T", bin.Right)
	}
}

func TestPrecedenceComparisonOverAnd(t *testing.T) {
	// a = 1 AND b = 2 → (a = 1) AND (b = 2)
	stmt := parseMust(t, "SELECT a WHERE a = 1 AND b = 2")
	sel := stmt.(*SelectStmt)
	and, ok := sel.Where.(BinaryExpr)
	if !ok || and.Op != OpAnd {
		t.Fatalf("expected AND at top, got %T", sel.Where)
	}
	left, ok := and.Left.(BinaryExpr)
	if !ok || left.Op != OpEq {
		t.Fatalf("expected = on left, got %T", and.Left)
	}
	right, ok := and.Right.(BinaryExpr)
	if !ok || right.Op != OpEq {
		t.Fatalf("expected = on right, got %T", and.Right)
	}
}

func TestPrecedenceAndOverOr(t *testing.T) {
	// a OR b AND c → a OR (b AND c)
	stmt := parseMust(t, "SELECT a WHERE a OR b AND c")
	sel := stmt.(*SelectStmt)
	or, ok := sel.Where.(BinaryExpr)
	if !ok || or.Op != OpOr {
		t.Fatalf("expected OR at top, got %T", sel.Where)
	}
	right, ok := or.Right.(BinaryExpr)
	if !ok || right.Op != OpAnd {
		t.Fatalf("expected AND on right, got %T", or.Right)
	}
}

func TestPrecedenceParensOverride(t *testing.T) {
	// (a + b) * c → ((a + b) * c)
	stmt := parseMust(t, "SELECT (a + b) * c FROM t")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpMul {
		t.Fatalf("expected * at top, got %T", sel.Columns[0].Expr)
	}
	_, ok = bin.Left.(ParenExpr)
	if !ok {
		t.Fatalf("expected ParenExpr on left, got %T", bin.Left)
	}
}

// ============================================================================
// Complex expression tests
// ============================================================================

func TestNestedAndOr(t *testing.T) {
	stmt := parseMust(t, "SELECT a WHERE (a = 1 OR a = 2) AND b = 3")
	sel := stmt.(*SelectStmt)
	and, ok := sel.Where.(BinaryExpr)
	if !ok || and.Op != OpAnd {
		t.Fatalf("expected AND at top, got %T", sel.Where)
	}
	leftParen, ok := and.Left.(ParenExpr)
	if !ok {
		t.Fatalf("expected ParenExpr on left, got %T", and.Left)
	}
	or, ok := leftParen.Expr.(BinaryExpr)
	if !ok || or.Op != OpOr {
		t.Fatalf("expected OR inside parens, got %T", leftParen.Expr)
	}
}

func TestCaseInWhere(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE CASE WHEN x > 0 THEN 1 ELSE 0 END = 1")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Where.(BinaryExpr)
	if !ok || bin.Op != OpEq {
		t.Fatalf("expected = at top, got %T", sel.Where)
	}
	_, ok = bin.Left.(CaseExpr)
	if !ok {
		t.Fatalf("expected CaseExpr on left, got %T", bin.Left)
	}
}

// ============================================================================
// Table alias tests
// ============================================================================

func TestTableAlias(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM users AS u")
	sel := stmt.(*SelectStmt)
	if sel.From.Table.As != "u" {
		t.Fatalf("expected alias u, got %s", sel.From.Table.As)
	}
}

func TestTableImplicitAlias(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM users u")
	sel := stmt.(*SelectStmt)
	if sel.From.Table.As != "u" {
		t.Fatalf("expected alias u, got %s", sel.From.Table.As)
	}
}

// ============================================================================
// ParseAll tests
// ============================================================================

func TestParseAll(t *testing.T) {
	p := NewParser("SELECT 1; SELECT 2; SELECT 3")
	stmts, err := p.ParseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(stmts))
	}
}

func TestParseAllTrailingSemicolon(t *testing.T) {
	p := NewParser("SELECT 1;")
	stmts, err := p.ParseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(stmts))
	}
}

func TestParseAllEmpty(t *testing.T) {
	p := NewParser("")
	stmts, err := p.ParseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 0 {
		t.Fatalf("expected 0 statements, got %d", len(stmts))
	}
}

func TestParseAllSemicolonsOnly(t *testing.T) {
	p := NewParser(";;;")
	stmts, err := p.ParseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 0 {
		t.Fatalf("expected 0 statements, got %d", len(stmts))
	}
}

// ============================================================================
// Error tests
// ============================================================================

func TestErrorUnexpectedEOF(t *testing.T) {
	parseMustErr(t, "")
}

func TestErrorUnexpectedToken(t *testing.T) {
	parseMustErr(t, "blah blah")
}

func TestErrorSelectMissingFrom(t *testing.T) {
	// SELECT FROM is technically valid in SQLite (selects column named FROM)
	// Instead test that SELECT with invalid syntax fails
	parseMustErr(t, "SELECT , FROM t")
}

func TestErrorInsertNoValues(t *testing.T) {
	// INSERT INTO t with invalid values clause should fail
	parseMustErr(t, "INSERT INTO t VALUES")
}

func TestErrorUpdateNoSet(t *testing.T) {
	parseMustErr(t, "UPDATE t")
}

func TestErrorDeleteNoFrom(t *testing.T) {
	parseMustErr(t, "DELETE")
}

func TestErrorCreateTableNoLParen(t *testing.T) {
	parseMustErr(t, "CREATE TABLE t")
}

func TestErrorInvalidExpression(t *testing.T) {
	parseMustErr(t, "SELECT * FROM t WHERE =")
}

func TestErrorMissingRParen(t *testing.T) {
	parseMustErr(t, "SELECT (a + b FROM t")
}

func TestErrorInvalidToken(t *testing.T) {
	// @ is not valid SQL
	parseMustErr(t, "SELECT @@@")
}

// ============================================================================
// Comment tolerance tests
// ============================================================================

func TestLineComment(t *testing.T) {
	stmt := parseMust(t, "SELECT a -- comment\nFROM t")
	sel := stmt.(*SelectStmt)
	if sel.From == nil || sel.From.Table.Name != "t" {
		t.Fatal("expected FROM t after comment")
	}
}

func TestBlockComment(t *testing.T) {
	stmt := parseMust(t, "SELECT a /* block comment */ FROM t")
	sel := stmt.(*SelectStmt)
	if sel.From == nil || sel.From.Table.Name != "t" {
		t.Fatal("expected FROM t after block comment")
	}
}

func TestMultiLineComment(t *testing.T) {
	stmt := parseMust(t, "SELECT a /* multi\nline\ncomment */ FROM t")
	sel := stmt.(*SelectStmt)
	if sel.From == nil || sel.From.Table.Name != "t" {
		t.Fatal("expected FROM t after multi-line comment")
	}
}

// ============================================================================
// Quoted identifier tests
// ============================================================================

func TestQuotedIdentifier(t *testing.T) {
	stmt := parseMust(t, `SELECT "my column" FROM "my table"`)
	sel := stmt.(*SelectStmt)
	if sel.From.Table.Name != "my table" {
		t.Fatalf(`expected "my table", got %q`, sel.From.Table.Name)
	}
	cr, ok := sel.Columns[0].Expr.(ColumnRef)
	if !ok || cr.Column != "my column" {
		t.Fatalf(`expected "my column", got %T`, sel.Columns[0].Expr)
	}
}

// ============================================================================
// RowID expression tests
// ============================================================================

func TestRowIDExpr(t *testing.T) {
	stmt := parseMust(t, "SELECT rowid FROM t")
	sel := stmt.(*SelectStmt)
	if _, ok := sel.Columns[0].Expr.(RowIDExpr); !ok {
		t.Fatalf("expected RowIDExpr, got %T", sel.Columns[0].Expr)
	}
}

// ============================================================================
// String method tests
// ============================================================================

func TestLiteralExprString(t *testing.T) {
	tests := []struct {
		expr LiteralExpr
		want string
	}{
		{LiteralExpr{Type: DataTypeNull}, "NULL"},
		{LiteralExpr{Type: DataTypeInteger, IntVal: 42}, "42"},
		{LiteralExpr{Type: DataTypeFloat, FloatVal: 3.14}, "3.14"},
		{LiteralExpr{Type: DataTypeText, TextVal: "hello"}, "'hello'"},
		{LiteralExpr{Type: DataTypeText, TextVal: "it's"}, "'it''s'"},
	}
	for _, tt := range tests {
		got := tt.expr.String()
		if got != tt.want {
			t.Errorf("LiteralExpr{Type=%d}.String() = %q, want %q", tt.expr.Type, got, tt.want)
		}
	}
}

func TestColumnRefString(t *testing.T) {
	if got := (ColumnRef{Column: "a"}).String(); got != "a" {
		t.Errorf("got %q, want %q", got, "a")
	}
	if got := (ColumnRef{Table: "t", Column: "a"}).String(); got != "t.a" {
		t.Errorf("got %q, want %q", got, "t.a")
	}
}

func TestBinaryExprString(t *testing.T) {
	expr := BinaryExpr{
		Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 1},
		Op:    OpAdd,
		Right: LiteralExpr{Type: DataTypeInteger, IntVal: 2},
	}
	got := expr.String()
	want := "(1 + 2)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFunctionCallString(t *testing.T) {
	fc := FunctionCall{Name: "COUNT", Star: true}
	if got := fc.String(); got != "COUNT(*)" {
		t.Errorf("got %q, want %q", got, "COUNT(*)")
	}

	fc2 := FunctionCall{Name: "UPPER", Args: []Expr{ColumnRef{Column: "a"}}}
	if got := fc2.String(); got != "UPPER(a)" {
		t.Errorf("got %q, want %q", got, "UPPER(a)")
	}
}

// ============================================================================
// Complex real-world SQL tests
// ============================================================================

func TestComplexSelectWithJoinsAndAggregates(t *testing.T) {
	sql := `SELECT u.name, COUNT(o.id) AS order_count
		FROM users u
		INNER JOIN orders o ON u.id = o.user_id
		WHERE u.active = 1
		GROUP BY u.name
		HAVING COUNT(o.id) > 5
		ORDER BY order_count DESC
		LIMIT 10`
	stmt := parseMust(t, sql)
	sel := stmt.(*SelectStmt)
	if sel.Distinct {
		t.Fatal("should not be DISTINCT")
	}
	if len(sel.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sel.Columns))
	}
	if sel.Columns[1].As != "order_count" {
		t.Fatalf("expected alias order_count, got %q", sel.Columns[1].As)
	}
	if sel.From == nil {
		t.Fatal("expected FROM")
	}
	if len(sel.From.Joins) != 1 {
		t.Fatalf("expected 1 join, got %d", len(sel.From.Joins))
	}
	if len(sel.GroupBy) != 1 {
		t.Fatalf("expected 1 GROUP BY, got %d", len(sel.GroupBy))
	}
	if sel.Having == nil {
		t.Fatal("expected HAVING")
	}
	if len(sel.OrderBy) != 1 {
		t.Fatalf("expected 1 ORDER BY, got %d", len(sel.OrderBy))
	}
	if !sel.OrderBy[0].Desc {
		t.Fatal("expected DESC")
	}
}

func TestComplexWhereClause(t *testing.T) {
	sql := `SELECT a FROM t WHERE (x > 0 OR y IS NOT NULL) AND z NOT IN (1, 2, 3)`
	stmt := parseMust(t, sql)
	sel := stmt.(*SelectStmt)
	if sel.Where == nil {
		t.Fatal("expected WHERE")
	}
	and, ok := sel.Where.(BinaryExpr)
	if !ok || and.Op != OpAnd {
		t.Fatalf("expected AND at top, got %T", sel.Where)
	}
}

func TestSubqueryInWhere(t *testing.T) {
	sql := `SELECT a FROM t WHERE id IN (SELECT id FROM t2 WHERE active = 1)`
	stmt := parseMust(t, sql)
	sel := stmt.(*SelectStmt)
	in, ok := sel.Where.(InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", sel.Where)
	}
	if in.Select == nil {
		t.Fatal("expected subquery")
	}
	if in.Select.Where == nil {
		t.Fatal("expected WHERE in subquery")
	}
}

func TestInsertWithExpressions(t *testing.T) {
	sql := `INSERT INTO t (a, b) VALUES (1 + 2, UPPER('hello'))`
	stmt := parseMust(t, sql)
	ins := stmt.(*InsertStmt)
	if len(ins.Values) != 1 {
		t.Fatalf("expected 1 row, got %d", len(ins.Values))
	}
	if len(ins.Values[0]) != 2 {
		t.Fatalf("expected 2 values, got %d", len(ins.Values[0]))
	}
	// First value should be a binary expression
	if _, ok := ins.Values[0][0].(BinaryExpr); !ok {
		t.Fatalf("expected BinaryExpr, got %T", ins.Values[0][0])
	}
	// Second value should be a function call
	if _, ok := ins.Values[0][1].(FunctionCall); !ok {
		t.Fatalf("expected FunctionCall, got %T", ins.Values[0][1])
	}
}

func TestUpdateWithComplexExpr(t *testing.T) {
	sql := `UPDATE t SET count = count + 1, name = UPPER(name) WHERE id > 10`
	stmt := parseMust(t, sql)
	upd := stmt.(*UpdateStmt)
	if len(upd.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(upd.Sets))
	}
}

// ============================================================================
// VARCHAR type test
// ============================================================================

func TestCreateTableVarchar(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (name VARCHAR(255))")
	ct := stmt.(*CreateTableStmt)
	if ct.Columns[0].Type != "VARCHAR(255)" {
		t.Fatalf("expected VARCHAR(255), got %s", ct.Columns[0].Type)
	}
}

// ============================================================================
// Multiple statements test
// ============================================================================

func TestMultipleStatements(t *testing.T) {
	sql := `CREATE TABLE t (a INTEGER);
		INSERT INTO t VALUES (1);
		SELECT a FROM t;
		UPDATE t SET a = 2;
		DELETE FROM t WHERE a = 2;
		DROP TABLE t`
	p := NewParser(sql)
	stmts, err := p.ParseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 6 {
		t.Fatalf("expected 6 statements, got %d", len(stmts))
	}
	if _, ok := stmts[0].(*CreateTableStmt); !ok {
		t.Fatalf("stmt 0: expected CreateTableStmt, got %T", stmts[0])
	}
	if _, ok := stmts[1].(*InsertStmt); !ok {
		t.Fatalf("stmt 1: expected InsertStmt, got %T", stmts[1])
	}
	if _, ok := stmts[2].(*SelectStmt); !ok {
		t.Fatalf("stmt 2: expected SelectStmt, got %T", stmts[2])
	}
	if _, ok := stmts[3].(*UpdateStmt); !ok {
		t.Fatalf("stmt 3: expected UpdateStmt, got %T", stmts[3])
	}
	if _, ok := stmts[4].(*DeleteStmt); !ok {
		t.Fatalf("stmt 4: expected DeleteStmt, got %T", stmts[4])
	}
	if _, ok := stmts[5].(*DropTableStmt); !ok {
		t.Fatalf("stmt 5: expected DropTableStmt, got %T", stmts[5])
	}
}

// ============================================================================
// Whitespace tolerance tests
// ============================================================================

func TestExtraWhitespace(t *testing.T) {
	stmt := parseMust(t, "  SELECT   a  ,  b   FROM   t  ")
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sel.Columns))
	}
}

func TestNewlines(t *testing.T) {
	sql := "SELECT\n\ta,\n\tb\nFROM\n\tt\nWHERE\n\ta = 1"
	stmt := parseMust(t, sql)
	sel := stmt.(*SelectStmt)
	if len(sel.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sel.Columns))
	}
}

// ============================================================================
// Table with quoted identifier name
// ============================================================================

func TestQuotedTableName(t *testing.T) {
	stmt := parseMust(t, `CREATE TABLE "my table" (a INTEGER)`)
	ct := stmt.(*CreateTableStmt)
	if ct.Name != "my table" {
		t.Fatalf(`expected "my table", got %q`, ct.Name)
	}
}

// ============================================================================
// Expression in ORDER BY / LIMIT
// ============================================================================

func TestExpressionInLimit(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t LIMIT 10 + 5")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Limit.(BinaryExpr)
	if !ok || bin.Op != OpAdd {
		t.Fatalf("expected BinaryExpr + in LIMIT, got %T", sel.Limit)
	}
}

// ============================================================================
// CHECK constraint test
// ============================================================================

func TestCheckConstraint(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (age INTEGER CHECK(age >= 0))")
	ct := stmt.(*CreateTableStmt)
	hasCheck := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintCheck {
			hasCheck = true
		}
	}
	if !hasCheck {
		t.Fatal("expected CHECK constraint")
	}
}

// ============================================================================
// Default value expressions
// ============================================================================

func TestDefaultNullValue(t *testing.T) {
	stmt := parseMust(t, "CREATE TABLE t (a INTEGER DEFAULT NULL)")
	ct := stmt.(*CreateTableStmt)
	hasDefault := false
	for _, con := range ct.Columns[0].Constraints {
		if con.Type == ConstraintDefault {
			hasDefault = true
			if !literalNull(con.Value) {
				t.Fatalf("expected NULL default, got %v", con.Value)
			}
		}
	}
	if !hasDefault {
		t.Fatal("expected DEFAULT constraint")
	}
}

// ============================================================================
// CASE in SELECT column
// ============================================================================

func TestCaseAsColumn(t *testing.T) {
	sql := `SELECT CASE WHEN status = 1 THEN 'active' ELSE 'inactive' END AS status_text FROM users`
	stmt := parseMust(t, sql)
	sel := stmt.(*SelectStmt)
	if sel.Columns[0].As != "status_text" {
		t.Fatalf("expected alias status_text, got %q", sel.Columns[0].As)
	}
	_, ok := sel.Columns[0].Expr.(CaseExpr)
	if !ok {
		t.Fatalf("expected CaseExpr, got %T", sel.Columns[0].Expr)
	}
}

// ============================================================================
// Partial index WHERE clause
// ============================================================================

func TestPartialIndex(t *testing.T) {
	stmt := parseMust(t, "CREATE INDEX idx ON t(a) WHERE a IS NOT NULL")
	ci := stmt.(*CreateIndexStmt)
	if ci.Where == nil {
		t.Fatal("expected WHERE clause on index")
	}
	_, ok := ci.Where.(IsNullExpr)
	if !ok {
		t.Fatalf("expected IsNullExpr, got %T", ci.Where)
	}
}

// ============================================================================
// Nested function calls
// ============================================================================

func TestNestedFunctionCalls(t *testing.T) {
	stmt := parseMust(t, "SELECT UPPER(SUBSTR(name, 1, 5)) FROM t")
	sel := stmt.(*SelectStmt)
	fc, ok := sel.Columns[0].Expr.(FunctionCall)
	if !ok {
		t.Fatalf("expected FunctionCall, got %T", sel.Columns[0].Expr)
	}
	if fc.Name != "UPPER" {
		t.Fatalf("expected UPPER, got %s", fc.Name)
	}
	if len(fc.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(fc.Args))
	}
	inner, ok := fc.Args[0].(FunctionCall)
	if !ok {
		t.Fatalf("expected inner FunctionCall, got %T", fc.Args[0])
	}
	if inner.Name != "SUBSTR" {
		t.Fatalf("expected SUBSTR, got %s", inner.Name)
	}
}

// ============================================================================
// IN with mixed types
// ============================================================================

func TestInWithMixedTypes(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE a IN (1, 'two', 3.14)")
	sel := stmt.(*SelectStmt)
	in, ok := sel.Where.(InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", sel.Where)
	}
	if len(in.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(in.Values))
	}
	if !exprIsLiteral(in.Values[0], DataTypeInteger) {
		t.Fatalf("expected integer, got %T", in.Values[0])
	}
	if !exprIsLiteral(in.Values[1], DataTypeText) {
		t.Fatalf("expected text, got %T", in.Values[1])
	}
	if !exprIsLiteral(in.Values[2], DataTypeFloat) {
		t.Fatalf("expected float, got %T", in.Values[2])
	}
}

// ============================================================================
// Double equal sign (==) test
// ============================================================================

func TestDoubleEqual(t *testing.T) {
	stmt := parseMust(t, "SELECT a FROM t WHERE x == 1")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Where.(BinaryExpr)
	if !ok || bin.Op != OpEq {
		t.Fatalf("expected OpEq, got %T", sel.Where)
	}
}

// ============================================================================
// Negative test: division by expression
// ============================================================================

func TestArithmeticWithColumns(t *testing.T) {
	stmt := parseMust(t, "SELECT price * quantity FROM products")
	sel := stmt.(*SelectStmt)
	bin, ok := sel.Columns[0].Expr.(BinaryExpr)
	if !ok || bin.Op != OpMul {
		t.Fatalf("expected *, got %T", sel.Columns[0].Expr)
	}
}

// ============================================================================
// BETWEEN with AND not confused with logical AND
// ============================================================================

func TestBetweenWithAndPrecedence(t *testing.T) {
	// a BETWEEN 1 AND 10 AND b > 5
	// Should be: (a BETWEEN 1 AND 10) AND (b > 5)
	stmt := parseMust(t, "SELECT a FROM t WHERE a BETWEEN 1 AND 10 AND b > 5")
	sel := stmt.(*SelectStmt)
	and, ok := sel.Where.(BinaryExpr)
	if !ok || and.Op != OpAnd {
		t.Fatalf("expected AND at top, got %T", sel.Where)
	}
	left, ok := and.Left.(BetweenExpr)
	if !ok {
		t.Fatalf("expected BetweenExpr on left, got %T", and.Left)
	}
	lo, _ := literalInt(left.Low)
	hi, _ := literalInt(left.High)
	if lo != 1 || hi != 10 {
		t.Fatalf("expected BETWEEN 1 AND 10, got %d AND %d", lo, hi)
	}
}

// ============================================================================
// Multiple unary operators
// ============================================================================

func TestDoubleNegate(t *testing.T) {
	stmt := parseMust(t, "SELECT -(-5)")
	sel := stmt.(*SelectStmt)
	outer, ok := sel.Columns[0].Expr.(UnaryExpr)
	if !ok || outer.Op != OpNegate {
		t.Fatalf("expected outer OpNegate, got %T", sel.Columns[0].Expr)
	}
	inner, ok := outer.Expr.(ParenExpr)
	if !ok {
		t.Fatalf("expected ParenExpr, got %T", outer.Expr)
	}
	u2, ok := inner.Expr.(UnaryExpr)
	if !ok || u2.Op != OpNegate {
		t.Fatalf("expected inner OpNegate, got %T", inner.Expr)
	}
}

// ============================================================================
// Implicit alias that looks like keyword should work
// ============================================================================

func TestTableNameAsKeyword(t *testing.T) {
	// "order" is a keyword but should work as a table name
	stmt := parseMust(t, "SELECT a FROM \"order\"")
	sel := stmt.(*SelectStmt)
	if sel.From.Table.Name != "order" {
		t.Fatalf("expected table order, got %s", sel.From.Table.Name)
	}
}

// ============================================================================
// Unique index via CREATE UNIQUE INDEX
// ============================================================================

func TestCreateUniqueIndexExplicit(t *testing.T) {
	stmt := parseMust(t, "CREATE UNIQUE INDEX IF NOT EXISTS idx ON t(a DESC)")
	ci := stmt.(*CreateIndexStmt)
	if !ci.Unique {
		t.Fatal("expected UNIQUE")
	}
	if !ci.IfNotExists {
		t.Fatal("expected IF NOT EXISTS")
	}
	if !ci.Columns[0].Desc {
		t.Fatal("expected DESC")
	}
}

// ============================================================================
// Expression string method coverage
// ============================================================================

func TestIsNullExprString(t *testing.T) {
	e := IsNullExpr{Expr: ColumnRef{Column: "a"}, Negate: false}
	if got := e.String(); got != "(a IS NULL)" {
		t.Errorf("got %q, want %q", got, "(a IS NULL)")
	}
	e2 := IsNullExpr{Expr: ColumnRef{Column: "a"}, Negate: true}
	if got := e2.String(); got != "(a IS NOT NULL)" {
		t.Errorf("got %q, want %q", got, "(a IS NOT NULL)")
	}
}

func TestBetweenExprString(t *testing.T) {
	e := BetweenExpr{
		Expr:   ColumnRef{Column: "a"},
		Low:    LiteralExpr{Type: DataTypeInteger, IntVal: 1},
		High:   LiteralExpr{Type: DataTypeInteger, IntVal: 10},
		Negate: false,
	}
	if got := e.String(); got != "(a BETWEEN 1 AND 10)" {
		t.Errorf("got %q, want %q", got, "(a BETWEEN 1 AND 10)")
	}
}

func TestInExprString(t *testing.T) {
	e := InExpr{
		Expr: ColumnRef{Column: "a"},
		Values: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 1},
			LiteralExpr{Type: DataTypeInteger, IntVal: 2},
		},
	}
	if got := e.String(); got != "(a IN (1, 2))" {
		t.Errorf("got %q, want %q", got, "(a IN (1, 2))")
	}
}

func TestLikeExprString(t *testing.T) {
	e := LikeExpr{
		Expr:    ColumnRef{Column: "a"},
		Pattern: LiteralExpr{Type: DataTypeText, TextVal: "%test%"},
		Op:      LikeLike,
	}
	if got := e.String(); got != "(a LIKE '%test%')" {
		t.Errorf("got %q, want %q", got, "(a LIKE '%test%')")
	}
}

func TestParenExprString(t *testing.T) {
	e := ParenExpr{Expr: BinaryExpr{
		Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 1},
		Op:    OpAdd,
		Right: LiteralExpr{Type: DataTypeInteger, IntVal: 2},
	}}
	if got := e.String(); got != "((1 + 2))" {
		t.Errorf("got %q, want %q", got, "((1 + 2))")
	}
}

func TestCaseExprString(t *testing.T) {
	e := CaseExpr{
		Whens: []CaseWhen{
			{
				Condition: BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpEq, Right: LiteralExpr{Type: DataTypeInteger, IntVal: 1}},
				Result:    LiteralExpr{Type: DataTypeText, TextVal: "one"},
			},
		},
		Else: LiteralExpr{Type: DataTypeText, TextVal: "other"},
	}
	got := e.String()
	// BinaryExpr.String() wraps in parens, so condition is (a = 1)
	expected := "CASE WHEN (a = 1) THEN 'one' ELSE 'other' END"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestCastExprString(t *testing.T) {
	e := CastExpr{Expr: ColumnRef{Column: "a"}, Type: "INTEGER"}
	if got := e.String(); got != "CAST(a AS INTEGER)" {
		t.Errorf("got %q, want %q", got, "CAST(a AS INTEGER)")
	}
}

func TestUnaryExprString(t *testing.T) {
	e := UnaryExpr{Op: OpNegate, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 5}}
	if got := e.String(); got != "-5" {
		t.Errorf("got %q, want %q", got, "-5")
	}
	e2 := UnaryExpr{Op: OpNot, Expr: ColumnRef{Column: "a"}}
	if got := e2.String(); got != "NOT a" {
		t.Errorf("got %q, want %q", got, "NOT a")
	}
	e3 := UnaryExpr{Op: OpBitNot, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 5}}
	if got := e3.String(); got != "~5" {
		t.Errorf("got %q, want %q", got, "~5")
	}
}

// ============================================================================
// INSERT with DEFAULT VALUES is not supported yet, but basic insert should work
// ============================================================================

func TestInsertWithDefaultValues(t *testing.T) {
	// We don't support DEFAULT VALUES yet, but the parser should handle basic inserts
	stmt := parseMust(t, "INSERT INTO t (a, b) VALUES (DEFAULT, 1)")
	ins := stmt.(*InsertStmt)
	if len(ins.Values) != 1 {
		t.Fatalf("expected 1 row, got %d", len(ins.Values))
	}
}

// ============================================================================
// Backtick identifiers
// ============================================================================

func TestBacktickIdentifiers(t *testing.T) {
	stmt := parseMust(t, "SELECT `a` FROM `my table`")
	sel := stmt.(*SelectStmt)
	if sel.From.Table.Name != "my table" {
		t.Fatalf("expected 'my table', got %q", sel.From.Table.Name)
	}
}

// ============================================================================
// Scientific notation float
// ============================================================================

func TestScientificNotation(t *testing.T) {
	stmt := parseMust(t, "SELECT 1.5e10")
	sel := stmt.(*SelectStmt)
	f, ok := literalFloat(sel.Columns[0].Expr)
	if !ok {
		t.Fatalf("expected float, got %T", sel.Columns[0].Expr)
	}
	if f != 1.5e10 {
		t.Fatalf("expected 1.5e10, got %f", f)
	}
}
