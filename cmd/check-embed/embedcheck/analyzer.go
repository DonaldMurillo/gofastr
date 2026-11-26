// Package embedcheck reports embeddable surfaces whose screen renders a
// component that registers a G.serverAction, which a frame refuses, and each
// place on the way that static analysis could not follow. [Check] returns
// the violations; [CheckAll] adds the notes.
package embedcheck

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/actionscan"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

var renderWrapperPackages = map[string]bool{
	"github.com/DonaldMurillo/gofastr/core-ui/island":    true,
	"github.com/DonaldMurillo/gofastr/core-ui/component": true,
	"github.com/DonaldMurillo/gofastr/core-ui/app":       true,
}

const (
	embedPkgPath     = "github.com/DonaldMurillo/gofastr/framework/embed"
	appPkgPath       = "github.com/DonaldMurillo/gofastr/core-ui/app"
	componentPkgPath = "github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Analyzer runs the check Check runs as a go/analysis pass.
var Analyzer = &analysis.Analyzer{
	Name: "check_embed",
	Doc:  "report embeddable surfaces whose screen's component registers a G.serverAction, which is refused inside a frame",
	Run:  runPass,
}

func runPass(pass *analysis.Pass) (any, error) {
	findings, unresolved := analyze(pass.Pkg, pass.TypesInfo, pass.Files)
	for _, fnd := range findings {
		pass.Report(analysis.Diagnostic{Pos: fnd.Pos, Message: fnd.Format()})
	}
	for _, u := range unresolved {
		pass.Report(analysis.Diagnostic{Pos: u.Pos, Message: u.Format()})
	}
	return nil, nil
}

// Unresolved is one place on the path from an embeddable surface to the
// components it renders that the static walk could not follow. It is not a
// violation.
type Unresolved struct {
	Pos      token.Pos
	Surface  string // the surface Name; "<dynamic>" when not a string literal
	Reason   string
	Blocking bool // neither this walk nor the boot walk can cover the place
}

// Format renders the human-facing note.
func (u Unresolved) Format() string {
	return fmt.Sprintf(
		"embed surface %q: %s: check-embed cannot prove this surface is free of "+
			"server actions, and the boot walk cannot cover a child that does not "+
			"exist until Render runs. Hold the child in a field instead of "+
			"building it in Render, or move its type into this package, so the "+
			"surface can be verified.",
		u.Surface, u.Reason)
}

// Finding is one provable server action reachable from an embeddable surface.
type Finding struct {
	Pos       token.Pos
	Surface   string // the surface Name; "<dynamic>" when not a string literal
	Component string // concrete component type name
	Action    string // the On() event name; "<dynamic>" when not a string literal
}

// Format renders the human-facing message.
func (f Finding) Format() string {
	return fmt.Sprintf(
		"embed surface %q renders component %q which registers a server action "+
			"%q: G.serverAction(...) is refused inside a frame (the action registry "+
			"is app-global with no relationship to any surface, so honouring an "+
			"embed grant would let a credential minted for one surface invoke any "+
			"action in the app). The compiler accepts only the canonical spelling "+
			"G.serverAction(...), with no whitespace before '('. Use an island RPC, "+
			"a form POST, or polling instead: all three work in a frame.",
		f.Surface, f.Component, f.Action)
}

func analyze(pkg *types.Package, info *types.Info, files []*ast.File) ([]Finding, []Unresolved) {
	if pkg == nil || info == nil {
		return nil, nil
	}
	r := newResolver(pkg, info, files)
	var findings []Finding
	var notes []Unresolved
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isEmbedSurfaceLit(info, lit) {
				return true
			}
			surfName, screenExpr := surfaceFields(lit)
			note := func(pos token.Pos, reason string) {
				notes = append(notes, Unresolved{Pos: pos, Surface: surfName, Reason: reason})
			}
			blockingNote := func(pos token.Pos, reason string) {
				notes = append(notes, Unresolved{Pos: pos, Surface: surfName, Reason: reason, Blocking: true})
			}
			comp := r.resolveScreenToComponent(screenExpr)
			if comp == nil {
				note(lit.Pos(), "the Screen field does not resolve to an app.NewScreen(...) "+
					"call in this package (computed, cross-package, or built another way)")
				return true
			}
			named := r.concreteComponentType(comp)
			if named == nil {
				note(comp.Pos(), "the screen's component has no concrete type visible in this package")
				return true
			}
			bootVisible := r.bootVisibleComponents(named, comp)
			for _, reached := range r.reachableComponents(named, comp, note, blockingNote) {
				acts, actNotes := r.serverActions(reached)
				for _, act := range acts {
					findings = append(findings, Finding{
						Pos:       act.pos,
						Surface:   surfName,
						Component: reached.Obj().Name(),
						Action:    act.event,
					})
				}
				for _, an := range actNotes {
					if bootVisible[reached] {
						note(an.pos, an.reason)
					} else {
						blockingNote(an.pos, an.reason)
					}
				}
			}
			return true
		})
	}
	return findings, notes
}

