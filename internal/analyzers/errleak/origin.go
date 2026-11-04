package errleak

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The decode and rewrap arms (2026-11 bug bashes). Both ask where an
// error value came from, so both share this origin walk.
//
// Decode (1a4d9e2a): a malformed body answered with encoding/json's own
// text, which names the server's Go types ("cannot unmarshal number into
// Go struct field batchDeleteRequest.ids of type string", "Go value of
// type map[string]interface {}"). The 4xx posture above is right for a
// parser written for clients; encoding/json's text is written for the
// programmer. An error that comes from json.Unmarshal or
// (*json.Decoder).Decode, directly, wrapped by fmt.Errorf("%w") or
// errors.Join, or returned by a function that returns one (an exported
// rawJSONErr fact, so readRequestBody-style helpers in another package
// count), fires when its text reaches a client response at ANY status:
// a call carrying a 4xx/5xx status constant, a call carrying a JSON-RPC
// error-code constant (an Err*/Code* integer constant: ErrParse,
// ErrInvalidParams), or a struct literal with Code and Message fields
// (an *RPCError). The fix spelling is handler.JSONErrorDetail(err): the
// JSON path in the client's key spelling and the JSON type expected
// there, as a phrase; its result is not a json error, so
// JSONErrorDetail(err).Error() is quiet.
//
// Rewrap (d01462e8a): WithToolGate and the server-wide gate wrapped a
// gate's own *RPCError as &RPCError{Code: ErrInvalidParams, Message:
// gErr.Error()}, so a client keyed on the code the host chose saw
// -32602. It fires on a composite literal of an error type T (T or *T
// implements error) with a field filled from x.Error(), or on a call
// whose result is *T or carries a *T field (newErrorResponse) handed
// x.Error(), when x is an interface-typed error that may already be a
// *T: the result of a call through a func value (a host callback: a
// gate, a hook), of a function that returns one (an exported
// foreignErr fact; checkServerGate returns gate(ctx)), or a parameter of
// the enclosing function. Quiet when the function tests x for *T first
// (errors.As with a *T target, errors.AsType[*T], x.(*T), a type switch
// with a *T case: gateRefusal's pass-through), when x is a concrete
// type, and when x came from a declared function that never returns a
// callback's error (a module toggle, a validator).

// rawJSONErr marks a function whose error result may be encoding/json's
// own error, unphrased.
type rawJSONErr struct{}

func (*rawJSONErr) AFact()         {}
func (*rawJSONErr) String() string { return "rawJSONErr" }

// foreignErr marks a function whose error result may be a callback's
// error, of any concrete type.
type foreignErr struct{}

func (*foreignErr) AFact()         {}
func (*foreignErr) String() string { return "foreignErr" }

type origin uint8

const (
	fromJSON origin = 1 << iota
	fromForeign
	fromParam
)

type originCtx struct {
	pass   *analysis.Pass
	json   map[*types.Func]bool
	foreig map[*types.Func]bool
}

// fnOrigins evaluates origins inside one declaration.
type fnOrigins struct {
	*originCtx
	fd     *ast.FuncDecl
	params map[types.Object]bool
	defs   map[types.Object][]errDef
	busy   map[types.Object]bool
}

type errDef struct {
	pos token.Pos
	rhs ast.Expr
}

