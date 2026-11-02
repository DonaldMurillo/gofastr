// Package nullarray catches a nil slice reaching a JSON field the wire
// schema requires to be an array, where it encodes as null.
//
// The bugs (core/mcp): tools/call answered `"content": null` when a
// handler returned a nil []Content or a ToolResult flagged IsError with no
// blocks (bdb716482), and prompts/get answered `"messages": null` for a
// prompt handler's nil messages (9e207f909). Both fields are required
// arrays in the MCP schema, and validating clients (the TypeScript SDK)
// reject null, so an in-band failure became a client-side protocol
// error. The fixes route the value through contentArray, or guard it
// with `if messages == nil { messages = []PromptMessage{} }`.
//
// Marker-scoped: a slice-typed struct field whose json tag has no
// omitempty and that carries //gofastr:required-array (in its doc comment
// or trailing comment) is a required array. The marker is exported as a
// fact, so a field declared in core/a2a and filled in another package is
// checked there too. A marked field with omitempty is reported: omitempty
// drops an empty slice, which is no more an array than null.
//
// It fires where a marked field is set (a keyed or positional composite
// literal element, or `x.F = v`) from a value not proven non-nil. Proven
// non-nil, each silent:
//   - a composite literal (`[]Content{}`, `[]Content{v}`), make(...);
//   - append with at least one element argument;
//   - a slice expression or conversion of a proven value;
//   - a local every assignment of which is proven (declared with one,
//     never `var x []T` bare), or one guarded before the sink by
//     `if x == nil { x = <proven> }` or `if x == nil { return … }` (also
//     spelled len(x) == 0), where the guard dominates the sink (it sits
//     directly in a block that encloses the sink, not under some other
//     condition), or one read inside `if len(x) > 0` / `if x != nil`;
//   - X.F = append(X.F, …): the field's earlier value was checked where
//     it was set;
//   - a call to a function whose every return is proven (contentArray:
//     its `return c` sits behind `if c == nil { return []Content{} }`),
//     exported as a fact so a helper in another package counts; or one
//     whose every return is proven or a slice of one parameter, called
//     with a proven argument there (core/mcp pageList returns items[:0]
//     or items[a:b]). A multi-result call's first result is the one the
//     facts describe (page, next := pageList(...)).
//
// Anything else fires: a callback's or parameter's result, a field read
// off a parameter, a type-switch binding, a bare `var out []T` filled by
// a loop that may never run, a literal nil.
package nullarray

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name:      "nullarray",
	Doc:       "forbids a possibly-nil slice in a //gofastr:required-array JSON field, which encodes as null where the schema requires an array",
	Run:       run,
	FactTypes: []analysis.Fact{new(requiredArray), new(nonNilResult), new(passThrough)},
}

const marker = "//gofastr:required-array"

// requiredArray marks a struct field as a schema-required array.
type requiredArray struct{}

func (*requiredArray) AFact()         {}
func (*requiredArray) String() string { return "requiredArray" }

// nonNilResult marks a function whose first result is never a nil slice.
type nonNilResult struct{}

func (*nonNilResult) AFact()         {}
func (*nonNilResult) String() string { return "nonNilResult" }

// passThrough marks a function whose first result is non-nil whenever
// its Param-th argument is: every return is proven, or a slice of that
// parameter (core/mcp pageList returns items[:0] or items[a:b]).
type passThrough struct{ Param int }

func (*passThrough) AFact()           {}
func (p *passThrough) String() string { return "passThrough(" + strconv.Itoa(p.Param) + ")" }

func run(pass *analysis.Pass) (any, error) {
	var files []*ast.File
	for _, f := range pass.Files {
		if !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			files = append(files, f)
		}
	}
	collectMarkers(pass, files)

	// Non-nil helpers, to a fixed point (a helper may call another).
	var decls []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && fd.Type.Results != nil && fd.Type.Results.NumFields() >= 1 {
				decls = append(decls, fd)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok || pass.ImportObjectFact(fn, new(nonNilResult)) {
				continue
			}
			if _, isSlice := fn.Signature().Results().At(0).Type().Underlying().(*types.Slice); !isSlice {
				continue
			}
			if pass.ImportObjectFact(fn, new(passThrough)) {
				continue
			}
			ok, param := returnsNonNil(pass, fd)
			switch {
			case ok && param < 0:
				pass.ExportObjectFact(fn, new(nonNilResult))
				changed = true
			case ok:
				pass.ExportObjectFact(fn, &passThrough{Param: param})
				changed = true
			}
		}
	}

	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			checkBody(pass, fd.Body)
		}
	}
	return nil, nil
}

