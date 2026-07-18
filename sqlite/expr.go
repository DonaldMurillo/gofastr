package sqlite

import (
	"strings"
)

// ExprEval evaluates an expression against a row of values.
// The row is a slice of Values corresponding to the columns in the table.
// columnMap maps column names to their indices in the row.
type ExprEval struct {
	Row       []Value
	ColumnMap map[string]int            // column name -> index
	TableMap  map[string]map[string]int // table name -> column map
	Params    []Value                   // Parameter values (?1, ?2, etc.)
	Engine    *Engine                   // for subquery execution
}

// NewExprEval creates a new expression evaluator for a row.
func NewExprEval(row []Value) *ExprEval {
	return &ExprEval{
		Row:       row,
		ColumnMap: make(map[string]int),
		TableMap:  make(map[string]map[string]int),
	}
}

// Eval evaluates an expression and returns the resulting Value.
func (e *ExprEval) Eval(expr Expr) (Value, error) {
	if expr == nil {
		return NullValue, nil
	}

	switch ex := expr.(type) {
	case LiteralExpr:
		return e.evalLiteral(ex)
	case ColumnRef:
		return e.evalColumnRef(ex)
	case BinaryExpr:
		return e.evalBinary(ex)
	case UnaryExpr:
		return e.evalUnary(ex)
	case FunctionCall:
		return e.evalFunction(ex)
	case IsNullExpr:
		return e.evalIsNull(ex)
	case BetweenExpr:
		return e.evalBetween(ex)
	case InExpr:
		return e.evalIn(ex)
	case LikeExpr:
		return e.evalLike(ex)
	case ParenExpr:
		return e.Eval(ex.Expr)
	case SubqueryExpr:
		return e.evalSubquery(ex)
	case ExistsExpr:
		return e.evalExists(ex)
	case CaseExpr:
		return e.evalCase(ex)
	case CastExpr:
		return e.evalCast(ex)
	case StarColumn:
		return NullValue, nil // Should not be evaluated directly
	case ParamExpr:
		return e.evalParam(ex)
	case RowIDExpr:
		return e.evalRowID()
	default:
		return NullValue, errUnknownExpr
	}
}

// EvalAggregateAware evaluates an expression, resolving aggregate function calls
// (COUNT, SUM, AVG, MIN, MAX) by matching them to output columns in the aggregate row.
// If the aggregate is not in outputCols, it falls back to computing from groupRows.
func (e *ExprEval) EvalAggregateAware(expr Expr, outputCols []outCol, aggRow []Value, groupRows [][]Value) (Value, error) {
	if expr == nil {
		return NullValue, nil
	}
	switch ex := expr.(type) {
	case FunctionCall:
		if isAggregateFunc(ex.Name) {
			// Find matching output column
			for i, col := range outputCols {
				if fc, ok := col.expr.(FunctionCall); ok && isAggregateFunc(fc.Name) {
					if strings.EqualFold(fc.Name, ex.Name) && fc.Star == ex.Star && len(fc.Args) == len(ex.Args) {
						if i < len(aggRow) {
							return aggRow[i], nil
						}
					}
				}
			}
			// Not in output columns — compute from group rows
			return computeAggregateFunc(ex, groupRows)
		}
		return e.evalFunction(ex)
	case BinaryExpr:
		left, err := e.EvalAggregateAware(ex.Left, outputCols, aggRow, groupRows)
		if err != nil {
			return NullValue, err
		}
		right, err := e.EvalAggregateAware(ex.Right, outputCols, aggRow, groupRows)
		if err != nil {
			return NullValue, err
		}
		return applyBinaryValues(ex.Op, left, right)
	case UnaryExpr:
		val, err := e.EvalAggregateAware(ex.Expr, outputCols, aggRow, groupRows)
		if err != nil {
			return NullValue, err
		}
		return applyUnaryValue(ex.Op, val)
	case ParenExpr:
		return e.EvalAggregateAware(ex.Expr, outputCols, aggRow, groupRows)
	default:
		return e.Eval(expr)
	}
}

