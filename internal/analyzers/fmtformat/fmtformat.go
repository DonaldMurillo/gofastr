// Package fmtformat catches URL-encoder output (url.Values.Encode,
// url.QueryEscape, url.PathEscape) becoming part of a fmt format
// string. Encoders EMIT %XX escape sequences; fmt reads every '%' as a
// directive start, so an encoded query concatenated with verb literals
// ("sort=%s&dir=%s") produces a pattern whose verbs are shifted or
// consumed by the escapes — %!s(MISSING), misaligned arguments,
// corrupted hrefs. Real instance: framework/ui patternWith built
// "?q=a%26b&sort=%s&dir=%s" and every DataTable sort href rendered
// garbage (pinned by TestSortHrefCarryPatternFmtSafe on the redtest
// branch).
//
// Lane: vettool (type-aware), NOT the contracts pattern lane. Encoder
// and sink identification require types.Info package/type identity —
// import aliases (u "net/url", f "fmt") defeat string-pattern rules,
// which is the bypass class that puts type-dependent invariants in
// this lane (cf. mapwriter). A pattern-shaped port would both
// over-match and duplicate.
//
// Sanctioned postures that stay silent:
//   - %%-double the encoded segment before concatenation
//     (strings.ReplaceAll(enc, "%", "%%")) — taint never flows through
//     unrelated calls, so a helper that doubles returns clean values.
//   - consume the verb with strings.Replace instead of fmt — only fmt
//     format positions are sinks.
//
// Two diagnostics, both intra-package:
//
//  1. SINK: a tainted expression (encoder call, concatenation of one,
//     an assignment of one, or a call to a same-package helper whose
//     RETURN expressions are tainted) in the FORMAT argument of a fmt
//     call.
//
//  2. CALLSITE: a call to a tainted-return helper passing a string
//     literal that contains a fmt verb — the builder shape; this fires
//     where encoder output is joined with verb literals, even when the
//     eventual Sprintf sink lives in another package behind a struct
//     field. Direct concatenation is deliberately NOT flagged: a local
//     pattern built and consumed by strings.Replace is a sanctioned
//     posture, indistinguishable at the concat site from a bug.
package fmtformat

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const Doc = "report URL-encoded values concatenated with fmt verbs or used as fmt formats"

var Analyzer = &analysis.Analyzer{
	Name: "fmtformat",
	Doc:  Doc,
	Run:  run,
}

var fmtFormatArg = map[string]int{
	"Sprintf": 0,
	"Printf":  0,
	"Errorf":  0,
	"Fprintf": 1,
}

func run(pass *analysis.Pass) (any, error) {
	// Pass A: which functions RETURN tainted strings? Computed from
	// return expressions with only local taint (encoders + local
	// assignment flow); helper-call taint is excluded here so taint
	// provenance stays one level deep and doubling helpers stay clean.
	returnsTainted := map[string]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fnReturnsTainted(pass, fd.Body) {
					returnsTainted[fd.Name.Name] = true
				}
			}
		}
	}
	// Pass B: diagnostics.
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				diagnose(pass, fd.Body, returnsTainted)
			}
		}
	}
	return nil, nil
}

// fnReturnsTainted reports whether any return expression is tainted
// under local flow only.
func fnReturnsTainted(pass *analysis.Pass, body *ast.BlockStmt) bool {
	tainted := map[string]bool{}
	ret := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range st.Rhs {
				if taint(pass, rhs, nil, tainted) != nil && i < len(st.Lhs) {
					if id, ok := st.Lhs[i].(*ast.Ident); ok {
						tainted[id.Name] = true
					}
				}
			}
		case *ast.ReturnStmt:
			for _, r := range st.Results {
				if taint(pass, r, nil, tainted) != nil {
					ret = true
				}
			}
		}
		return true
	})
	return ret
}

func diagnose(pass *analysis.Pass, body *ast.BlockStmt, returnsTainted map[string]bool) {
	tainted := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range st.Rhs {
				markWriterReceiver(pass, rhs, returnsTainted, tainted)
				if taint(pass, rhs, returnsTainted, tainted) != nil && i < len(st.Lhs) {
					if id, ok := st.Lhs[i].(*ast.Ident); ok {
						tainted[id.Name] = true
					}
					if _, ok := st.Lhs[i].(*ast.SelectorExpr); ok && taintedVerbConcat(pass, rhs, returnsTainted, tainted) {
						pass.Reportf(rhs.Pos(), "fmtformat: URL-encoded value concatenated with fmt verbs into a handed-onward pattern; %%XX escapes act as verbs wherever this is formatted — %%-double the encoded segment or consume the verb without fmt")
					}
				}
			}
		case *ast.ExprStmt:
			markWriterReceiver(pass, st.X, returnsTainted, tainted)
		case *ast.KeyValueExpr:
			if taintedVerbConcat(pass, st.Value, returnsTainted, tainted) {
				pass.Reportf(st.Value.Pos(), "fmtformat: URL-encoded value concatenated with fmt verbs into a handed-onward pattern; %%XX escapes act as verbs wherever this is formatted — %%-double the encoded segment or consume the verb without fmt")
			}
		case *ast.ReturnStmt:
			for _, r := range st.Results {
				if taintedVerbConcat(pass, r, returnsTainted, tainted) {
					pass.Reportf(r.Pos(), "fmtformat: URL-encoded value concatenated with fmt verbs into a handed-onward pattern; %%XX escapes act as verbs wherever this is formatted — %%-double the encoded segment or consume the verb without fmt")
				}
			}
		case *ast.CallExpr:
			if key, argIdx := fmtCall(pass, st); key != "" && argIdx < len(st.Args) {
				if taint(pass, st.Args[argIdx], returnsTainted, tainted) != nil {
					pass.Reportf(st.Pos(), "fmtformat: URL-encoded value in the %s format string; %%XX escapes act as verbs — pass it as a value argument or %%-double it", key)
				}
			}
			if id, ok := st.Fun.(*ast.Ident); ok && returnsTainted[id.Name] {
				for _, a := range st.Args {
					if lit, ok := unwrapParen(a).(*ast.BasicLit); ok && literalHasVerb(lit) {
						pass.Reportf(st.Pos(), "fmtformat: %s returns URL-encoded output and this call joins it with fmt verbs; %%XX escapes will act as verbs if the result becomes a format — %%-double inside the helper or at the join", id.Name)
						break
					}
				}
			}
		}
		return true
	})
}

