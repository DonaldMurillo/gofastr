// Package unboundedresp catches the unbounded read of an HTTP
// RESPONSE body: io.ReadAll(resp.Body), or a json/xml/yaml Decoder
// seated on it, with no io.LimitReader or http.MaxBytesReader anywhere
// on that body's chain in the function — or one hop away in a
// same-package helper the body is passed to.
//
// Probe: cmd/gofastr/semantic.go remoteQuery/remoteGet (2026-09-07
// round-5 red probes). Both fetch a user-named (GOFASTR_URL) endpoint
// and read its answer with io.ReadAll(resp.Body) /
// json.NewDecoder(resp.Body): a hostile or wedged endpoint pins memory
// or holds the CLI forever on a drip feed. evalrunner/mcpprobe.go
// decodes the booted candidate's /mcp answer the same way.
//
// credfetch reported this shape for credential-bearing fetches only;
// that arm now lives here and covers every client, so the two never
// double-report. unboundedbody owns REQUEST bodies (r.Body) and
// deliberately ignores *http.Response; this is the other half of that
// contract.
//
// Credit (the fix posture): the response body wrapped in
// io.LimitReader / http.MaxBytesReader in the same function — directly
// or through one same-package helper hop (a helper whose body wraps
// its parameter). battery/auth/oidc.go reading 1<<20 is the model.
//
// Silent postures, deliberately:
//   - request bodies (http.Request.Body): unboundedbody owns them;
//   - _test.go files: a test client POSTing fixtures to a local
//     httptest server is not attacker surface;
//   - a body whose response variable's last binding is a composite
//     literal (a program-constructed *http.Response): the byte count
//     is the program's own, not the network's;
//   - reads through helpers more than one hop away: the cap must be
//     visible where the read happens or one call away.
package unboundedresp

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unboundedresp",
	Doc:  "forbids reading an HTTP response body with no size bound (io.LimitReader / http.MaxBytesReader): the endpoint controls the byte count and can pin memory or drip-feed the reader forever",
	Run:  run,
}

const readMsg = "unbounded read of an *http.Response body: the endpoint controls the byte count — wrap it in io.LimitReader or http.MaxBytesReader before reading (battery/auth/oidc.go reads 1<<20)"

func run(pass *analysis.Pass) (any, error) {
	// Same-package helpers whose body wraps a parameter in a limiter:
	// passing resp.Body to one is credit.
	cappingHelpers := map[*types.Func]int{} // fn → index of the capped parameter
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type == nil || fd.Type.Params == nil {
				continue
			}
			idx := 0
			for _, field := range fd.Type.Params.List {
				n := len(field.Names)
				if n == 0 {
					n = 1
				}
				for _, name := range field.Names {
					if wrapsInLimiter(pass, fd.Body, pass.TypesInfo.ObjectOf(name)) {
						if _, ok := cappingHelpers[funcOf(pass, fd)]; !ok {
							cappingHelpers[funcOf(pass, fd)] = indexOfParam(pass, fd, name)
						}
					}
				}
				idx += n
			}
		}
	}

	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		// Response variables that are program-constructed, not fetched.
		literalResp := map[types.Object]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range assign.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(assign.Rhs) {
					continue
				}
				rhs := assign.Rhs[i]
				if u, ok := rhs.(*ast.UnaryExpr); ok && u.Op == token.AND {
					rhs = u.X
				}
				if lit, ok := rhs.(*ast.CompositeLit); ok && isResponseLit(pass, lit) {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						literalResp[obj] = true
					}
				}
			}
			return true
		})

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// io.ReadAll(resp.Body)
			if qualified(pass, call.Fun) == "io.ReadAll" && len(call.Args) == 1 {
				if obj, ok := bodyOwner(pass, call.Args[0]); ok {
					reportIfUnbounded(pass, f, call, obj, literalResp, cappingHelpers)
				}
				return true
			}
			// json.NewDecoder(resp.Body) / xml.NewDecoder / yaml decoders
			switch qualified(pass, call.Fun) {
			case "json.NewDecoder", "xml.NewDecoder", "yaml.NewDecoder":
				if len(call.Args) >= 1 {
					if obj, ok := bodyOwner(pass, call.Args[0]); ok {
						reportIfUnbounded(pass, f, call, obj, literalResp, cappingHelpers)
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

// reportIfUnbounded fires when the response variable's body is read
// without a limiter anywhere in the file's function: directly, or
// through a one-hop helper that caps it.
func reportIfUnbounded(pass *analysis.Pass, f *ast.File, call *ast.CallExpr, resp types.Object, literalResp map[types.Object]bool, cappingHelpers map[*types.Func]int) {
	if literalResp[resp] {
		return
	}
	fn := enclosingFunc(f, call.Pos())
	if fn == nil {
		return
	}
	if funcBoundsBody(pass, fn, resp) {
		return
	}
	// One hop: a same-package call passing this response's body (or
	// the response itself) to a helper that caps that parameter.
	if passesToCappingHelper(pass, fn, resp, cappingHelpers) {
		return
	}
	pass.Reportf(call.Pos(), readMsg)
}

// funcBoundsBody reports whether fn wraps resp's Body (or the whole
// resp) in io.LimitReader / http.MaxBytesReader.
func funcBoundsBody(pass *analysis.Pass, fn *ast.FuncDecl, resp types.Object) bool {
	bounded := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if bounded {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualified(pass, call.Fun) {
		case "io.LimitReader", "http.MaxBytesReader":
			for _, arg := range call.Args {
				if mentionsObject(arg, resp, pass) {
					bounded = true
					return false
				}
			}
		}
		return true
	})
	return bounded
}

// passesToCappingHelper reports whether fn passes this response's body
// (or the response) to a same-package helper that wraps that
// parameter in a limiter.
func passesToCappingHelper(pass *analysis.Pass, fn *ast.FuncDecl, resp types.Object, cappingHelpers map[*types.Func]int) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := calleeFunc(pass, call)
		if !ok {
			return true
		}
		argIdx, ok := cappingHelpers[callee]
		if !ok || argIdx >= len(call.Args) {
			return true
		}
		if mentionsObject(call.Args[argIdx], resp, pass) {
			found = true
			return false
		}
		return true
	})
	return found
}

