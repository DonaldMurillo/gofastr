package expr

import (
	"testing"
)

func TestNegatesGoInt(t *testing.T) {
	e, err := Compile("-x")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Eval(MapScope{"x": 5}, nil)
	if err != nil || got != int64(-5) {
		t.Fatalf("got %v, %v; want -5", got, err)
	}
}

func TestNilFunctionReturnsError(t *testing.T) {
	e, err := Compile("missing()")
	if err != nil {
		t.Fatal(err)
	}
	var env Env
	env.Register("missing", nil)
	if _, err := e.Eval(nil, &env); err == nil {
		t.Fatal("want error for nil function")
	}
}

func TestMixedMaxPreservesExactInteger(t *testing.T) {
	got, err := EvalBool("max(9007199254740992.0, 9007199254740993) == 9007199254740993", nil, nil)
	if err != nil || !got {
		t.Fatalf("got %v, %v; want true", got, err)
	}
}
