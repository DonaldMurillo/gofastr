package expr_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/expr"
)

func eval(t *testing.T, src string, scope expr.MapScope) any {
	t.Helper()
	e, err := expr.Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	v, err := e.Eval(scope, expr.DefaultEnv())
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return v
}

func evalErr(t *testing.T, src string, scope expr.MapScope) error {
	t.Helper()
	e, err := expr.Compile(src)
	if err != nil {
		return err
	}
	_, err = e.Eval(scope, expr.DefaultEnv())
	return err
}

func TestLiterals(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"1", int64(1)},
		{"-2", int64(-2)},
		{"1.5", 1.5},
		{`"hello"`, "hello"},
		{`'world'`, "world"},
		{"true", true},
		{"false", false},
		{"null", nil},
	}
	for _, c := range cases {
		got := eval(t, c.src, nil)
		if got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.src, got, got, c.want, c.want)
		}
	}
}

func TestArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"1 + 2", int64(3)},
		{"10 - 4", int64(6)},
		{"2 * 3", int64(6)},
		{"7 / 2", int64(3)},
		{"7 % 2", int64(1)},
		{"1 + 2 * 3", int64(7)},
		{"(1 + 2) * 3", int64(9)},
		{"1.5 + 2.5", 4.0},
		{`"foo" + "bar"`, "foobar"},
	}
	for _, c := range cases {
		got := eval(t, c.src, nil)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.src, got, c.want)
		}
	}
}

func TestComparisons(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"1 == 1", true},
		{"1 == 2", false},
		{"1 != 2", true},
		{"1 < 2", true},
		{"2 < 1", false},
		{"1 <= 1", true},
		{"3 >= 2", true},
		{`"a" == "a"`, true},
		{`"a" != "b"`, true},
		{"true == true", true},
		{"null == null", true},
	}
	for _, c := range cases {
		got := eval(t, c.src, nil)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.src, got, c.want)
		}
	}
}

func TestLogical(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"true && true", true},
		{"true && false", false},
		{"false || true", true},
		{"false || false", false},
		{"!true", false},
		{"!false", true},
		{"!(1 == 2)", true},
		{"true && (1 < 2)", true},
		{"(1 == 1) || (2 == 3)", true},
	}
	for _, c := range cases {
		got := eval(t, c.src, nil)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.src, got, c.want)
		}
	}
}

func TestShortCircuit(t *testing.T) {
	// Right side calls a function that would error if evaluated.
	scope := expr.MapScope{}
	if got := eval(t, `false && fail()`, scope); got != false {
		t.Errorf("expected short-circuit on false &&, got %v", got)
	}
	if got := eval(t, `true || fail()`, scope); got != true {
		t.Errorf("expected short-circuit on true ||, got %v", got)
	}
}

func TestVariableAccess(t *testing.T) {
	scope := expr.MapScope{
		"entity": map[string]any{
			"title": "hello",
			"author": map[string]any{
				"name": "donald",
			},
		},
		"ctx": map[string]any{
			"user_id": int64(42),
		},
	}
	if got := eval(t, "entity.title", scope); got != "hello" {
		t.Errorf("entity.title: got %v", got)
	}
	if got := eval(t, "entity.author.name", scope); got != "donald" {
		t.Errorf("entity.author.name: got %v", got)
	}
	if got := eval(t, "ctx.user_id == 42", scope); got != true {
		t.Errorf("ctx.user_id == 42: got %v", got)
	}
}

func TestMissingVariableErrors(t *testing.T) {
	if err := evalErr(t, "missing", nil); err == nil {
		t.Fatal("undefined var must error")
	}
	if err := evalErr(t, "entity.missing", expr.MapScope{"entity": map[string]any{}}); err == nil {
		t.Fatal("missing field must error")
	}
}

func TestBuiltins(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`len("hello")`, int64(5)},
		{`len([1, 2, 3])`, int64(3)},
		{`lower("HELLO")`, "hello"},
		{`upper("foo")`, "FOO"},
		{`contains("hello world", "world")`, true},
		{`starts_with("foo bar", "foo")`, true},
		{`ends_with("foo bar", "bar")`, true},
		{`abs(-5)`, int64(5)},
		{`min(3, 7)`, int64(3)},
		{`max(3, 7)`, int64(7)},
	}
	for _, c := range cases {
		got := eval(t, c.src, nil)
		if got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.src, got, got, c.want, c.want)
		}
	}
}

func TestList(t *testing.T) {
	got := eval(t, "[1, 2, 3]", nil).([]any)
	if len(got) != 3 || got[0] != int64(1) {
		t.Errorf("got %v", got)
	}
}

func TestNestedExpressions(t *testing.T) {
	scope := expr.MapScope{
		"entity": map[string]any{"title": "Hello World", "draft": true},
		"ctx":    map[string]any{"user": map[string]any{"role": "admin"}},
	}
	if got := eval(t, `len(entity.title) > 0 && (entity.draft || ctx.user.role == "admin")`, scope); got != true {
		t.Errorf("complex: got %v", got)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		`1 +`,
		`(1 + 2`,
		`"unterminated`,
		`x..y`,
		`@`,
	}
	for _, s := range bad {
		if _, err := expr.Compile(s); err == nil {
			t.Errorf("expected parse error for %q", s)
		}
	}
}

func TestStringMethodsErrors(t *testing.T) {
	// len applied to int should fail
	err := evalErr(t, `len(5)`, nil)
	if err == nil {
		t.Fatal("len(int) must error")
	}
	if !strings.Contains(err.Error(), "len") {
		t.Errorf("error mentions len? %v", err)
	}
}

func TestEvalBool(t *testing.T) {
	got, err := expr.EvalBool(`1 == 1`, nil, nil)
	if err != nil || !got {
		t.Errorf("EvalBool true case: %v %v", got, err)
	}
	if _, err := expr.EvalBool(`"x"`, nil, nil); err == nil {
		t.Error("EvalBool on string should fail")
	}
}

func TestCustomFunction(t *testing.T) {
	env := expr.DefaultEnv()
	env.Register("double", func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, expr.ErrArity
		}
		n, ok := args[0].(int64)
		if !ok {
			return nil, expr.ErrType
		}
		return n * 2, nil
	})
	e, err := expr.Compile(`double(21)`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	v, err := e.Eval(nil, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != int64(42) {
		t.Errorf("got %v, want 42", v)
	}
}
