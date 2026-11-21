package errleak

import (
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
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
// count), fires when its text reaches a call carrying a 4xx/5xx
// HTTP status constant, a call carrying a JSON-RPC
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
	args   map[types.Object]origin
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
		obj := info.ObjectOf(x)
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
			if argument, ok := f.args[obj]; ok {
				return argument
			}
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
	case *ast.IndexExpr:
		return f.of(x.X, pos)
	case *ast.CompositeLit:
		var combined origin
		for _, element := range x.Elts {
			if entry, ok := element.(*ast.KeyValueExpr); ok {
				element = entry.Value
			}
			combined |= f.of(element, pos)
		}
		return combined
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
	case *ast.FuncLit:
		return f.litReturns(fun, call.Args, pos)
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
					o |= f.litReturns(ast.Unparen(d.rhs).(*ast.FuncLit), call.Args, pos)
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
			format, ok := astx.StringConstant(f.pass, call.Args[0])
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
func (f *fnOrigins) litReturns(lit *ast.FuncLit, args []ast.Expr, pos token.Pos) origin {
	if lit == nil || lit.Body == nil {
		return 0
	}
	invocation := *f
	invocation.args = make(map[types.Object]origin, len(f.args)+len(args))
	for obj, argument := range f.args {
		invocation.args[obj] = argument
	}
	argument := 0
	if lit.Type.Params != nil {
		for _, field := range lit.Type.Params.List {
			if len(field.Names) == 0 {
				argument++
				continue
			}
			for _, name := range field.Names {
				if argument < len(args) {
					boundOrigin := f.of(args[argument], pos)
					if _, variadic := field.Type.(*ast.Ellipsis); variadic {
						for _, actual := range args[argument+1:] {
							boundOrigin |= f.of(actual, pos)
						}
					}
					invocation.args[f.pass.TypesInfo.ObjectOf(name)] = boundOrigin
				}
				argument++
			}
		}
	}
	var o origin
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(x.Results) == 0 && lit.Type.Results != nil {
				for _, field := range lit.Type.Results.List {
					for _, name := range field.Names {
						if isErrorType(f.pass.TypesInfo.TypeOf(name)) {
							o |= invocation.of(name, x.Pos())
						}
					}
				}
			}
			for _, r := range x.Results {
				if isErrorType(f.pass.TypesInfo.TypeOf(r)) {
					o |= invocation.of(r, x.Pos())
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

// errorTexts yields each call that renders error text and the corresponding error operand.
func errorTexts(pass *analysis.Pass, e ast.Node) iter.Seq2[*ast.CallExpr, ast.Expr] {
	return func(yield func(*ast.CallExpr, ast.Expr) bool) {
		for n := range ast.Preorder(e) {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if sel.Sel.Name == "Error" && len(call.Args) == 0 && isErrorType(pass.TypesInfo.TypeOf(sel.X)) {
				if !yield(call, sel.X) {
					return
				}
				continue
			}
			args := call.Args
			switch pathflow.QualifiedFunc(pass, sel) {
			case "fmt.Sprint", "fmt.Sprintln":
			case "fmt.Sprintf":
				if len(args) < 2 {
					continue
				}
				format, ok := astx.StringConstant(pass, args[0])
				if !ok {
					continue
				}
				args = formattedTextArgs(format, args[1:])
			default:
				continue
			}
			for _, arg := range args {
				if isErrorType(pass.TypesInfo.TypeOf(arg)) && !yield(call, arg) {
					return
				}
			}
		}
	}
}

func formattedTextArgs(format string, args []ast.Expr) []ast.Expr {
	var text []ast.Expr
	next := 0
	reordered := false
	for d := range astx.FormatDirectives(format, len(args)) {
		next = d.NextArg
		reordered = reordered || d.Indexed
		if !d.Invalid && d.Arg < len(args) && strings.ContainsRune("vsq", d.Verb) {
			text = append(text, args[d.Arg])
		}
	}
	if !reordered {
		text = append(text, args[next:]...)
	}
	return text
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
	parents := dominance.Parents(fd.Body)
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
				if f.preservesTypedError(t, x, parents) {
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

// preservesTypedError checks the successful type test on the path to the rebuild.
func (f *fnOrigins) preservesTypedError(t *types.Named, err ast.Expr, parents map[ast.Node]ast.Node) bool {
	pass := f.pass
	body := f.fd.Body
	ptr := types.NewPointer(t)
	object := func(e ast.Expr) types.Object {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok && id.Name != "_" {
			return pass.TypesInfo.ObjectOf(id)
		}
		return nil
	}
	sameError := func(tested ast.Expr) bool {
		obj := object(tested)
		if obj == nil || obj != object(err) {
			return false
		}
		for _, d := range f.defs[obj] {
			if tested.Pos() < d.pos && d.pos < err.Pos() {
				return false
			}
		}
		return true
	}
	isT := func(e ast.Expr) bool {
		tt := pass.TypesInfo.TypeOf(e)
		return types.Identical(tt, ptr) || types.Identical(tt, t)
	}
	switchInput := func(sw *ast.TypeSwitchStmt) ast.Expr {
		var guard ast.Expr
		switch bind := sw.Assign.(type) {
		case *ast.ExprStmt:
			guard = bind.X
		case *ast.AssignStmt:
			guard = bind.Rhs[0]
		}
		if assertion, ok := ast.Unparen(guard).(*ast.TypeAssertExpr); ok && sameError(assertion.X) {
			return assertion.X
		}
		return nil
	}
	asCall := func(e ast.Expr, name string) (*ast.CallExpr, bool) {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok || len(call.Args) == 0 || !sameError(call.Args[0]) {
			return nil, false
		}
		fn := typeutil.StaticCallee(pass.TypesInfo, call)
		return call, fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "errors" && fn.Name() == name
	}
	typedOnSuccess := func(iff *ast.IfStmt) (types.Object, bool) {
		cond := ast.Unparen(iff.Cond)
		wantTrue := true
		for {
			not, ok := cond.(*ast.UnaryExpr)
			if !ok || not.Op != token.NOT {
				break
			}
			cond = ast.Unparen(not.X)
			wantTrue = !wantTrue
		}
		if call, ok := asCall(cond, "As"); ok && len(call.Args) == 2 {
			if addr, ok := ast.Unparen(call.Args[1]).(*ast.UnaryExpr); ok && addr.Op == token.AND && isT(addr.X) {
				return object(addr.X), wantTrue
			}
		}
		verdict := object(cond)
		if verdict == nil {
			return nil, false
		}
		var lookup ast.Expr
		var result types.Object
		for i := len(f.defs[verdict]) - 1; i >= 0; i-- {
			d := f.defs[verdict][i]
			if d.pos >= cond.Pos() {
				continue
			}
			as, ok := parents[d.rhs].(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 2 || object(as.Lhs[1]) != verdict {
				return nil, false
			}
			if as == iff.Init || statementPrecedes(as, iff, body, parents) {
				lookup, result = as.Rhs[0], object(as.Lhs[0])
			}
			break
		}
		if result == nil {
			return nil, false
		}
		if assertion, ok := ast.Unparen(lookup).(*ast.TypeAssertExpr); ok && assertion.Type != nil && isT(assertion.Type) && sameError(assertion.X) {
			return result, wantTrue
		}
		if call, ok := asCall(lookup, "AsType"); ok {
			if tuple, ok := pass.TypesInfo.TypeOf(call).(*types.Tuple); ok && (types.Identical(tuple.At(0).Type(), ptr) || types.Identical(tuple.At(0).Type(), t)) {
				return result, wantTrue
			}
		}
		return nil, false
	}

	for _, stmts := range dominance.Prefix(err, parents, body) {
		for _, st := range stmts {
			if iff := dominance.IfStmtOf(st); iff != nil {
				result, success := typedOnSuccess(iff)
				arm := iff.Else
				if success {
					arm = iff.Body
				}
				if preservesOnReturn(pass, arm, result) {
					return true
				}
			}
			sw, ok := st.(*ast.TypeSwitchStmt)
			if !ok {
				continue
			}
			input := switchInput(sw)
			if input == nil {
				continue
			}
			for _, stmt := range sw.Body.List {
				cc := stmt.(*ast.CaseClause)
				result := pass.TypesInfo.Implicits[cc]
				if result == nil {
					result = object(input)
				}
				if slices.ContainsFunc(cc.List, isT) && preservesOnReturn(pass, &ast.BlockStmt{List: cc.Body}, result) {
					return true
				}
			}
		}
	}
	for child, parent := ast.Node(err), parents[err]; parent != nil; child, parent = parent, parents[parent] {
		switch arm := parent.(type) {
		case *ast.IfStmt:
			if child == arm.Body || child == arm.Else {
				result, success := typedOnSuccess(arm)
				if result != nil && success != (child == arm.Body) {
					return true
				}
			}
		case *ast.CaseClause:
			sw, ok := parents[parents[arm]].(*ast.TypeSwitchStmt)
			if !ok {
				continue
			}
			input := switchInput(sw)
			if input == nil {
				continue
			}
			inputType := pass.TypesInfo.TypeOf(input)
			excludes := func(candidate types.Type) bool {
				return !types.AssignableTo(candidate, inputType) || typeSwitchArm(pass, sw, candidate) != arm
			}
			if excludes(ptr) && excludes(t) {
				return true
			}
		}
	}
	return false
}

// typeSwitchArm finds the first matching case for a dynamic type, or the default.
func typeSwitchArm(pass *analysis.Pass, sw *ast.TypeSwitchStmt, dynamic types.Type) *ast.CaseClause {
	var fallback *ast.CaseClause
	for _, stmt := range sw.Body.List {
		arm := stmt.(*ast.CaseClause)
		if len(arm.List) == 0 {
			fallback = arm
		}
		for _, expr := range arm.List {
			if types.AssignableTo(dynamic, pass.TypesInfo.TypeOf(expr)) {
				return arm
			}
		}
	}
	return fallback
}

func statementPrecedes(want ast.Stmt, use ast.Node, body *ast.BlockStmt, parents map[ast.Node]ast.Node) bool {
	for _, stmts := range dominance.Prefix(use, parents, body) {
		if slices.Contains(stmts, want) {
			return true
		}
	}
	return false
}

func preservesOnReturn(pass *analysis.Pass, st ast.Stmt, result types.Object) bool {
	if result == nil || st == nil {
		return false
	}
	switch st := st.(type) {
	case *ast.ReturnStmt:
		var carries func(ast.Expr) bool
		carries = func(expr ast.Expr) bool {
			switch ex := ast.Unparen(expr).(type) {
			case *ast.Ident:
				return pass.TypesInfo.Uses[ex] == result
			case *ast.CompositeLit:
				return slices.ContainsFunc(ex.Elts, carries)
			case *ast.KeyValueExpr:
				return carries(ex.Value)
			case *ast.UnaryExpr:
				return ex.Op == token.AND && carries(ex.X)
			}
			return false
		}
		return slices.ContainsFunc(st.Results, carries)
	case *ast.BlockStmt:
		for _, stmt := range st.List {
			if dominance.Diverges(stmt) {
				return preservesOnReturn(pass, stmt, result)
			}
		}
	case *ast.IfStmt:
		return preservesOnReturn(pass, st.Body, result) && preservesOnReturn(pass, st.Else, result)
	}
	return false
}
