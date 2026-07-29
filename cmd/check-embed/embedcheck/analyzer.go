// Package embedcheck is the build-time gate for server actions on embeddable
// surfaces.
//
// G.serverAction does not work inside an embed frame: the action registry is
// app-global, keyed by (componentID, action) with no relationship to any
// surface, so honouring an embed grant at /__gofastr/action would let a
// credential minted for one surface invoke any action registered anywhere.
// framework/uihost already panics at boot when a surface's screen registers one
// (enforceNoServerActionsOnEmbeds in embed_actions.go). This package catches
// the same condition at `gofastr build` / `make build`, before anything runs.
//
// # The signal
//
// The property "this action posts to the server" is carried by the ClientJS
// passed to component.WithClientJS. The compiler rewrites only the canonical
// "G.serverAction(" spelling. This analyzer also detects legal whitespace
// before "(", then reports the canonical spelling instead of allowing a dead
// call to ship.
//
// component.Server(...) and ActionDef.Server look like the marker but are dead
// API: Server(...) has one call site in the whole repo (a unit test), and On()
// never sets ActionDef.Server nor does the compiler read it. Keying on either
// would record a *declaration* rather than the property — the exact failure
// mode issue #150 rejected a marker interface for — so they are deliberately
// not matched.
//
// # Reachability, and where each step gives up
//
// embed.Surface now carries the screen value, so the link from a surface to the
// component tree it renders is a Go value graph. findFindings resolves as much
// of it as go/analysis + go/types honestly can, per package:
//
//  1. embed.Surface{...} composite literals — identified by resolved type, so a
//     same-named struct elsewhere is never mistaken for one.
//  2. The Screen field → the app.NewScreen(path, comp) call that built it,
//     following one level of identifier → initializer within the package.
//  3. comp → its concrete named type, following an identifier whose declared
//     type is the component.Component interface back to its initializer.
//  4. the WHOLE component tree reachable from that type, not just the root:
//     struct fields (including embedded ones, and through pointers, slices,
//     arrays and maps), concrete components handed to the constructor
//     expression that built the root, and concrete components named in the
//     root's own Render / RenderCtx body.
//  5. each reachable type's Actions() method → executable component.On(...)
//     calls with a literal component.WithClientJS(...) option containing a
//     G.serverAction call outside JavaScript comments and strings.
//
// Step 4 is why the root is not the unit. A root that renders a child ships the
// CHILD's compiled actions to the frame — every compiled registry travels in one
// bundle — so a gate that inspected only the root passed a surface whose button
// 401s in the customer's page.
//
// # Where it stops, and why it says so
//
// Static analysis cannot follow an interface-typed field resolved at runtime, a
// component type whose Actions() body lives in another package, a component
// produced by calling a function value, ClientJS that is not a string literal,
// or a registration nested inside a function literal. Each of those is reported
// as an [Unresolved] — NOT as silence. A gate that quietly gives up reads
// exactly like a gate that checked and found nothing, which is how the
// rendered-child hole survived a release.
//
// Most Unresolved notes are advisory: the boot walk in framework/uihost reads
// live component VALUES, so a child held in a field — through an interface, a
// map key, or an island wrapper — is checked at Mount. One class is not, and
// carries Blocking: a child built inside Render() whose type lives in another
// package. It does not exist as a value when the walk runs, and its Actions()
// body is not in this syntax tree, so neither gate can vouch for it and
// `gofastr build` stops.
//
// Failing on EVERY note was tried and reverted. It rejected clean island
// surfaces — the shape the blueprint emits for every island block — plus
// interface-typed fields the analyzer had already resolved and the fixture
// named for false positives, and the advertised remedy ("hold the child in a
// field") is impossible for a wrapper.
//
// [Check] returns violations alone for callers that want only those;
// [CheckAll] returns both and is what the build gate uses.
package embedcheck

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// renderWrapperPackages hold components that WRAP another component rather
// than being a leaf: the value walk at Mount descends through them into the
// child, so seeing one built inside Render is not a place analysis gave up.
var renderWrapperPackages = map[string]bool{
	"github.com/DonaldMurillo/gofastr/core-ui/island":    true,
	"github.com/DonaldMurillo/gofastr/core-ui/component": true,
	"github.com/DonaldMurillo/gofastr/core-ui/app":       true,
}

const (
	embedPkgPath     = "github.com/DonaldMurillo/gofastr/framework/embed"
	appPkgPath       = "github.com/DonaldMurillo/gofastr/core-ui/app"
	componentPkgPath = "github.com/DonaldMurillo/gofastr/core-ui/component"
	serverActionName = "G.serverAction"
)

