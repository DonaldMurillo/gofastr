package analyzers

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// Route is one statically discovered registration.
type Route struct {
	// Method is the HTTP verb exactly as written, case included, since a
	// lowercase one is a finding rather than something to normalise away.
	Method string
	// Pattern is the full path with any resolvable group prefix applied.
	Pattern string
	// RawPattern is the literal passed at the call site, before prefixing.
	RawPattern string
	// Group is the receiver identifier, "" for a direct router call.
	Group string
	// Guarded is true when the route was registered on a group carrying
	// access or middleware, which is the framework's guarding seam.
	Guarded bool
	// Handler is the handler expression, for evidence.
	Handler string
	// Screen marks a route declared through `app.NewScreen`. Screens are
	// matched by core-ui's own router, NOT by ServeMux, and that router
	// takes `:id` parameters natively. It even rewrites `{id}` into
	// `:id`. Every rule about ServeMux pattern syntax must therefore skip
	// them, or it reports the correct spelling as a bug.
	Screen bool
	// Package is the import path of the registering package. Duplicate
	// detection is scoped by it: two example apps in one repository both
	// serving "/healthz" are not a conflict, they are two programs.
	Package string

	File string
	Line int
	Col  int
	Pos  token.Pos
}

// EntityDecl is one `app.Entity(...)` / `GroupEntity(...)` call.
type EntityDecl struct {
	Name  string
	Group string
	File  string
	Line  int
	Pos   token.Pos
}

// RouteTable is everything the static pass could learn about the app's
// surface. It is memoized on the pass, so the routing, permissions,
// testing, and guidance analyzers all read one traversal.
type RouteTable struct {
	Routes   []Route
	Entities []EntityDecl
	// Registered is true when at least one registration was found,
	// distinguishing "no routes" from "this project does not register
	// routes in a way the static pass can see". Analyzers stay quiet in
	// the second case rather than reporting an empty app.
	Registered bool
}

// routeVerbs are the RouteGroup / Router convenience methods, mapped to
// the verb they register.
var routeVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT",
	"Patch": "PATCH", "Delete": "DELETE",
	"GetFunc": "GET", "PostFunc": "POST", "PutFunc": "PUT",
	"PatchFunc": "PATCH", "DeleteFunc": "DELETE",
}

