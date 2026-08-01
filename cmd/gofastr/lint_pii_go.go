package main

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Go-source PII lint — the Go-declared counterpart of lintBlueprintPIIRoot.
// CLAUDE.md hard rule #6 is a property of the declared entity, not of the
// authoring format: an `app.Entity("members", framework.EntityConfig{
// Fields: []schema.Field{{Name: "email", Type: schema.String}}})` with
// auto-CRUD on and no owner_field/multi_tenant/access exposes every row to
// every OTHER authenticated user exactly as a blueprint entity would, so it
// must surface the SAME rule "unscoped-pii" — it's the same defect class.
//
// The declaration is rebuilt from the AST via pack.go's entity-config
// readers (packEntityDeclFromCall — the exact reverse `gofastr pack` uses to
// recover a blueprint), then judged through the identical exposure/scope/
// field-name logic by wrapping it in a throwaway Blueprint and calling
// lintUnscopedPII. One yardstick for both declaration styles.
//
// Like the pack reader this is best-effort over the AST: a config bound to a
// variable before the call (`cfg := EntityConfig{...}; app.Entity(n, cfg)`)
// has no composite literal to read and is silently skipped — the generator
// and conventional hand-written registrations inline the literal, which is
// the shape this lint targets.
func lintGoSourcePII(rel string, body []byte) []LintFinding {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil || file == nil {
		return nil
	}
	var out []LintFinding
	for _, call := range goEntityCalls(file) {
		decl := packEntityDeclFromCall(call)
		if decl.Name == "" {
			continue // not an entity registration we can read
		}
		bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
		for _, f := range lintUnscopedPII(bp) {
			out = append(out, LintFinding{
				File:    rel,
				Line:    fset.Position(call.Pos()).Line,
				Rule:    "unscoped-pii",
				Message: f.Message(),
			})
		}
	}
	return out
}

// goEntityCalls returns every `app.Entity(<name>, <config>)` call expression
// in file. The receiver must be an identifier named "app" — the
// conventional host-app builder the generator and hand-written
// registrations use — with exactly two arguments, so unrelated `.Entity(a,
// b)` methods elsewhere in app code don't generate noise. Declaring the
// entity through any other receiver is outside this lint's contract.
func goEntityCalls(file *ast.File) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Entity" || len(call.Args) != 2 {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "app" {
			return true
		}
		calls = append(calls, call)
		return true
	})
	return calls
}
