package sqlite

// ============================================================================
// AST nodes for SQL statements
// ============================================================================

// Statement represents a parsed SQL statement.
type Statement interface {
	statementNode()
}

// ============================================================================
// SELECT
// ============================================================================

// SelectStmt represents a SELECT statement.
type SelectStmt struct {
	Distinct bool
	Columns  []SelectColumn // nil means SELECT *
	From     *FromClause
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []OrderItem
	Limit    Expr
	Offset   Expr
}

func (s *SelectStmt) statementNode() {}

// SetOp represents a set operation type.
type SetOp int

const (
	SetOpUnion SetOp = iota
	SetOpUnionAll
	SetOpIntersect
	SetOpExcept
)

// CompoundSelect represents a compound SELECT (UNION, INTERSECT, EXCEPT).
type CompoundSelect struct {
	Left    Statement // SelectStmt or CompoundSelect
	Right   Statement // SelectStmt or CompoundSelect
	Op      SetOp
	OrderBy []OrderItem
	Limit   Expr
	Offset  Expr
}

func (s *CompoundSelect) statementNode() {}

// SelectColumn represents a column (or expression) in a SELECT list.
type SelectColumn struct {
	Expr Expr
	As   string // Alias, empty if none
}

// StarColumn is a sentinel Expr meaning "all columns" (SELECT *).
type StarColumn struct{}

func (StarColumn) exprNode() {}

// FromClause represents the FROM clause of a SELECT.
type FromClause struct {
	Table *TableRef
	Joins []JoinClause
}

// TableRef represents a table name (optionally schema-qualified and aliased).
type TableRef struct {
	Schema string
	Name   string
	As     string // Alias
}

// JoinClause represents a JOIN.
type JoinClause struct {
	Type  JoinType
	Table TableRef
	On    Expr
	Using []string
}

// JoinType represents the type of JOIN.
type JoinType int

const (
	JoinInner JoinType = iota
	JoinLeft
	JoinRight
	JoinFull
	JoinCross
)

// OrderItem represents an item in an ORDER BY clause.
type OrderItem struct {
	Expr Expr
	Desc bool // true for DESC
}

// ============================================================================
// INSERT
// ============================================================================

// InsertStmt represents an INSERT statement.
type InsertStmt struct {
	Table     TableRef
	Columns   []string    // Column names, empty means all columns in order
	Values    [][]Expr    // Multiple rows of values
	Select    *SelectStmt // INSERT ... SELECT ...
	OrIgnore  bool
	OrReplace bool
	Conflict  *InsertConflict
	Returning []string
}

func (s *InsertStmt) statementNode() {}

type InsertConflict struct {
	Target    []string
	DoNothing bool
	Updates   []SetClause
}

// ============================================================================
// UPDATE
// ============================================================================

// UpdateStmt represents an UPDATE statement.
type UpdateStmt struct {
	Table     TableRef
	Sets      []SetClause
	Where     Expr
	Returning []string
}

func (s *UpdateStmt) statementNode() {}

// SetClause represents a SET assignment in an UPDATE.
type SetClause struct {
	Column string
	Expr   Expr
}

// ============================================================================
// DELETE
// ============================================================================

// DeleteStmt represents a DELETE statement.
type DeleteStmt struct {
	Table TableRef
	Where Expr
}

func (s *DeleteStmt) statementNode() {}

// ============================================================================
// CREATE TABLE
// ============================================================================

// CreateTableStmt represents a CREATE TABLE statement.
type CreateTableStmt struct {
	IfNotExists      bool
	Name             string
	Columns          []ColumnDefAST
	TableConstraints []TableConstraint
}

func (s *CreateTableStmt) statementNode() {}

// ColumnDefAST is the AST version of a column definition (before resolution).

type TableConstraint struct {
	Type    ConstraintType
	Columns []string
}
type ColumnDefAST struct {
	Name        string
	Type        string
	Constraints []ColumnConstraint
}

// ColumnConstraint represents a constraint on a column.
type ColumnConstraint struct {
	Type     ConstraintType
	Name     string   // Named constraint
	Value    Expr     // For DEFAULT, CHECK
	Collate  string   // For COLLATE
	RefTable string   // For REFERENCES
	RefCols  []string // For REFERENCES
}

// ConstraintType represents the type of column constraint.
type ConstraintType int

const (
	ConstraintPrimaryKey ConstraintType = iota
	ConstraintNotNull
	ConstraintUnique
	ConstraintDefault
	ConstraintCheck
	ConstraintForeignKey
)

// ============================================================================
// CREATE INDEX
// ============================================================================

// CreateIndexStmt represents a CREATE INDEX statement.
type CreateIndexStmt struct {
	IfNotExists bool
	Name        string
	Table       string
	Columns     []IndexedColumn
	Unique      bool
	Where       Expr // Partial index
}

func (s *CreateIndexStmt) statementNode() {}

// IndexedColumn represents a column in a CREATE INDEX.
type IndexedColumn struct {
	Name    string
	Collate string
	Desc    bool
}

// ============================================================================
// DROP TABLE / DROP INDEX
// ============================================================================

// DropTableStmt represents a DROP TABLE statement.
type DropTableStmt struct {
	IfExists bool
	Name     string
}

func (s *DropTableStmt) statementNode() {}

// DropIndexStmt represents a DROP INDEX statement.
type DropIndexStmt struct {
	IfExists bool
	Name     string
}

func (s *DropIndexStmt) statementNode() {}

// AlterAddColumnStmt represents ALTER TABLE ... ADD COLUMN ...
type AlterAddColumnStmt struct {
	Table  string
	Column ColumnDefAST
}

func (s *AlterAddColumnStmt) statementNode() {}

