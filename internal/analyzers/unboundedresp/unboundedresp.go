// Package unboundedresp catches the unbounded read of an HTTP
// RESPONSE body: io.ReadAll(resp.Body), or a json/xml/yaml Decoder
// seated on it, with no io.LimitReader or http.MaxBytesReader anywhere
// on that body's chain in the function — or one hop away in a
// same-package helper the body is passed to.
//
// Probe: cmd/gofastr/internal/semanticcmd/semantic.go remoteQuery/remoteGet (2026-09-07
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
// The fire shape is a read whose argument IS resp.Body. The fix posture
// reads through the limiter instead — io.ReadAll(io.LimitReader(
// resp.Body, n)), battery/auth/oidc.go reading 1<<20 is the model — and
// that read is no sink at all. The one credit for a read of resp.Body
// itself is a REBIND before it: resp.Body = io.NopCloser(
// io.LimitReader(resp.Body, n)) or http.MaxBytesReader(w, resp.Body, n)
// in the same function, or resp handed first to a same-package helper
// that rebinds its parameter's Body that way.
//
// Bug bash 2026-10-27 I15 narrowed the credit: it used to be any
// limiter in the function that MENTIONED the body (wrapsInLimiter), so
// `_ = io.LimitReader(resp.Body, 1); io.ReadAll(resp.Body)` stayed
// quiet, and so did an error-path io.ReadAll(resp.Body) beside a capped
// success-path decode, and a direct read after resp.Body went to a
// capped reading helper. A limiter bounds what reads through it; a read
// of the raw body beside it is unbounded.
//
// A read is any spelling that pulls the body to EOF: io.ReadAll and
// ioutil.ReadAll, a json/xml/yaml decoder, io.Copy / io.CopyBuffer into
// a destination that keeps the bytes in memory, and ReadFrom on one
// (bytes.Buffer, strings.Builder, a []byte-typed writer). The body may reach it through a local bound once to
// resp.Body, or through bufio.NewReader / NewReaderSize / io.TeeReader,
// none of which bounds anything. The 2026-11-02 guard bash found every
// one of these silent; only io.ReadAll and the decoders on the bare
// resp.Body selector counted.
//
// Silent postures, deliberately:
//   - io.Copy(io.Discard, resp.Body): the keep-alive drain holds no
//     memory (the client timeout bounds the drip feed);
//   - a streaming copy: io.Copy / CopyBuffer / ReadFrom into a writer
//     that does not keep the bytes (a proxied http.ResponseWriter, an
//     *os.File download, an io.Writer parameter of unknown type) holds
//     one chunk at a time, for the same reason the Discard drain is
//     quiet. Round 3's widening fired on these; the review found them
//     false positives. Bounding a download's disk use is not this
//     rule's memory-exhaustion shape;
//   - request bodies (http.Request.Body): unboundedbody owns them;
//   - _test.go files: a test client POSTing fixtures to a local
//     httptest server is not attacker surface;
//   - a body whose response variable's last binding is a composite
//     literal (a program-constructed *http.Response): the byte count
//     is the program's own, not the network's;
//   - reads through helpers more than one hop away: the cap must be
//     visible where the read happens or one call away;
//   - resp.Body handed to a helper (capped or not): the read inside the
//     helper sees a bare parameter, not a resp.Body selector, so it is
//     out of this rule's fire shape.
package unboundedresp

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unboundedresp",
	Doc:  "forbids reading an HTTP response body with no size bound (io.LimitReader / http.MaxBytesReader): the endpoint controls the byte count and can pin memory or drip-feed the reader forever",
	Run:  run,
}

const readMsg = "unbounded read of an *http.Response body: the endpoint controls the byte count — wrap it in io.LimitReader or http.MaxBytesReader before reading (battery/auth/oidc.go reads 1<<20)"

func run(pass *analysis.Pass) (any, error) {
	// Same-package helpers that REBIND a response parameter's Body to a
	// limited reader: handing the response to one first is credit.
	cappingHelpers := map[*types.Func]int{} // fn → index of the rebound parameter
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type == nil || fd.Type.Params == nil {
				continue
			}
			for _, field := range fd.Type.Params.List {
				for _, name := range field.Names {
					if rebindsBody(pass, fd.Body, pass.TypesInfo.ObjectOf(name), fd.Body.End()) {
						if _, ok := cappingHelpers[funcOf(pass, fd)]; !ok {
							cappingHelpers[funcOf(pass, fd)] = indexOfParam(pass, fd, name)
						}
					}
				}
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

		aliases := readerAliases(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var src ast.Expr
			switch qualified(pass, call.Fun) {
			case "io.ReadAll", "ioutil.ReadAll":
				// io.ReadAll(resp.Body), and its deprecated alias.
				if len(call.Args) == 1 {
					src = call.Args[0]
				}
			case "json.NewDecoder", "xml.NewDecoder", "yaml.NewDecoder":
				// json.NewDecoder(resp.Body) / xml.NewDecoder / yaml decoders
				if len(call.Args) >= 1 {
					src = call.Args[0]
				}
			case "io.Copy", "io.CopyBuffer":
				// io.Copy(&buf, resp.Body) reads it all into memory as
				// surely as ReadAll. A copy into a writer that does not
				// keep the bytes (a ResponseWriter, a file, io.Discard)
				// streams in fixed chunks and stays quiet.
				if len(call.Args) >= 2 && buffersInMemory(pass, call.Args[0]) {
					src = call.Args[1]
				}
			default:
				// buf.ReadFrom(resp.Body) on a bytes.Buffer (or another
				// in-memory io.ReaderFrom) reads to EOF into memory;
				// (*os.File).ReadFrom streams.
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ReadFrom" && len(call.Args) == 1 && buffersInMemory(pass, sel.X) {
					src = call.Args[0]
				}
			}
			if src == nil {
				return true
			}
			if obj, ok := bodyOwner(pass, readSource(pass, src, aliases)); ok {
				reportIfUnbounded(pass, f, call, obj, literalResp, cappingHelpers)
			}
			return true
		})
	}
	return nil, nil
}

