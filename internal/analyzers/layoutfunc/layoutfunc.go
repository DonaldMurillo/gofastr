// Package layoutfunc flags route-state reads that are lexically inside
// a layout build (an app.LayoutFunc body) and outside a RouteArea
// closure (docs/DESIGN-layout-outlets.md, "The lint").
//
// Why: a tree layout's build markup is the STATIC chrome of its layer.
// On a subtree partial the KEPT layers' builds run in collect mode and
// their markup is discarded — only RouteArea fns travel, as fills. A
// build that reads the route match (app.MatchFromContext, Match.Param,
// Match.Path, route.From) or the request (app.RequestFromContext)
// therefore bakes request-derived chrome into the DOM that no later
// navigation refreshes: after the first click the shell shows the
// previous route's state. The correct tools are l.RouteArea (the state
// arrives as the fn's Match parameter and re-renders per navigation)
// and the route.* signal bindings, which follow every applied seed.
//
// Reads lexically inside a RouteArea closure are the allowed case —
// including nested closures, and a closure passed by identifier — and
// everything outside a LayoutFunc body (a resolver, a screen Load, a
// helper) is silent: the lint is about where the read lives, not
// whether route state is readable.
//
// Born with the 404-outlet work a shell that
// derived its nav from MatchFromContext went stale on every partial.
package layoutfunc

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "layoutfunc",
	Doc:  "flags app.MatchFromContext / Match.Param / Match.Path / route.From / app.RequestFromContext read inside a LayoutFunc body and outside a RouteArea closure: the build's markup is static chrome a partial never refreshes, so request-derived chrome goes stale after the first navigation",
	Run:  run,
}

// moduleFlag overrides the module the layout packages live under. go
// vet populates pass.Module; analysistest's fixture loader reports an
// empty module path, so hermetic runs set this flag explicitly (same
// contract as recovercallback's).
var moduleFlag *string

func init() {
	moduleFlag = Analyzer.Flags.String("module", "", "module path override when the driver reports no module (test fixtures)")
}

const finding = "layout build reads route state outside a RouteArea: the build's markup is static chrome a partial never refreshes, so it goes stale after the first navigation — take the state from l.RouteArea's fn or bind route.* signals"

func run(pass *analysis.Pass) (any, error) {
	mod := *moduleFlag
	if mod == "" && pass.Module != nil {
		mod = pass.Module.Path
	}
	if mod == "" {
		return nil, nil // no module context: nothing can match
	}
	w := &walker{
		pass:       pass,
		appPath:    mod + "/core-ui/app",
		routePath:  mod + "/core-ui/route",
		decls:      map[token.Pos]ast.Node{},
		areaBodies: map[ast.Node]bool{},
	}
	w.indexDeclarations()
	w.collectAreaClosures()
	w.walkFiles()
	return nil, nil
}

// walker carries the package paths, the declaration index, and the
// collected RouteArea closures.
type walker struct {
	pass       *analysis.Pass
	appPath    string
	routePath  string
	decls      map[token.Pos]ast.Node
	areaBodies map[ast.Node]bool
}

// indexDeclarations maps every declaring identifier to what it
// declares: a FuncDecl's body, a value's func literal, or the ValueSpec
// itself. The defining object's Pos is the declaring identifier's
// position, so Uses lookups key straight into it; literals bound to an
// identifier (package-level or local: x := func…, var x = func…) key
// the same way, so a RouteArea closure or a layout build passed by
// name resolves to its body.
func (w *walker) indexDeclarations() {
	for _, f := range w.pass.Files {
		if pathflow.IsTestFile(w.pass, f) {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				w.decls[d.Name.Pos()] = d
			case *ast.GenDecl:
				for _, s := range d.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok || len(vs.Names) == 0 {
						continue
					}
					if len(vs.Values) > 0 {
						if fl, ok := vs.Values[0].(*ast.FuncLit); ok {
							w.decls[vs.Names[0].Pos()] = fl
							continue
						}
					}
					w.decls[vs.Names[0].Pos()] = vs
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				fl, ok := rhs.(*ast.FuncLit)
				if !ok || i >= len(assign.Lhs) {
					continue
				}
				id, ok := assign.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				if _, clash := w.decls[id.Pos()]; !clash {
					w.decls[id.Pos()] = fl
				}
			}
			return true
		})
	}
}