func collectMarkers(pass *analysis.Pass, files []*ast.File) {
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fl := range st.Fields.List {
				if !hasMarker(fl.Doc) && !hasMarker(fl.Comment) {
					continue
				}
				t := pass.TypesInfo.TypeOf(fl.Type)
				if _, ok := t.Underlying().(*types.Slice); !ok {
					pass.Reportf(fl.Pos(), "%s marks a field that is not a slice", marker)
					continue
				}
				if fl.Tag != nil {
					if tag, err := strconv.Unquote(fl.Tag.Value); err == nil {
						if _, opts, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ","); strings.Contains(","+opts+",", ",omitempty,") {
							pass.Reportf(fl.Pos(), "%s field has omitempty: an empty slice is dropped from the object, which no more satisfies a required array than null does", marker)
						}
					}
				}
				for _, name := range fl.Names {
					if obj, ok := pass.TypesInfo.Defs[name].(*types.Var); ok {
						pass.ExportObjectFact(obj, new(requiredArray))
					}
				}
			}
			return true
		})
	}
}

func hasMarker(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if c.Text == marker || strings.HasPrefix(c.Text, marker+" ") {
			return true
		}
	}
	return false
}

func isRequired(pass *analysis.Pass, obj types.Object) bool {
	v, ok := obj.(*types.Var)
	if !ok || !v.IsField() {
		return false
	}
	return pass.ImportObjectFact(v.Origin(), new(requiredArray))
}

func checkBody(pass *analysis.Pass, body *ast.BlockStmt) {
	report := func(field types.Object, val ast.Expr) {
		if proven(pass, body, val, val.Pos(), 0) {
			return
		}
		pass.Reportf(val.Pos(), "%s is a required JSON array, and this value may be nil, which encodes as null; a validating client rejects it. Guard it (if x == nil { x = []T{} }) or route it through a helper that never returns nil", field.Name())
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			t := pass.TypesInfo.TypeOf(x)
			if t == nil {
				return true
			}
			if p, ok := t.Underlying().(*types.Pointer); ok {
				t = p.Elem()
			}
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			for i, el := range x.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					id, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil && isRequired(pass, obj) {
						report(obj, kv.Value)
					}
					continue
				}
				if i < st.NumFields() && isRequired(pass, st.Field(i)) {
					report(st.Field(i), el)
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, l := range x.Lhs {
				sel, ok := l.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if obj := pass.TypesInfo.ObjectOf(sel.Sel); obj != nil && isRequired(pass, obj) {
					report(obj, x.Rhs[i])
				}
			}
		}
		return true
	})
}

// proven reports whether e, evaluated at pos inside body, is never a nil
// slice.
func proven(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr, pos token.Pos, depth int) bool {
	if depth > 8 {
		return false
	}
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.CompositeLit:
		return true
	case *ast.SliceExpr:
		if t := pass.TypesInfo.TypeOf(x.X); t != nil {
			if _, isArr := t.Underlying().(*types.Array); isArr {
				return true
			}
			if p, ok := t.Underlying().(*types.Pointer); ok {
				if _, isArr := p.Elem().Underlying().(*types.Array); isArr {
					return true
				}
			}
		}
		return proven(pass, body, x.X, pos, depth+1)
	case *ast.CallExpr:
		if tv, ok := pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
			return proven(pass, body, x.Args[0], pos, depth+1) // conversion
		}
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok {
				switch b.Name() {
				case "make":
					return true
				case "append":
					if len(x.Args) >= 2 && !x.Ellipsis.IsValid() {
						return true
					}
					// X.F = append(X.F, more...): the field's earlier
					// value was itself checked where it was set.
					if sel, ok := ast.Unparen(x.Args[0]).(*ast.SelectorExpr); ok {
						if obj := pass.TypesInfo.ObjectOf(sel.Sel); obj != nil && isRequired(pass, obj) {
							return true
						}
					}
					if len(x.Args) >= 3 {
						return true
					}
					return len(x.Args) >= 1 && proven(pass, body, x.Args[0], pos, depth+1)
				}
				return false
			}
		}
		var id *ast.Ident
		switch f := ast.Unparen(x.Fun).(type) {
		case *ast.Ident:
			id = f
		case *ast.SelectorExpr:
			id = f.Sel
		case *ast.IndexExpr: // generic instantiation
			if g, ok := f.X.(*ast.Ident); ok {
				id = g
			}
		}
		if id != nil {
			if fn, ok := pass.TypesInfo.Uses[id].(*types.Func); ok {
				if pass.ImportObjectFact(fn.Origin(), new(nonNilResult)) {
					return true
				}
				var pt passThrough
				if pass.ImportObjectFact(fn.Origin(), &pt) && pt.Param < len(x.Args) {
					return proven(pass, body, x.Args[pt.Param], pos, depth+1)
				}
			}
		}
		return false
	case *ast.Ident:
		obj, ok := pass.TypesInfo.Uses[x].(*types.Var)
		if !ok || obj.IsField() || obj.Parent() == pass.Pkg.Scope() {
			return false
		}
		if guarded(pass, body, obj, pos, depth) || insideNonEmpty(pass, body, obj, pos) {
			return true
		}
		return allAssignsProven(pass, body, obj, pos, depth)
	}
	return false
}

