// Package growmap catches a per-caller map on a long-lived struct that a
// request grows and nothing bounds: no size cap and no time-driven sweep.
//
// The bug (d3cfff016, core/mcp): Server.resourceSubsBy recorded each
// (caller, uri) subscription so an unsubscribe released only its own
// caller's count. The caller is the TCP peer host for an anonymous
// client, and subscribe needs no open stream, so nothing ever expired an
// entry: a client with many addresses (an IPv6 prefix) minted a fresh
// caller per address, and 5000 peers x 10 uris retained 50000 entries
// while the shared per-uri count beside it held 10. The fix caps the
// record across every caller (maxResourceSubscriptionPairs) with a
// len(s.resourceSubsBy) check before the insert.
//
// It fires on a write that can add a key — `X.f[k] = v`, `X.f[k] op= v`,
// `X.f[k]++` — to a map-typed struct field f, inside a function on a
// request-dispatch path (one with a context.Context or *http.Request
// parameter, or a literal inside one), when the key k derives from that
// context or request: it names the ctx or the request (r.RemoteAddr, a
// header, a cookie) directly, through a call that takes it
// (subscriptionCaller(ctx), principal.From(ctx)), through a composite
// literal field, or through locals assigned from any of those.
//
// Silent, each for a reason:
//   - the package compares len(X.f) anywhere (with <, <=, >, >=, == or
//     !=): a cap exists, the fix posture;
//   - the package deletes from (or clears or reassigns) f in a
//     time-driven function: one that starts a ticker or timer, or reads
//     the clock or compares times (time.Now/Since/Until, Before/After/
//     Sub) — a sweep or a TTL check. A function's name is no evidence:
//     the pre-fix core/mcp had expireResourceSubsIfIdleLocked, which
//     cleared the record only when the last SSE stream closed, and
//     subscribe needs no stream. A delete driven by the caller
//     (unsubscribe, logout) does not count either: the attacker never
//     sends it;
//   - the writing function defers a release of the same field (directly,
//     or through a package function that names it: core/stream's
//     `defer b.spliceSeatLocked(sub)`): the entry lives as long as the
//     request or stream that made it, so it is bounded by the concurrent
//     connections, which have caps of their own;
//   - keys from the request BODY or route params: a body-keyed record
//     is the same growth, but the rule keeps to the identity-derived key
//     (who is asking), where the per-caller record lives; a body key is
//     usually bounded by a registry the server owns;
//   - map fields of a local struct value (a per-request accumulator dies
//     with the request): only a field reached through a pointer receiver
//     or a package-level variable is long-lived;
//   - _test.go files.
//
// A map that is bounded by construction (keys from a closed set the
// server mints) carries //gofastr:allow(growmap) <why>.
package growmap

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "growmap",
	Doc:  "forbids growing a long-lived map keyed by the caller's identity (ctx, request) with no len cap and no time-driven sweep",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	var files []*ast.File
	for _, f := range pass.Files {
		if !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			files = append(files, f)
		}
	}
	lenChecked := map[*types.Var]bool{}
	swept := map[*types.Var]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if be, ok := n.(*ast.BinaryExpr); ok {
				switch be.Op {
				case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ:
					for _, side := range []ast.Expr{be.X, be.Y} {
						if fv := lenOfField(pass, side); fv != nil {
							lenChecked[fv] = true
						}
					}
				}
			}
			return true
		})
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if timeDriven(pass, fd.Body) {
				markSweeps(pass, fd.Body, swept)
			}
		}
	}

	// mentions: per package function, the map fields its body names, so
	// a deferred call to a cleanup helper (b.spliceSeatLocked(sub)) counts
	// as touching the field the helper releases.
	mentions := map[*types.Func]map[*types.Var]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				mentions[fn] = fieldsNamed(pass, fd.Body)
			}
		}
	}

	done := map[*types.Var]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			roots := identityParams(pass, fd.Type)
			released := deferReleased(pass, fd.Body, mentions)
			checkBody(pass, fd.Body, roots, lenChecked, swept, released, done)
		}
	}
	return nil, nil
}

// fieldsNamed returns the struct fields body selects.
func fieldsNamed(pass *analysis.Pass, body ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if fv := fieldOf(pass, sel); fv != nil {
				out[fv] = true
			}
		}
		return true
	})
	return out
}

// deferReleased returns the map fields a defer in body touches, directly
// or through a same-package function it calls: an entry tied to the
// request's own lifetime (released when the stream or request ends) is
// bounded by the concurrent requests, not by the identities a caller
// mints.
func deferReleased(pass *analysis.Pass, body *ast.BlockStmt, mentions map[*types.Func]map[*types.Var]bool) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		ds, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		ast.Inspect(ds.Call, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.SelectorExpr:
				if fv := fieldOf(pass, x); fv != nil {
					out[fv] = true
				}
				if fn, ok := pass.TypesInfo.Uses[x.Sel].(*types.Func); ok {
					for fv := range mentions[fn] {
						out[fv] = true
					}
				}
			case *ast.Ident:
				if fn, ok := pass.TypesInfo.Uses[x].(*types.Func); ok {
					for fv := range mentions[fn] {
						out[fv] = true
					}
				}
			}
			return true
		})
		return true
	})
	return out
}

// identityParams returns the context.Context and *http.Request parameters.
func identityParams(pass *analysis.Pass, ft *ast.FuncType) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, fl := range ft.Params.List {
		t := pass.TypesInfo.TypeOf(fl.Type)
		if !isIdentityType(t) {
			continue
		}
		for _, n := range fl.Names {
			if obj := pass.TypesInfo.Defs[n]; obj != nil {
				out[obj] = true
			}
		}
	}
	return out
}

