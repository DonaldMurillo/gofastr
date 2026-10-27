// Package floatnum catches JSON decoded into a target that holds an
// interface value (any, map[string]any, []any, or a struct field of one)
// without UseNumber. encoding/json stores every number under an
// interface as a float64, so an integer past 2^53 comes back with
// different digits: 9007199254740993 reads as 9007199254740992.
//
// The bug class: the 2026-10-26 bug bash fixed about fifteen of these —
// typed hooks round-tripping a field through `var decoded any`, MCP tool
// arguments and results, the generated Go client and CLI, semantic
// search metadata and its HTTP routes, MCP enum comparison, JSON fields
// in CRUD. Each was a json.Unmarshal or Decoder.Decode into an
// interface-holding value. The fix decodes through a json.Decoder that
// calls UseNumber (internal/jsonexact.Unmarshal does that and
// converts each json.Number), or passes handler.UseNumber() to
// handler.UnmarshalStrict.
//
// It fires on json.Unmarshal, (*json.Decoder).Decode, and
// handler.UnmarshalStrict / handler.DecodeStrict, and stays silent when:
//   - the decoder is a local whose UseNumber() is called in the same
//     function, or UnmarshalStrict is passed handler.UseNumber();
//   - the target holds no empty interface (typed numbers keep their Go
//     types), or reaches one only through a type with its own
//     UnmarshalJSON (json.RawMessage is one);
//   - the file is a _test.go file.
//
// A destination that is an interface parameter (`func decode(b []byte,
// v any)`) has its type at the caller. Such a function exports a
// lossyParams fact naming the parameter, a wrapper that hands its own
// interface parameter on inherits it, and the report lands on the call
// that passes a pointer to an interface-holding type, in this package or
// any importer. The A2A JSON-RPC decodeParams and kiln's journal
// Entry.Decode were that shape, and the pointer-only check missed both.
//
// A decode whose numbers are never read (only a string discriminator is
// looked at) carries //gofastr:allow(floatnum) <why>. On a passthrough
// decode the marker also keeps the fact from being exported.
package floatnum

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "floatnum",
	Doc:       "forbids decoding JSON into an interface-holding value without UseNumber; integers past 2^53 lose digits as float64",
	Run:       run,
	FactTypes: []analysis.Fact{(*lossyParams)(nil)},
}

const handlerPkg = "github.com/DonaldMurillo/gofastr/core/handler"

const advice = "decode with jsonexact.Unmarshal (or a json.Decoder that calls UseNumber, then jsonexact.Settle) or pass handler.UseNumber()"

// lossyParams marks a function that decodes JSON without UseNumber
// straight into the interface parameters at these indexes.
type lossyParams struct{ Index []int }

func (*lossyParams) AFact() {}

func (f *lossyParams) String() string { return fmt.Sprintf("lossyParams%v", f.Index) }

func run(pass *analysis.Pass) (any, error) {
	var fns []*ast.FuncDecl
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				fns = append(fns, fn)
			}
		}
	}
	allowed := allow.Allowed(pass, "floatnum")
	local := passthroughs(pass, fns, allowed)
	for _, obj := range slices.SortedFunc(maps.Keys(local), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		pass.ExportObjectFact(obj, &lossyParams{Index: local[obj]})
	}
	for _, fn := range fns {
		checkBody(pass, fn.Body, local)
	}
	return nil, nil
}

// passthroughs finds the functions in fns that decode lossily into one of
// their own interface parameters, directly or by handing it to a function
// that does, iterating until no function gains an index.
func passthroughs(pass *analysis.Pass, fns []*ast.FuncDecl, allowed func(token.Pos) bool) map[*types.Func][]int {
	local := map[*types.Func][]int{}
	add := func(fn *types.Func, i int) bool {
		if slices.Contains(local[fn], i) {
			return false
		}
		local[fn] = append(local[fn], i)
		slices.Sort(local[fn])
		return true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			params := interfaceParams(pass, obj)
			if len(params) == 0 {
				continue
			}
			useNumber := useNumberDecoders(pass, fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || allowed(call.Pos()) {
					return true
				}
				if target, ok := decodeTarget(pass, call, useNumber); ok {
					if i, ok := paramIndex(pass, params, target); ok && add(obj, i) {
						changed = true
					}
					return true
				}
				for _, j := range lossyIndexes(pass, call, local) {
					if i, ok := paramIndex(pass, params, call.Args[j]); ok && add(obj, i) {
						changed = true
					}
				}
				return true
			})
		}
	}
	return local
}

// interfaceParams maps each interface-typed parameter of fn to its index.
// A variadic parameter is a slice, never one of them.
func interfaceParams(pass *analysis.Pass, fn *types.Func) map[types.Object]int {
	sig := fn.Type().(*types.Signature)
	out := map[types.Object]int{}
	for i := range sig.Params().Len() {
		if v := sig.Params().At(i); types.IsInterface(v.Type()) {
			out[v] = i
		}
	}
	return out
}

func paramIndex(pass *analysis.Pass, params map[types.Object]int, e ast.Expr) (int, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return 0, false
	}
	i, ok := params[pass.TypesInfo.ObjectOf(id)]
	return i, ok
}