func (c *originCtx) forFunc(fd *ast.FuncDecl) *fnOrigins {
	f := &fnOrigins{originCtx: c, fd: fd, params: map[types.Object]bool{}, defs: map[types.Object][]errDef{}, busy: map[types.Object]bool{}}
	if fd.Type.Params != nil {
		for _, field := range fd.Type.Params.List {
			for _, n := range field.Names {
				f.params[c.pass.TypesInfo.Defs[n]] = true
			}
		}
	}
	info := c.pass.TypesInfo
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok {
					continue
				}
				obj := info.Defs[id]
				if obj == nil {
					obj = info.Uses[id]
				}
				if obj == nil {
					continue
				}
				var rhs ast.Expr
				if len(x.Rhs) == len(x.Lhs) {
					rhs = x.Rhs[i]
				} else if len(x.Rhs) == 1 {
					rhs = x.Rhs[0]
				}
				f.defs[obj] = append(f.defs[obj], errDef{x.Pos(), rhs})
			}
		case *ast.ValueSpec:
			for i, n := range x.Names {
				var rhs ast.Expr
				if i < len(x.Values) {
					rhs = x.Values[i]
				} else if len(x.Values) == 1 {
					rhs = x.Values[0]
				}
				f.defs[info.Defs[n]] = append(f.defs[info.Defs[n]], errDef{x.Pos(), rhs})
			}
		}
		return true
	})
	return f
}

// of is the origin of the error expression e as it stands at pos.
func (f *fnOrigins) of(e ast.Expr, pos token.Pos) origin {
	info := f.pass.TypesInfo
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := info.Uses[x]
		if obj == nil || f.busy[obj] {
			return 0
		}
		// The latest assignment before pos is the reaching one.
		var best *errDef
		for i := range f.defs[obj] {
			d := &f.defs[obj][i]
			if d.pos < pos && (best == nil || d.pos > best.pos) {
				best = d
			}
		}
		if best == nil {
			if f.params[obj] {
				return fromParam
			}
			return 0
		}
		if best.rhs == nil {
			return 0
		}
		f.busy[obj] = true
		defer delete(f.busy, obj)
		return f.of(best.rhs, best.pos)
	case *ast.CallExpr:
		return f.callOrigin(x, pos)
	}
	return 0
}

func (f *fnOrigins) callOrigin(call *ast.CallExpr, pos token.Pos) origin {
	info := f.pass.TypesInfo
	var obj types.Object
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj = info.Uses[fun]
	case *ast.SelectorExpr:
		obj = info.Uses[fun.Sel]
	case *ast.IndexExpr: // generic instantiation
		if s, ok := fun.X.(*ast.SelectorExpr); ok {
			obj = info.Uses[s.Sel]
		} else if id, ok := fun.X.(*ast.Ident); ok {
			obj = info.Uses[id]
		}
	default:
		return 0
	}
	switch ob := obj.(type) {
	case *types.Var:
		// A call through a func value: a host callback, whose error can
		// be any type the host chose. A local bound only to func
		// literals in this body is the function's own code (json
		// detail's pick helper), not a callback: the origin is the
		// union of the literals' own returns, so a decoder bound to a
		// literal still carries encoding/json's error.
		if _, ok := ob.Type().Underlying().(*types.Signature); !ok {
			return 0
		}
		if ds := f.defs[ob]; len(ds) > 0 {
			own := true
			for _, d := range ds {
				if _, lit := ast.Unparen(d.rhs).(*ast.FuncLit); !lit {
					own = false
				}
			}
			if own {
				// The literal's own code: take the origin of its
				// returns instead of dropping it — a decoder bound to
				// a literal still returns encoding/json's error
				// (fromJSONLit), while a callback's error stays
				// foreign only when the literal itself calls one. A
				// literal that calls the var itself (re-bound in a
				// retry loop, or two literals calling each other)
				// re-enters here: the busy guard cuts the cycle —
				// any origin reachable through the self call is
				// reachable through the literal's other returns too.
				if f.busy[ob] {
					return 0
				}
				f.busy[ob] = true
				defer delete(f.busy, ob)
				var o origin
				for _, d := range ds {
					o |= f.litReturns(ast.Unparen(d.rhs).(*ast.FuncLit))
				}
				return o
			}
		}
		return fromForeign
	case *types.Func:
		fn := ob.Origin()
		if fn.Pkg() != nil && fn.Pkg().Path() == "encoding/json" {
			switch fn.Name() {
			case "Unmarshal", "Decode":
				return fromJSON
			}
			return 0
		}
		if fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && fn.Name() == "Errorf" {
			if len(call.Args) == 0 {
				return 0
			}
			format, ok := constString(f.pass, call.Args[0])
			if !ok || !strings.Contains(format, "%w") {
				return 0
			}
			var o origin
			for _, a := range call.Args[1:] {
				o |= f.of(a, pos)
			}
			return o &^ fromParam
		}
		if fn.Pkg() != nil && fn.Pkg().Path() == "errors" && fn.Name() == "Join" {
			var o origin
			for _, a := range call.Args {
				o |= f.of(a, pos)
			}
			return o &^ fromParam
		}
		var o origin
		if f.json[fn] || f.importFact(fn, &rawJSONErr{}) {
			o |= fromJSON
		}
		if f.foreig[fn] || f.importFact(fn, &foreignErr{}) {
			o |= fromForeign
		}
		return o
	}
	return 0
}

