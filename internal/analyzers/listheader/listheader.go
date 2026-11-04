// Package listheader catches a list-valued HTTP header read with
// Header.Get and then split or token-compared.
//
// The bug: Header.Get returns the FIRST field line only. RFC 9110 §5.3
// lets a sender split one list header across several lines
// ("Connection: keep-alive" then "Connection: Upgrade"), and proxies do
// it routinely, so a token in the second line is invisible to Get.
// Found by the 2026-11 bug bash:
//   - df472714: core/stream's websocket handshake read
//     Header.Get("Connection") for the upgrade token and
//     Header.Get("Sec-WebSocket-Protocol") for the offered
//     subprotocols, so a client splitting either across lines failed the
//     handshake or lost its subprotocol.
//
// The fix spelling reads every line: Header.Values(name) and split each
// (core/stream's headerHasToken(values, token)), join them first
// (strings.Join(h.Values(name), ",")), or
// httpguts.HeaderValuesContainsToken(h.Values(name), token) for a bare
// token test.
//
// Shape: a call h.Get(name) on an http.Header (r.Header, w.Header(),
// resp.Header, a local) whose constant name is one of the list-valued
// headers this rule knows (Connection, Upgrade, Sec-WebSocket-Protocol,
// Sec-WebSocket-Extensions, Accept-Encoding, TE, Cache-Control, Vary;
// any case), whose result, directly or through a local it is bound to
// (an assignment or a var declaration, a wrapper call like
// strings.TrimSpace in between), is SPLIT or TOKEN-COMPARED: an
// argument to strings.Split/SplitN/SplitSeq/Fields/FieldsSeq/
// FieldsFunc/Cut/Contains/ContainsAny/EqualFold/HasPrefix/HasSuffix/
// Index, an operand of == or != against a non-empty constant, the tag
// of a switch whose cases are constants, or an argument to a
// same-package function that does one of those to its own string
// parameter (acceptsGzip, varyHasStar, the old headerHasToken).
// Reported at the Get.
//
// Silent postures, deliberately:
//   - a presence test (h.Get(name) != "" or == ""): the first line
//     answers "is it there";
//   - a Get passed on whole (logged, escaped into a URL, copied to
//     another header): nothing reads a token out of it here;
//   - single-valued headers (Sec-WebSocket-Key, Sec-WebSocket-Version,
//     Content-Type, Origin, Host): one line by definition;
//   - h["Connection"] and h.Values(name): every line;
//   - _test.go files.
package listheader

import (
	"go/ast"
	"go/token"
	"go/types"
	"net/http"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "listheader",
	Doc:  "report Header.Get of a list-valued header whose result is split or token-compared (Get reads only the first field line)",
	Run:  run,
}

// listHeaders are the canonical names of the list-valued headers.
var listHeaders = map[string]bool{
	"Connection":               true,
	"Upgrade":                  true,
	"Sec-Websocket-Protocol":   true,
	"Sec-Websocket-Extensions": true,
	"Accept-Encoding":          true,
	"Te":                       true,
	"Cache-Control":            true,
	"Vary":                     true,
}

// tokenFuncs are the strings functions that split or token-compare
// their first argument.
var tokenFuncs = map[string]bool{
	"strings.Split": true, "strings.SplitN": true, "strings.SplitSeq": true,
	"strings.SplitAfter": true, "strings.Fields": true, "strings.FieldsSeq": true,
	"strings.FieldsFunc": true, "strings.Cut": true, "strings.Contains": true,
	"strings.ContainsAny": true, "strings.EqualFold": true, "strings.HasPrefix": true,
	"strings.HasSuffix": true, "strings.Index": true,
}

func run(pass *analysis.Pass) (any, error) {
	splitters := computeSplitters(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			checkBody(pass, astx.BodyOf(fn), splitters)
		}
	}
	return nil, nil
}

// computeSplitters: same-package functions that split or token-compare
// one of their string parameters, by index, to a fixed point.
func computeSplitters(pass *analysis.Pass) map[*types.Func][]int {
	out := map[*types.Func][]int{}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			i := 0
			for _, field := range fd.Type.Params.List {
				for _, name := range field.Names {
					obj := pass.TypesInfo.Defs[name]
					if obj != nil && isString(obj.Type()) && !contains(out[fn], i) &&
						tokenUsed(pass, fd.Body, obj, out) {
						out[fn] = append(out[fn], i)
						changed = true
					}
					i++
				}
				if len(field.Names) == 0 {
					i++
				}
			}
		}
	}
	return out
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.String
}

// tokenUsed: obj is split or token-compared somewhere in body.
func tokenUsed(pass *analysis.Pass, body *ast.BlockStmt, obj types.Object, splitters map[*types.Func][]int) bool {
	parents := dominance.Parents(body)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && pass.TypesInfo.Uses[id] == obj && tokenUse(pass, id, parents, splitters) {
			found = true
		}
		return true
	})
	return found
}

