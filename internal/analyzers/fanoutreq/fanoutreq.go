// Package fanoutreq catches one backend call per element of a
// collection whose length the request chooses.
//
// The bug (a81e9c2d, the 2026-11-01 battery/auth bug bash): the auth
// battery's sessionCookieCandidates returned the value of every cookie
// named like the session cookie, and SessionMiddleware, logout and
// /auth/me judged each candidate with a session-store query (two on
// logout). net/http parses up to 3000 cookies, so one anonymous request
// with a 42 KB Cookie header drove 2999 queries through the app-wide
// middleware. The fix spelling reads only the named cookies, drops
// duplicates and stops at a small cap:
//
//	for _, c := range r.CookiesNamed(name) {
//		if c.Value == "" || slices.Contains(out, c.Value) {
//			continue
//		}
//		out = append(out, c.Value)
//		if len(out) == maxCandidates {
//			break
//		}
//	}
//
// Shape: a range over a request-sized collection — r.Cookies(),
// r.CookiesNamed(k), r.Header.Values(k), r.Header[k], r.Form[k],
// r.PostForm[k], r.URL.Query()[k], r.MultipartForm.Value[k], a local
// bound to one, or the result of a same-package function that collects
// one into the slice it returns with no cap — whose body makes a
// backend call: any call taking a context.Context first (logging,
// tracing and the context package excepted), a database/sql query or
// exec, or an http.Client request.
//
// A cap is a break (or return) under a condition that compares an
// integer (if len(out) == max { break }, if i >= max { return }), or a
// range over a re-sliced collection (xs[:n]). A break taken on success
// (if ok { break }) is no cap: the attacker sends nothing but misses.
//
// Silent postures, deliberately:
//   - a body with no backend call: hashing every Cookie line, copying
//     header values, a constant-time compare per cookie (the desktop and
//     service gates) are CPU on bytes already read and bounded by the
//     header size limit;
//   - a capped loop, and a capped collector (its callers range a
//     bounded slice);
//   - _test.go files.
package fanoutreq

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "fanoutreq",
	Doc:  "report a backend call per element of a request-sized collection (cookies, header values, form values) with no cap",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	decls := pathflow.FuncsWithBodies(pass)
	// collectors: same-package functions returning a slice built by an
	// uncapped range over a request-sized collection.
	collectors := map[*types.Func]bool{}
	for changed := true; changed; {
		changed = false
		for _, d := range decls {
			fn, _ := pass.TypesInfo.Defs[d.Name].(*types.Func)
			if fn == nil || collectors[fn] || !returnsSlice(fn) {
				continue
			}
			bound := pathflow.Bindings(pass, d.Body)
			ast.Inspect(d.Body, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				rs, ok := n.(*ast.RangeStmt)
				if !ok || collectors[fn] {
					return !collectors[fn]
				}
				if src := requestSized(pass, rs.X, bound, collectors); src != "" && !capped(pass, rs) && appends(rs.Body) {
					collectors[fn] = true
					changed = true
				}
				return true
			})
		}
	}

	for _, body := range astx.AllBodies(pass) {
		if f := fileOf(pass, body); f != nil && pathflow.IsTestFile(pass, f) {
			continue
		}
		bound := pathflow.Bindings(pass, body)
		ast.Inspect(body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false // its own body
			}
			rs, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			src := requestSized(pass, rs.X, bound, collectors)
			if src == "" || capped(pass, rs) {
				return true
			}
			if call := backendCall(pass, rs.Body); call != nil {
				pass.Reportf(rs.Pos(),
					"fanoutreq: %s runs once per element of %s, a collection the request sizes (net/http parses up to 3000 cookies); de-duplicate and cap it first (r.CookiesNamed, slices.Contains, break at a small max)",
					types.ExprString(call.Fun), src)
			}
			return true
		})
	}
	return nil, nil
}

func fileOf(pass *analysis.Pass, n ast.Node) *ast.File {
	for _, f := range pass.Files {
		if f.Pos() <= n.Pos() && n.End() <= f.End() {
			return f
		}
	}
	return nil
}

func returnsSlice(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return false
	}
	_, ok = sig.Results().At(0).Type().Underlying().(*types.Slice)
	return ok
}

// requestSized names e when it is a collection the request sizes, ""
// otherwise.
func requestSized(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, collectors map[*types.Func]bool) string {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
			if s := requestSized(pass, b, nil, collectors); s != "" {
				return s
			}
		}
		return ""
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		fn, ok := astx.CalleeFunc(pass, ast.Unparen(x.Fun))
		if !ok {
			return ""
		}
		if collectors[fn] {
			return types.ExprString(x)
		}
		if recvIs(fn, "net/http", "Request") && (fn.Name() == "Cookies" || fn.Name() == "CookiesNamed") {
			return types.ExprString(x)
		}
		if recvIs(fn, "net/http", "Header") && fn.Name() == "Values" && fromRequest(pass, x.Fun.(*ast.SelectorExpr).X) {
			return types.ExprString(x)
		}
	case *ast.IndexExpr:
		t := pass.TypesInfo.TypeOf(x.X)
		if (astx.IsNamed(t, "net/url", "Values") || astx.IsNamed(t, "net/http", "Header") || isMultipartValues(pass, x.X)) && fromRequest(pass, x.X) {
			return types.ExprString(x)
		}
	}
	return ""
}