// markWriterReceiver marks x tainted when e is a call of the shape
// receiver.WriteString(arg) with a tainted argument: the builder object
// (strings.Builder and like-shaped writers) accumulates encoded bytes;
// its later zero-arg String() renders them.
func markWriterReceiver(pass *analysis.Pass, e ast.Expr, rt, tainted map[string]bool) {
	call, ok := unwrapParen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteString" {
		return
	}
	if id, ok := sel.X.(*ast.Ident); ok {
		if taint(pass, call.Args[0], rt, tainted) != nil {
			tainted[id.Name] = true
		}
	}
}

// taintedVerbConcat reports whether e is a concatenation whose operands
// include both taint and a string literal bearing a fmt verb: the
// pattern-in-the-making shape. Only meaningful where the result is
// handed onward (field assignment, composite-literal key, return);
// local consumption via strings.Replace stays silent.
func taintedVerbConcat(pass *analysis.Pass, e ast.Expr, rt, tainted map[string]bool) bool {
	b, ok := unwrapParen(e).(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return false
	}
	if taint(pass, b, rt, tainted) == nil {
		return false
	}
	found := false
	var walkLit func(x ast.Expr)
	walkLit = func(x ast.Expr) {
		if found {
			return
		}
		switch v := unwrapParen(x).(type) {
		case *ast.BasicLit:
			if literalHasVerb(v) {
				found = true
			}
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				walkLit(v.X)
				walkLit(v.Y)
			}
		}
	}
	walkLit(b)
	return found
}

// taint returns the tainting sub-expression of e, or nil.
func taint(pass *analysis.Pass, e ast.Expr, returnsTainted, tainted map[string]bool) ast.Expr {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return taint(pass, v.X, returnsTainted, tainted)
	case *ast.CallExpr:
		if isEncoder(pass, v) {
			return v
		}
		// strings.Builder (and any like-shaped writer) state: a receiver
		// marked tainted by WriteString(tainted) renders tainted output
		// through its zero-arg String() method.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "String" && len(v.Args) == 0 {
			if id, ok := sel.X.(*ast.Ident); ok && tainted[id.Name] {
				return v
			}
		}
		if id, ok := v.Fun.(*ast.Ident); ok && returnsTainted != nil && returnsTainted[id.Name] {
			return v
		}
		if key, argIdx := fmtCall(pass, v); key != "" {
			for _, a := range v.Args[min(1, argIdx):] {
				if taint(pass, a, returnsTainted, tainted) != nil {
					return v
				}
			}
		}
	case *ast.Ident:
		if tainted[v.Name] {
			return v
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			if s := taint(pass, v.X, returnsTainted, tainted); s != nil {
				return s
			}
			return taint(pass, v.Y, returnsTainted, tainted)
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fmtCall identifies a fmt package printing call and its format-arg index.
func fmtCall(pass *analysis.Pass, call *ast.CallExpr) (string, int) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", 0
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", 0
	}
	pn, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok || pn.Imported().Path() != "fmt" {
		return "", 0
	}
	if i, ok := fmtFormatArg[sel.Sel.Name]; ok {
		return "fmt." + sel.Sel.Name, i
	}
	return "", 0
}

func isEncoder(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "QueryEscape", "PathEscape":
		if id, ok := sel.X.(*ast.Ident); ok {
			if pn, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "net/url" {
				return true
			}
		}
	case "Encode":
		if tv, ok := pass.TypesInfo.Types[sel.X]; ok {
			if named, ok := tv.Type.(*types.Named); ok {
				if pkg := named.Obj().Pkg(); pkg != nil && pkg.Path() == "net/url" && named.Obj().Name() == "Values" {
					return true
				}
			}
		}
	}
	return false
}

func unwrapParen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// literalHasVerb reports whether a string literal contains a fmt verb
// like %s or %d. %% (the doubling fix) does not count.
func literalHasVerb(lit *ast.BasicLit) bool {
	if lit.Kind != token.STRING {
		return false
	}
	v := strings.Trim(lit.Value, "\"`")
	for i := 0; i+1 < len(v); i++ {
		if v[i] != '%' {
			continue
		}
		if v[i+1] == '%' {
			i++
			continue
		}
		if strings.IndexByte("vTbcdoqxXUeEfgGsp+", v[i+1]) >= 0 {
			return true
		}
	}
	return false
}