// collectAreaClosures records the body of every fn expression handed
// to app.RouteArea. The lint's exemption is lexical INSIDE the
// closure, and a closure passed by identifier is the closure wherever
// it is declared.
func (w *walker) collectAreaClosures() {
	for _, f := range w.pass.Files {
		if pathflow.IsTestFile(w.pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			if fn, ok := w.appFn(call.Fun); ok && fn.Name() == "RouteArea" {
				if body := w.resolveFn(call.Args[len(call.Args)-1]); body != nil {
					w.areaBodies[body] = true
				}
			}
			return true
		})
	}
}

// walkFiles finds layout builds in LayoutFunc-typed positions — the
// build argument of app.NewLayout, an explicitly app.LayoutFunc
// typed variable, assignment, or conversion — and walks each body.
func (w *walker) walkFiles() {
	for _, f := range w.pass.Files {
		if pathflow.IsTestFile(w.pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if fn, ok := w.appFn(n.Fun); ok && fn.Name() == "NewLayout" && len(n.Args) >= 3 {
					w.walkBuild(w.resolveFn(n.Args[2]), false)
					return true // the args' non-build parts hold no layout scope of their own
				}
				if w.isLayoutFuncConversion(n) {
					for _, a := range n.Args {
						if fl, ok := a.(*ast.FuncLit); ok {
							w.walkBuild(fl, false)
						}
					}
				}
			case *ast.GenDecl:
				for _, s := range n.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok || len(vs.Values) == 0 {
						continue
					}
					if !w.isLayoutFuncNamed(vs.Type) {
						continue
					}
					for _, v := range vs.Values {
						if fl, ok := v.(*ast.FuncLit); ok {
							w.walkBuild(fl, false)
						}
					}
				}
			case *ast.AssignStmt:
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						break
					}
					fl, ok := rhs.(*ast.FuncLit)
					if !ok || !w.isLayoutFuncType(w.typeOf(n.Lhs[i])) {
						continue
					}
					w.walkBuild(fl, false)
				}
			}
			return true
		})
	}
}

// resolveFn maps a function expression to its body: a literal's own
// body, or the declaration an identifier/selector names (same package
// only — a cross-package build is analyzed in its own package's pass).
func (w *walker) resolveFn(expr ast.Expr) ast.Node {
	switch e := expr.(type) {
	case *ast.FuncLit:
		return e
	case *ast.Ident:
		obj := w.pass.TypesInfo.Uses[e]
		if obj == nil {
			return nil
		}
		return w.declBody(obj)
	case *ast.SelectorExpr:
		obj := w.pass.TypesInfo.Uses[e.Sel]
		if obj == nil {
			return nil
		}
		return w.declBody(obj)
	}
	return nil
}

// declBody finds the body a defined object declares: a function's
// FuncDecl, a literal bound to an identifier, or a variable's
// func-literal value.
func (w *walker) declBody(obj types.Object) ast.Node {
	decl, ok := w.decls[obj.Pos()]
	if !ok {
		return nil
	}
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Body == nil {
			return nil
		}
		return d
	case *ast.FuncLit:
		return d
	case *ast.ValueSpec:
		for _, v := range d.Values {
			if fl, ok := v.(*ast.FuncLit); ok {
				return fl
			}
		}
	}
	return nil
}