// isMultipartValues reports whether e is a multipart.Form's Value map.
func isMultipartValues(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Value" && astx.IsNamed(astx.Deref(pass.TypesInfo.TypeOf(sel.X)), "mime/multipart", "Form")
}

// fromRequest reports whether e reads off an *http.Request: r.Header,
// r.Form, r.PostForm, r.URL.Query(), r.MultipartForm.Value.
func fromRequest(pass *analysis.Pass, e ast.Expr) bool {
	for i := 0; i < 4; i++ {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.SelectorExpr:
			if astx.IsNamed(astx.Deref(pass.TypesInfo.TypeOf(x.X)), "net/http", "Request") {
				return true
			}
			e = x.X
		case *ast.CallExpr:
			sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			e = sel.X
		default:
			return false
		}
	}
	return false
}

func recvIs(fn *types.Func, pkg, name string) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil && astx.IsNamed(astx.Deref(sig.Recv().Type()), pkg, name)
}

// capped reports whether rs bounds its iterations: a re-sliced
// collection, or a break/return under an integer comparison.
func capped(pass *analysis.Pass, rs *ast.RangeStmt) bool {
	if s, ok := ast.Unparen(rs.X).(*ast.SliceExpr); ok && s.High != nil {
		return true
	}
	found := false
	var walk func(n ast.Node, nested bool)
	walk = func(n ast.Node, nested bool) {
		ast.Inspect(n, func(n ast.Node) bool {
			if found {
				return false
			}
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				if x != ast.Node(rs) && !nested {
					// An unlabeled break inside targets the inner statement;
					// a return still leaves.
					walk(x, true)
					return false
				}
			case *ast.IfStmt:
				if integerComparison(pass, x.Cond) && leavesLoop(x.Body, nested) {
					found = true
					return false
				}
			}
			return true
		})
	}
	walk(rs.Body, false)
	return found
}

// leavesLoop reports whether body ends in a break of the loop under
// test (an unlabeled break counts only outside nested breakables) or a
// return.
func leavesLoop(body *ast.BlockStmt, nested bool) bool {
	if len(body.List) == 0 {
		return false
	}
	switch s := body.List[len(body.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.BREAK && (!nested || s.Label != nil)
	}
	return false
}

// integerComparison reports whether cond (or a conjunct/disjunct)
// compares an integer value.
func integerComparison(pass *analysis.Pass, cond ast.Expr) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if !ok || found {
			return !found
		}
		switch b.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			if isInteger(pass.TypesInfo.TypeOf(b.X)) && isInteger(pass.TypesInfo.TypeOf(b.Y)) {
				found = true
			}
		}
		return !found
	})
	return found
}

func isInteger(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// appends reports whether body appends to a slice.
func appends(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "append" {
				found = true
			}
		}
		return !found
	})
	return found
}

// quietPkgs are context-taking packages that do no backend work.
var quietPkgs = []string{"context", "log/slog", "runtime/trace", "go.opentelemetry.io/"}

// backendCall returns the first backend call in body: a call taking a
// context.Context first, a database/sql query or exec, or an
// http.Client request.
func backendCall(pass *analysis.Pass, body *ast.BlockStmt) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
		if !ok {
			// A func-typed field or variable: judge by its signature.
			if sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature); ok && takesContext(sig) {
				found = call
			}
			return true
		}
		if fn.Pkg() != nil && quiet(fn.Pkg().Path()) {
			return true
		}
		sig, _ := fn.Type().(*types.Signature)
		switch {
		case sig != nil && takesContext(sig):
			found = call
		case recvIs(fn, "database/sql", "DB") || recvIs(fn, "database/sql", "Tx") || recvIs(fn, "database/sql", "Conn") || recvIs(fn, "database/sql", "Stmt"):
			if strings.HasPrefix(fn.Name(), "Query") || strings.HasPrefix(fn.Name(), "Exec") {
				found = call
			}
		case recvIs(fn, "net/http", "Client"):
			found = call
		case fn.Pkg() != nil && fn.Pkg().Path() == "net/http" && sig != nil && sig.Recv() == nil:
			switch fn.Name() {
			case "Get", "Post", "Head", "PostForm":
				found = call
			}
		}
		return true
	})
	return found
}

// quiet reports whether path is one of quietPkgs or below one of them.
func quiet(path string) bool {
	for _, p := range quietPkgs {
		root := strings.TrimSuffix(p, "/")
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func takesContext(sig *types.Signature) bool {
	return sig.Params().Len() > 0 && astx.IsNamed(sig.Params().At(0).Type(), "context", "Context")
}
