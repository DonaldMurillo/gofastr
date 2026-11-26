// Package guardbypass catches a direct call to an interface method that
// already has a recover guard: the guard exists, and this path walks
// around it.
//
// The bugs (round 12 bug bash, S1a-05 and S1b-09): core-ui/app wraps
// the app-supplied screen hooks in recover guards,
//
//	func SafeScreenTitle(ctx context.Context, titler ScreenTitler) (title string) {
//		defer func() {
//			if r := recover(); r != nil {
//				title = ""
//			}
//		}()
//		return titler.ScreenTitle(ctx)
//	}
//
// and safeScreenLang around ScreenLang the same way. The full page load
// went through the guards; framework/uihost's soft-navigation head and
// RenderScreen called t.ScreenTitle(ctx) and x.ScreenLang() directly.
// A panicking screen method then killed soft navigation while a full
// load of the same page fell back to the route's title and language.
//
// Shape. A guard is a declared function or method whose body defers a
// recover on every path (a top-level `defer func() { … recover() … }()`,
// or `defer h()` for a same-package h, judged by astx.DirectRecover, the
// test recovercallback uses) and whose only work after that defer is one
// call of a method on one of its own interface-typed parameters:
// `return p.M(…)`, `p.M(…)` or `v = p.M(…)`, beside returns and ifs that
// call nothing. An exported guard is exported as a fact, so a package
// importing core-ui/app knows SafeScreenTitle; an unexported guard
// (safeScreenLang) counts only in its own package. The rule reports a
// call `x.M(…)` whose receiver is an interface and whose method is one a
// guard wraps. The method is go/types' own object, so a call through an
// interface that embeds the guarded one matches too; an interface that
// declares its own method of the same name does not.
//
// Fix spellings it accepts: call the guard, or recover in the calling
// function itself. A call is quiet inside the guard, or after a
// top-level deferred recover in its own function or in a function
// literal's synchronous owner (not across a go statement: the
// goroutine's panic never reaches the launching frame's recover).
// Any literal not started with go counts as synchronous, so a closure
// that escapes its owner (returned, stored) and runs after the owner's
// frame is gone is a known miss.
//
// Silent postures, deliberately:
//   - a function that recovers around more than the one call is not a
//     guard: a handler that defers a recover and then writes to its
//     http.ResponseWriter would otherwise make every w.Write in its
//     package a finding;
//   - a guard whose recover is conditional or re-panics guards nothing,
//     so it is not a guard;
//   - a method value (`f := x.M`) or a concrete receiver: the guard takes
//     the interface, and a concrete method is not the supplied hook;
//   - _test.go files, for guards and calls both: tests call hooks
//     directly on purpose.
//
// Measured over ./... on 2026-11-26: seven guards (core-ui/app's
// SafeScreenTitle, safeScreenLang and safeScreenLoad; notify's
// channelSendSafely, semantic's chunkSafely, framework's
// wrapSandboxSafely, the harness Bus.readLog) and four direct calls.
// One was a bypass: uihost's resolveScreenSEOFor read ScreenTitle bare
// for an article headline, so a panicking title escaped ServeHTTP on
// every AsArticle page (fixed, TestArticleHeadlineTitlePanicServes).
// Three are deliberate and carry //gofastr:allow(guardbypass): App.Register's
// boot-time title read and examples/site's copy of it, which fail fast,
// and uihost's notFoundBodyScreen.ScreenTitle, a delegating wrapper whose
// callers read it through a guard.
package guardbypass

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name:      "guardbypass",
	Doc:       "report a direct call to an interface method that a recover guard (a function deferring recover around param.M()) already wraps: call the guard",
	Run:       run,
	FactTypes: []analysis.Fact{(*guardFact)(nil)},
}

// guardFact marks an exported guard with the interface-method call it
// recovers: which parameter, which method name.
type guardFact struct {
	Param  int
	Method string
}

func (*guardFact) AFact()           {}
func (f *guardFact) String() string { return "recover guard for " + f.Method }