// buffersInMemory reports whether a copy destination keeps every byte
// written to it: a bytes.Buffer, a strings.Builder, or a writer whose
// type is a []byte. Any other destination (an http.ResponseWriter, an
// *os.File, io.Discard, an io.Writer interface of unknown dynamic type)
// streams, holding one chunk at a time, so a copy into it is not the
// memory exhaustion this rule exists for.
func buffersInMemory(pass *analysis.Pass, dst ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[dst]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
		switch n.Obj().Pkg().Path() + "." + n.Obj().Name() {
		case "bytes.Buffer", "strings.Builder":
			return true
		}
	}
	if sl, ok := t.Underlying().(*types.Slice); ok {
		if b, ok := sl.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return true
		}
	}
	return false
}

// reportIfUnbounded fires when a read takes resp.Body itself and
// nothing rebound resp.Body to a limited reader before the read:
// directly in this function, or by handing resp to a one-hop helper
// that rebinds its parameter's Body. A limiter that merely MENTIONS the
// body is no credit (I15): it bounds whatever reads through it, and
// this read does not.
func reportIfUnbounded(pass *analysis.Pass, f *ast.File, call *ast.CallExpr, resp types.Object, literalResp map[types.Object]bool, cappingHelpers map[*types.Func]int) {
	if literalResp[resp] {
		return
	}
	fn := enclosingFunc(f, call.Pos())
	if fn == nil {
		return
	}
	if rebindsBody(pass, fn.Body, resp, call.Pos()) {
		return
	}
	if passesToCappingHelper(pass, fn, resp, cappingHelpers, call.Pos()) {
		return
	}
	pass.Reportf(call.Pos(), readMsg)
}

// passesToCappingHelper reports whether fn, before pos, hands the
// response itself (not its body: a helper given resp.Body cannot rebind
// resp.Body) to a same-package helper that rebinds that parameter's
// Body to a limited reader.
func passesToCappingHelper(pass *analysis.Pass, fn *ast.FuncDecl, resp types.Object, cappingHelpers map[*types.Func]int, pos token.Pos) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= pos {
			return true
		}
		callee, ok := astx.CalleeFunc(pass, call.Fun)
		if !ok {
			return true
		}
		argIdx, ok := cappingHelpers[callee]
		if !ok || argIdx >= len(call.Args) {
			return true
		}
		if respObject(pass, call.Args[argIdx]) == resp {
			found = true
			return false
		}
		return true
	})
	return found
}

// rebindsBody reports whether body, before pos, assigns resp's Body
// field from an expression holding a limiter over resp's own body:
// resp.Body = io.NopCloser(io.LimitReader(resp.Body, n)), or
// http.MaxBytesReader(w, resp.Body, n). Only the rebind makes a later
// read of resp.Body bounded.
func rebindsBody(pass *analysis.Pass, body *ast.BlockStmt, resp types.Object, pos token.Pos) bool {
	if resp == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Pos() >= pos || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, lhs := range assign.Lhs {
			if obj, ok := bodyOwner(pass, lhs); !ok || obj != resp {
				continue
			}
			if limitsBodyOf(pass, assign.Rhs[i], resp) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// limitsBodyOf reports whether e contains an io.LimitReader /
// http.MaxBytesReader call whose reader argument mentions resp.
func limitsBodyOf(pass *analysis.Pass, e ast.Expr, resp types.Object) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
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
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// respObject resolves a call argument naming a response value (resp,
// &resp, x.resp) to its object, or nil.
func respObject(pass *analysis.Pass, e ast.Expr) types.Object {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	switch x := e.(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(x)
	case *ast.SelectorExpr:
		return pass.TypesInfo.ObjectOf(x.Sel)
	}
	return nil
}

// readSource sees through what does not bound a read: parentheses, a
// local that was bound to the body (body := resp.Body), and the
// pass-through wrappers bufio.NewReader / NewReaderSize and
// io.TeeReader, each of which reads its source to EOF on demand. A
// limiter is not unwrapped: a read through io.LimitReader is bounded.
func readSource(pass *analysis.Pass, e ast.Expr, aliases map[types.Object]ast.Expr) ast.Expr {
	for range 8 {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			b, ok := aliases[pass.TypesInfo.ObjectOf(x)]
			if !ok {
				return e
			}
			e = b
		case *ast.CallExpr:
			switch qualified(pass, x.Fun) {
			case "bufio.NewReader", "bufio.NewReaderSize", "io.TeeReader":
				if len(x.Args) == 0 {
					return e
				}
				e = x.Args[0]
			default:
				return e
			}
		default:
			return e
		}
	}
	return e
}

// readerAliases maps each local bound exactly once, by a single-value
// assignment or declaration, to its value: the alias a read may name
// in place of resp.Body. A local bound more than once is left out,
// since which binding the read sees is flow-dependent.
func readerAliases(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	out := map[types.Object]ast.Expr{}
	seen := map[types.Object]int{}
	bind := func(id *ast.Ident, v ast.Expr) {
		obj := pass.TypesInfo.ObjectOf(id)
		if obj == nil || id.Name == "_" {
			return
		}
		seen[obj]++
		out[obj] = v
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) == len(st.Rhs) {
				for i, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bind(id, st.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) == len(st.Values) {
				for i, id := range st.Names {
					bind(id, st.Values[i])
				}
			}
		}
		return true
	})
	for obj, n := range seen {
		if n > 1 {
			delete(out, obj)
		}
	}
	return out
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