// mutatingMethods are the verbs that change state: the ones an access
// declaration has to cover.
var mutatingMethods = map[string]bool{
	"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

const routeTableKey = "analyzers.routetable"

// Routes returns the pass's route table, computing it at most once.
func Routes(p *contracts.Pass) *RouteTable {
	return p.Memo(routeTableKey, func() any { return buildRouteTable(p) }).(*RouteTable)
}

// routerScope tracks which identifiers in a file refer to a router or
// route group, and what prefix and guarding each carries. Scoping is
// per-file rather than per-block: route wiring is conventionally a flat
// sequence of statements in one function, and a block-accurate resolver
// would cost far more than the precision is worth here.
type routerScope struct {
	prefix  map[string]string
	guarded map[string]bool
	known   map[string]bool
}

func newRouterScope() *routerScope {
	return &routerScope{
		prefix:  map[string]string{},
		guarded: map[string]bool{},
		known:   map[string]bool{},
	}
}

func buildRouteTable(p *contracts.Pass) *RouteTable {
	table := &RouteTable{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		// Route registration only happens in files that can reach the
		// router. Requiring the import keeps `.Get(` on a cache or a map
		// from being read as a route.
		if !importsAny(file, "core/router", "framework/routegroup", "framework", "core-ui/app", "net/http") {
			continue
		}
		scope := newRouterScope()
		collectRouterIdents(file, importAliases(file), scope)
		collectRegistrations(p, f, file, scope, table)
	}
	sort.Slice(table.Routes, func(i, j int) bool {
		a, b := table.Routes[i], table.Routes[j]
		if a.Pattern != b.Pattern {
			return a.Pattern < b.Pattern
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return table
}

// collectRouterIdents records which identifiers in a file hold a router
// or a route group, and what prefix and guarding each carries. Three
// shapes produce one:
//
//	r := router.New()          // constructed directly
//	r := app.Router()          // the app's router
//	g := app.Group("/admin")   // a prefixed, possibly guarded group
func collectRouterIdents(file *ast.File, aliases map[string]string, scope *routerScope) {
	// A router handed in as a parameter, `func wire(r *router.Router)`,
	// is how most apps split their route registration across files. Miss
	// it and those files report nothing at all, which reads as a clean
	// bill of health rather than as "not analysed".
	ast.Inspect(file, func(n ast.Node) bool {
		var params *ast.FieldList
		switch v := n.(type) {
		case *ast.FuncDecl:
			params = v.Type.Params
		case *ast.FuncLit:
			params = v.Type.Params
		default:
			return true
		}
		if params == nil {
			return true
		}
		for _, field := range params.List {
			if !isRouterType(field.Type) {
				continue
			}
			for _, name := range field.Names {
				if name.Name != "_" {
					scope.known[name.Name] = true
				}
			}
		}
		return true
	})

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		name, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || name.Name == "_" {
			return true
		}
		// A router constructed straight from the package. Without this,
		// a program that does not go through App registers routes the
		// analyzers cannot see, and reports zero findings, which reads
		// like a clean bill of health.
		for _, ctor := range []string{"New", "NewRouter"} {
			if _, isCtor := qualifiedCall(assign.Rhs[0], aliases, "core/router", ctor); isCtor {
				scope.known[name.Name] = true
				return true
			}
		}
		recv, method, call, ok := selectorCall(assign.Rhs[0])
		if !ok {
			return true
		}
		switch method {
		case "Router":
			scope.known[name.Name] = true
			scope.prefix[name.Name] = inheritedPrefix(scope, recv)
			scope.guarded[name.Name] = inheritedGuard(scope, recv)
		case "Group":
			if len(call.Args) == 0 {
				return true
			}
			prefix, litOK := stringLit(call.Args[0])
			if !litOK {
				// A computed prefix means every route under it is
				// unresolvable. Record the group so its registrations are
				// still recognised, but leave the prefix unknown.
				prefix = ""
			}
			scope.known[name.Name] = true
			scope.prefix[name.Name] = joinPath(inheritedPrefix(scope, recv), prefix)
			// A group is treated as guarded when it was handed anything
			// beyond its prefix: WithAccess and WithMiddleware are the
			// options that exist, and both are guards.
			scope.guarded[name.Name] = inheritedGuard(scope, recv) || len(call.Args) > 1
		}
		return true
	})
}

// routerTypeNames are the types a parameter can have that make it a
// routing surface. Matched on the type name alone. The import alias is
// already established by the file importing the package at all.
var routerTypeNames = map[string]bool{"Router": true, "RouteGroup": true}

func isRouterType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.StarExpr:
		return isRouterType(v.X)
	case *ast.SelectorExpr:
		return routerTypeNames[v.Sel.Name]
	case *ast.Ident:
		return routerTypeNames[v.Name]
	}
	return false
}

func inheritedPrefix(scope *routerScope, recv ast.Expr) string {
	if id, ok := recv.(*ast.Ident); ok {
		return scope.prefix[id.Name]
	}
	return ""
}

func inheritedGuard(scope *routerScope, recv ast.Expr) bool {
	if id, ok := recv.(*ast.Ident); ok {
		return scope.guarded[id.Name]
	}
	return false
}

// routerish reports whether a receiver expression denotes a router or
// group: a tracked identifier, or a direct `.Router()` / `.Group(...)`
// chain.
func routerish(scope *routerScope, recv ast.Expr) (name string, prefix string, guarded bool, ok bool) {
	switch v := recv.(type) {
	case *ast.Ident:
		if scope.known[v.Name] {
			return v.Name, scope.prefix[v.Name], scope.guarded[v.Name], true
		}
	case *ast.CallExpr:
		inner, method, call, isSel := selectorCall(v)
		if !isSel {
			return "", "", false, false
		}
		switch method {
		case "Router":
			return exprText(recv), inheritedPrefix(scope, inner), inheritedGuard(scope, inner), true
		case "Group":
			pre := ""
			if len(call.Args) > 0 {
				pre, _ = stringLit(call.Args[0])
			}
			return exprText(recv),
				joinPath(inheritedPrefix(scope, inner), pre),
				inheritedGuard(scope, inner) || len(call.Args) > 1, true
		}
	}
	return "", "", false, false
}