// guarded reports whether an `if x == nil` (or len(x) == 0) before pos
// either rebinds x to a proven value or leaves the function.
func guarded(pass *analysis.Pass, body *ast.BlockStmt, obj *types.Var, pos token.Pos, depth int) bool {
	// Only a guard that dominates the sink counts: an if statement listed
	// directly in a block (or case clause) that also encloses pos. A guard
	// nested under another condition (core/mcp richResult's
	// `if len(content) == 0` inside `if r.Structured != nil`) leaves the
	// other path nil.
	var guards []*ast.IfStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos >= n.End() {
			return false
		}
		var list []ast.Stmt
		switch b := n.(type) {
		case *ast.BlockStmt:
			list = b.List
		case *ast.CaseClause:
			list = b.Body
		case *ast.CommClause:
			list = b.Body
		}
		for _, st := range list {
			if ifs, ok := st.(*ast.IfStmt); ok && ifs.End() <= pos {
				guards = append(guards, ifs)
			}
		}
		return true
	})
	found := false
	for _, ifs := range guards {
		if found || !nilTest(pass, ifs.Cond, obj) || len(ifs.Body.List) == 0 {
			continue
		}
		switch last := ifs.Body.List[len(ifs.Body.List)-1].(type) {
		case *ast.ReturnStmt:
			found = true
		case *ast.BranchStmt:
			found = true
		case *ast.AssignStmt:
			for i, l := range last.Lhs {
				if id, ok := l.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj && i < len(last.Rhs) &&
					proven(pass, body, last.Rhs[i], last.Pos(), depth+1) {
					found = true
				}
			}
		}
	}
	return found
}

// insideNonEmpty reports whether pos sits in the body of an
// `if len(x) > 0` / `len(x) != 0` / `x != nil` (alone or as a conjunct).
func insideNonEmpty(pass *analysis.Pass, body *ast.BlockStmt, obj *types.Var, pos token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || found {
			return !found
		}
		if pos >= ifs.Body.Pos() && pos < ifs.Body.End() && nonEmptyTest(pass, ifs.Cond, obj) {
			found = true
		}
		return true
	})
	return found
}

func nonEmptyTest(pass *analysis.Pass, cond ast.Expr, obj *types.Var) bool {
	be, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if be.Op == token.LAND {
		return nonEmptyTest(pass, be.X, obj) || nonEmptyTest(pass, be.Y, obj)
	}
	if be.Op != token.NEQ && be.Op != token.GTR {
		return false
	}
	isObj := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && pass.TypesInfo.Uses[id] == obj
	}
	if call, ok := ast.Unparen(be.X).(*ast.CallExpr); ok && len(call.Args) == 1 {
		if fid, ok := call.Fun.(*ast.Ident); ok && fid.Name == "len" && isObj(call.Args[0]) {
			tv, ok := pass.TypesInfo.Types[be.Y]
			return ok && tv.Value != nil && tv.Value.String() == "0"
		}
	}
	if be.Op == token.NEQ && isObj(be.X) {
		tv, ok := pass.TypesInfo.Types[be.Y]
		return ok && tv.IsNil()
	}
	return false
}