type checker struct {
	pass  *analysis.Pass
	decls map[*types.Func]*ast.FuncDecl
	// guards maps an interface method (its Origin) to the functions
	// that recover around a call to it.
	guards map[*types.Func][]*types.Func
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{pass: pass, decls: astx.FuncDecls(pass), guards: map[*types.Func][]*types.Func{}}
	c.findLocalGuards()
	c.importGuards()
	for m, gs := range c.guards {
		slices.SortFunc(gs, func(a, b *types.Func) int { return guardRank(m, a, b) })
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.PreorderStack(f, nil, func(n ast.Node, stack []ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				c.check(call, stack)
			}
			return true
		})
	}
	return nil, nil
}

func (c *checker) findLocalGuards() {
	fns := make([]*types.Func, 0, len(c.decls))
	for fn := range c.decls {
		fns = append(fns, fn)
	}
	slices.SortFunc(fns, func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) })
	for _, fn := range fns {
		fd := c.decls[fn]
		if pathflow.InTestFile(c.pass, fd) {
			continue
		}
		gc, ok := c.guardedCall(fn, fd.Body)
		if !ok {
			continue
		}
		c.guards[gc.method] = append(c.guards[gc.method], fn)
		if fn.Exported() {
			c.pass.ExportObjectFact(fn, &guardFact{Param: gc.param, Method: gc.method.Name()})
		}
	}
}

func (c *checker) importGuards() {
	for _, of := range c.pass.AllObjectFacts() {
		fact, ok := of.Fact.(*guardFact)
		if !ok {
			continue
		}
		fn, ok := of.Object.(*types.Func)
		if !ok || fn.Pkg() == c.pass.Pkg {
			continue
		}
		params := fn.Signature().Params()
		if fact.Param >= params.Len() {
			continue
		}
		obj, _, _ := types.LookupFieldOrMethod(params.At(fact.Param).Type(), false, fn.Pkg(), fact.Method)
		if m, ok := obj.(*types.Func); ok {
			c.guards[m.Origin()] = append(c.guards[m.Origin()], fn)
		}
	}
}

type localCall struct {
	param  int
	method *types.Func
}

// guardedCall returns the one interface-method call on fn's own
// parameters that body wraps: after a top-level deferred recover, the
// body's only work is that call, as `return p.M(…)`, `p.M(…)` or
// `v = p.M(…)`, beside returns and ifs that call nothing. A function
// that recovers around more work (a handler writing to its
// http.ResponseWriter) is not a guard for every method it calls.
func (c *checker) guardedCall(fn *types.Func, body *ast.BlockStmt) (localCall, bool) {
	after := c.recoverFrom(body)
	if after == token.NoPos {
		return localCall{}, false
	}
	params := fn.Signature().Params()
	index := map[types.Object]int{}
	for i := range params.Len() {
		if types.IsInterface(params.At(i).Type()) {
			index[params.At(i)] = i
		}
	}
	var found localCall
	ok := false
	for _, st := range body.List {
		if st.Pos() < after {
			continue
		}
		if _, isDefer := st.(*ast.DeferStmt); isDefer {
			continue
		}
		if call := wrappedCall(st); call != nil && !ok {
			if lc, isParam := c.paramCall(call, index); isParam {
				found, ok = lc, true
				continue
			}
		}
		switch st.(type) {
		case *ast.ReturnStmt, *ast.IfStmt:
			if !callsAnything(st) {
				continue
			}
		}
		return localCall{}, false
	}
	return found, ok
}

// wrappedCall returns the call a statement is made of: `return f()`,
// `f()`, or `v = f()` / `v, err := f()`.
func wrappedCall(st ast.Stmt) *ast.CallExpr {
	var e ast.Expr
	switch x := st.(type) {
	case *ast.ReturnStmt:
		if len(x.Results) == 1 {
			e = x.Results[0]
		}
	case *ast.ExprStmt:
		e = x.X
	case *ast.AssignStmt:
		if len(x.Rhs) == 1 {
			e = x.Rhs[0]
		}
	}
	call, _ := ast.Unparen(e).(*ast.CallExpr)
	return call
}

// paramCall matches `p.M(…)` for p one of the interface parameters in
// index.
func (c *checker) paramCall(call *ast.CallExpr, index map[types.Object]int) (localCall, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return localCall{}, false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return localCall{}, false
	}
	i, ok := index[c.pass.TypesInfo.Uses[id]]
	if !ok {
		return localCall{}, false
	}
	m := c.ifaceMethod(sel)
	if m == nil {
		return localCall{}, false
	}
	return localCall{param: i, method: m}, true
}