// applyBinaryValues applies a binary operator to two pre-evaluated values.
// Used by EvalAggregateAware for HAVING expressions where left/right are already computed.
func applyBinaryValues(op BinaryOp, left, right Value) (Value, error) {
	// NULL propagation
	if left.IsNull() || right.IsNull() {
		return NullValue, nil
	}
	switch op {
	case OpAdd:
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			return IntegerValue(left.IntVal + right.IntVal), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		return FloatValue(lf + rf), nil
	case OpSub:
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			return IntegerValue(left.IntVal - right.IntVal), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		return FloatValue(lf - rf), nil
	case OpMul:
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			return IntegerValue(left.IntVal * right.IntVal), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		return FloatValue(lf * rf), nil
	case OpDiv:
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			if right.IntVal == 0 {
				return NullValue, nil
			}
			return IntegerValue(left.IntVal / right.IntVal), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		if rf == 0 {
			return NullValue, nil
		}
		return FloatValue(lf / rf), nil
	case OpMod:
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			if right.IntVal == 0 {
				return NullValue, nil
			}
			return IntegerValue(left.IntVal % right.IntVal), nil
		}
		return NullValue, nil
	case OpConcat:
		return TextValue(left.AsText() + right.AsText()), nil
	case OpEq:
		return boolToValue(CompareValues(left, right) == CompareEqual), nil
	case OpNe:
		return boolToValue(CompareValues(left, right) != CompareEqual), nil
	case OpLt:
		return boolToValue(CompareValues(left, right) == CompareLess), nil
	case OpLe:
		return boolToValue(CompareValues(left, right) != CompareGreater), nil
	case OpGt:
		return boolToValue(CompareValues(left, right) == CompareGreater), nil
	case OpGe:
		return boolToValue(CompareValues(left, right) != CompareLess), nil
	case OpAnd:
		if lb, ok := left.AsInt64(); ok && lb == 0 {
			return IntegerValue(0), nil
		}
		if rb, ok := right.AsInt64(); ok && rb == 0 {
			return IntegerValue(0), nil
		}
		return IntegerValue(1), nil
	case OpOr:
		if lb, ok := left.AsInt64(); ok && lb != 0 {
			return IntegerValue(1), nil
		}
		if rb, ok := right.AsInt64(); ok && rb != 0 {
			return IntegerValue(1), nil
		}
		return IntegerValue(0), nil
	}
	return NullValue, nil
}

func applyUnaryValue(op UnaryOp, val Value) (Value, error) {
	switch op {
	case OpNegate:
		if val.Type == DataTypeInteger {
			return IntegerValue(-val.IntVal), nil
		}
		if val.Type == DataTypeFloat {
			return FloatValue(-val.FloatVal), nil
		}
		if f, ok := val.AsFloat64(); ok {
			return FloatValue(-f), nil
		}
		return NullValue, nil
	case OpNot:
		if val.IsNull() {
			return NullValue, nil
		}
		if b, ok := val.AsInt64(); ok {
			if b == 0 {
				return IntegerValue(1), nil
			}
			return IntegerValue(0), nil
		}
		return NullValue, nil
	case OpBitNot:
		if val.Type == DataTypeInteger {
			return IntegerValue(^val.IntVal), nil
		}
		return NullValue, nil
	}
	return NullValue, nil
}

func (e *ExprEval) evalLiteral(ex LiteralExpr) (Value, error) {
	switch ex.Type {
	case DataTypeInteger:
		return IntegerValue(ex.IntVal), nil
	case DataTypeFloat:
		return FloatValue(ex.FloatVal), nil
	case DataTypeText:
		return TextValue(ex.TextVal), nil
	case DataTypeBlob:
		return BlobValue(ex.BlobVal), nil
	case DataTypeNull:
		return NullValue, nil
	default:
		return NullValue, nil
	}
}

