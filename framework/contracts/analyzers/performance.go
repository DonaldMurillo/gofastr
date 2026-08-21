package analyzers

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "performance",
		Doc:  "Per-request work that belongs at init: regexp compilation, N+1 queries, reflection.",
		Rules: []string{
			contracts.RuleRegexpCompilePerCall,
			contracts.RuleQueryInLoop,
			contracts.RuleReflectionPerRequest,
		},
		Run: runPerformance,
	})
}

// queryMethods are the calls that reach the database. Names are matched
// on the selector alone, which is why the set is narrow: `Get` and `Find`
// are excluded because they belong to a hundred non-database types.
var queryMethods = map[string]bool{
	"Query": true, "QueryContext": true, "QueryRow": true, "QueryRowContext": true,
	"Exec": true, "ExecContext": true,
}

// repoQueryMethods are the framework repository calls. They are only
// treated as queries when the receiver name looks like a repository,
// which keeps `list.Get(i)` out of the results.
var repoQueryMethods = map[string]bool{
	"GetByID": true, "FindByID": true, "List": true, "Count": true, "Create": true, "Update": true,
}

func runPerformance(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		aliases := importAliases(file)
		out = append(out, regexpInFunc(p, f.Rel, file, aliases)...)
		out = append(out, queryInLoop(p, f.Rel, file)...)
		out = append(out, reflectInHandler(p, f.Rel, file, aliases)...)
	}
	return out, nil
}

// regexpInFunc reports regexp compilation anywhere inside a function
// body. Package-level `var re = regexp.MustCompile(...)` runs once and is
// the shape being asked for, so it is invisible to this walk by
// construction. Only FuncDecl and FuncLit bodies are visited.
func regexpInFunc(p *contracts.Pass, rel string, file *ast.File, aliases map[string]string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	visitFuncBodies(file, func(body *ast.BlockStmt, _ *ast.FuncType) {
		ast.Inspect(body, func(n ast.Node) bool {
			for _, fn := range []string{"MustCompile", "Compile", "MustCompilePOSIX", "CompilePOSIX"} {
				call, ok := qualifiedCall(n, aliases, "regexp", fn)
				if !ok {
					continue
				}
				// A pattern built from a variable cannot be hoisted, so
				// the advice would not apply.
				if len(call.Args) != 1 {
					return true
				}
				if _, isLit := stringLit(call.Args[0]); !isLit {
					return true
				}
				d := diag(p, contracts.RuleRegexpCompilePerCall, rel, call.Pos(),
					fmt.Sprintf("regexp.%s runs on every call to this function", fn))
				out = append(out, d)
				return true
			}
			return true
		})
	})
	return out
}

// queryInLoop reports the N+1 shape: a query executed once per item,
// parameterised by the item.
//
// The discriminator is which argument the loop variable lands in. Two
// loops look identical to a naive matcher:
//
//	for _, ddl := range schema { db.Exec(ctx, ddl) }        // fine
//	for _, o := range orders   { db.Query(ctx, q, o.ID) }   // N+1
//
// In the first the loop variable *is* the statement, a batch of DDL run
// once at startup, which is exactly how you are supposed to write it. In
// the second the statement is fixed and the variable is a parameter. So
// the rule requires a literal SQL string in the call (proving the
// statement is not the loop variable) and requires the loop variable to
// appear somewhere else in the arguments.
func queryInLoop(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	report := func(body ast.Node, loopVars []string) {
		if len(loopVars) == 0 {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
			// A nested closure is usually a goroutine or a callback with
			// its own lifetime, not the per-row shape.
			if _, isFunc := n.(*ast.FuncLit); isFunc {
				return false
			}
			recv, method, call, ok := selectorCall(n)
			if !ok {
				return true
			}
			isRepo := false
			isQuery := queryMethods[method]
			if !isQuery && repoQueryMethods[method] {
				name := strings.ToLower(exprText(recv))
				isRepo = strings.Contains(name, "repo") || strings.Contains(name, "store") ||
					strings.Contains(name, "dao")
				isQuery = isRepo
			}
			if !isQuery {
				return true
			}
			// A raw query needs a literal statement; without one the loop
			// variable is the statement and this is a batch, not an N+1.
			// Repository calls carry no SQL, so they are exempt from that
			// half.
			if !isRepo && !hasStringLiteralArg(call) {
				return true
			}
			if !usesAnyIdent(call.Args, loopVars) {
				return true
			}
			d := diag(p, contracts.RuleQueryInLoop, rel, n.Pos(),
				fmt.Sprintf("%s.%s runs once per iteration, parameterised by the loop variable", exprText(recv), method))
			d.Evidence = map[string]string{
				"call": exprText(recv) + "." + method,
				"loop": strings.Join(loopVars, ","),
			}
			out = append(out, d)
			return true
		})
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.RangeStmt:
			report(v.Body, identNames(v.Key, v.Value))
		case *ast.ForStmt:
			report(v.Body, forLoopVars(v))
		}
		return true
	})
	return out
}

func identNames(exprs ...ast.Expr) []string {
	var out []string
	for _, e := range exprs {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			out = append(out, id.Name)
		}
	}
	return out
}

func forLoopVars(f *ast.ForStmt) []string {
	assign, ok := f.Init.(*ast.AssignStmt)
	if !ok {
		return nil
	}
	return identNames(assign.Lhs...)
}

func hasStringLiteralArg(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if _, ok := stringLit(arg); ok {
			return true
		}
	}
	return false
}

// usesAnyIdent reports whether any of the named identifiers appears
// anywhere inside the given expressions.
func usesAnyIdent(exprs []ast.Expr, names []string) bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	found := false
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && want[id.Name] {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func reflectInHandler(p *contracts.Pass, rel string, file *ast.File, aliases map[string]string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	visitFuncBodies(file, func(body *ast.BlockStmt, sig *ast.FuncType) {
		if !isRequestHandler(sig) {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
			recv, method, _, ok := selectorCall(n)
			if !ok {
				return true
			}
			ident, isIdent := recv.(*ast.Ident)
			if !isIdent || aliases[ident.Name] != "reflect" {
				return true
			}
			out = append(out, diag(p, contracts.RuleReflectionPerRequest, rel, n.Pos(),
				fmt.Sprintf("reflect.%s runs on every request through this handler", method)))
			return true
		})
	})
	return out
}

// visitFuncBodies calls fn for every function declaration and function
// literal in the file, with its signature.
func visitFuncBodies(file *ast.File, fn func(body *ast.BlockStmt, sig *ast.FuncType)) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Body != nil {
				fn(v.Body, v.Type)
			}
		case *ast.FuncLit:
			if v.Body != nil {
				fn(v.Body, v.Type)
			}
		}
		return true
	})
}
