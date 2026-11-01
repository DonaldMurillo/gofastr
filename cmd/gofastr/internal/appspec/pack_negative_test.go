package appspec

import (
	"go/ast"
	"go/parser"
	"math"
	"testing"
)

// The generator writes a negative default or seed cell as -1, which Go
// parses as a unary minus over a literal; pack read it as nil.
func TestAstAnyReadsNegativeNumbers(t *testing.T) {
	for src, want := range map[string]any{
		"-1":                   int64(-1),
		"-9223372036854775808": int64(math.MinInt64),
		"-2.5":                 -2.5,
		"[]any{-3, 4}":         []any{int64(-3), int64(4)},
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		got := AstAny(e)
		if s, ok := want.([]any); ok {
			g, _ := got.([]any)
			if len(g) != len(s) || g[0] != s[0] || g[1] != s[1] {
				t.Errorf("AstAny(%s) = %#v, want %#v", src, got, want)
			}
			continue
		}
		if got != want {
			t.Errorf("AstAny(%s) = %#v, want %#v", src, got, want)
		}
	}
}

func TestPackFieldKeepsNegativeDefaultAndMin(t *testing.T) {
	src := `app.Entity("readings", framework.EntityConfig{Fields: []schema.Field{{Name: "temp", Type: schema.Int, Default: -1, Min: floatPtr(-40), Max: floatPtr(-0.5)}}})`
	call, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	decl, err := PackEntityDeclFromCall(call.(*ast.CallExpr))
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	f := decl.Fields[0]
	if f.Default != int64(-1) {
		t.Errorf("Default = %#v, want -1", f.Default)
	}
	if f.Min == nil || *f.Min != -40 || f.Max == nil || *f.Max != -0.5 {
		t.Errorf("Min/Max = %v/%v, want -40/-0.5", f.Min, f.Max)
	}
}