func (r *resolver) bootVisibleComponents(root *types.Named, expression ast.Expr) map[*types.Named]bool {
	visible := map[*types.Named]bool{root: true}
	seen := map[ast.Expr]bool{}
	var inspect func(ast.Expr)
	inspect = func(expression ast.Expr) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		ast.Inspect(expression, func(node ast.Node) bool {
			var value ast.Expr
			switch node := node.(type) {
			case *ast.FuncLit:
				return false
			case *ast.Ident:
				if variable, ok := r.info.Uses[node].(*types.Var); ok && !variable.IsField() && variable.Pkg() == r.pkg && !r.variableWrittenBefore(variable, node.Pos()) {
					inspect(r.varInitializer(variable))
				}
				return false
			case *ast.CompositeLit:
				value = node
			case *ast.CallExpr:
				if identifier, ok := node.Fun.(*ast.Ident); ok {
					if builtin, ok := r.info.Uses[identifier].(*types.Builtin); ok && builtin.Name() == "new" {
						value = node
					}
				}
				if value == nil && !r.info.Types[node.Fun].IsType() {
					return false
				}
			}
			if value != nil {
				if named := namedOf(r.info.TypeOf(value)); named != nil && hasRenderMethod(named) {
					visible[named] = true
				}
			}
			return true
		})
	}
	inspect(expression)
	return visible
}

func (r *resolver) variableWrittenBefore(variable *types.Var, before token.Pos) bool {
	written := false
	check := func(expression ast.Expr) {
		if identifier, ok := ast.Unparen(expression).(*ast.Ident); ok && r.info.Uses[identifier] == variable {
			written = true
		}
	}
	for _, file := range r.files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || node.Pos() >= before || written {
				return false
			}
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, target := range node.Lhs {
					check(target)
				}
			case *ast.RangeStmt:
				if node.Tok == token.ASSIGN {
					check(node.Key)
					check(node.Value)
				}
			}
			return !written
		})
	}
	return written
}

const maxReachDepth = 12

func (r *resolver) reachableComponents(root *types.Named, rootExpr ast.Expr, note, blockingNote func(token.Pos, string)) []*types.Named {
	var out []*types.Named
	seen := map[*types.Named]bool{}
	var queue []*types.Named
	push := func(n *types.Named) {
		if n == nil || seen[n] {
			return
		}
		if n.Obj() == nil || n.Obj().Pkg() != r.pkg {
			return
		}
		seen[n] = true
		out = append(out, n)
		queue = append(queue, n)
	}
	push(root)
	for _, n := range r.componentsInExpr(rootExpr) {
		push(n)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		r.walkComponentType(cur, push, note)
		for _, n := range r.componentsInRenderBodies(cur, blockingNote) {
			push(n)
		}
	}
	return out
}

func (r *resolver) walkComponentType(cur *types.Named, push func(*types.Named), note func(token.Pos, string)) {
	st, ok := cur.Underlying().(*types.Struct)
	if !ok {
		return
	}
	owner := cur.Obj().Name()
	var walk func(t types.Type, depth int, pos token.Pos, label string)
	visited := map[types.Type]bool{}
	walk = func(t types.Type, depth int, pos token.Pos, label string) {
		if t == nil || depth > maxReachDepth || visited[t] {
			return
		}
		visited[t] = true
		switch x := t.(type) {
		case *types.Pointer:
			walk(x.Elem(), depth+1, pos, label)
		case *types.Slice:
			walk(x.Elem(), depth+1, pos, label)
		case *types.Array:
			walk(x.Elem(), depth+1, pos, label)
		case *types.Map:
			walk(x.Elem(), depth+1, pos, label)
		case *types.Signature:
			if signatureMakesComponent(x) {
				note(pos, fmt.Sprintf("%s returns a component built at call time, "+
					"so what it renders is not knowable here", label))
			}
		case *types.Interface:
			if interfaceHasRender(x) {
				note(pos, fmt.Sprintf("%s is interface-typed, so the component it "+
					"holds is chosen at runtime", label))
			}
		case *types.Named:
			if iface, ok := x.Underlying().(*types.Interface); ok {
				if interfaceHasRender(iface) {
					note(pos, fmt.Sprintf("%s is interface-typed, so the component it "+
						"holds is chosen at runtime", label))
				}
				return
			}
			if hasRenderMethod(x) {
				if x.Obj() != nil && x.Obj().Pkg() == r.pkg {
					push(x)
					return
				}
				note(pos, fmt.Sprintf("%s has component type %s from another package, "+
					"whose Actions() body is not visible here", label, x.String()))
				return
			}
			if x.Obj() != nil && x.Obj().Pkg() == r.pkg {
				walk(x.Underlying(), depth+1, pos, label)
			}
		case *types.Struct:
			for f := range x.Fields() {
				walk(f.Type(), depth+1, f.Pos(), fmt.Sprintf("field %s.%s", owner, f.Name()))
			}
		}
	}
	for f := range st.Fields() {
		walk(f.Type(), 0, f.Pos(), fmt.Sprintf("field %s.%s", owner, f.Name()))
	}
}

