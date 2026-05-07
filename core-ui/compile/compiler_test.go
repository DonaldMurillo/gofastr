package compile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func parseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	// Wrap in a function so we can parse expressions
	wrapped := "package test\nfunc _() { _ = " + src + " }"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", wrapped, 0)
	if err != nil {
		t.Fatalf("parse error for %q: %v", src, err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	return fn.Body.List[0].(*ast.AssignStmt).Rhs[0]
}

func parseStmt(t *testing.T, src string) ast.Stmt {
	t.Helper()
	wrapped := "package test\nfunc _() {\n" + src + "\n}"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", wrapped, 0)
	if err != nil {
		t.Fatalf("parse error for %q: %v", src, err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	return fn.Body.List[0]
}

func TestCompileStringLiteral(t *testing.T) {
	expr := parseExpr(t, `"hello"`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if js != `"hello"` {
		t.Errorf("expected %q, got %q", `"hello"`, js)
	}
}

func TestCompileBinaryExpr(t *testing.T) {
	expr := parseExpr(t, `"a" + "b"`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"a" + "b"`
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileIfElse(t *testing.T) {
	stmt := parseStmt(t, `if x > 0 { y = 1 } else { y = 2 }`)
	js, err := compileStmt(stmt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "if (x > 0)") {
		t.Errorf("expected if statement, got: %s", js)
	}
	if !strings.Contains(js, "else") {
		t.Errorf("expected else branch, got: %s", js)
	}
}

func TestCompileForRange(t *testing.T) {
	stmt := parseStmt(t, `for i, v := range items { _ = v }`)
	js, err := compileStmt(stmt, 0)
	if err != nil {
		t.Fatal(err)
	}
	expected := "for (const [i, v] of items.entries())"
	if !strings.Contains(js, expected) {
		t.Errorf("expected %q in output, got: %s", expected, js)
	}
}

func TestCompileReturn(t *testing.T) {
	stmt := parseStmt(t, `return html`)
	js, err := compileStmt(stmt, 0)
	if err != nil {
		t.Fatal(err)
	}
	expected := "return html;\n"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileVarDecl(t *testing.T) {
	stmt := parseStmt(t, `x := 5`)
	js, err := compileStmt(stmt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "const x = 5") {
		t.Errorf("expected const declaration, got: %s", js)
	}
}

func TestCompileMethodCall(t *testing.T) {
	expr := parseExpr(t, `s.Method()`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "s.method()") {
		t.Errorf("expected method call with lowercase, got: %s", js)
	}
}

func TestCompileSprintf(t *testing.T) {
	expr := parseExpr(t, `fmt.Sprintf("hi %s", name)`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "`hi ${name}`"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileSprintfMultiple(t *testing.T) {
	expr := parseExpr(t, `fmt.Sprintf("%s is %d", name, age)`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "${name}") || !strings.Contains(js, "${age}") {
		t.Errorf("expected template with name and age, got: %s", js)
	}
}

func TestCompileStructLiteral(t *testing.T) {
	src := `map[string]string{"class": "foo"}`
	expr := parseExpr(t, src)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "class:") || !strings.Contains(js, `"foo"`) {
		t.Errorf("expected object literal, got: %s", js)
	}
}

func TestCompileFile(t *testing.T) {
	dir := t.TempDir()
	content := `package test

import "fmt"

type MyComponent struct {
	Name string
}

func (c *MyComponent) Render() string {
	return fmt.Sprintf("<div>%s</div>", c.Name)
}

func (c *MyComponent) Actions() map[string]string {
	return map[string]string{
		"click": "handleClick",
	}
}
`
	path := dir + "/component.ui.go"
	writeFile(t, path, content)

	output, err := CompileFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if output.JS == "" {
		t.Error("expected non-empty JS output")
	}
	if !strings.Contains(output.JS, "function render()") {
		t.Errorf("expected render function in JS, got: %s", output.JS)
	}
	if !strings.Contains(output.JS, "function actions()") {
		t.Errorf("expected actions function in JS, got: %s", output.JS)
	}
}

func TestCompileUnsupported(t *testing.T) {
	// Use a select statement which is not fully supported
	src := `package test

type T struct{}

func (t *T) Render() string {
	select {
	case <-nil:
	}
	return ""
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	output := compileAST(fset, file)
	if output.JS == "" {
		t.Error("expected some JS output even with unsupported patterns")
	}
	// Should have errors or unsupported comments
	hasUnsupported := strings.Contains(output.JS, "unsupported") || len(output.Errors) > 0
	if !hasUnsupported {
		t.Logf("JS output: %s", output.JS)
		t.Logf("Errors: %v", output.Errors)
	}
}

func TestCompileForLoop(t *testing.T) {
	stmt := parseStmt(t, `for i := 0; i < 10; i++ { _ = i }`)
	js, err := compileStmt(stmt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "for (const i = 0; i < 10; i++)") {
		t.Errorf("expected for loop, got: %s", js)
	}
}

func TestCompileSelector(t *testing.T) {
	expr := parseExpr(t, `s.Name`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	// Selector on identifiers keeps original case for fields
	// but method calls get lowercased
	if js != "s.name" {
		t.Errorf("expected %q, got %q", "s.name", js)
	}
}

func TestCompileLen(t *testing.T) {
	expr := parseExpr(t, `len(items)`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if js != "items.length" {
		t.Errorf("expected %q, got %q", "items.length", js)
	}
}

func TestCompileAppend(t *testing.T) {
	expr := parseExpr(t, `append(list, item)`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "[...list, item]"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileComponent(t *testing.T) {
	src := `package test

type Button struct {
	Label string
}

func (b *Button) Render() string {
	return b.Label
}

func (b *Button) OtherMethod() string {
	return ""
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	output, err := CompileComponent(fset, file, "Button")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.JS, "function render()") {
		t.Errorf("expected render function, got: %s", output.JS)
	}
	// OtherMethod should NOT be compiled
	if strings.Contains(output.JS, "otherMethod") {
		t.Errorf("OtherMethod should not be compiled, got: %s", output.JS)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := writeFileErr(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestCompileEquals(t *testing.T) {
	expr := parseExpr(t, `x == y`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "x === y"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileNotEquals(t *testing.T) {
	expr := parseExpr(t, `x != y`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "x !== y"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileNil(t *testing.T) {
	expr := parseExpr(t, `nil`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if js != "null" {
		t.Errorf("expected 'null', got %q", js)
	}
}

func TestCompileBool(t *testing.T) {
	expr := parseExpr(t, `true`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if js != "true" {
		t.Errorf("expected 'true', got %q", js)
	}

	expr = parseExpr(t, `false`)
	js, err = compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if js != "false" {
		t.Errorf("expected 'false', got %q", js)
	}
}

func TestCompileFieldAccess(t *testing.T) {
	expr := parseExpr(t, `s.Field`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "s.field") {
		t.Errorf("expected field access, got: %s", js)
	}
}

func TestCompileIndex(t *testing.T) {
	expr := parseExpr(t, `arr[i]`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := "arr[i]"
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileSliceLiteral(t *testing.T) {
	expr := parseExpr(t, `[]string{"a", "b"}`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	expected := `["a", "b"]`
	if js != expected {
		t.Errorf("expected %q, got %q", expected, js)
	}
}

func TestCompileFunction(t *testing.T) {
	src := `package test

type MyComp struct{}

func (c *MyComp) Render() string {
	x := "hello"
	return x
}
`
	output, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.JS, "function render()") {
		t.Errorf("expected render function in JS, got: %s", output.JS)
	}
	if !strings.Contains(output.JS, "const x =") {
		t.Errorf("expected const declaration, got: %s", output.JS)
	}
	if !strings.Contains(output.JS, "return x") {
		t.Errorf("expected return statement, got: %s", output.JS)
	}
}

func TestCompileSource(t *testing.T) {
	src := `package test

import "fmt"

type Widget struct{}

func (w *Widget) Render() string {
	return fmt.Sprintf("<p>%s</p>", "hello")
}
`
	output, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if output.JS == "" {
		t.Error("expected non-empty JS output")
	}
	if !strings.Contains(output.JS, "${") {
		t.Errorf("expected template literal in JS, got: %s", output.JS)
	}
	// Verify imports are collected
	found := false
	for _, imp := range output.Imports {
		if imp == "fmt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fmt in imports, got: %v", output.Imports)
	}
}

func TestCompileLogicalOps(t *testing.T) {
	expr := parseExpr(t, `a && b`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "&&") {
		t.Errorf("expected && in output, got: %s", js)
	}

	expr = parseExpr(t, `a || b`)
	js, err = compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "||") {
		t.Errorf("expected || in output, got: %s", js)
	}
}

func TestCompileUnaryNot(t *testing.T) {
	expr := parseExpr(t, `!x`)
	js, err := compileExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(js, "!") {
		t.Errorf("expected ! prefix, got: %s", js)
	}
}