func (f *fnOrigins) importFact(fn *types.Func, fact analysis.Fact) bool {
	if fn.Pkg() == nil || fn.Pkg() == f.pass.Pkg {
		return false
	}
	return f.pass.ImportObjectFact(fn, fact)
}

// litReturns is the union of the origins a func literal's error
// results carry.
func (f *fnOrigins) litReturns(lit *ast.FuncLit) origin {
	if lit == nil || lit.Body == nil {
		return 0
	}
	var o origin
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, r := range x.Results {
				if isErrorType(f.pass.TypesInfo.TypeOf(r)) {
					o |= f.of(r, x.Pos())
				}
			}
		}
		return true
	})
	return o
}

// returnsOrigin is the union of the origins a declaration's error
// results carry, literals' own returns excluded.
func (f *fnOrigins) returnsOrigin() origin {
	var o origin
	ast.Inspect(f.fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, r := range x.Results {
				if isErrorType(f.pass.TypesInfo.TypeOf(r)) {
					o |= f.of(r, x.Pos())
				}
			}
		}
		return true
	})
	return o
}

func isErrorType(t types.Type) bool {
	return t != nil && types.Implements(t, errorInterface)
}

func constString(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return "", false
	}
	s, err := strconv.Unquote(tv.Value.ExactString())
	return s, err == nil
}

// summarize computes the rawJSONErr and foreignErr summaries of every
// declaration in the package to a fixed point and exports them.
func summarize(pass *analysis.Pass, decls []*ast.FuncDecl) *originCtx {
	c := &originCtx{pass: pass, json: map[*types.Func]bool{}, foreig: map[*types.Func]bool{}}
	for round := 0; round < 16; round++ {
		changed := false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			o := c.forFunc(fd).returnsOrigin()
			if o&fromJSON != 0 && !c.json[fn] {
				c.json[fn], changed = true, true
			}
			if o&fromForeign != 0 && !c.foreig[fn] {
				c.foreig[fn], changed = true, true
			}
		}
		if !changed {
			break
		}
	}
	// The standard library is not summarised: its callbacks (a
	// sort.Slice less, an fs.WalkDir visitor) are its own business, and
	// json.Unmarshal / Decode are named outright.
	if isStdlib(pass) {
		return c
	}
	for fn := range c.json {
		pass.ExportObjectFact(fn, &rawJSONErr{})
	}
	for fn := range c.foreig {
		pass.ExportObjectFact(fn, &foreignErr{})
	}
	return c
}

// isStdlib: an import path with no dot in its first element, outside a
// testdata tree (fixture packages share the spelling).
func isStdlib(pass *analysis.Pass) bool {
	if strings.Contains(strings.SplitN(pass.Pkg.Path(), "/", 2)[0], ".") {
		return false
	}
	for _, f := range pass.Files {
		return !strings.Contains(filepath.ToSlash(pass.Fset.Position(f.Pos()).Filename), "/testdata/")
	}
	return false
}