// Analyzer is the go/analysis pass. analysistest exercises it directly, and a
// future `go vet` attachment would run it; the cmd/check-embed CLI and the
// `gofastr build` gate both call the same findFindings core via Check.
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

// Unresolved is one place the static walk could not follow, on the path from an
// embeddable surface to the components it renders.
//
// It is never a violation — the surface may well be clean. It exists so that
// "I found nothing" and "I could not look" are not the same output.
//
// Most notes are advisory, because the boot-time walk in
// framework/uihost/embed_actions.go covers what they describe: it reads live
// component VALUES, so a child held in a field — including through an
// interface, a map key, or an island wrapper — is checked at Mount.
//
// Blocking is the exception: a child CONSTRUCTED inside Render() whose type
// lives in another package is visible to neither gate. It does not exist as a
// value when the boot walk runs, and its Actions() body is not in this syntax
// tree. Only that class fails the build.
type Unresolved struct {
	Pos     token.Pos
	Surface string // the surface Name; "<dynamic>" when not a string literal
	Reason  string
	// Blocking marks the note class no gate can cover, which is what
	// `gofastr build` refuses to build past.
	Blocking bool
}

// Format renders the human-facing note.
func (u Unresolved) Format() string {
	return fmt.Sprintf(
		"embed surface %q: %s — check-embed cannot prove this surface is free of "+
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

// Format renders the human-facing message, mirroring the boot-walk panic so a
// developer sees the same explanation at build time and at boot.
func (f Finding) Format() string {
	return fmt.Sprintf(
		"embed surface %q renders component %q which registers a server action "+
			"%q: G.serverAction(...) is refused inside a frame (the action registry "+
			"is app-global with no relationship to any surface, so honouring an "+
			"embed grant would let a credential minted for one surface invoke any "+
			"action in the app). The compiler accepts only the canonical spelling "+
			"G.serverAction(...), with no whitespace before '('. Use an island RPC, "+
			"a form POST, or polling instead — all three work in a frame.",
		f.Surface, f.Component, f.Action)
}

// findFindings returns the violations alone. Kept as the name the CLI, the
// build gate and the tests already use.
func findFindings(pkg *types.Package, info *types.Info, files []*ast.File) []Finding {
	findings, _ := analyze(pkg, info, files)
	return findings
}

// analyze resolves every embed.Surface in the package to the component TREE its
// screen renders, and reports both the server actions that tree registers and
// every place the walk could not follow. It operates on one package:
// cross-package references (a screen or component declared elsewhere) are a
// reported give-up, not silence.
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
					note(an.pos, an.reason)
				}
			}
			return true
		})
	}
	return findings, notes
}

// maxReachDepth bounds the type walk. Component trees are shallow; the cap stops
// a self-referential type graph from spinning without needing a second visited
// set for every intermediate type.
const maxReachDepth = 12

// reachableComponents returns every component type reachable from root that is
// declared in this package, root first, and calls note for each step it cannot
// follow.
//
// Three sources feed the walk, and each answers a shape real code uses:
//
//   - struct fields — a root that holds its children (the shape the boot gate
//     caught shipping a child's action to a frame);
//   - the construction expression — `app.NewScreen("/x", NewPanel(&child{}))`,
//     where the concrete child is an argument and the field that stores it is
//     interface-typed;
//   - the root's Render / RenderCtx body — `return c.child.Render()`, and any
//     child built inline there.
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
	// Concrete components handed to whatever built the root: composite-literal
	// elements and call arguments, at any nesting.
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

// walkComponentType follows cur's fields (through pointers, slices, arrays and
// maps, and into nested plain structs declared in this package), pushing every
// in-package component type it finds and noting every one it cannot follow.
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
			// A NAMED interface (component.Component is one) is an interface
			// first: it has a Render method but no body anywhere, and what it
			// holds is a runtime choice.
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
			// A plain struct in this package can still hold components.
			if x.Obj() != nil && x.Obj().Pkg() == r.pkg {
				walk(x.Underlying(), depth+1, pos, label)
			}
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				f := x.Field(i)
				walk(f.Type(), depth+1, f.Pos(), fmt.Sprintf("field %s.%s", owner, f.Name()))
			}
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		walk(f.Type(), 0, f.Pos(), fmt.Sprintf("field %s.%s", owner, f.Name()))
	}
}

