package expr_test

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/expr"
)

// Property: no expression string can terminate the host process.
//
// Kiln's build mode accepts expression sources over HTTP (add_hook,
// add_validate, respond_json status/body, route conditions), so every
// source below is attacker-reachable. Two crash classes matter:
//
//   - a Go panic from an unguarded runtime operation, and
//   - a goroutine stack overflow, which is a FATAL runtime error that
//     recover() cannot catch and no middleware can contain.
//
// depth_test.go pins the two surfaces that were already guarded (nested
// '(' and nested '['). This file pins the surfaces that were not: the
// iterative precedence loops and the postfix chain build left-deep trees
// without touching the parser's depth counter, and equals() compared
// interface values whose dynamic type may be uncomparable.

// TestUncomparableOperandsNoPanic pins that comparing values with
// uncomparable dynamic types yields a result, never a runtime panic.
func TestUncomparableOperandsNoPanic(t *testing.T) {
	for _, src := range []string{
		"[1] == [2]",              // slice vs slice
		"[1] != [1]",              // the != path shares equals()
		`[1] == "x"`,              // slice vs comparable
		"contains([[1]], [1])",    // equals() reached via a builtin
		"[[1], [2]] == [[1], []]", // nested slices
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q panicked: %v", src, r)
				}
			}()
			if _, err := expr.EvalBool(src, nil, nil); err != nil {
				t.Logf("%q -> error (acceptable): %v", src, err)
			}
		}()
	}
}

// TestUncomparableEqualsIsFalse pins the semantics chosen for the fix:
// two uncomparable operands are not equal rather than an error, so an
// existing `!=` guard in a world IR keeps evaluating to true.
func TestUncomparableEqualsIsFalse(t *testing.T) {
	got, err := expr.EvalBool("[1] == [1]", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("uncomparable operands must not compare equal")
	}
}

// TestFlatChainDepthRejected pins the AST surfaces the parser's grouping
// guard never saw. Each builds a tree of depth O(n) through an ITERATIVE
// parse loop, so no amount of parser recursion guarding catches them; the
// crash lands later, in the recursive evaluator.
func TestFlatChainDepthRejected(t *testing.T) {
	const n = 200000
	for name, src := range map[string]string{
		"binary": "1" + strings.Repeat("+1", n),       // parseAdd loop
		"member": "a" + strings.Repeat(".b", n),       // parsePostfix '.'
		"index":  "a" + strings.Repeat("[0]", n),      // parsePostfix '['
		"unary":  strings.Repeat("!", n) + "true",     // parseUnary recursion
		"mixed":  "1" + strings.Repeat("+a.b*2", n/4), // interleaved loops
	} {
		if _, err := expr.Compile(src); err == nil {
			t.Errorf("%s chain of %d compiled; eval would recurse that deep", name, n)
		}
	}
}

// TestOversizeSourceRejected pins that Compile bounds its input before
// lexing. Without a cap, a 20 MB source allocates a token per byte and a
// node per term before any depth check can run.
func TestOversizeSourceRejected(t *testing.T) {
	src := "1" + strings.Repeat("+1", 5_000_000) // ~10 MB
	if _, err := expr.Compile(src); err == nil {
		t.Fatal("oversize source compiled")
	}
}

// TestOrdinaryExpressionsStillCompile guards against an over-aggressive
// cap: real world-IR conditions must keep working.
func TestOrdinaryExpressionsStillCompile(t *testing.T) {
	scope := expr.MapScope{"entity": map[string]any{"status": "active", "n": int64(3)}}
	for src, want := range map[string]bool{
		`entity.status == "active"`:                       true,
		`entity.n > 1 && entity.n < 10`:                   true,
		`contains(["a", "b"], "b") || entity.n == 99`:     true,
		`!(entity.status == "draft")`:                     true,
		`min(entity.n, 5) == 3 && len(entity.status) > 0`: true,
	} {
		got, err := expr.EvalBool(src, scope, nil)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}

// TestMinMaxMixedNumericTypes pins that the numeric fold stays
// type-monotone: one float operand must force the float path for the
// whole argument list. The int case used to re-set the all-int flag, so a
// float argument followed by an int returned intValues[0], a zero that
// was never assigned, silently defeating a `min(...) > 0` guard.
func TestMinMaxMixedNumericTypes(t *testing.T) {
	scope := expr.MapScope{"i": 7, "f": 1.5}
	for src, want := range map[string]float64{
		"min(f, i)": 1.5,
		"max(f, i)": 7,
		"min(i, f)": 1.5,
		"max(i, f)": 7,
	} {
		v, err := expr.Compile(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		got, err := v.Eval(scope, nil)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		var f float64
		switch n := got.(type) {
		case float64:
			f = n
		case int64:
			f = float64(n)
		default:
			t.Fatalf("%q returned %T", src, got)
		}
		if f != want {
			t.Errorf("%q = %v, want %v", src, f, want)
		}
	}
}

// Property: every DefaultEnv builtin is a total function — any argument
// shape the parser can produce yields a value or an error, never a panic.
//
// Builtins receive scope-resolved values (entity fields, ctx), so their
// argument types are attacker-chosen even when the call site itself is a
// benign IR expression. The builtins after min/max (abs, starts_with,
// ends_with, now) had no pin at all; this loops the whole catalog so a
// newly added builtin fails here the moment it trusts its arguments.
func TestBuiltinsTotalOnHostileArgs(t *testing.T) {
	env := expr.DefaultEnv()
	shapes := [][]any{
		{},                       // zero args
		{nil},                    // JSON null
		{true},                   // bool
		{int64(1)},               // int64
		{1},                      // int
		{1.5},                    // float
		{"s"},                    // string
		{[]any{1}},               // list
		{map[string]any{"k": 1}}, // map
		{[]any{[]any{1}}},        // nested uncomparable
		{"a", 1},                 // two mixed args
		{"a", "b", "c"},          // three args
		{[]any{"x"}, []any{"y"}}, // uncomparable pair (equals path)
	}
	names := make([]string, 0, len(env.Functions))
	for name := range env.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fn := env.Functions[name]
		for i, args := range shapes {
			func() {
				defer func() {
					if r := recover(); r != nil {
						var first any = "<none>"
						if len(args) > 0 {
							first = args[0]
						}
						t.Errorf("%s(arg shape %d, %d args, first type %T) panicked: %v", name, i, len(args), first, r)
					}
				}()
				_, _ = fn(args)
			}()
		}
	}
	// The same hostility through the compiled path, so the parser's
	// argument plumbing (arg list build, evaluation order) stays in the
	// loop too.
	for _, src := range []string{
		"len(1)", "lower([1])", "upper(map)", "abs(\"x\")", "now(1)",
		"contains(1, 2)", "starts_with(nil, \"x\")", "ends_with(\"x\", nil)",
		"min(\"a\")", "max(true)", "len()", "now()",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q panicked: %v", src, r)
				}
			}()
			_, _ = expr.EvalBool(src, nil, nil)
		}()
	}
}