// wrapsInLimiter reports whether body wraps the parameter in a
// limiter (the readBodyCap helper shape: io.LimitReader(param, n)).
func wrapsInLimiter(pass *analysis.Pass, body *ast.BlockStmt, param types.Object) bool {
	if param == nil {
		return false
	}
	wrapped := false
	ast.Inspect(body, func(n ast.Node) bool {
		if wrapped {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualified(pass, call.Fun) {
		case "io.LimitReader", "http.MaxBytesReader":
			for _, arg := range call.Args {
				if mentionsObject(arg, param, pass) {
					wrapped = true
					return false
				}
			}
		}
		return true
	})
	return wrapped
}

// bodyOwner resolves X in X.Body to X's object, when X.Body is the
// *http.Response field (not a request body or a user struct).
func bodyOwner(pass *analysis.Pass, arg ast.Expr) (types.Object, bool) {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Body" {
		return nil, false
	}
	base := pass.TypesInfo.TypeOf(sel.X)
	if base == nil {
		return nil, false
	}
	if p, ok := base.(*types.Pointer); ok {
		base = p.Elem()
	}
	named, ok := types.Unalias(base).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil ||
		named.Obj().Pkg().Path() != "net/http" || named.Obj().Name() != "Response" {
		return nil, false
	}
	switch x := sel.X.(type) {
	case *ast.Ident:
		if obj := pass.TypesInfo.ObjectOf(x); obj != nil {
			return obj, true
		}
	case *ast.SelectorExpr:
		if obj := pass.TypesInfo.ObjectOf(x.Sel); obj != nil {
			return obj, true
		}
	case *ast.StarExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				return obj, true
			}
		}
	}
	return nil, false
}

func isResponseLit(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj() != nil && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Response"
}

// mentionsObject reports whether e mentions obj (the response
// variable) — as resp, resp.Body, &resp.
func mentionsObject(e ast.Expr, obj types.Object, pass *analysis.Pass) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
			found = true
			return false
		}
		return true
	})
	return found
}

// funcOf resolves a FuncDecl to its *types.Func object.
func funcOf(pass *analysis.Pass, fd *ast.FuncDecl) *types.Func {
	if fd.Name == nil {
		return nil
	}
	if obj := pass.TypesInfo.ObjectOf(fd.Name); obj != nil {
		if fn, ok := obj.(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// indexOfParam returns the positional index of name among fd's
// parameters.
func indexOfParam(pass *analysis.Pass, fd *ast.FuncDecl, name *ast.Ident) int {
	idx := 0
	for _, field := range fd.Type.Params.List {
		for _, n := range field.Names {
			if pass.TypesInfo.ObjectOf(n) == pass.TypesInfo.ObjectOf(name) {
				return idx
			}
			idx++
		}
	}
	return idx
}

func enclosingFunc(f *ast.File, pos token.Pos) *ast.FuncDecl {
	var match *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Pos() <= pos && pos <= fd.End() {
			match = fd
		}
	}
	return match
}

func calleeFunc(pass *analysis.Pass, call *ast.CallExpr) (*types.Func, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		fn, ok := pass.TypesInfo.ObjectOf(fun).(*types.Func)
		return fn, ok
	case *ast.SelectorExpr:
		fn, ok := pass.TypesInfo.Uses[fun.Sel].(*types.Func)
		return fn, ok
	}
	return nil, false
}

// qualified renders a call target as "pkg.Func" through the type
// checker so an aliased import is still the real package.
func qualified(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if pass == nil {
		return id.Name + "." + sel.Sel.Name
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Name() + "." + sel.Sel.Name
}

func isTest(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}