// componentsInExpr collects every sub-expression of expr whose resolved type is
// a concrete component type declared in this package. Used on the expression
// that built the root component, so a child passed to a constructor is followed
// even when the field it lands in is interface-typed.
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

// componentsInRenderBodies collects the in-package component types named in
// cur's Render / RenderCtx bodies. `return c.child.Render()` is the plainest
// spelling of "this root renders that child", and it carries no field the type
// walk could have found when the child is reached through an interface.
func (r *resolver) componentsInRenderBodies(cur *types.Named, blockingNote func(token.Pos, string)) []*types.Named {
	var out []*types.Named
	noted := map[*types.Named]bool{}
	for _, m := range []string{"Render", "RenderCtx"} {
		decl := r.funcDeclForMethod(cur, m)
		if decl == nil || decl.Body == nil {
			continue
		}
		ast.Inspect(decl.Body, func(n ast.Node) bool {
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
			// A named INTERFACE (component.Component itself) is not a child
			// built here — it is the static type of one held elsewhere, and
			// walkComponentType already notes it as runtime-chosen. Noting it
			// again would double every interface-typed field.
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				return true
			}
			// Known composition wrappers are not a give-up: the boot walk
			// descends THROUGH them into the child they hold (island is in
			// neither reachStopPackages nor this list), so the child is
			// covered at Mount and there is nothing to warn about. Noting
			// them turned every island-rendering surface — the shape the
			// blueprint emits for every island block — into a build failure.
			if renderWrapperPackages[named.Obj().Pkg().Path()] {
				return true
			}
			// A child BUILT here whose type comes from another package is the
			// one shape NEITHER gate can clear: it does not exist as a value
			// until Render runs, so the boot-time walk in framework/uihost
			// cannot see it, and its Actions() body is not in this syntax
			// tree, so nothing here can read it either. Dropping it silently
			// is what let a child's server action reach a customer's frame
			// with both gates green.
			if !noted[named] {
				noted[named] = true
				blockingNote(e.Pos(), fmt.Sprintf("%s.%s builds component %s from another package, "+
					"whose Actions() body is not visible here",
					cur.Obj().Name(), m, named.String()))
			}
			return true
		})
	}
	return out
}

// hasRenderMethod reports whether t (or *t) declares Render — the one method
// every component has. Used instead of types.Implements against
// component.Component so a package that declares a component without importing
// core-ui/component is still followed.
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
	for i := 0; i < x.NumMethods(); i++ {
		if x.Method(i).Name() == "Render" {
			return true
		}
	}
	return false
}