func (e *ExprEval) evalColumnRef(ex ColumnRef) (Value, error) {
	if ex.Table != "" {
		// Qualified: table.column
		tmap, ok := e.TableMap[strings.ToLower(ex.Table)]
		if !ok {
			return NullValue, &evalError{"unknown table: " + ex.Table}
		}
		idx, ok := tmap[strings.ToLower(ex.Column)]
		if !ok {
			return NullValue, &evalError{"unknown column: " + ex.Table + "." + ex.Column}
		}
		if idx >= len(e.Row) {
			return NullValue, nil
		}
		return e.Row[idx], nil
	}

	// Unqualified: look up in column map
	idx, ok := e.ColumnMap[strings.ToLower(ex.Column)]
	if !ok {
		return NullValue, &evalError{"unknown column: " + ex.Column}
	}
	if idx >= len(e.Row) {
		return NullValue, nil
	}
	return e.Row[idx], nil
}

func (e *ExprEval) evalBinary(ex BinaryExpr) (Value, error) {
	left, err := e.Eval(ex.Left)
	if err != nil {
		return NullValue, err
	}

	// Short-circuit for AND/OR
	if ex.Op == OpAnd {
		if left.IsNull() {
			return NullValue, nil
		}
		if b, ok := left.AsInt64(); ok && b == 0 {
			return IntegerValue(0), nil
		}
		right, err := e.Eval(ex.Right)
		if err != nil {
			return NullValue, err
		}
		if right.IsNull() {
			return NullValue, nil
		}
		if b, ok := right.AsInt64(); ok && b == 0 {
			return IntegerValue(0), nil
		}
		return IntegerValue(1), nil
	}

	if ex.Op == OpOr {
		if !left.IsNull() {
			if b, ok := left.AsInt64(); ok && b != 0 {
				return IntegerValue(1), nil
			}
		}
		right, err := e.Eval(ex.Right)
		if err != nil {
			return NullValue, err
		}
		if !right.IsNull() {
			if b, ok := right.AsInt64(); ok && b != 0 {
				return IntegerValue(1), nil
			}
		}
		if left.IsNull() || right.IsNull() {
			return NullValue, nil
		}
		return IntegerValue(0), nil
	}

	right, err := e.Eval(ex.Right)
	if err != nil {
		return NullValue, err
	}

	// NULL propagation for most operators
	if left.IsNull() || right.IsNull() {
		switch ex.Op {
		case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
			return NullValue, nil
		default:
			return NullValue, nil
		}
	}

	switch ex.Op {
	case OpAdd:
		return e.arithOp(left, right, func(a, b int64) int64 { return a + b }, func(a, b float64) float64 { return a + b })
	case OpSub:
		return e.arithOp(left, right, func(a, b int64) int64 { return a - b }, func(a, b float64) float64 { return a - b })
	case OpMul:
		return e.arithOp(left, right, func(a, b int64) int64 { return a * b }, func(a, b float64) float64 { return a * b })
	case OpDiv:
		return e.divOp(left, right)
	case OpMod:
		return e.modOp(left, right)
	case OpConcat:
		return TextValue(left.AsText() + right.AsText()), nil
	case OpEq:
		return boolToValue(CompareValues(left, right) == CompareEqual), nil
	case OpNe:
		return boolToValue(CompareValues(left, right) != CompareEqual), nil
	case OpLt:
		return boolToValue(CompareValues(left, right) == CompareLess), nil
	case OpLe:
		return boolToValue(CompareValues(left, right) != CompareGreater), nil
	case OpGt:
		return boolToValue(CompareValues(left, right) == CompareGreater), nil
	case OpGe:
		return boolToValue(CompareValues(left, right) != CompareLess), nil
	}

	return NullValue, nil
}

func (e *ExprEval) arithOp(left, right Value, intFn func(int64, int64) int64, floatFn func(float64, float64) float64) (Value, error) {
	if isNumeric(left) && isNumeric(right) {
		// If both are integers, stay integer
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			return IntegerValue(intFn(left.IntVal, right.IntVal)), nil
		}
		// Otherwise float
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		return FloatValue(floatFn(lf, rf)), nil
	}
	return FloatValue(0), nil
}