// Property: arithmetic and comparison over extreme operands error or
// produce a defined value; nothing crashes the evaluating goroutine.
// int64 overflow wraps (defined Go behaviour), float overflow yields
// ±Inf, and Inf-Inf yields NaN — all values equals()/compare() must
// tolerate.
func TestArithExtremeOperandsNoPanic(t *testing.T) {
	scope := expr.MapScope{
		"min": int64(math.MinInt64), "max": int64(math.MaxInt64),
		"inf": math.Inf(1),
	}
	for _, src := range []string{
		"1 / 0", "1 % 0", "1.5 / 0", "1.5 % 0",
		"min / -1", "min % -1", "min * -1", "min - 1", "max + 1",
		"(inf - inf) == (inf - inf)", // NaN == NaN via equals()
		"(inf - inf) != 0",           // NaN != via equals()
		"(inf - inf) < 1",            // NaN ordering via compare()
		"(inf - inf) >= min",         // NaN ordering, int64 operand
		"inf * 0 == inf",             // NaN == Inf
		"min == max",                 // int64 extremes
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q panicked: %v", src, r)
				}
			}()
			if _, err := expr.EvalBool(src, scope, nil); err != nil {
				t.Logf("%q -> error (acceptable): %v", src, err)
			}
		}()
	}
}

// Property: abs() never returns a negative result.
//
// builtinAbs negates with -v, which wraps for math.MinInt64 and silently
// returns the most negative value from a `abs(balance) < 100` style
// guard. Reachable through Go-typed scopes (int64 literals from
// value_literal set_field); JSON-decoded numbers arrive as float64 and
// take the float path, which is correct.
func TestAbsNeverReturnsNegative(t *testing.T) {
	e, err := expr.Compile("abs(min)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := e.Eval(expr.MapScope{"min": int64(math.MinInt64)}, nil)
	if err != nil {
		t.Fatalf("eval abs(min): %v", err)
	}
	n, ok := got.(int64)
	if !ok {
		t.Fatalf("abs(min) = %T, want int64", got)
	}
	if n < 0 {
		t.Errorf("abs(-9223372036854775808) = %d: negation wrapped, abs returned a negative value", n)
	}
}

// Property: indexing with a hostile index is a validation error, never an
// out-of-bounds panic. Pins the int64-width bounds guard in indexNode
// (int(i) truncates on 32-bit builds, so the check must compare at
// int64 width) plus the type guards for both containers.
func TestHostileIndexErrorsNotPanics(t *testing.T) {
	scope := expr.MapScope{
		"list": []any{int64(1), int64(2), int64(3)},
		"m":    map[string]any{"k": "v"},
		"n":    "scalar",
	}
	for _, src := range []string{
		"list[9223372036854775807]", // int64 max index
		"list[20000000000]",         // index above 32-bit int range
		"list[-1]",
		"list[1.5]",  // non-integral float
		"list[true]", // bool index
		`list["1"]`,  // string index on a list
		"m[0]",       // int key on a map
		"m[true]",    // bool key
		"m[1.0]",     // float key
		"n[0]",       // index a scalar
		"list[1][0]", // index into a scalar element
		`m["missing"]`,
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q panicked: %v", src, r)
				}
			}()
			if _, err := expr.EvalBool(src, scope, nil); err != nil {
				t.Logf("%q -> error (acceptable): %v", src, err)
			}
		}()
	}
	// In-range indexes keep working, including the integral-float form.
	got, err := expr.EvalBool("list[2.0] == 3 && m[\"k\"] == \"v\"", scope, nil)
	if err != nil {
		t.Fatalf("in-range control: %v", err)
	}
	if !got {
		t.Error("in-range index stopped evaluating")
	}
}
