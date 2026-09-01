// Package unboundedbody catches an inbound HTTP request body that is read
// or decoded without a size cap, so a single request can spend the
// server's memory.
//
// The class is a repeat offender here, fixed one sink at a time: multipart
// bodies capped in memory but not on the wire, an unbounded IN-list split,
// retained rate-limit key bytes, SSE subscription state, attacker-named
// DNS lookups. Every fix was correct and none of them stopped the next
// one, because "did anyone cap this body" is not visible at the call site.
//
// The check is type-aware on purpose. The dangerous shape is
// io.ReadAll(r.Body) where r is an inbound *http.Request; the identical
// spelling on an *http.Response is an outbound call whose peer you chose,
// a different and much smaller risk. A grep cannot tell those apart, which
// is why this lives in the vet lane rather than the contracts one.
//
// A cap counts when http.MaxBytesReader or io.LimitReader appears anywhere
// in the same file: middleware that wraps a body is conventionally written
// beside the handlers it wraps (battery/semantic/routes.go is the model),
// and demanding the wrap be in the same function would flag correct code.
package unboundedbody

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrunboundedbody",
	Doc:  "forbids reading or decoding an inbound *http.Request body with no size cap; wrap it in http.MaxBytesReader first",
	Run:  run,
}

// sinks are the calls that consume a whole body. The int is the argument
// position holding the reader.
var sinks = map[string]int{
	"io.ReadAll":        0,
	"io.Copy":           1,
	"json.NewDecoder":   0,
	"xml.NewDecoder":    0,
	"json.Unmarshal":    0,
	"httputil.DumpBody": 0,
	"io.ReadFull":       0,
}

func run(pass *analysis.Pass) (any, error) {
	cappers := cappingHelpers(pass)
	for _, f := range pass.Files {
		filename := pass.Fset.Position(f.Pos()).Filename
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		if fileHasCap(f, cappers) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			argIdx, ok := sinks[qualifiedName(pass, call.Fun)]
			if !ok || argIdx >= len(call.Args) {
				return true
			}
			sel, ok := call.Args[argIdx].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" {
				return true
			}
			if !isInboundRequest(pass, sel.X) {
				return true
			}
			pass.Reportf(call.Pos(),
				"reads an inbound request body with no size cap: wrap it first (r.Body = http.MaxBytesReader(w, r.Body, max)) so one request cannot spend the server's memory")
			return true
		})
	}
	return nil, nil
}

// isInboundRequest reports whether expr is a *net/http.Request — the
// inbound side. An *http.Response body is a different risk class and is
// deliberately not flagged.
func isInboundRequest(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	t := tv.Type
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Request" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "net/http"
}

// cappingHelpers collects the package's own functions that establish a body
// cap, so a package that factors the wrap into a named helper
// (`limitJSONBody(w, r)`) is read the same as one that inlines
// http.MaxBytesReader. Without this the analyzer sees only the file it is
// looking at, and factoring out the wrap — the tidier way to write it —
// would fail the check.
func cappingHelpers(pass *analysis.Pass) map[string]bool {
	out := map[string]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if hasCapCall(fn.Body) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// fileHasCap reports whether this file establishes a body cap anywhere,
// either directly or by calling one of the package's capping helpers.
// Middleware that wraps a body lives beside its handlers by convention.
func fileHasCap(f *ast.File, cappers map[string]bool) bool {
	if hasCapCall(f) {
		return true
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && cappers[id.Name] {
			found = true
		}
		return !found
	})
	return found
}

// hasCapCall reports whether n mentions one of the standard capping
// constructors.
func hasCapCall(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "MaxBytesReader", "LimitReader", "LimitedReader":
			found = true
		}
		return !found
	})
	return found
}

// qualifiedName renders a call target as "pkg.Func", resolving the import
// through the type checker so an aliased import is still the real package.
func qualifiedName(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkgName, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Name() + "." + sel.Sel.Name
}