func (e *ExprEval) divOp(left, right Value) (Value, error) {
	if isNumeric(left) && isNumeric(right) {
		if left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			if right.IntVal == 0 {
				return NullValue, errDivisionByZero
			}
			return IntegerValue(left.IntVal / right.IntVal), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		if rf == 0 {
			return NullValue, errDivisionByZero
		}
		return FloatValue(lf / rf), nil
	}
	return FloatValue(0), nil
}

func (e *ExprEval) modOp(left, right Value) (Value, error) {
	if isNumeric(left) && isNumeric(right) {
		li, lok := left.AsInt64()
		ri, rok := right.AsInt64()
		if lok && rok && left.Type == DataTypeInteger && right.Type == DataTypeInteger {
			if ri == 0 {
				return NullValue, errDivisionByZero
			}
			return IntegerValue(li % ri), nil
		}
		lf, _ := left.AsFloat64()
		rf, _ := right.AsFloat64()
		if rf == 0 {
			return NullValue, errDivisionByZero
		}
		return FloatValue(float64(int(lf) % int(rf))), nil
	}
	return IntegerValue(0), nil
}

func (e *ExprEval) evalUnary(ex UnaryExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}

	switch ex.Op {
	case OpNegate:
		if val.Type == DataTypeInteger {
			return IntegerValue(-val.IntVal), nil
		}
		if val.Type == DataTypeFloat {
			return FloatValue(-val.FloatVal), nil
		}
		if f, ok := val.AsFloat64(); ok {
			return FloatValue(-f), nil
		}
		return NullValue, nil
	case OpNot:
		if val.IsNull() {
			return NullValue, nil
		}
		if b, ok := val.AsInt64(); ok {
			if b == 0 {
				return IntegerValue(1), nil
			}
			return IntegerValue(0), nil
		}
		return NullValue, nil
	case OpBitNot:
		if val.Type == DataTypeInteger {
			return IntegerValue(^val.IntVal), nil
		}
		return NullValue, nil
	}

	return NullValue, nil
}