func isIdentityType(t types.Type) bool {
	if t == nil {
		return false
	}
	if isNamed(t, "context", "Context") {
		return true
	}
	if p, ok := t.(*types.Pointer); ok && isNamed(p.Elem(), "net/http", "Request") {
		return true
	}
	return false
}

func isNamed(t types.Type, pkg, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		if a, ok := t.(*types.Alias); ok {
			return isNamed(types.Unalias(a), pkg, name)
		}
		return false
	}
	return n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

// checkBody walks one function body; nested literals inherit the
// enclosing identity roots and add their own.
func checkBody(pass *analysis.Pass, body *ast.BlockStmt, roots map[types.Object]bool, lenChecked, swept, released, done map[*types.Var]bool) {
	assigns := map[types.Object][]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) == len(s.Rhs) {
				for i, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							assigns[obj] = append(assigns[obj], s.Rhs[i])
						}
					}
				}
			} else if len(s.Rhs) == 1 {
				// v, ok := f(ctx): every left side takes the call.
				for _, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							assigns[obj] = append(assigns[obj], s.Rhs[0])
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if i < len(s.Values) {
					if obj := pass.TypesInfo.Defs[id]; obj != nil {
						assigns[obj] = append(assigns[obj], s.Values[i])
					}
				}
			}
		}
		return true
	})

	report := func(idx *ast.IndexExpr) {
		if len(roots) == 0 {
			return
		}
		fv := longLivedMapField(pass, idx.X)
		if fv == nil || lenChecked[fv] || swept[fv] || released[fv] || done[fv] {
			return
		}
		if !derivesFrom(pass, idx.Index, roots, assigns, 0) {
			return
		}
		done[fv] = true // one report per field: its first growing write
		pass.Reportf(idx.Pos(), "%s grows by a key derived from the caller (context or request) and nothing bounds it: no len(%s) cap in the package and no time-driven sweep deletes from it, so a caller minting identities (peer addresses) grows it without limit; refuse past a max or expire it on a ticker", fv.Name(), fv.Name())
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.FuncLit:
			inner := identityParams(pass, s.Type)
			for k := range roots {
				inner[k] = true
			}
			checkBody(pass, s.Body, inner, lenChecked, swept, released, done)
			return false
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				return true
			}
			for _, l := range s.Lhs {
				if idx, ok := l.(*ast.IndexExpr); ok {
					report(idx)
				}
			}
		case *ast.IncDecStmt:
			if idx, ok := s.X.(*ast.IndexExpr); ok {
				report(idx)
			}
		}
		return true
	})
}

// longLivedMapField returns f for X.f of map type, X a pointer (a
// receiver, a shared struct) or a package-level variable's field.
func longLivedMapField(pass *analysis.Pass, e ast.Expr) *types.Var {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	fv, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !ok || !fv.IsField() {
		return nil
	}
	if _, ok := fv.Type().Underlying().(*types.Map); !ok {
		return nil
	}
	base := pass.TypesInfo.TypeOf(sel.X)
	if _, ok := base.(*types.Pointer); ok {
		return fv
	}
	if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && v.Parent() == pass.Pkg.Scope() {
			return fv
		}
	}
	return nil
}

// derivesFrom reports whether e names an identity root, directly, through
// a call or composite literal holding one, or through locals assigned
// from one.
func derivesFrom(pass *analysis.Pass, e ast.Expr, roots map[types.Object]bool, assigns map[types.Object][]ast.Expr, depth int) bool {
	if depth > 6 || e == nil {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.Uses[id]
		if obj == nil {
			return true
		}
		if roots[obj] {
			found = true
			return false
		}
		if v, ok := obj.(*types.Var); ok && !v.IsField() {
			for _, rhs := range assigns[obj] {
				if derivesFrom(pass, rhs, roots, assigns, depth+1) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// lenOfField returns f for len(X.f).
func lenOfField(pass *analysis.Pass, e ast.Expr) *types.Var {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); !ok || b.Name() != "len" {
		return nil
	}
	return fieldOf(pass, call.Args[0])
}

func fieldOf(pass *analysis.Pass, e ast.Expr) *types.Var {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	fv, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !ok || !fv.IsField() {
		return nil
	}
	return fv
}

// timeDriven reports whether body starts a ticker or timer, or reads
// the clock or compares times (time.Now/Since/Until, a time.Time's
// Before/After/Sub): the marks of a sweep or a TTL check. A function
// named expire* that never looks at a clock is not one (core/mcp's
// expireResourceSubsIfIdleLocked ran only when the last stream closed,
// which is the bug d3cfff016 fixed).
func timeDriven(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "time" {
			switch fn.Name() {
			case "NewTicker", "Tick", "AfterFunc", "NewTimer", "Now", "Since", "Until", "Before", "After", "Sub":
				found = true
			}
		}
		return true
	})
	return found
}

// markSweeps records the map fields a time-driven body deletes from,
// clears or replaces.
func markSweeps(pass *analysis.Pass, body *ast.BlockStmt, swept map[*types.Var]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			id, ok := x.Fun.(*ast.Ident)
			if !ok || len(x.Args) == 0 {
				return true
			}
			b, ok := pass.TypesInfo.Uses[id].(*types.Builtin)
			if !ok || (b.Name() != "delete" && b.Name() != "clear") {
				return true
			}
			if fv := fieldOf(pass, x.Args[0]); fv != nil {
				swept[fv] = true
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if fv := fieldOf(pass, l); fv != nil {
					if _, ok := fv.Type().Underlying().(*types.Map); ok {
						swept[fv] = true
					}
				}
			}
		}
		return true
	})
}
