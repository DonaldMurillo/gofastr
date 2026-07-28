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
// The property "this action posts to the server" is carried solely by the
// ClientJS a component registers, because that is what the action compiler keys
// on — framework/uihost/uihost.go rewrites the literal "G.serverAction(" to
// "G._serverActionFor(<componentID>, ". Matching what the compiler matches is
// as precise as the compiler is.
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
//  4. that type's Actions() method → On(...) registrations whose ClientJS
//     contains "G.serverAction(".
//
// Where a step does not resolve — a screen built in a loop, a component chosen
// at runtime, a component or screen from another package — the analyzer says
// nothing and the boot walk catches it. A false positive in a build gate is
// worse than a miss here, because the miss is already covered.
package embedcheck

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	embedPkgPath      = "github.com/DonaldMurillo/gofastr/framework/embed"
	appPkgPath        = "github.com/DonaldMurillo/gofastr/core-ui/app"
	serverActionToken = "G.serverAction(" // not-a-secret: the JS call the action compiler string-replaces; the signal this gate matches
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
	for _, fnd := range findFindings(pass.Pkg, pass.TypesInfo, pass.Files) {
		pass.Report(analysis.Diagnostic{Pos: fnd.Pos, Message: fnd.Format()})
	}
	return nil, nil
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
			"%q: G.serverAction is refused inside a frame (the action registry is "+
			"app-global with no relationship to any surface, so honouring an embed "+
			"grant would let a credential minted for one surface invoke any action "+
			"in the app). Use an island RPC, a form POST, or polling instead — "+
			"all three work in a frame.",
		f.Surface, f.Component, f.Action)
}

// findFindings resolves every embed.Surface in the package to its screen's
// component and reports any server action that component registers in its
// Actions() method. It operates on one package: cross-package references (a
// screen or component declared elsewhere) are a documented give-up.
func findFindings(pkg *types.Package, info *types.Info, files []*ast.File) []Finding {
	if pkg == nil || info == nil {
		return nil
	}
	r := newResolver(pkg, info, files)
	var out []Finding
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if !isEmbedSurfaceLit(info, lit) {
				return true
			}
			surfName, screenExpr := surfaceFields(lit)
			comp := r.resolveScreenToComponent(screenExpr)
			if comp == nil {
				return true // Screen does not resolve to a *app.Screen built by NewScreen.
			}
			named := r.concreteComponentType(comp)
			if named == nil {
				return true // component type not statically resolvable in this package.
			}
			for _, act := range r.serverActions(named) {
				out = append(out, Finding{
					Pos:       act.pos,
					Surface:   surfName,
					Component: named.Obj().Name(),
					Action:    act.event,
				})
			}
			return true
		})
	}
	return out
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
	if n := namedOf(r.info.Types[expr].Type); n != nil {
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

// serverActions returns the On(...) registrations in named's Actions() method
// whose ClientJS contains "G.serverAction(". It only inspects components
// defined in this package; a component from another package is a give-up.
func (r *resolver) serverActions(named *types.Named) []registration {
	if named.Obj().Pkg() == nil || named.Obj().Pkg() != r.pkg {
		return nil
	}
	decl := r.funcDeclForMethod(named, "Actions")
	if decl == nil || decl.Body == nil {
		return nil
	}
	var out []registration
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// component.On(event, handler, opts…) is the registration seam. Matching
		// the simple name "On" (qualified or dot-imported) is precise enough
		// here: we are already inside this specific component's Actions() body,
		// and On is the only call that registers into the action registry that
		// ExtractActions collects. ActionDef.Server / component.Server are dead
		// and intentionally not matched.
		if simpleName(call.Fun) != "On" {
			return true
		}
		event := "<dynamic>"
		if len(call.Args) > 0 {
			event = stringLitValue(call.Args[0], "<dynamic>")
		}
		if containsServerActionToken(call) {
			out = append(out, registration{pos: call.Pos(), event: event})
		}
		return true
	})
	return out
}

// containsServerActionToken reports whether call (an On registration) carries a
// string literal containing "G.serverAction(" in any argument, including the
// ClientJS passed via a nested WithClientJS(...) option. This mirrors the
// action compiler's literal scan exactly.
func containsServerActionToken(call *ast.CallExpr) bool {
	hit := false
	ast.Inspect(call, func(n ast.Node) bool {
		if hit {
			return false
		}
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(bl.Value); err == nil {
			if strings.Contains(v, serverActionToken) {
				hit = true
			}
		} else if strings.Contains(bl.Value, serverActionToken) {
			hit = true
		}
		return true
	})
	return hit
}

func simpleName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
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