func (r *resolver) componentsInExpr(expr ast.Expr) []*types.Named {
	var out []*types.Named
	if expr == nil {
		return nil
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if named := namedOf(r.info.Types[e].Type); named != nil && hasRenderMethod(named) {
			if named.Obj() != nil && named.Obj().Pkg() == r.pkg {
				out = append(out, named)
			}
		}
		return true
	})
	return out
}

func (r *resolver) componentsInRenderBodies(cur *types.Named, blockingNote func(token.Pos, string)) []*types.Named {
	var out []*types.Named
	noted := map[*types.Named]bool{}

	type frame struct {
		body   *ast.BlockStmt
		method string // the Render method this body was reached from
	}
	var queue []frame
	walked := map[*ast.BlockStmt]bool{}
	for _, m := range []string{"Render", "RenderCtx"} {
		if decl := r.funcDeclForMethod(cur, m); decl != nil && decl.Body != nil {
			queue = append(queue, frame{decl.Body, m})
		}
	}

	for len(queue) > 0 {
		fr := queue[0]
		queue = queue[1:]
		if walked[fr.body] {
			continue
		}
		walked[fr.body] = true

		ast.Inspect(fr.body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if b := r.componentHelperBody(call); b != nil {
					queue = append(queue, frame{b, fr.method})
				}
			}
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			named := namedOf(r.info.Types[e].Type)
			if named == nil || named == cur || !hasRenderMethod(named) || named.Obj() == nil {
				return true
			}
			if named.Obj().Pkg() == r.pkg {
				out = append(out, named)
				return true
			}
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				return true
			}
			if renderWrapperPackages[named.Obj().Pkg().Path()] {
				return true
			}
			if !noted[named] {
				noted[named] = true
				blockingNote(e.Pos(), fmt.Sprintf("%s.%s builds component %s from another package, "+
					"whose Actions() body is not visible here",
					cur.Obj().Name(), fr.method, named.String()))
			}
			return true
		})
	}
	return out
}

func (r *resolver) componentHelperBody(call *ast.CallExpr) *ast.BlockStmt {
	var name *ast.Ident
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun
	case *ast.SelectorExpr:
		name = fun.Sel
	default:
		return nil
	}
	obj, ok := r.info.Uses[name].(*types.Func)
	if !ok || obj.Pkg() != r.pkg {
		return nil
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return nil
	}
	if !componentShaped(sig.Results().At(0).Type()) {
		return nil
	}
	for _, f := range r.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Body == nil {
				continue
			}
			if r.info.Defs[fd.Name] == obj {
				return fd.Body
			}
		}
	}
	return nil
}

func componentShaped(t types.Type) bool {
	if sl, ok := t.Underlying().(*types.Slice); ok {
		t = sl.Elem()
	}
	if iface, ok := t.Underlying().(*types.Interface); ok {
		for i := 0; i < iface.NumMethods(); i++ {
			if iface.Method(i).Name() == "Render" {
				return true
			}
		}
		return false
	}
	named := namedOf(t)
	return named != nil && hasRenderMethod(named)
}

func hasRenderMethod(t types.Type) bool {
	if t == nil {
		return false
	}
	if obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Render"); isMethod(obj) {
		return true
	}
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, "Render")
	return isMethod(obj)
}

func isMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil && sig.Params().Len() == 0 && sig.Results().Len() == 1
}