func signatureMakesComponent(sig *types.Signature) bool {
	for i := 0; i < sig.Results().Len(); i++ {
		t := sig.Results().At(i).Type()
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

// ---- identifier → initializer following --------------------------------

type resolver struct {
	pkg   *types.Package
	info  *types.Info
	files []*ast.File

	// defIdent maps an object to its defining *ast.Ident within this package,
	// so an identifier reference can be followed to the expression that
	// initialized it (a var spec or a := assignment).
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

// resolveScreenToComponent follows a Surface.Screen expression to the
// app.NewScreen(path, comp) call that built it and returns the comp argument.
// Returns nil when the screen is not statically a *app.Screen from NewScreen in
// this package (cross-package, computed, or a different construction).
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
		// Only follow vars declared in THIS package; a screen value from
		// another package is a give-up (the boot walk covers it).
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

// screenFromCallChain handles app.NewScreen(...) and method chains on its
// result (WithTitle, WithPolicy, WithDescription, … — all return *Screen). The
// base of the chain is the NewScreen call, so a method-call receiver is
// unwrapped until it is reached.
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

// newScreenComponent reports whether call is app.NewScreen(path, comp) and
// returns the comp argument. It confirms the callee's package via go/types so a
// same-named function in another package is not mistaken for it.
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

// varInitializer returns the initializer expression of a package- or
// block-scoped var in this package, or nil. It matches by the defining
// identifier's token position (stable and unique within a file).
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
			// `name := …` (DEFINE). A plain `=` to a blank identifier or a
			// multi-assign does not carry the screen value we can follow.
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

// ---- component type resolution -----------------------------------------

// concreteComponentType resolves a component expression to its concrete named
// type, following an identifier whose declared type is the component.Component
// interface back to its initializer. Returns nil when the concrete type is not
// visible in this package.
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

// ---- server-action registration scan ----------------------------------

type registration struct {
	pos   token.Pos
	event string
}

// actionNote is one give-up inside an Actions() body.
type actionNote struct {
	pos    token.Pos
	reason string
}

// serverActions returns executable component.On(...) registrations in named's
// Actions method whose literal WithClientJS option calls G.serverAction, plus
// the registrations it could not decide about.
//
// It skips function literals because their bodies do not execute merely by
// being declared — but a skipped body that CONTAINS a server action is reported
// as unresolved rather than passed over, because an immediately-invoked literal
// executes and this walk cannot tell the two apart. The compiled-registry boot
// gate is what covers them.
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

// serverActionInFuncLit reports whether lit contains a component.On(...) whose
// literal ClientJS calls G.serverAction, and where.
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

// hasClientJSOption reports whether the registration passes a WithClientJS
// option at all, so a non-literal argument can be told apart from an action
// that declares no client handler.
func (r *resolver) hasClientJSOption(call *ast.CallExpr) bool {
	if len(call.Args) < 3 {
		return false
	}
	for _, arg := range call.Args[2:] {
		opt, ok := arg.(*ast.CallExpr)
		if ok && r.isComponentCall(opt, "WithClientJS") {
			return true
		}
	}
	return false
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

// clientJSLiteral returns the literal JavaScript passed directly to a
// component.WithClientJS option on this registration. Identifiers,
// concatenations, wrapper options, and options returned by helpers are static
// give-ups; evaluating them would risk reporting code that never registers.
func (r *resolver) clientJSLiteral(call *ast.CallExpr) (string, bool) {
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

// executableServerActionCall scans JavaScript without treating text in
// comments or string/template literals as code. It accepts whitespace and
// comments between the callee and "(", both legal JavaScript forms that the
// compiler cannot rewrite and that this gate must reject.
func executableServerActionCall(src string) bool {
	for i := 0; i < len(src); {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipJSLineComment(src, i+2)
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipJSBlockComment(src, i+2)
		case src[i] == '\'' || src[i] == '"' || src[i] == '`':
			i = skipJSString(src, i+1, src[i])
		case strings.HasPrefix(src[i:], serverActionName) &&
			(i == 0 || !isJSIdentByte(src[i-1])):
			after := i + len(serverActionName)
			if after < len(src) && isJSIdentByte(src[after]) {
				i = after
				continue
			}
			after = skipJSTrivia(src, after)
			if after < len(src) && src[after] == '(' {
				return true
			}
			i = after
		default:
			i++
		}
	}
	return false
}

func skipJSTrivia(src string, i int) int {
	for i < len(src) {
		if size := jsSpaceLen(src[i:]); size > 0 {
			i += size
			continue
		}
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipJSLineComment(src, i+2)
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipJSBlockComment(src, i+2)
		default:
			return i
		}
	}
	return i
}

func skipJSLineComment(src string, i int) int {
	for i < len(src) && src[i] != '\n' && src[i] != '\r' {
		i++
	}
	return i
}

func skipJSBlockComment(src string, i int) int {
	for i+1 < len(src) {
		if src[i] == '*' && src[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(src)
}

func skipJSString(src string, i int, quote byte) int {
	for i < len(src) {
		if src[i] == '\\' && i+1 < len(src) {
			i += 2
			continue
		}
		if src[i] == quote {
			return i + 1
		}
		i++
	}
	return len(src)
}

func jsSpaceLen(src string) int {
	r, size := utf8.DecodeRuneInString(src)
	if unicode.IsSpace(r) {
		return size
	}
	return 0
}

func isJSIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}

// funcDeclForMethod finds the FuncDecl for methodName on named by receiver type
// name, scanning this package's files. Matching by receiver type name + method
// name avoids depending on token-position semantics of method declarations.
func (r *resolver) funcDeclForMethod(named *types.Named, methodName string) *ast.FuncDecl {
	typeName := named.Obj().Name()
	for _, f := range r.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name == nil || fd.Name.Name != methodName || len(fd.Recv.List) == 0 {
				continue
			}
			if receiverTypeName(fd.Recv.List[0].Type) == typeName {
				return fd
			}
		}
	}
	return nil
}

func receiverTypeName(e ast.Expr) string {
	for {
		star, ok := e.(*ast.StarExpr)
		if !ok {
			break
		}
		e = star.X
	}
	// strip generic instantiation if present: Foo[T] → Foo
	if idx, ok := e.(*ast.IndexExpr); ok {
		e = idx.X
	} else if ix, ok := e.(*ast.IndexListExpr); ok {
		e = ix.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
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