// walkBuild walks one layout build body. inArea marks everything from
// a RouteArea closure inward (nested closures included) as the allowed
// case.
func (w *walker) walkBuild(root ast.Node, inArea bool) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			// A closure this package hands to RouteArea is the area
			// closure wherever it is declared: its whole subtree may
			// read route state. The BODY, not the literal: walking the
			// literal again would re-enter this arm forever.
			if w.areaBodies[n] {
				w.walkBuild(n.Body, true)
				return false
			}
			return true
		case *ast.CallExpr:
			if fn, ok := w.appFn(n.Fun); ok {
				switch fn.Name() {
				case "MatchFromContext", "RequestFromContext":
					if !inArea {
						w.pass.Reportf(n.Pos(), "%s", finding)
					}
				case "RouteArea":
					// The area closure is the lint's exemption, lexically:
					// its whole subtree may read route state. The fn may be
					// a literal here, or a named closure already covered by
					// the areaBodies arm above.
					if len(n.Args) >= 2 {
						if body := w.resolveFn(n.Args[len(n.Args)-1]); body != nil && !w.areaBodies[body] {
							w.walkBuild(body, true)
						}
					}
					return false // the other children carry no scope of their own
				}
				return true
			}
			if fn, ok := w.routeFn(n.Fun); ok && fn.Name() == "From" && !inArea {
				w.pass.Reportf(n.Pos(), "%s", finding)
			}
			return true
		case *ast.SelectorExpr:
			// Match.Param / Match.Path reads: keyed on the receiver's
			// static type, so a Match from anywhere (a helper's return,
			// a closure capture) is caught, not just the context read.
			if !inArea && (n.Sel.Name == "Param" || n.Sel.Name == "Path") {
				if named, ok := w.typeOf(n.X).(*types.Named); ok && named.Obj() != nil {
					if p := named.Obj().Pkg(); p != nil && p.Path() == w.appPath && named.Obj().Name() == "Match" {
						w.pass.Reportf(n.Pos(), "%s", finding)
					}
				}
			}
			return true
		}
		return true
	})
}

// appFn resolves expr to a function declared in the app package: a
// plain identifier, or pkg.Fn / recv.Fn through Uses.
func (w *walker) appFn(expr ast.Expr) (*types.Func, bool) {
	fn, path, ok := w.fnIn(expr)
	return fn, ok && path == w.appPath
}

// routeFn resolves expr to a function declared in the route package.
func (w *walker) routeFn(expr ast.Expr) (*types.Func, bool) {
	fn, path, ok := w.fnIn(expr)
	return fn, ok && path == w.routePath
}

func (w *walker) fnIn(expr ast.Expr) (*types.Func, string, bool) {
	var obj types.Object
	switch e := expr.(type) {
	case *ast.Ident:
		obj = w.pass.TypesInfo.Uses[e]
	case *ast.SelectorExpr:
		obj = w.pass.TypesInfo.Uses[e.Sel]
	default:
		return nil, "", false
	}
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return nil, "", false
	}
	return fn, fn.Pkg().Path(), true
}

// isLayoutFuncConversion reports whether call is the conversion form
// app.LayoutFunc(func literal).
func (w *walker) isLayoutFuncConversion(call *ast.CallExpr) bool {
	var obj types.Object
	switch e := call.Fun.(type) {
	case *ast.Ident:
		obj = w.pass.TypesInfo.Uses[e]
	case *ast.SelectorExpr:
		obj = w.pass.TypesInfo.Uses[e.Sel]
	default:
		return false
	}
	if obj == nil {
		return false
	}
	p := obj.Pkg()
	return p != nil && p.Path() == w.appPath && obj.Name() == "LayoutFunc"
}

// isLayoutFuncNamed reports whether a written type expression is
// exactly app.LayoutFunc.
func (w *walker) isLayoutFuncNamed(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	return w.isLayoutFuncType(w.typeOf(expr))
}

// isLayoutFuncType reports whether t is exactly the app.LayoutFunc
// named type.
func (w *walker) isLayoutFuncType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil {
		return false
	}
	p := named.Obj().Pkg()
	return p != nil && p.Path() == w.appPath && named.Obj().Name() == "LayoutFunc"
}

func (w *walker) typeOf(expr ast.Expr) types.Type {
	if tv, ok := w.pass.TypesInfo.Types[expr]; ok {
		return tv.Type
	}
	return nil
}