// errorTexts returns every error expression whose text e builds in
// place: x.Error() receivers, and the error operands of fmt.Sprint /
// Sprintln / Sprintf with a text verb.
func errorTexts(pass *analysis.Pass, e ast.Node) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Error" && len(call.Args) == 0 {
			if tv, ok := pass.TypesInfo.Types[sel.X]; ok && isErrorType(tv.Type) {
				out = append(out, sel.X)
			}
			return true
		}
		if xid, ok := sel.X.(*ast.Ident); ok {
			if pkg, ok := pass.TypesInfo.Uses[xid].(*types.PkgName); ok && pkg.Imported().Path() == "fmt" {
				args := call.Args
				switch sel.Sel.Name {
				case "Sprint", "Sprintln":
				case "Sprintf":
					if len(args) < 2 {
						return true
					}
					s, ok := constString(pass, args[0])
					if !ok || !textVerb.MatchString(s) {
						return true
					}
					args = args[1:]
				default:
					return true
				}
				for _, a := range args {
					if tv, ok := pass.TypesInfo.Types[a]; ok && isErrorType(tv.Type) {
						out = append(out, a)
					}
				}
			}
		}
		return true
	})
	return out
}

// rpcCodeName: a JSON-RPC error-code constant (ErrParse,
// ErrInvalidParams, CodeInvalidRequest).
var rpcCodeName = regexp.MustCompile(`^(Err|Code)[A-Z]\w*$`)

func hasStatusArg(pass *analysis.Pass, args []ast.Expr) bool {
	for _, a := range args {
		if n, ok := intConst(pass, a); ok && n >= 400 && n <= 599 {
			return true
		}
	}
	return false
}

func hasRPCCodeArg(pass *analysis.Pass, args []ast.Expr) bool {
	for _, a := range args {
		var id *ast.Ident
		switch x := ast.Unparen(a).(type) {
		case *ast.Ident:
			id = x
		case *ast.SelectorExpr:
			id = x.Sel
		}
		if id == nil || !rpcCodeName.MatchString(id.Name) {
			continue
		}
		if _, ok := intConst(pass, a); ok {
			return true
		}
	}
	return false
}

func intConst(pass *analysis.Pass, e ast.Expr) (int64, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return 0, false
	}
	b, ok := tv.Type.Underlying().(*types.Basic)
	if !ok || b.Info()&types.IsInteger == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(tv.Value.ExactString(), 10, 64)
	return n, err == nil
}

// codeMessageStruct: a struct literal type with Code and Message
// fields, the JSON-RPC error object.
func codeMessageStruct(t types.Type) bool {
	s, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	var code, message bool
	for i := 0; i < s.NumFields(); i++ {
		switch s.Field(i).Name() {
		case "Code":
			code = true
		case "Message":
			message = true
		}
	}
	return code && message
}

const decodeMsg = "sends encoding/json's error text to the client: it names the server's Go types and struct fields (cannot unmarshal number into Go struct field T.ids of type string); answer handler.JSONErrorDetail(err), the JSON path and type as a phrase"

// checkDecode is the decode arm over one declaration.
func checkDecode(pass *analysis.Pass, c *originCtx, fd *ast.FuncDecl, reported map[*ast.CallExpr]bool) {
	f := c.forFunc(fd)
	check := func(args []ast.Expr) bool {
		for _, a := range args {
			for _, x := range errorTexts(pass, a) {
				if f.of(x, x.Pos())&fromJSON != 0 {
					pass.Reportf(x.Pos(), decodeMsg)
					return true
				}
			}
		}
		return false
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if reported[x] {
				return true
			}
			if hasStatusArg(pass, x.Args) || hasRPCCodeArg(pass, x.Args) {
				check(x.Args)
			}
		case *ast.CompositeLit:
			if t := pass.TypesInfo.TypeOf(x); t != nil && codeMessageStruct(t) {
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Message" {
							check([]ast.Expr{kv.Value})
						}
					}
				}
			}
		}
		return true
	})
}