func callsAnything(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}

// ifaceMethod returns the interface method sel selects (its Origin),
// or nil when sel is not a method call on an interface value.
func (c *checker) ifaceMethod(sel *ast.SelectorExpr) *types.Func {
	s := c.pass.TypesInfo.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !types.IsInterface(s.Recv()) {
		return nil
	}
	m, ok := s.Obj().(*types.Func)
	if !ok {
		return nil
	}
	return m.Origin()
}

// guardRank orders a method's guards for the message: one declared
// beside the interface first (SafeScreenTitle sits with ScreenTitler),
// then exported ones, then by name.
func guardRank(m, a, b *types.Func) int {
	rank := func(g *types.Func) int {
		r := 0
		if g.Pkg() != m.Pkg() {
			r += 2
		}
		if !g.Exported() {
			r++
		}
		return r
	}
	if d := rank(a) - rank(b); d != 0 {
		return d
	}
	return strings.Compare(a.FullName(), b.FullName())
}

// recoverFrom returns the end of body's first top-level defer that
// recovers on every path, or NoPos. Every call after that position runs
// under the recover.
func (c *checker) recoverFrom(body *ast.BlockStmt) token.Pos {
	if body == nil {
		return token.NoPos
	}
	for _, st := range body.List {
		d, ok := st.(*ast.DeferStmt)
		if !ok {
			continue
		}
		switch fun := ast.Unparen(d.Call.Fun).(type) {
		case *ast.FuncLit:
			if astx.DirectRecover(c.pass, fun.Body) {
				return d.End()
			}
		case *ast.Ident:
			if fn, ok := c.pass.TypesInfo.Uses[fun].(*types.Func); ok {
				if fd := c.decls[fn]; fd != nil && astx.DirectRecover(c.pass, fd.Body) {
					return d.End()
				}
			}
		}
	}
	return token.NoPos
}

func (c *checker) check(call *ast.CallExpr, stack []ast.Node) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	m := c.ifaceMethod(sel)
	if m == nil {
		return
	}
	guards := c.guards[m]
	if len(guards) == 0 || c.covered(call, stack, guards) {
		return
	}
	g := c.guardName(guards[0])
	c.pass.Reportf(call.Pos(), "%s is called directly, but %s exists to recover its panic: call %s (or recover here)", m.Name(), g, g)
}

// covered reports whether call sits inside one of its guards, or after
// a deferred recover in its own function or a synchronous owner of the
// literal it sits in.
func (c *checker) covered(call *ast.CallExpr, stack []ast.Node, guards []*types.Func) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncDecl:
			if obj, ok := c.pass.TypesInfo.Defs[fn.Name].(*types.Func); ok && slices.Contains(guards, obj) {
				return true
			}
			return c.recoverBefore(fn.Body, call)
		case *ast.FuncLit:
			if c.recoverBefore(fn.Body, call) {
				return true
			}
			if launched(stack, i) {
				return false
			}
		}
	}
	return false
}

func (c *checker) recoverBefore(body *ast.BlockStmt, call *ast.CallExpr) bool {
	at := c.recoverFrom(body)
	return at != token.NoPos && at <= call.Pos()
}

// launched reports whether the literal at stack[i] is the function of a
// go statement: it runs on its own goroutine.
func launched(stack []ast.Node, i int) bool {
	if i < 2 {
		return false
	}
	call, ok := stack[i-1].(*ast.CallExpr)
	if !ok || ast.Unparen(call.Fun) != stack[i] {
		return false
	}
	_, ok = stack[i-2].(*ast.GoStmt)
	return ok
}

// guardName spells the guard for the message: qualified by its package
// when it lives elsewhere, by its receiver type when it is a method.
func (c *checker) guardName(fn *types.Func) string {
	name := fn.Name()
	if recv := fn.Signature().Recv(); recv != nil {
		if n := astx.NamedOf(recv.Type()); n != nil {
			name = n.Obj().Name() + "." + name
		}
	}
	if fn.Pkg() != nil && fn.Pkg() != c.pass.Pkg {
		name = fn.Pkg().Name() + "." + name
	}
	return name
}