func interfaceHasRender(x *types.Interface) bool {
	for method := range x.Methods() {
		if method.Name() == "Render" {
			return true
		}
	}
	return false
}

func signatureMakesComponent(sig *types.Signature) bool {
	for v := range sig.Results().Variables() {
		t := v.Type()
		if iface, ok := t.Underlying().(*types.Interface); ok {
			if interfaceHasRender(iface) {
				return true
			}
			continue
		}
		if named := namedOf(t); named != nil && hasRenderMethod(named) {
			return true
		}
	}
	return false
}

type resolver struct {
	pkg   *types.Package
	info  *types.Info
	files []*ast.File

	defIdent map[types.Object]*ast.Ident
}

func newResolver(pkg *types.Package, info *types.Info, files []*ast.File) *resolver {
	r := &resolver{pkg: pkg, info: info, files: files, defIdent: map[types.Object]*ast.Ident{}}
	for id, obj := range info.Defs {
		if obj == nil {
			continue
		}
		if _, ok := r.defIdent[obj]; !ok {
			r.defIdent[obj] = id
		}
	}
	return r
}

func isEmbedSurfaceLit(info *types.Info, lit *ast.CompositeLit) bool {
	tv, ok := info.Types[lit]
	if !ok || tv.Type == nil {
		return false
	}
	named, ok := tv.Type.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == embedPkgPath && obj.Name() == "Surface"
}

func surfaceFields(lit *ast.CompositeLit) (name string, screen ast.Expr) {
	name = "<dynamic>"
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch id.Name {
		case "Name":
			name = stringLitValue(kv.Value, "<dynamic>")
		case "Screen":
			screen = kv.Value
		}
	}
	return name, screen
}

func (r *resolver) resolveScreenToComponent(expr ast.Expr) ast.Expr {
	return r.followScreen(expr, map[types.Object]bool{})
}

func (r *resolver) followScreen(expr ast.Expr, seen map[types.Object]bool) ast.Expr {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return r.followScreen(e.X, seen)
	case *ast.Ident:
		obj, ok := r.info.Uses[e].(*types.Var)
		if !ok || seen[obj] {
			return nil
		}
		seen[obj] = true
		if obj.Pkg() == nil || obj.Pkg() != r.pkg {
			return nil
		}
		rhs := r.varInitializer(obj)
		if rhs == nil {
			return nil
		}
		return r.followScreen(rhs, seen)
	case *ast.CallExpr:
		return r.screenFromCallChain(e)
	}
	return nil
}

func (r *resolver) screenFromCallChain(call *ast.CallExpr) ast.Expr {
	if comp, ok := r.newScreenComponent(call); ok {
		return comp
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if inner, ok := sel.X.(*ast.CallExpr); ok {
			return r.screenFromCallChain(inner)
		}
	}
	return nil
}

func (r *resolver) newScreenComponent(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "NewScreen" {
		return nil, false
	}
	fn, ok := r.info.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != appPkgPath {
		return nil, false
	}
	if len(call.Args) < 2 {
		return nil, false
	}
	return call.Args[1], true
}

func (r *resolver) varInitializer(v *types.Var) ast.Expr {
	id := r.defIdent[v]
	if id == nil {
		return nil
	}
	f := r.fileContaining(id.Pos())
	if f == nil {
		return nil
	}
	var result ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		switch s := n.(type) {
		case *ast.ValueSpec:
			for i, name := range s.Names {
				if name.Pos() == id.Pos() && i < len(s.Values) {
					result = s.Values[i]
					return false
				}
			}
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				for i, lhs := range s.Lhs {
					if lid, ok := lhs.(*ast.Ident); ok && lid.Pos() == id.Pos() && i < len(s.Rhs) {
						result = s.Rhs[i]
						return false
					}
				}
			}
		}
		return true
	})
	return result
}

func (r *resolver) fileContaining(pos token.Pos) *ast.File {
	for _, f := range r.files {
		if f.Pos() <= pos && pos <= f.End() {
			return f
		}
	}
	return nil
}