func collectRegistrations(p *contracts.Pass, src contracts.SourceFile, file *ast.File, scope *routerScope, table *RouteTable) {
	rel, pkg := src.Rel, src.Package
	ast.Inspect(file, func(n ast.Node) bool {
		recv, method, call, ok := selectorCall(n)
		if !ok {
			return true
		}

		// Entity declarations: app.Entity("name", cfg) and
		// app.GroupEntity(group, "name", cfg).
		switch method {
		case "Entity":
			if len(call.Args) >= 1 {
				if name, litOK := stringLit(call.Args[0]); litOK {
					pos := p.Position(call.Pos())
					table.Entities = append(table.Entities, EntityDecl{
						Name: name, File: rel, Line: pos.Line, Pos: call.Pos(),
					})
					table.Registered = true
				}
			}
			return true
		case "GroupEntity":
			if len(call.Args) >= 2 {
				if name, litOK := stringLit(call.Args[1]); litOK {
					pos := p.Position(call.Pos())
					group := exprText(call.Args[0])
					table.Entities = append(table.Entities, EntityDecl{
						Name: name, Group: group, File: rel, Line: pos.Line, Pos: call.Pos(),
					})
					table.Registered = true
				}
			}
			return true
		}

		// Screen registration: app.NewScreen("/path", component) is a GET
		// page route, and it is how most of a UI app's surface is declared.
		if method == "NewScreen" && len(call.Args) >= 1 {
			if pattern, litOK := stringLit(call.Args[0]); litOK && strings.HasPrefix(pattern, "/") {
				pos := p.Position(call.Pos())
				table.Routes = append(table.Routes, Route{
					Method: "GET", Pattern: pattern, RawPattern: pattern,
					Handler: exprText(call.Args[len(call.Args)-1]),
					File:    rel, Line: pos.Line, Col: pos.Column, Pos: call.Pos(),
					Package: pkg,
					Screen:  true,
					Guarded: true, // screens run through the host's policy chain
				})
				table.Registered = true
			}
			return true
		}

		name, prefix, guarded, isRouter := routerish(scope, recv)
		if !isRouter {
			return true
		}

		switch {
		case (method == "Handle" || method == "HandleFunc") && len(call.Args) >= 2:
			verb, verbOK := stringLit(call.Args[0])
			pattern, patOK := stringLit(call.Args[1])
			if !verbOK || !patOK {
				return true
			}
			appendRoute(p, table, rel, pkg, call, name, verb, pattern, prefix, guarded, handlerText(call, 2))
		case routeVerbs[method] != "" && len(call.Args) >= 1:
			pattern, patOK := stringLit(call.Args[0])
			if !patOK {
				return true
			}
			appendRoute(p, table, rel, pkg, call, name, routeVerbs[method], pattern, prefix, guarded, handlerText(call, 1))
		}
		return true
	})
}

func handlerText(call *ast.CallExpr, idx int) string {
	if idx < len(call.Args) {
		return exprText(call.Args[idx])
	}
	return ""
}

func appendRoute(p *contracts.Pass, table *RouteTable, rel, pkg string, call *ast.CallExpr, group, verb, pattern, prefix string, guarded bool, handler string) {
	pos := p.Position(call.Pos())
	table.Routes = append(table.Routes, Route{
		Method:     verb,
		Pattern:    joinPath(prefix, pattern),
		RawPattern: pattern,
		Group:      group,
		Guarded:    guarded,
		Handler:    handler,
		Package:    pkg,
		File:       rel,
		Line:       pos.Line,
		Col:        pos.Column,
		Pos:        call.Pos(),
	})
	table.Registered = true
}

// joinPath concatenates a group prefix with a pattern the way the router
// does: plain concatenation, then a squeeze of any doubled slash.
func joinPath(prefix, pattern string) string {
	joined := prefix + pattern
	for strings.Contains(joined, "//") {
		joined = strings.ReplaceAll(joined, "//", "/")
	}
	if joined == "" {
		return "/"
	}
	return joined
}