// lossyIndexes returns the argument indexes of call that its static
// callee decodes lossily, from this package's passthroughs or an
// imported fact.
func lossyIndexes(pass *analysis.Pass, call *ast.CallExpr, local map[*types.Func][]int) []int {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.IndexExpr:
		id, _ = f.X.(*ast.Ident)
	}
	if id == nil {
		return nil
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	fn = fn.Origin()
	idx, ok := local[fn]
	if !ok {
		var fact lossyParams
		if fn.Pkg() == pass.Pkg || !pass.ImportObjectFact(fn, &fact) {
			return nil
		}
		idx = fact.Index
	}
	return slices.DeleteFunc(slices.Clone(idx), func(i int) bool { return i >= len(call.Args) })
}

func checkBody(pass *analysis.Pass, body *ast.BlockStmt, local map[*types.Func][]int) {
	useNumber := useNumberDecoders(pass, body)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if target, ok := decodeTarget(pass, call, useNumber); ok {
			// An interface destination is a passthrough: its type is the
			// caller's, and the report lands there.
			if t, ok := interfaceHolding(pass, target); ok {
				pass.Reportf(call.Pos(), "JSON decoded into %s without UseNumber: an integer past 2^53 comes back as a rounded float64; %s",
					types.TypeString(t, types.RelativeTo(pass.Pkg)), advice)
			}
			return true
		}
		for _, j := range lossyIndexes(pass, call, local) {
			if t, ok := interfaceHolding(pass, call.Args[j]); ok {
				pass.Reportf(call.Pos(), "JSON decoded into %s through %s, which decodes without UseNumber: an integer past 2^53 comes back as a rounded float64; in that function, %s",
					types.TypeString(t, types.RelativeTo(pass.Pkg)), types.ExprString(call.Fun), advice)
			}
		}
		return true
	})
}

// interfaceHolding returns the type e points at when it is a pointer to a
// type a decode can fill with an interface number.
func interfaceHolding(pass *analysis.Pass, e ast.Expr) (types.Type, bool) {
	p, ok := pass.TypesInfo.TypeOf(e).(*types.Pointer)
	if !ok || !holdsInterface(p.Elem(), map[types.Type]bool{}) {
		return nil, false
	}
	return p.Elem(), true
}

// decodeTarget returns the destination argument of a JSON decode call
// that does not keep numbers exact, and false for any other call.
func decodeTarget(pass *analysis.Pass, call *ast.CallExpr, useNumber map[types.Object]bool) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func); ok && fn.Pkg() != nil {
		switch {
		case fn.Pkg().Path() == "encoding/json" && fn.Name() == "Unmarshal" && len(call.Args) == 2:
			return call.Args[1], true
		case fn.Pkg().Path() == handlerPkg && fn.Name() == "DecodeStrict" && len(call.Args) == 2:
			return call.Args[1], true
		case fn.Pkg().Path() == handlerPkg && fn.Name() == "UnmarshalStrict" && len(call.Args) >= 2:
			for _, opt := range call.Args[2:] {
				if isHandlerUseNumber(pass, opt) {
					return nil, false
				}
			}
			return call.Args[1], true
		}
	}
	if sel.Sel.Name != "Decode" || len(call.Args) != 1 || !isJSONDecoder(pass.TypesInfo.TypeOf(sel.X)) {
		return nil, false
	}
	if id, ok := sel.X.(*ast.Ident); ok && useNumber[pass.TypesInfo.ObjectOf(id)] {
		return nil, false
	}
	return call.Args[0], true
}

// useNumberDecoders collects the locals whose UseNumber() is called in
// body.
func useNumberDecoders(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "UseNumber" || !isJSONDecoder(pass.TypesInfo.TypeOf(sel.X)) {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			out[pass.TypesInfo.ObjectOf(id)] = true
		}
		return true
	})
	return out
}

func isHandlerUseNumber(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == handlerPkg && fn.Name() == "UseNumber"
}

func isJSONDecoder(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "encoding/json" && obj.Name() == "Decoder"
}

// holdsInterface reports whether decoding into t can store a number in
// an empty interface: t is one, or reaches one through pointers, slices,
// arrays, map values or exported struct fields, without passing a type
// that decodes itself.
func holdsInterface(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	if decodesItself(t) {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return u.Empty()
	case *types.Pointer:
		return holdsInterface(u.Elem(), seen)
	case *types.Slice:
		return holdsInterface(u.Elem(), seen)
	case *types.Array:
		return holdsInterface(u.Elem(), seen)
	case *types.Map:
		return holdsInterface(u.Elem(), seen)
	case *types.Struct:
		for i := range u.NumFields() {
			f := u.Field(i)
			if (f.Exported() || f.Embedded()) && holdsInterface(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// decodesItself reports whether t or *t has an UnmarshalJSON method.
// json.RawMessage is one, and a []byte besides.
func decodesItself(t types.Type) bool {
	for _, typ := range []types.Type{t, types.NewPointer(t)} {
		if obj, _, _ := types.LookupFieldOrMethod(typ, true, nil, "UnmarshalJSON"); obj != nil {
			if _, ok := obj.(*types.Func); ok {
				return true
			}
		}
	}
	return false
}