// errorTypeOf returns the named error type T when t is T or *T and one
// of them implements error.
func errorTypeOf(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	if _, isStruct := n.Underlying().(*types.Struct); !isStruct {
		return nil
	}
	if types.Implements(n, errorInterface) || types.Implements(types.NewPointer(n), errorInterface) {
		return n
	}
	return nil
}

// carriedErrorType: a call's result that is an error type T, or a
// struct one of whose fields is (newErrorResponse's Response.Error).
func carriedErrorType(t types.Type) *types.Named {
	if n := errorTypeOf(t); n != nil {
		return n
	}
	s, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := 0; i < s.NumFields(); i++ {
		if n := errorTypeOf(s.Field(i).Type()); n != nil {
			return n
		}
	}
	return nil
}

// checkRewrap is the rewrap arm over one declaration.
func checkRewrap(pass *analysis.Pass, c *originCtx, fd *ast.FuncDecl) {
	f := c.forFunc(fd)
	report := func(t *types.Named, args []ast.Expr) {
		for _, a := range args {
			for _, x := range errorTexts(pass, a) {
				tv := pass.TypesInfo.Types[x]
				if _, isIface := tv.Type.Underlying().(*types.Interface); !isIface {
					continue // a concrete error is what it is
				}
				if f.of(x, x.Pos())&(fromForeign|fromParam) == 0 {
					continue
				}
				if testsFor(pass, fd.Body, t, x.Pos()) {
					continue
				}
				pass.Reportf(x.Pos(), "rebuilds a *%s from an error's text: an error that already is a *%s loses its code and fields (a gate's own refusal answered -32602); pass it through first: if e, ok := errors.AsType[*%s](err); ok { return e }",
					t.Obj().Name(), t.Obj().Name(), t.Obj().Name())
				return
			}
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			t := errorTypeOf(pass.TypesInfo.TypeOf(x))
			if t == nil {
				return true
			}
			var vals []ast.Expr
			for _, el := range x.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					vals = append(vals, kv.Value)
				} else {
					vals = append(vals, el)
				}
			}
			report(t, vals)
		case *ast.CallExpr:
			tv, ok := pass.TypesInfo.Types[x]
			if !ok || tv.IsType() {
				return true
			}
			if _, isTuple := tv.Type.(*types.Tuple); isTuple {
				return true
			}
			if t := carriedErrorType(tv.Type); t != nil {
				report(t, x.Args)
			}
		}
		return true
	})
}

// testsFor reports whether body tests some error for *T before pos:
// errors.As with a *T target, errors.AsType[*T], a .(*T) assertion, or
// a type switch with a *T case.
func testsFor(pass *analysis.Pass, body ast.Node, t *types.Named, pos token.Pos) bool {
	ptr := types.NewPointer(t)
	isT := func(e ast.Expr) bool {
		tt := pass.TypesInfo.TypeOf(e)
		return tt != nil && (types.Identical(tt, ptr) || types.Identical(tt, t))
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil || n.Pos() >= pos {
			return !found
		}
		switch x := n.(type) {
		case *ast.TypeAssertExpr:
			if x.Type != nil && isT(x.Type) {
				found = true
			}
		case *ast.CaseClause:
			for _, e := range x.List {
				if isT(e) {
					found = true
				}
			}
		case *ast.CallExpr:
			fun := ast.Unparen(x.Fun)
			if ix, ok := fun.(*ast.IndexExpr); ok { // errors.AsType[*T]
				if sel, ok := ix.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "AsType" && isT(ix.Index) {
					found = true
				}
			}
			if sel, ok := fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "As" && len(x.Args) == 2 {
				if tt := pass.TypesInfo.TypeOf(x.Args[1]); tt != nil {
					if p, ok := tt.(*types.Pointer); ok && (types.Identical(p.Elem(), ptr) || types.Identical(p.Elem(), t)) {
						found = true
					}
				}
			}
		}
		return !found
	})
	return found
}