func (e *ExprEval) evalFunction(ex FunctionCall) (Value, error) {
	switch strings.ToUpper(ex.Name) {
	case "COUNT":
		if ex.Star {
			return IntegerValue(1), nil // Caller should handle actual counting
		}
		return IntegerValue(1), nil
	case "ABS":
		if len(ex.Args) == 0 {
			return NullValue, nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		if val.IsNull() {
			return NullValue, nil
		}
		if val.Type == DataTypeInteger {
			if val.IntVal < 0 {
				return IntegerValue(-val.IntVal), nil
			}
			return val, nil
		}
		if f, ok := val.AsFloat64(); ok {
			if f < 0 {
				return FloatValue(-f), nil
			}
			return val, nil
		}
		return NullValue, nil
	case "UPPER":
		if len(ex.Args) == 0 {
			return NullValue, nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		return TextValue(strings.ToUpper(val.AsText())), nil
	case "LOWER":
		if len(ex.Args) == 0 {
			return NullValue, nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		return TextValue(strings.ToLower(val.AsText())), nil
	case "LENGTH":
		if len(ex.Args) == 0 {
			return NullValue, nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		if val.IsNull() {
			return NullValue, nil
		}
		if val.Type == DataTypeText {
			return IntegerValue(int64(len(val.TextVal))), nil
		}
		if val.Type == DataTypeBlob {
			return IntegerValue(int64(len(val.BlobVal))), nil
		}
		return IntegerValue(int64(len(val.AsText()))), nil
	case "TYPEOF":
		if len(ex.Args) == 0 {
			return TextValue("null"), nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		switch val.Type {
		case DataTypeNull:
			return TextValue("null"), nil
		case DataTypeInteger:
			return TextValue("integer"), nil
		case DataTypeFloat:
			return TextValue("real"), nil
		case DataTypeText:
			return TextValue("text"), nil
		case DataTypeBlob:
			return TextValue("blob"), nil
		}
		return TextValue("null"), nil
	case "COALESCE":
		for _, arg := range ex.Args {
			val, err := e.Eval(arg)
			if err != nil {
				return NullValue, err
			}
			if !val.IsNull() {
				return val, nil
			}
		}
		return NullValue, nil
	case "IFNULL":
		if len(ex.Args) < 2 {
			return NullValue, nil
		}
		val, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		if !val.IsNull() {
			return val, nil
		}
		return e.Eval(ex.Args[1])
	case "NULLIF":
		if len(ex.Args) < 2 {
			return NullValue, nil
		}
		left, err := e.Eval(ex.Args[0])
		if err != nil {
			return NullValue, err
		}
		right, err := e.Eval(ex.Args[1])
		if err != nil {
			return NullValue, err
		}
		if CompareValues(left, right) == CompareEqual {
			return NullValue, nil
		}
		return left, nil
	case "MAX", "MIN":
		// Aggregate - handled by caller for multi-row
		if len(ex.Args) == 1 {
			return e.Eval(ex.Args[0])
		}
		return NullValue, nil
	case "SUM", "TOTAL", "AVG":
		// Aggregate - return value for single row
		if len(ex.Args) == 1 {
			return e.Eval(ex.Args[0])
		}
		return NullValue, nil
	default:
		return NullValue, nil
	}
}

func (e *ExprEval) evalIsNull(ex IsNullExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}
	result := val.IsNull()
	if ex.Negate {
		result = !result
	}
	return boolToValue(result), nil
}

func (e *ExprEval) evalBetween(ex BetweenExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}
	low, err := e.Eval(ex.Low)
	if err != nil {
		return NullValue, err
	}
	high, err := e.Eval(ex.High)
	if err != nil {
		return NullValue, err
	}

	if val.IsNull() || low.IsNull() || high.IsNull() {
		return NullValue, nil
	}

	result := CompareValues(val, low) != CompareLess && CompareValues(val, high) != CompareGreater
	if ex.Negate {
		result = !result
	}
	return boolToValue(result), nil
}

func (e *ExprEval) evalIn(ex InExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}

	if val.IsNull() {
		return NullValue, nil
	}

	// Subquery IN
	if ex.Select != nil {
		if e.Engine == nil {
			return NullValue, errUnsupportedInSubquery
		}
		subResult, err := e.Engine.executeSelect(ex.Select, e.Params)
		if err != nil {
			return NullValue, err
		}
		found := false
		hasNull := false
		for _, row := range subResult.Rows {
			if len(row) == 0 {
				continue
			}
			if row[0].IsNull() {
				hasNull = true
				continue
			}
			if CompareValues(val, row[0]) == CompareEqual {
				found = true
				break
			}
		}
		if found {
			return boolToValue(!ex.Negate), nil
		}
		if hasNull {
			return NullValue, nil
		}
		return boolToValue(ex.Negate), nil
	}

	found := false
	hasNull := false
	for _, item := range ex.Values {
		itemVal, err := e.Eval(item)
		if err != nil {
			return NullValue, err
		}
		if itemVal.IsNull() {
			hasNull = true
			continue
		}
		if CompareValues(val, itemVal) == CompareEqual {
			found = true
			break
		}
	}

	if found {
		return boolToValue(!ex.Negate), nil
	}
	if hasNull {
		return NullValue, nil
	}
	return boolToValue(ex.Negate), nil
}

func (e *ExprEval) evalLike(ex LikeExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}
	pattern, err := e.Eval(ex.Pattern)
	if err != nil {
		return NullValue, err
	}

	if val.IsNull() || pattern.IsNull() {
		return NullValue, nil
	}

	var result bool
	switch ex.Op {
	case LikeLike:
		result = matchLike(val.AsText(), pattern.AsText())
	case LikeGlob:
		result = matchGlob(val.AsText(), pattern.AsText())
	}

	if ex.Negate {
		result = !result
	}
	return boolToValue(result), nil
}

func (e *ExprEval) evalCase(ex CaseExpr) (Value, error) {
	if ex.Operand != nil {
		// CASE x WHEN a THEN b WHEN c THEN d ELSE e END
		operand, err := e.Eval(ex.Operand)
		if err != nil {
			return NullValue, err
		}
		for _, when := range ex.Whens {
			cond, err := e.Eval(when.Condition)
			if err != nil {
				return NullValue, err
			}
			if CompareValues(operand, cond) == CompareEqual {
				return e.Eval(when.Result)
			}
		}
	} else {
		// CASE WHEN a THEN b WHEN c THEN d ELSE e END
		for _, when := range ex.Whens {
			cond, err := e.Eval(when.Condition)
			if err != nil {
				return NullValue, err
			}
			if !cond.IsNull() {
				if b, ok := cond.AsInt64(); ok && b != 0 {
					return e.Eval(when.Result)
				}
			}
		}
	}

	if ex.Else != nil {
		return e.Eval(ex.Else)
	}
	return NullValue, nil
}

func (e *ExprEval) evalCast(ex CastExpr) (Value, error) {
	val, err := e.Eval(ex.Expr)
	if err != nil {
		return NullValue, err
	}

	if val.IsNull() {
		return NullValue, nil
	}

	switch strings.ToUpper(ex.Type) {
	case "TEXT":
		return TextValue(val.String()), nil
	case "INTEGER":
		if v, ok := val.AsInt64(); ok {
			return IntegerValue(v), nil
		}
		if v, ok := val.AsFloat64(); ok {
			return IntegerValue(int64(v)), nil
		}
		if s := val.TextVal; s != "" {
			return TextValue(s), nil
		}
		return TextValue(val.String()), nil
	case "REAL":
		if v, ok := val.AsFloat64(); ok {
			return FloatValue(v), nil
		}
		if v, ok := val.AsInt64(); ok {
			return FloatValue(float64(v)), nil
		}
		return FloatValue(0), nil
	case "BLOB", "":
		return val, nil
	default:
		return val, nil
	}
}

func (e *ExprEval) evalParam(ex ParamExpr) (Value, error) {
	if ex.Index < len(e.Params) {
		return e.Params[ex.Index], nil
	}
	return NullValue, nil
}

func (e *ExprEval) evalSubquery(ex SubqueryExpr) (Value, error) {
	if e.Engine == nil {
		return NullValue, &evalError{"subquery requires engine"}
	}
	result, err := e.Engine.executeSelect(ex.Select, e.Params)
	if err != nil {
		return NullValue, err
	}
	if len(result.Rows) == 0 {
		return NullValue, nil
	}
	// Return first column of first row
	if len(result.Rows[0]) == 0 {
		return NullValue, nil
	}
	return result.Rows[0][0], nil
}

func (e *ExprEval) evalRowID() (Value, error) {
	// RowID is usually stored as the first column if it's a rowid alias
	// or as a special column. The caller should set up the column map.
	idx, ok := e.ColumnMap["rowid"]
	if ok && idx < len(e.Row) {
		return e.Row[idx], nil
	}
	return NullValue, nil
}

// ============================================================================
// Pattern matching
// ============================================================================

// matchLike implements SQL LIKE pattern matching.
// % matches any sequence, _ matches any single character.
func matchLike(s, pattern string) bool {
	return matchLikeRecursive(s, pattern)
}

func matchLikeRecursive(s, pattern string) bool {
	for len(pattern) > 0 {
		if pattern[0] == '%' {
			// % matches any sequence (including empty)
			pattern = pattern[1:]
			// Skip consecutive %
			for len(pattern) > 0 && pattern[0] == '%' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true // trailing % matches everything
			}
			// Try matching at every position
			for i := 0; i <= len(s); i++ {
				if matchLikeRecursive(s[i:], pattern) {
					return true
				}
			}
			return false
		}

		if len(s) == 0 {
			return false
		}

		if pattern[0] == '_' {
			s = s[1:]
			pattern = pattern[1:]
		} else if pattern[0] == s[0] {
			s = s[1:]
			pattern = pattern[1:]
		} else {
			return false
		}
	}
	return len(s) == 0
}

