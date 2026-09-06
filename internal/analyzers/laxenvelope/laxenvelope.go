// Package laxenvelope catches a transport that decodes the SAME
// envelope type lax at one site while the same package decodes that
// type strictly at another.
//
// Probe: core/mcp/stdioenv_red_test.go::TestStdioEnvelopeAmbiguityRefused
// (2026-09-05 round-4 red probes). core/mcp/transport.go decoded the
// JSON-RPC request strictly on the HTTP path (handler.UnmarshalStrict,
// which refuses duplicate and case-folded top-level keys) but served
// the same *Request type on stdio through a bare json.Unmarshal: a
// frame carrying {"method":"safe","method":"danger"} executes the
// second method over stdio while every first-occurrence parser —
// proxy, audit log, WAF — read the first. The strictness contract
// belongs to the TYPE, not to the transport that happens to carry it.
//
// Shape: within one package, a type T is decoded through
// UnmarshalStrict / DecodeStrict (by name, any package that provides
// one) at one site, and through json.Unmarshal /
// (*json.Decoder).Decode at another; the lax site is reported. The fix
// posture is to decode that site through the same strict helper.
//
// Silent postures, deliberately:
//   - types decoded lax here but never strictly in this package: no
//     strictness contract exists to break — a config file read through
//     json.Unmarshal is not an envelope;
//   - destinations of type any / an interface / a type parameter
//     (generic decode helpers like crud's UnmarshalStrict(raw, v) with
//     v any): no concrete type to compare, and the concrete call sites
//     are where the contract is visible;
//   - _test.go files on both sides: a test's lax decode pins no
//     production contract and must not decide one.
package laxenvelope

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "laxenvelope",
	Doc:  "forbids decoding a type lax (json.Unmarshal / json.Decoder.Decode) at one site while the same package decodes that type strictly (UnmarshalStrict/DecodeStrict) at another; decode every site of the type through the strict helper",
	Run:  run,
}

const laxMsg = "lax decode of %s, which this package also decodes strictly elsewhere: stdlib json keeps the last duplicate key and folds key case, so a first-occurrence parser reads a different request than this dispatcher runs — decode this site through the strict helper too"

func run(pass *analysis.Pass) (any, error) {
	strict := map[types.Type]bool{}
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			fn, ok := calleeFunc(pass, call)
			if !ok {
				return true
			}
			switch fn.Name() {
			case "UnmarshalStrict", "DecodeStrict":
				if t, ok := dstType(pass, call.Args[1]); ok {
					strict[t] = true
				}
			}
			return true
		})
	}
	if len(strict) == 0 {
		return nil, nil
	}
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			dst, lax := laxDecodeDst(pass, call)
			if !lax {
				return true
			}
			t, ok := dstType(pass, dst)
			if !ok || !strict[t] {
				return true
			}
			pass.Reportf(call.Pos(), laxMsg, typeLabel(pass, t))
			return true
		})
	}
	return nil, nil
}

// laxDecodeDst recognizes the lax decode spellings — json.Unmarshal
// and a Decode method on a *json.Decoder — returning the destination
// argument.
func laxDecodeDst(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func); ok && len(call.Args) == 2 {
		if fn.Pkg() != nil && fn.Pkg().Path() == "encoding/json" && fn.Name() == "Unmarshal" {
			return call.Args[1], true
		}
		return nil, false
	}
	if sel.Sel.Name != "Decode" || len(call.Args) != 1 {
		return nil, false
	}
	recv, ok := pass.TypesInfo.Types[sel.X]
	if !ok || recv.Type == nil {
		return nil, false
	}
	if p, ok := recv.Type.(*types.Pointer); ok {
		if named, ok := p.Elem().(*types.Named); ok && named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "encoding/json" && named.Obj().Name() == "Decoder" {
			return call.Args[0], true
		}
	}
	return nil, false
}

// dstType resolves the decoded type: &v and v (already a pointer) both
// give T. any/interface/type-parameter destinations have no concrete
// type to compare.
func dstType(pass *analysis.Pass, arg ast.Expr) (types.Type, bool) {
	t := pass.TypesInfo.TypeOf(arg)
	if t == nil {
		return nil, false
	}
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	switch t.(type) {
	case *types.Interface, *types.TypeParam:
		return nil, false
	}
	if _, ok := t.Underlying().(*types.Interface); ok {
		return nil, false
	}
	return t, true
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

func typeLabel(pass *analysis.Pass, t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == pass.Pkg {
			return ""
		}
		return p.Name()
	})
}

func isTest(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}
