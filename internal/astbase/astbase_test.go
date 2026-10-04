package astbase

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// funcsOf parses src and returns its function declarations in source
// order.
func funcsOf(t *testing.T, src string) []*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out []*ast.FuncDecl
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			out = append(out, fn)
		}
	}
	return out
}

func TestRecvBaseName(t *testing.T) {
	fns := funcsOf(t, `package p
func Free()                 {}
func (r rec) Value()         {}
func (r *rec) Pointer()      {}
func (r rec[P]) GenericV()   {}
func (r *rec[P]) GenericP()  {}
func (r **rec) DoublePtr()   {}
`)
	cases := []struct{ name, want string }{
		{"Free", ""},
		{"Value", "rec"},
		{"Pointer", "rec"},
		{"GenericV", ""},
		{"GenericP", ""},
		{"DoublePtr", ""},
	}
	byName := map[string]string{}
	for _, fn := range fns {
		byName[fn.Name.Name] = RecvBaseName(fn)
	}
	for _, c := range cases {
		if got := byName[c.name]; got != c.want {
			t.Errorf("RecvBaseName(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}