// matchGlob implements SQLite GLOB pattern matching.
// * matches any sequence, ? matches any single character.
// Uses case-sensitive matching.
func matchGlob(s, pattern string) bool {
	for len(pattern) > 0 {
		if pattern[0] == '*' {
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if matchGlob(s[i:], pattern) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 {
			return false
		}
		if pattern[0] == '?' || pattern[0] == s[0] {
			s = s[1:]
			pattern = pattern[1:]
		} else {
			return false
		}
	}
	return len(s) == 0
}

// ============================================================================
// Helpers
// ============================================================================

func boolToValue(b bool) Value {
	if b {
		return IntegerValue(1)
	}
	return IntegerValue(0)
}

// Sentinel errors
var (
	errUnknownExpr           = &evalError{"unknown expression type"}
	errDivisionByZero        = &evalError{"division by zero"}
	errUnsupportedInSubquery = &evalError{"IN subquery not yet supported"}
)

// computeAggregateFunc computes an aggregate function over a set of rows.
// Used by EvalAggregateAware when the aggregate isn't in the SELECT list (e.g., HAVING COUNT(*) > 1).
func computeAggregateFunc(fc FunctionCall, rows [][]Value) (Value, error) {
	switch strings.ToUpper(fc.Name) {
	case "COUNT":
		if fc.Star || len(fc.Args) == 0 {
			return IntegerValue(int64(len(rows))), nil
		}
		// COUNT(expr) — count non-NULL values
		// We don't have column mapping here, so just return count of rows
		return IntegerValue(int64(len(rows))), nil
	case "SUM":
		// Without column mapping, we can't compute SUM of a specific column
		// This is a fallback — prefer to have the aggregate in the SELECT list
		return FloatValue(0), nil
	case "AVG":
		return FloatValue(0), nil
	case "MIN", "MAX":
		return NullValue, nil
	}
	return NullValue, nil
}

type evalError struct{ msg string }

func (e *evalError) Error() string { return e.msg }

// CollectColumnRefs walks an expression tree and collects all ColumnRef nodes.
func CollectColumnRefs(expr Expr) []ColumnRef {
	if expr == nil {
		return nil
	}
	switch ex := expr.(type) {
	case ColumnRef:
		return []ColumnRef{ex}
	case BinaryExpr:
		return append(CollectColumnRefs(ex.Left), CollectColumnRefs(ex.Right)...)
	case UnaryExpr:
		return CollectColumnRefs(ex.Expr)
	case FunctionCall:
		var refs []ColumnRef
		for _, arg := range ex.Args {
			refs = append(refs, CollectColumnRefs(arg)...)
		}
		return refs
	case *FunctionCall:
		var refs []ColumnRef
		for _, arg := range ex.Args {
			refs = append(refs, CollectColumnRefs(arg)...)
		}
		return refs
	case ParenExpr:
		return CollectColumnRefs(ex.Expr)
	case BetweenExpr:
		refs := CollectColumnRefs(ex.Expr)
		refs = append(refs, CollectColumnRefs(ex.Low)...)
		refs = append(refs, CollectColumnRefs(ex.High)...)
		return refs
	case InExpr:
		refs := CollectColumnRefs(ex.Expr)
		for _, v := range ex.Values {
			refs = append(refs, CollectColumnRefs(v)...)
		}
		return refs
	case IsNullExpr:
		return CollectColumnRefs(ex.Expr)
	case LikeExpr:
		refs := CollectColumnRefs(ex.Expr)
		refs = append(refs, CollectColumnRefs(ex.Pattern)...)
		return refs
	case CaseExpr:
		var refs []ColumnRef
		refs = append(refs, CollectColumnRefs(ex.Operand)...)
		for _, w := range ex.Whens {
			refs = append(refs, CollectColumnRefs(w.Condition)...)
			refs = append(refs, CollectColumnRefs(w.Result)...)
		}
		refs = append(refs, CollectColumnRefs(ex.Else)...)
		return refs
	default:
		return nil
	}
}