// nilTest reports whether cond is `x == nil` or `len(x) == 0` (alone or
// as a disjunct).
func nilTest(pass *analysis.Pass, cond ast.Expr, obj *types.Var) bool {
	be, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if be.Op == token.LOR {
		return nilTest(pass, be.X, obj) || nilTest(pass, be.Y, obj)
	}
	if be.Op != token.EQL {
		return false
	}
	for _, pair := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
		lhs := ast.Unparen(pair[0])
		if call, ok := lhs.(*ast.CallExpr); ok && len(call.Args) == 1 {
			if fid, ok := call.Fun.(*ast.Ident); ok && fid.Name == "len" {
				if tv, ok := pass.TypesInfo.Types[pair[1]]; ok && tv.Value != nil && tv.Value.String() == "0" {
					lhs = ast.Unparen(call.Args[0])
				}
			}
		}
		id, ok := lhs.(*ast.Ident)
		if !ok || pass.TypesInfo.Uses[id] != obj {
			continue
		}
		if lhs != ast.Unparen(pair[0]) {
			return true // len(x) == 0
		}
		if tv, ok := pass.TypesInfo.Types[pair[1]]; ok && tv.IsNil() {
			return true
		}
	}
	return false
}

// allAssignsProven reports whether obj is declared with a proven value
// and every later assignment before pos is proven too.
func allAssignsProven(pass *analysis.Pass, body *ast.BlockStmt, obj *types.Var, pos token.Pos, depth int) bool {
	declared := false
	ok := true
	ast.Inspect(body, func(n ast.Node) bool {
		if !ok {
			return false
		}
		switch s := n.(type) {
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if pass.TypesInfo.Defs[id] != obj {
					continue
				}
				declared = true
				if i >= len(s.Values) || !proven(pass, body, s.Values[i], s.Pos(), depth+1) {
					ok = false
				}
			}
		case *ast.AssignStmt:
			if s.Pos() > pos {
				return true
			}
			for i, l := range s.Lhs {
				id, isID := l.(*ast.Ident)
				if !isID || pass.TypesInfo.ObjectOf(id) != obj {
					continue
				}
				if s.Tok == token.DEFINE {
					declared = true
				}
				switch {
				case len(s.Lhs) == len(s.Rhs):
					if !proven(pass, body, s.Rhs[i], s.Pos(), depth+1) {
						ok = false
					}
				case len(s.Rhs) == 1 && i == 0:
					// page, next := pageList(...): the facts speak for
					// a call's first result.
					if !proven(pass, body, s.Rhs[0], s.Pos(), depth+1) {
						ok = false
					}
				default:
					ok = false
				}
			}
		case *ast.RangeStmt:
			for _, v := range []ast.Expr{s.Key, s.Value} {
				if id, isID := v.(*ast.Ident); isID && pass.TypesInfo.ObjectOf(id) == obj {
					ok = false
				}
			}
		case *ast.TypeSwitchStmt:
			for _, cc := range s.Body.List {
				if pass.TypesInfo.Implicits[cc] == obj {
					ok = false
				}
			}
		}
		return true
	})
	return declared && ok
}

// returnsNonNil reports whether every return of fd yields a proven first
// result (param < 0), or every return is proven or a slice of one and the
// same parameter (param is its index: the result is non-nil whenever that
// argument is).
func returnsNonNil(pass *analysis.Pass, fd *ast.FuncDecl) (bool, int) {
	params := map[types.Object]int{}
	i := 0
	for _, fl := range fd.Type.Params.List {
		if len(fl.Names) == 0 {
			i++
			continue
		}
		for _, n := range fl.Names {
			if obj := pass.TypesInfo.Defs[n]; obj != nil {
				params[obj] = i
			}
			i++
		}
	}
	param := -1
	ok := true
	sawReturn := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		r, isRet := n.(*ast.ReturnStmt)
		if !isRet || !ok {
			return ok
		}
		sawReturn = true
		if len(r.Results) == 0 {
			ok = false
			return false
		}
		if proven(pass, fd.Body, r.Results[0], r.Pos(), 0) {
			return true
		}
		if p, isParam := sliceOfParam(pass, r.Results[0], params); isParam && (param < 0 || param == p) {
			param = p
			return true
		}
		ok = false
		return true
	})
	return ok && sawReturn, param
}

// sliceOfParam reports whether e is a parameter or a slice expression of
// one, and which.
func sliceOfParam(pass *analysis.Pass, e ast.Expr, params map[types.Object]int) (int, bool) {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.SliceExpr:
			e = x.X
		case *ast.Ident:
			p, ok := params[pass.TypesInfo.Uses[x]]
			return p, ok
		default:
			return 0, false
		}
	}
}