// AlterRenameTableStmt represents ALTER TABLE ... RENAME TO ...
type AlterRenameTableStmt struct {
	OldName string
	NewName string
}

func (s *AlterRenameTableStmt) statementNode() {}

// AlterRenameColumnStmt represents ALTER TABLE ... RENAME COLUMN ... TO ...
type AlterRenameColumnStmt struct {
	Table   string
	OldName string
	NewName string
}

func (s *AlterRenameColumnStmt) statementNode() {}

// PragmaStmt represents a PRAGMA statement.
type PragmaStmt struct {
	Name  string
	Value Expr // nil for read, non-nil for set
}

func (s *PragmaStmt) statementNode() {}

type VacuumStmt struct{}

func (s *VacuumStmt) statementNode() {}

type ReindexStmt struct {
	TableName string // empty for all tables
}

func (s *ReindexStmt) statementNode() {}

type CreateViewStmt struct {
	Name string
	As   Statement // The SELECT statement
	SQL  string
}

func (s *CreateViewStmt) statementNode() {}

type DropViewStmt struct {
	Name     string
	IfExists bool
}

func (s *DropViewStmt) statementNode() {}

// ============================================================================
// Transaction statements
// ============================================================================

// BeginStmt represents a BEGIN [TRANSACTION] statement.
type BeginStmt struct{}

func (s *BeginStmt) statementNode() {}

// CommitStmt represents a COMMIT [TRANSACTION] statement.
type CommitStmt struct{}

func (s *CommitStmt) statementNode() {}

// RollbackStmt represents a ROLLBACK [TRANSACTION] statement.
type RollbackStmt struct{}

func (s *RollbackStmt) statementNode() {}

// ============================================================================
// Expressions
// ============================================================================

// Expr represents an expression in a SQL statement.
type Expr interface {
	exprNode()
}

// LiteralExpr represents a literal value (integer, float, string, NULL, blob).
type LiteralExpr struct {
	Type     DataType
	IntVal   int64
	FloatVal float64
	TextVal  string
	BlobVal  []byte
}

func (LiteralExpr) exprNode() {}

// ColumnRef represents a reference to a column (optionally table-qualified).
type ColumnRef struct {
	Table  string // Empty if not qualified
	Column string
}

func (ColumnRef) exprNode() {}

// BinaryExpr represents a binary operator expression.
type BinaryExpr struct {
	Left  Expr
	Op    BinaryOp
	Right Expr
}

func (BinaryExpr) exprNode() {}

// BinaryOp represents a binary operator.
type BinaryOp int

const (
	OpAdd        BinaryOp = iota // +
	OpSub                        // -
	OpMul                        // *
	OpDiv                        // /
	OpMod                        // %
	OpConcat                     // ||
	OpEq                         // = or ==
	OpNe                         // != or <>
	OpLt                         // <
	OpLe                         // <=
	OpGt                         // >
	OpGe                         // >=
	OpAnd                        // AND
	OpOr                         // OR
	OpBitAnd                     // &
	OpBitOr                      // |
	OpShiftLeft                  // <<
	OpShiftRight                 // >>
)

// UnaryExpr represents a unary operator expression.
type UnaryExpr struct {
	Op   UnaryOp
	Expr Expr
}

func (UnaryExpr) exprNode() {}

// UnaryOp represents a unary operator.
type UnaryOp int

const (
	OpNegate UnaryOp = iota // -
	OpBitNot                // ~
	OpNot                   // NOT
)

// FunctionCall represents a function call.
type FunctionCall struct {
	Name     string
	Args     []Expr
	Distinct bool
	Star     bool // COUNT(*)
}

func (FunctionCall) exprNode() {}

// IsNullExpr represents IS NULL / IS NOT NULL.
type IsNullExpr struct {
	Expr   Expr
	Negate bool // IS NOT NULL
}

func (IsNullExpr) exprNode() {}

// BetweenExpr represents BETWEEN ... AND ...
type BetweenExpr struct {
	Expr   Expr
	Low    Expr
	High   Expr
	Negate bool
}

func (BetweenExpr) exprNode() {}

// InExpr represents IN (...) or IN (SELECT ...).
type InExpr struct {
	Expr   Expr
	Values []Expr      // Explicit list
	Select *SelectStmt // Subquery
	Negate bool
}

func (InExpr) exprNode() {}

// LikeExpr represents LIKE / GLOB.
type LikeExpr struct {
	Expr    Expr
	Pattern Expr
	Escape  Expr
	Op      LikeOp
	Negate  bool
}

func (LikeExpr) exprNode() {}

// LikeOp represents LIKE or GLOB.
type LikeOp int

const (
	LikeLike LikeOp = iota
	LikeGlob
)

// ParenExpr represents a parenthesized expression.
type ParenExpr struct {
	Expr Expr
}

func (ParenExpr) exprNode() {}

// CaseExpr represents a CASE expression.
type CaseExpr struct {
	Operand Expr // Optional
	Whens   []CaseWhen
	Else    Expr
}

func (CaseExpr) exprNode() {}

// CaseWhen represents one WHEN ... THEN ... clause.
type CaseWhen struct {
	Condition Expr
	Result    Expr
}

// CastExpr represents a CAST expression.
type CastExpr struct {
	Expr Expr
	Type string
}

func (CastExpr) exprNode() {}

// RowIDExpr represents the implicit rowid column.
type RowIDExpr struct{}

func (RowIDExpr) exprNode() {}

// ParamExpr represents a parameter placeholder (? or $name).
type ParamExpr struct {
	Name  string // empty for positional (?)
	Index int    // 1-based position for positional params
}

func (ParamExpr) exprNode() {}

// SubqueryExpr represents a scalar subquery used as an expression: (SELECT ...)
type SubqueryExpr struct {
	Select *SelectStmt
}

func (SubqueryExpr) exprNode() {}
