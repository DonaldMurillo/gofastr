package macos

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The open, save and folder panels ran runModal through onMain, whose
// ten-second default deadline gave up while the panel was still open:
// the bridge answered internal and the path the user then picked was
// never granted. A modal waits on a person, so every runModal in the
// shell must run inside a closure handed to
// onMainWithTimeout(modalTimeout, ...). The panels cannot be driven
// from a test, so this reads the source.
func TestModalsWaitForThePerson(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			if !isSelLiteral(n, "runModal") {
				return true
			}
			found++
			if !insideModalWait(stack) {
				t.Errorf("%s: runModal outside onMainWithTimeout(modalTimeout, ...)", fset.Position(n.Pos()))
			}
			return true
		})
	}
	// The alert and the three panels.
	if found < 4 {
		t.Fatalf("found %d runModal calls, want at least 4: the scan is not reading the shell", found)
	}
}

// isSelLiteral reports whether n is objc.Sel("<name>").
func isSelLiteral(n ast.Node, name string) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sel" {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == name
}

// insideModalWait reports whether the innermost closure on the stack is
// an argument of s.onMainWithTimeout(modalTimeout, ...).
func insideModalWait(stack []ast.Node) bool {
	for i := len(stack) - 1; i > 0; i-- {
		if _, ok := stack[i].(*ast.FuncLit); !ok {
			continue
		}
		call, ok := stack[i-1].(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "onMainWithTimeout" || len(call.Args) != 2 {
			return false
		}
		id, ok := call.Args[0].(*ast.Ident)
		return ok && id.Name == "modalTimeout"
	}
	return false
}
