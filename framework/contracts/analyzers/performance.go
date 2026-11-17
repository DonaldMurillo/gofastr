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

var queryMethods = map[string]bool{
	"Query": true, "QueryContext": true, "QueryRow": true, "QueryRowContext": true,
	"Exec": true, "ExecContext": true,
}

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

func regexpInFunc(p *contracts.Pass, rel string, file *ast.File, aliases map[string]string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	visitFuncBodies(file, func(body *ast.BlockStmt, _ *ast.FuncType) {
		ast.Inspect(body, func(n ast.Node) bool {
			for _, fn := range []string{"MustCompile", "Compile", "MustCompilePOSIX", "CompilePOSIX"} {
				call, ok := qualifiedCall(n, aliases, "regexp", fn)
				if !ok {
					continue
				}
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

// queryInLoop reports a query whose statement is a literal and whose other arguments use the loop variable.
func queryInLoop(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	report := func(body ast.Node, loopVars []string) {
		if len(loopVars) == 0 {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
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
