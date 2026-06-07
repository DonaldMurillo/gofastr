package expr_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/expr"
)

// TestDeepNestingRejected asserts a pathologically deep expression is
// rejected at compile time instead of exhausting the parser's call stack.
// Reachable from unauthenticated input via add_hook → RunHook/runValidate.
func TestDeepNestingRejected(t *testing.T) {
	// 50k nested parens: deep enough to overflow a recursive-descent
	// parser's stack if there's no depth guard.
	const depth = 50000
	src := strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth)
	_, err := expr.Compile(src)
	if err == nil {
		t.Fatal("expected deep nesting to be rejected, got nil error")
	}
}

// TestDeepBracketNestingRejected is the same guard via '[' list nesting.
func TestDeepBracketNestingRejected(t *testing.T) {
	const depth = 50000
	src := strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth)
	_, err := expr.Compile(src)
	if err == nil {
		t.Fatal("expected deep bracket nesting to be rejected, got nil error")
	}
}

// TestModerateNestingStillCompiles guards against an over-aggressive limit:
// ordinary expressions with reasonable nesting must keep compiling.
func TestModerateNestingStillCompiles(t *testing.T) {
	const depth = 20
	src := strings.Repeat("(", depth) + "1 + 2" + strings.Repeat(")", depth)
	e, err := expr.Compile(src)
	if err != nil {
		t.Fatalf("moderate nesting should compile: %v", err)
	}
	v, err := e.Eval(nil, expr.DefaultEnv())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != int64(3) {
		t.Errorf("got %v, want 3", v)
	}
}