// tokenUse: the expression e (a Get call or a use of a local holding
// one) is split or token-compared where it stands.
func tokenUse(pass *analysis.Pass, e ast.Expr, parents map[ast.Node]ast.Node, splitters map[*types.Func][]int) bool {
	var p ast.Node = parents[e]
	for {
		if pe, ok := p.(*ast.ParenExpr); ok {
			e = pe
			p = parents[pe]
			continue
		}
		break
	}
	switch p := p.(type) {
	case *ast.CallExpr:
		if len(p.Args) == 0 {
			return false
		}
		if tokenFuncs[astx.PkgFuncName(pass, ast.Unparen(p.Fun))] && p.Args[0] == e {
			return true
		}
		if fn, ok := astx.CalleeFunc(pass, ast.Unparen(p.Fun)); ok {
			for _, i := range splitters[fn] {
				if i < len(p.Args) && p.Args[i] == e {
					return true
				}
			}
		}
	case *ast.SwitchStmt:
		// A switch on the value whose cases are constants is a token
		// compare against each of them.
		return p.Tag == e && constCase(pass, p)
	case *ast.BinaryExpr:
		if p.Op != token.EQL && p.Op != token.NEQ {
			return false
		}
		other := p.X
		if other == e {
			other = p.Y
		}
		if s, ok := astx.StringConstant(pass, other); ok && s != "" {
			return true
		}
	}
	return false
}

// constCase reports whether sw has at least one non-empty constant
// string case, the token a switch on a header value compares against.
func constCase(pass *analysis.Pass, sw *ast.SwitchStmt) bool {
	if sw.Body == nil {
		return false
	}
	for _, cc := range sw.Body.List {
		cl, ok := cc.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, e := range cl.List {
			if s, ok := astx.StringConstant(pass, e); ok && s != "" {
				return true
			}
		}
	}
	return false
}

// checkBody reports list-header Gets whose result is split or
// token-compared, directly or through a local.
func checkBody(pass *analysis.Pass, body *ast.BlockStmt, splitters map[*types.Func][]int) {
	if body == nil {
		return
	}
	parents := dominance.Parents(body)
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isListGet(pass, call) {
			return true
		}
		if tokenUse(pass, call, parents, splitters) {
			report(pass, call)
			return true
		}
		// Bound to a local: `cc := h.Get("Cache-Control")`, in an
		// assignment or an if/switch init.
		if obj := boundLocal(pass, call, parents); obj != nil && tokenUsed(pass, body, obj, splitters) {
			report(pass, call)
		}
		return true
	})
}

func report(pass *analysis.Pass, call *ast.CallExpr) {
	name, _ := astx.StringConstant(pass, call.Args[0])
	pass.Reportf(call.Pos(),
		"listheader: Header.Get(%q) reads only the first field line, and its tokens are read here; a sender may split this list across lines (RFC 9110 §5.3), so a token in a later line is missed (read h.Values(%q) and split each line, or httpguts.HeaderValuesContainsToken)",
		name, name)
}

// isListGet: call is h.Get(<constant list-header name>) on an
// http.Header.
func isListGet(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Get" || len(call.Args) != 1 {
		return false
	}
	if !astx.IsNamed(pass.TypesInfo.TypeOf(sel.X), "net/http", "Header") {
		return false
	}
	name, ok := astx.StringConstant(pass, call.Args[0])
	return ok && listHeaders[http.CanonicalHeaderKey(name)]
}

// boundLocal: the local variable call's value is bound to, directly or
// through wrapper calls that carry it onward (strings.TrimSpace of a
// Get), nil when it is not a single-value binding to an identifier. A
// var declaration binds like an assignment.
func boundLocal(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node) types.Object {
	e := ast.Expr(call)
	for hops := 0; hops < 3; hops++ {
		switch p := parents[e].(type) {
		case *ast.CallExpr:
			// The value rides inside a wrapper call whose result is
			// what gets bound.
			if p.Fun == e {
				return nil
			}
			e = p
			continue
		case *ast.AssignStmt:
			if len(p.Lhs) != len(p.Rhs) {
				return nil
			}
			for i, rhs := range p.Rhs {
				if rhs != e {
					continue
				}
				if id, ok := p.Lhs[i].(*ast.Ident); ok {
					return pass.TypesInfo.ObjectOf(id)
				}
				return nil
			}
			return nil
		case *ast.ValueSpec:
			for i, v := range p.Values {
				if v != e {
					continue
				}
				if i < len(p.Names) {
					return pass.TypesInfo.ObjectOf(p.Names[i])
				}
				return nil
			}
			return nil
		}
		return nil
	}
	return nil
}
