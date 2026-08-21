package expr_test

import (
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
