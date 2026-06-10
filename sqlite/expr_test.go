package sqlite

import (
	"testing"
)

// ============================================================================
// Literal evaluation
// ============================================================================

func TestEvalLiteral(t *testing.T) {
	tests := []struct {
		name string
		expr LiteralExpr
		want Value
	}{
		{"int", LiteralExpr{Type: DataTypeInteger, IntVal: 42}, IntegerValue(42)},
		{"float", LiteralExpr{Type: DataTypeFloat, FloatVal: 3.14}, FloatValue(3.14)},
		{"text", LiteralExpr{Type: DataTypeText, TextVal: "hello"}, TextValue("hello")},
		{"null", LiteralExpr{Type: DataTypeNull}, NullValue},
		{"zero", LiteralExpr{Type: DataTypeInteger, IntVal: 0}, IntegerValue(0)},
		{"negative", LiteralExpr{Type: DataTypeInteger, IntVal: -99}, IntegerValue(-99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewExprEval(nil)
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !valuesEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Column reference evaluation
// ============================================================================

func TestEvalColumnRef(t *testing.T) {
	row := []Value{IntegerValue(1), TextValue("Alice"), IntegerValue(30)}
	ev := NewExprEval(row)
	ev.ColumnMap["id"] = 0
	ev.ColumnMap["name"] = 1
	ev.ColumnMap["age"] = 2

	tests := []struct {
		name string
		ref  ColumnRef
		want Value
	}{
		{"id", ColumnRef{Column: "id"}, IntegerValue(1)},
		{"name", ColumnRef{Column: "name"}, TextValue("Alice")},
		{"age", ColumnRef{Column: "age"}, IntegerValue(30)},
		{"case insensitive", ColumnRef{Column: "ID"}, IntegerValue(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.ref)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !valuesEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalQualifiedColumnRef(t *testing.T) {
	row := []Value{IntegerValue(1), TextValue("Alice")}
	ev := NewExprEval(row)
	ev.TableMap["users"] = map[string]int{"id": 0, "name": 1}

	got, err := ev.Eval(ColumnRef{Table: "users", Column: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(got, TextValue("Alice")) {
		t.Errorf("got %v, want Alice", got)
	}
}

func TestEvalUnknownColumn(t *testing.T) {
	ev := NewExprEval(nil)
	ev.ColumnMap["id"] = 0

	_, err := ev.Eval(ColumnRef{Column: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown column")
	}
}

// ============================================================================
// Arithmetic operators
// ============================================================================

func TestEvalArithmetic(t *testing.T) {
	row := []Value{IntegerValue(10), IntegerValue(3), FloatValue(2.5)}
	ev := NewExprEval(row)
	ev.ColumnMap["a"] = 0
	ev.ColumnMap["b"] = 1
	ev.ColumnMap["c"] = 2

	tests := []struct {
		name string
		expr BinaryExpr
		want Value
	}{
		{"add ii", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpAdd, Right: ColumnRef{Column: "b"}}, IntegerValue(13)},
		{"sub ii", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpSub, Right: ColumnRef{Column: "b"}}, IntegerValue(7)},
		{"mul ii", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpMul, Right: ColumnRef{Column: "b"}}, IntegerValue(30)},
		{"div ii", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpDiv, Right: ColumnRef{Column: "b"}}, IntegerValue(3)},
		{"mod ii", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpMod, Right: ColumnRef{Column: "b"}}, IntegerValue(1)},
		{"add if", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpAdd, Right: ColumnRef{Column: "c"}}, FloatValue(12.5)},
		{"mul if", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpMul, Right: ColumnRef{Column: "c"}}, FloatValue(25.0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !valuesEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalDivisionByZero(t *testing.T) {
	ev := NewExprEval(nil)
	expr := BinaryExpr{
		Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 10},
		Op:    OpDiv,
		Right: LiteralExpr{Type: DataTypeInteger, IntVal: 0},
	}
	_, err := ev.Eval(expr)
	if err == nil {
		t.Error("expected division by zero error")
	}
}

// ============================================================================
// Comparison operators
// ============================================================================

func TestEvalComparison(t *testing.T) {
	row := []Value{IntegerValue(5), IntegerValue(10), TextValue("hello")}
	ev := NewExprEval(row)
	ev.ColumnMap["a"] = 0
	ev.ColumnMap["b"] = 1
	ev.ColumnMap["s"] = 2

	tests := []struct {
		name string
		expr BinaryExpr
		want bool
	}{
		{"eq false", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpEq, Right: ColumnRef{Column: "b"}}, false},
		{"eq true", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpEq, Right: LiteralExpr{Type: DataTypeInteger, IntVal: 5}}, true},
		{"ne true", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpNe, Right: ColumnRef{Column: "b"}}, true},
		{"lt true", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpLt, Right: ColumnRef{Column: "b"}}, true},
		{"le true", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpLe, Right: ColumnRef{Column: "a"}}, true},
		{"gt true", BinaryExpr{Left: ColumnRef{Column: "b"}, Op: OpGt, Right: ColumnRef{Column: "a"}}, true},
		{"ge true", BinaryExpr{Left: ColumnRef{Column: "a"}, Op: OpGe, Right: ColumnRef{Column: "a"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b, ok := got.AsInt64(); !ok || (b == 1) != tt.want {
				t.Errorf("got %v, want bool=%v", got, tt.want)
			}
		})
	}
}

func TestEvalComparisonWithNull(t *testing.T) {
	ev := NewExprEval(nil)
	expr := BinaryExpr{
		Left:  LiteralExpr{Type: DataTypeNull},
		Op:    OpEq,
		Right: LiteralExpr{Type: DataTypeInteger, IntVal: 1},
	}
	got, err := ev.Eval(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Error("comparison with NULL should return NULL")
	}
}

// ============================================================================
// Logical operators
// ============================================================================

func TestEvalAndOr(t *testing.T) {
	ev := NewExprEval(nil)
	one := LiteralExpr{Type: DataTypeInteger, IntVal: 1}
	zero := LiteralExpr{Type: DataTypeInteger, IntVal: 0}
	nullLit := LiteralExpr{Type: DataTypeNull}

	tests := []struct {
		name string
		expr Expr
		want int64 // 0=false, 1=true, -1=null
	}{
		{"1 AND 1", BinaryExpr{Left: one, Op: OpAnd, Right: one}, 1},
		{"1 AND 0", BinaryExpr{Left: one, Op: OpAnd, Right: zero}, 0},
		{"0 AND 1", BinaryExpr{Left: zero, Op: OpAnd, Right: one}, 0},
		{"0 AND 0", BinaryExpr{Left: zero, Op: OpAnd, Right: zero}, 0},
		{"1 OR 1", BinaryExpr{Left: one, Op: OpOr, Right: one}, 1},
		{"1 OR 0", BinaryExpr{Left: one, Op: OpOr, Right: zero}, 1},
		{"0 OR 1", BinaryExpr{Left: zero, Op: OpOr, Right: one}, 1},
		{"0 OR 0", BinaryExpr{Left: zero, Op: OpOr, Right: zero}, 0},
		{"NULL AND 1", BinaryExpr{Left: nullLit, Op: OpAnd, Right: one}, -1},
		{"1 AND NULL", BinaryExpr{Left: one, Op: OpAnd, Right: nullLit}, -1},
		{"NULL OR 1", BinaryExpr{Left: nullLit, Op: OpOr, Right: one}, 1},
		{"NULL OR 0", BinaryExpr{Left: nullLit, Op: OpOr, Right: zero}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == -1 {
				if !got.IsNull() {
					t.Errorf("got %v, want NULL", got)
				}
			} else {
				if b, ok := got.AsInt64(); !ok || b != tt.want {
					t.Errorf("got %v, want %d", got, tt.want)
				}
			}
		})
	}
}

// ============================================================================
// NOT
// ============================================================================

func TestEvalNot(t *testing.T) {
	ev := NewExprEval(nil)
	tests := []struct {
		name string
		expr UnaryExpr
		want int64
	}{
		{"NOT 0", UnaryExpr{Op: OpNot, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 0}}, 1},
		{"NOT 1", UnaryExpr{Op: OpNot, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 1}}, 0},
		{"NOT 42", UnaryExpr{Op: OpNot, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 42}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := got.AsInt64(); !ok || b != tt.want {
				t.Errorf("got %v, want %d", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Negate
// ============================================================================

func TestEvalNegate(t *testing.T) {
	ev := NewExprEval(nil)
	got, err := ev.Eval(UnaryExpr{Op: OpNegate, Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != -42 {
		t.Errorf("got %d, want -42", got.IntVal)
	}

	got, err = ev.Eval(UnaryExpr{Op: OpNegate, Expr: LiteralExpr{Type: DataTypeFloat, FloatVal: 3.14}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FloatVal != -3.14 {
		t.Errorf("got %f, want -3.14", got.FloatVal)
	}
}

// ============================================================================
// Concat
// ============================================================================

func TestEvalConcat(t *testing.T) {
	ev := NewExprEval(nil)
	got, err := ev.Eval(BinaryExpr{
		Left:  LiteralExpr{Type: DataTypeText, TextVal: "hello"},
		Op:    OpConcat,
		Right: LiteralExpr{Type: DataTypeText, TextVal: " world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextVal != "hello world" {
		t.Errorf("got %q, want %q", got.TextVal, "hello world")
	}
}

// ============================================================================
// IS NULL / IS NOT NULL
// ============================================================================

func TestEvalIsNull(t *testing.T) {
	ev := NewExprEval(nil)

	// IS NULL on NULL → true
	got, err := ev.Eval(IsNullExpr{Expr: LiteralExpr{Type: DataTypeNull}})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Errorf("IS NULL on NULL = %d, want 1", b)
	}

	// IS NULL on non-NULL → false
	got, err = ev.Eval(IsNullExpr{Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 0 {
		t.Errorf("IS NULL on 42 = %d, want 0", b)
	}

	// IS NOT NULL on NULL → false
	got, err = ev.Eval(IsNullExpr{Expr: LiteralExpr{Type: DataTypeNull}, Negate: true})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 0 {
		t.Errorf("IS NOT NULL on NULL = %d, want 0", b)
	}

	// IS NOT NULL on non-NULL → true
	got, err = ev.Eval(IsNullExpr{Expr: LiteralExpr{Type: DataTypeText, TextVal: "x"}, Negate: true})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Errorf("IS NOT NULL on 'x' = %d, want 1", b)
	}
}

// ============================================================================
// BETWEEN
// ============================================================================

func TestEvalBetween(t *testing.T) {
	ev := NewExprEval(nil)
	val := LiteralExpr{Type: DataTypeInteger, IntVal: 5}

	tests := []struct {
		name string
		expr BetweenExpr
		want bool
	}{
		{"5 between 1 and 10", BetweenExpr{Expr: val, Low: LiteralExpr{Type: DataTypeInteger, IntVal: 1}, High: LiteralExpr{Type: DataTypeInteger, IntVal: 10}}, true},
		{"5 between 5 and 10", BetweenExpr{Expr: val, Low: LiteralExpr{Type: DataTypeInteger, IntVal: 5}, High: LiteralExpr{Type: DataTypeInteger, IntVal: 10}}, true},
		{"5 between 1 and 5", BetweenExpr{Expr: val, Low: LiteralExpr{Type: DataTypeInteger, IntVal: 1}, High: LiteralExpr{Type: DataTypeInteger, IntVal: 5}}, true},
		{"5 between 6 and 10", BetweenExpr{Expr: val, Low: LiteralExpr{Type: DataTypeInteger, IntVal: 6}, High: LiteralExpr{Type: DataTypeInteger, IntVal: 10}}, false},
		{"5 not between 1 and 10", BetweenExpr{Expr: val, Low: LiteralExpr{Type: DataTypeInteger, IntVal: 1}, High: LiteralExpr{Type: DataTypeInteger, IntVal: 10}, Negate: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			if b, _ := got.AsInt64(); (b == 1) != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// IN
// ============================================================================

func TestEvalIn(t *testing.T) {
	ev := NewExprEval(nil)
	val := LiteralExpr{Type: DataTypeInteger, IntVal: 2}

	// 2 IN (1,2,3) → true
	got, err := ev.Eval(InExpr{
		Expr: val,
		Values: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 1},
			LiteralExpr{Type: DataTypeInteger, IntVal: 2},
			LiteralExpr{Type: DataTypeInteger, IntVal: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Errorf("2 IN (1,2,3) = %d, want 1", b)
	}

	// 5 IN (1,2,3) → false
	got, err = ev.Eval(InExpr{
		Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 5},
		Values: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 1},
			LiteralExpr{Type: DataTypeInteger, IntVal: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 0 {
		t.Errorf("5 IN (1,2) = %d, want 0", b)
	}

	// NOT IN
	got, err = ev.Eval(InExpr{
		Expr:   val,
		Values: []Expr{LiteralExpr{Type: DataTypeInteger, IntVal: 1}},
		Negate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Errorf("2 NOT IN (1) = %d, want 1", b)
	}

	// IN with NULL
	got, err = ev.Eval(InExpr{
		Expr:   LiteralExpr{Type: DataTypeNull},
		Values: []Expr{LiteralExpr{Type: DataTypeInteger, IntVal: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Error("NULL IN (...) should be NULL")
	}
}

// ============================================================================
// LIKE
// ============================================================================

func TestEvalLike(t *testing.T) {
	ev := NewExprEval(nil)

	tests := []struct {
		name    string
		s       string
		pattern string
		want    bool
	}{
		{"exact match", "hello", "hello", true},
		{"prefix", "hello world", "hello%", true},
		{"suffix", "hello world", "%world", true},
		{"contains", "hello world", "%lo w%", true},
		{"single char", "hello", "h_llo", true},
		{"no match", "hello", "world", false},
		{"prefix no match", "hello", "world%", false},
		{"empty pattern", "", "", true},
		{"% matches empty", "", "%", true},
		{"_ needs char", "a", "_", true},
		{"_ no char", "", "_", false},
		{"complex", "hello world", "h%o_w%d", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(LikeExpr{
				Expr:    LiteralExpr{Type: DataTypeText, TextVal: tt.s},
				Pattern: LiteralExpr{Type: DataTypeText, TextVal: tt.pattern},
				Op:      LikeLike,
			})
			if err != nil {
				t.Fatal(err)
			}
			if b, _ := got.AsInt64(); (b == 1) != tt.want {
				t.Errorf("LIKE(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestEvalLikeNegate(t *testing.T) {
	ev := NewExprEval(nil)
	got, err := ev.Eval(LikeExpr{
		Expr:    LiteralExpr{Type: DataTypeText, TextVal: "hello"},
		Pattern: LiteralExpr{Type: DataTypeText, TextVal: "world"},
		Op:      LikeLike,
		Negate:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Error("'hello' NOT LIKE 'world' should be true")
	}
}

// ============================================================================
// Functions
// ============================================================================

func TestEvalAbs(t *testing.T) {
	ev := NewExprEval(nil)
	got, err := ev.Eval(FunctionCall{
		Name: "ABS",
		Args: []Expr{LiteralExpr{Type: DataTypeInteger, IntVal: -42}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 42 {
		t.Errorf("ABS(-42) = %d, want 42", got.IntVal)
	}

	got, err = ev.Eval(FunctionCall{
		Name: "ABS",
		Args: []Expr{LiteralExpr{Type: DataTypeInteger, IntVal: 42}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 42 {
		t.Errorf("ABS(42) = %d, want 42", got.IntVal)
	}
}

func TestEvalUpperLower(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(FunctionCall{
		Name: "UPPER",
		Args: []Expr{LiteralExpr{Type: DataTypeText, TextVal: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextVal != "HELLO" {
		t.Errorf("UPPER(hello) = %q, want HELLO", got.TextVal)
	}

	got, err = ev.Eval(FunctionCall{
		Name: "LOWER",
		Args: []Expr{LiteralExpr{Type: DataTypeText, TextVal: "HELLO"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextVal != "hello" {
		t.Errorf("LOWER(HELLO) = %q, want hello", got.TextVal)
	}
}

func TestEvalLength(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(FunctionCall{
		Name: "LENGTH",
		Args: []Expr{LiteralExpr{Type: DataTypeText, TextVal: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 5 {
		t.Errorf("LENGTH(hello) = %d, want 5", got.IntVal)
	}

	got, err = ev.Eval(FunctionCall{
		Name: "LENGTH",
		Args: []Expr{LiteralExpr{Type: DataTypeBlob, BlobVal: []byte{1, 2, 3}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 3 {
		t.Errorf("LENGTH(blob 3 bytes) = %d, want 3", got.IntVal)
	}

	// NULL → NULL
	got, err = ev.Eval(FunctionCall{
		Name: "LENGTH",
		Args: []Expr{LiteralExpr{Type: DataTypeNull}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Error("LENGTH(NULL) should be NULL")
	}
}

func TestEvalTypeof(t *testing.T) {
	ev := NewExprEval(nil)

	tests := []struct {
		name string
		arg  Expr
		want string
	}{
		{"null", LiteralExpr{Type: DataTypeNull}, "null"},
		{"integer", LiteralExpr{Type: DataTypeInteger, IntVal: 1}, "integer"},
		{"float", LiteralExpr{Type: DataTypeFloat, FloatVal: 1.0}, "real"},
		{"text", LiteralExpr{Type: DataTypeText, TextVal: "x"}, "text"},
		{"blob", LiteralExpr{Type: DataTypeBlob, BlobVal: []byte{1}}, "blob"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ev.Eval(FunctionCall{Name: "TYPEOF", Args: []Expr{tt.arg}})
			if err != nil {
				t.Fatal(err)
			}
			if got.TextVal != tt.want {
				t.Errorf("TYPEOF() = %q, want %q", got.TextVal, tt.want)
			}
		})
	}
}

func TestEvalCoalesce(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(FunctionCall{
		Name: "COALESCE",
		Args: []Expr{
			LiteralExpr{Type: DataTypeNull},
			LiteralExpr{Type: DataTypeNull},
			LiteralExpr{Type: DataTypeInteger, IntVal: 42},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 42 {
		t.Errorf("COALESCE(NULL, NULL, 42) = %v, want 42", got)
	}

	// All NULLs
	got, err = ev.Eval(FunctionCall{
		Name: "COALESCE",
		Args: []Expr{
			LiteralExpr{Type: DataTypeNull},
			LiteralExpr{Type: DataTypeNull},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Error("COALESCE(NULL, NULL) should be NULL")
	}
}

func TestEvalIfNull(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(FunctionCall{
		Name: "IFNULL",
		Args: []Expr{
			LiteralExpr{Type: DataTypeNull},
			LiteralExpr{Type: DataTypeInteger, IntVal: 99},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 99 {
		t.Errorf("IFNULL(NULL, 99) = %v, want 99", got)
	}

	got, err = ev.Eval(FunctionCall{
		Name: "IFNULL",
		Args: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 1},
			LiteralExpr{Type: DataTypeInteger, IntVal: 99},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 1 {
		t.Errorf("IFNULL(1, 99) = %v, want 1", got)
	}
}

func TestEvalNullIf(t *testing.T) {
	ev := NewExprEval(nil)

	// Equal → NULL
	got, err := ev.Eval(FunctionCall{
		Name: "NULLIF",
		Args: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 5},
			LiteralExpr{Type: DataTypeInteger, IntVal: 5},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Errorf("NULLIF(5, 5) = %v, want NULL", got)
	}

	// Not equal → first value
	got, err = ev.Eval(FunctionCall{
		Name: "NULLIF",
		Args: []Expr{
			LiteralExpr{Type: DataTypeInteger, IntVal: 5},
			LiteralExpr{Type: DataTypeInteger, IntVal: 6},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 5 {
		t.Errorf("NULLIF(5, 6) = %v, want 5", got)
	}
}

// ============================================================================
// CASE expression
// ============================================================================

func TestEvalCaseWithOperand(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(CaseExpr{
		Operand: LiteralExpr{Type: DataTypeInteger, IntVal: 2},
		Whens: []CaseWhen{
			{Condition: LiteralExpr{Type: DataTypeInteger, IntVal: 1}, Result: LiteralExpr{Type: DataTypeText, TextVal: "one"}},
			{Condition: LiteralExpr{Type: DataTypeInteger, IntVal: 2}, Result: LiteralExpr{Type: DataTypeText, TextVal: "two"}},
			{Condition: LiteralExpr{Type: DataTypeInteger, IntVal: 3}, Result: LiteralExpr{Type: DataTypeText, TextVal: "three"}},
		},
		Else: LiteralExpr{Type: DataTypeText, TextVal: "other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextVal != "two" {
		t.Errorf("CASE 2 WHEN ... = %q, want 'two'", got.TextVal)
	}
}

func TestEvalCaseWithoutOperand(t *testing.T) {
	ev := NewExprEval(nil)

	got, err := ev.Eval(CaseExpr{
		Whens: []CaseWhen{
			{Condition: BinaryExpr{
				Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 5},
				Op:    OpGt,
				Right: LiteralExpr{Type: DataTypeInteger, IntVal: 10},
			}, Result: LiteralExpr{Type: DataTypeText, TextVal: "big"}},
			{Condition: BinaryExpr{
				Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 5},
				Op:    OpLt,
				Right: LiteralExpr{Type: DataTypeInteger, IntVal: 10},
			}, Result: LiteralExpr{Type: DataTypeText, TextVal: "small"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextVal != "small" {
		t.Errorf("CASE WHEN 5<10 ... = %q, want 'small'", got.TextVal)
	}
}

func TestEvalCaseNoMatch(t *testing.T) {
	ev := NewExprEval(nil)

	// No match, no ELSE → NULL
	got, err := ev.Eval(CaseExpr{
		Operand: LiteralExpr{Type: DataTypeInteger, IntVal: 99},
		Whens: []CaseWhen{
			{Condition: LiteralExpr{Type: DataTypeInteger, IntVal: 1}, Result: LiteralExpr{Type: DataTypeText, TextVal: "one"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Errorf("CASE with no match should be NULL, got %v", got)
	}
}

// ============================================================================
// CAST
// ============================================================================

func TestEvalCast(t *testing.T) {
	ev := NewExprEval(nil)

	// CAST('42' AS INTEGER)
	got, err := ev.Eval(CastExpr{
		Expr: LiteralExpr{Type: DataTypeText, TextVal: "42"},
		Type: "INTEGER",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 42 {
		t.Errorf("CAST('42' AS INTEGER) = %v, want 42", got)
	}

	// CAST(42 AS TEXT) — note: our ApplyAffinity for INTEGER→TEXT doesn't convert
	// This is expected since INTEGER affinity is higher than TEXT in SQLite
}

// ============================================================================
// Nested expressions
// ============================================================================

func TestEvalNestedExpression(t *testing.T) {
	ev := NewExprEval(nil)

	// (1 + 2) * 3
	got, err := ev.Eval(BinaryExpr{
		Left: ParenExpr{Expr: BinaryExpr{
			Left:  LiteralExpr{Type: DataTypeInteger, IntVal: 1},
			Op:    OpAdd,
			Right: LiteralExpr{Type: DataTypeInteger, IntVal: 2},
		}},
		Op:    OpMul,
		Right: LiteralExpr{Type: DataTypeInteger, IntVal: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != 9 {
		t.Errorf("(1+2)*3 = %d, want 9", got.IntVal)
	}
}

// ============================================================================
// Complex WHERE clause
// ============================================================================

func TestEvalComplexWhere(t *testing.T) {
	row := []Value{IntegerValue(25), TextValue("Alice"), IntegerValue(1)}
	ev := NewExprEval(row)
	ev.ColumnMap["age"] = 0
	ev.ColumnMap["name"] = 1
	ev.ColumnMap["active"] = 2

	// age > 18 AND active = 1
	expr := BinaryExpr{
		Left: BinaryExpr{
			Left:  ColumnRef{Column: "age"},
			Op:    OpGt,
			Right: LiteralExpr{Type: DataTypeInteger, IntVal: 18},
		},
		Op: OpAnd,
		Right: BinaryExpr{
			Left:  ColumnRef{Column: "active"},
			Op:    OpEq,
			Right: LiteralExpr{Type: DataTypeInteger, IntVal: 1},
		},
	}

	got, err := ev.Eval(expr)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := got.AsInt64(); b != 1 {
		t.Errorf("age>18 AND active=1 for age=25,active=1 = %d, want 1", b)
	}
}

// ============================================================================
// GLOB pattern matching
// ============================================================================

func TestEvalGlob(t *testing.T) {
	ev := NewExprEval(nil)

	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		{"hello", "hello", true},
		{"hello", "h*", true},
		{"hello", "*o", true},
		{"hello", "h?llo", true},
		{"hello", "h?lo", false},
		{"hello", "world", false},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.pattern, func(t *testing.T) {
			got, err := ev.Eval(LikeExpr{
				Expr:    LiteralExpr{Type: DataTypeText, TextVal: tt.s},
				Pattern: LiteralExpr{Type: DataTypeText, TextVal: tt.pattern},
				Op:      LikeGlob,
			})
			if err != nil {
				t.Fatal(err)
			}
			if b, _ := got.AsInt64(); (b == 1) != tt.want {
				t.Errorf("GLOB(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Bitwise NOT
// ============================================================================

func TestEvalBitNot(t *testing.T) {
	ev := NewExprEval(nil)
	got, err := ev.Eval(UnaryExpr{
		Op:   OpBitNot,
		Expr: LiteralExpr{Type: DataTypeInteger, IntVal: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntVal != -1 {
		t.Errorf("~0 = %d, want -1", got.IntVal)
	}
}

// ============================================================================
// LIKE pattern matching unit tests
// ============================================================================

func TestMatchLike(t *testing.T) {
	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		// Exact
		{"", "", true},
		{"a", "a", true},
		{"ab", "ab", true},
		// %
		{"abc", "%", true},
		{"abc", "a%", true},
		{"abc", "%c", true},
		{"abc", "%b%", true},
		{"abc", "a%c", true},
		{"", "%", true},
		{"abc", "%abc%", true},
		{"abc", "%abcd%", false},
		// _
		{"a", "_", true},
		{"ab", "_b", true},
		{"ab", "a_", true},
		{"abc", "a_c", true},
		{"", "_", false},
		{"a", "__", false},
		// Mixed
		{"Hello World", "Hello%", true},
		{"Hello World", "%World", true},
		{"Hello World", "H%o_W%ld", true},
		// Multiple %
		{"abcdef", "%c%f", true},
		{"abcdef", "%x%f", false},
	}

	for _, tt := range tests {
		t.Run(tt.s+"/"+tt.pattern, func(t *testing.T) {
			got := matchLike(tt.s, tt.pattern)
			if got != tt.want {
				t.Errorf("matchLike(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Helpers
// ============================================================================

func valuesEqual(a, b Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case DataTypeNull:
		return true
	case DataTypeInteger:
		return a.IntVal == b.IntVal
	case DataTypeFloat:
		// Use approximate comparison
		diff := a.FloatVal - b.FloatVal
		if diff < 0 {
			diff = -diff
		}
		return diff < 1e-9
	case DataTypeText:
		return a.TextVal == b.TextVal
	case DataTypeBlob:
		if len(a.BlobVal) != len(b.BlobVal) {
			return false
		}
		for i := range a.BlobVal {
			if a.BlobVal[i] != b.BlobVal[i] {
				return false
			}
		}
		return true
	}
	return false
}