func (r *resolver) concreteComponentType(expr ast.Expr) *types.Named {
	if expr == nil {
		return nil
	}
	if n := namedOf(r.info.Types[expr].Type); n != nil && !types.IsInterface(n) {
		return n
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := r.info.Uses[id].(*types.Var)
	if !ok || v.Pkg() == nil || v.Pkg() != r.pkg {
		return nil
	}
	rhs := r.varInitializer(v)
	if rhs == nil {
		return nil
	}
	return namedOf(r.info.Types[rhs].Type)
}

func namedOf(t types.Type) *types.Named {
	for {
		if t == nil {
			return nil
		}
		switch x := t.(type) {
		case *types.Named:
			return x
		case *types.Pointer:
			t = x.Elem()
		default:
			return nil
		}
	}
}

type registration struct {
	pos   token.Pos
	event string
}

type actionNote struct {
	pos    token.Pos
	reason string
}

func (r *resolver) serverActions(named *types.Named) ([]registration, []actionNote) {
	if named.Obj().Pkg() == nil || named.Obj().Pkg() != r.pkg {
		return nil, nil
	}
	decl := r.funcDeclForMethod(named, "Actions")
	if decl == nil || decl.Body == nil {
		return nil, nil
	}
	owner := named.Obj().Name()
	var out []registration
	var notes []actionNote
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			if pos, found := r.serverActionInFuncLit(lit); found {
				notes = append(notes, actionNote{pos: pos, reason: fmt.Sprintf(
					"%s registers a server action inside a function literal, which "+
						"this walk does not execute", owner)})
			}
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !r.isComponentCall(call, "On") {
			return true
		}
		clientJS, ok := r.clientJSLiteral(call)
		if !ok {
			if r.hasClientJSOption(call) {
				notes = append(notes, actionNote{pos: call.Pos(), reason: fmt.Sprintf(
					"%s registers an action whose WithClientJS argument is not a string "+
						"literal, so whether it calls G.serverAction is not knowable here",
					owner)})
			}
			return true
		}
		if !executableServerActionCall(clientJS) {
			return true
		}
		event := "<dynamic>"
		if len(call.Args) > 0 {
			event = stringLitValue(call.Args[0], "<dynamic>")
		}
		out = append(out, registration{pos: call.Pos(), event: event})
		return true
	})
	return out, notes
}

func (r *resolver) serverActionInFuncLit(lit *ast.FuncLit) (token.Pos, bool) {
	var pos token.Pos
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !r.isComponentCall(call, "On") {
			return true
		}
		if js, ok := r.clientJSLiteral(call); ok && executableServerActionCall(js) {
			pos, found = call.Pos(), true
			return false
		}
		return true
	})
	return pos, found
}

func (r *resolver) hasClientJSOption(call *ast.CallExpr) bool {
	if len(call.Args) < 3 {
		return false
	}
	for _, arg := range call.Args[2:] {
		opt, ok := arg.(*ast.CallExpr)
		if !ok || !r.isComponentCall(opt, componentOptionName(opt)) || r.isComponentCall(opt, "WithClientJS") {
			return true
		}
	}
	return false
}

func componentOptionName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		return function.Sel.Name
	case *ast.Ident:
		return function.Name
	}
	return ""
}

func (r *resolver) isComponentCall(call *ast.CallExpr, name string) bool {
	var obj types.Object
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		obj = r.info.Uses[fun]
	case *ast.SelectorExpr:
		obj = r.info.Uses[fun.Sel]
	default:
		return false
	}
	fn, ok := obj.(*types.Func)
	return ok && fn.Name() == name && fn.Pkg() != nil && fn.Pkg().Path() == componentPkgPath
}

func (r *resolver) clientJSLiteral(call *ast.CallExpr) (string, bool) {
	for _, arg := range call.Args[2:] {
		opt, ok := arg.(*ast.CallExpr)
		if !ok || !r.isComponentCall(opt, componentOptionName(opt)) {
			return "", false
		}
	}
	for _, arg := range call.Args[2:] {
		opt, ok := arg.(*ast.CallExpr)
		if !ok || !r.isComponentCall(opt, "WithClientJS") || len(opt.Args) != 1 {
			continue
		}
		lit, ok := opt.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return value, true
		}
		return strings.Trim(lit.Value, "\"`"), true
	}
	return "", false
}

func executableServerActionCall(src string) bool {
	return actionscan.ServerActionCall(src)
}

func (r *resolver) funcDeclForMethod(named *types.Named, methodName string) *ast.FuncDecl {
	typeName := named.Obj().Name()
	for _, f := range r.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name == nil || fd.Name.Name != methodName || len(fd.Recv.List) == 0 {
				continue
			}
			if astbase.RecvBaseNameGeneric(fd) == typeName {
				return fd
			}
		}
	}
	return nil
}

func stringLitValue(e ast.Expr, def string) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return def
	}
	if v, err := strconv.Unquote(bl.Value); err == nil {
		return v
	}
	return strings.Trim(bl.Value, "\"`")
}
