package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/piicheck"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "entities",
		Doc:  "Entity declarations: exposure surface and per-user scoping.",
		Rules: []string{
			contracts.RuleMCPWithoutCRUD,
			contracts.RulePublicEntity,
			contracts.RuleCrudWithoutAuth,
			contracts.RuleUnscopedPII,
		},
		Run: runEntities,
	})
	contracts.Register(&contracts.Analyzer{
		Name: "permissions",
		Doc:  "Access declarations on mutating routes, checked permission names against the registry, and whether configured auth is actually mounted.",
		Rules: []string{
			contracts.RuleUnguardedMutation,
			contracts.RuleAuthNotWired,
			contracts.RuleUnregisteredPermission,
		},
		Run: runPermissions,
	})
}

// EntityInfo is what the static pass can recover from an entity
// registration. It is a subset of framework/entity.EntityConfig, only
// the fields that decide exposure and scoping, read straight off the
// composite literal.
type EntityInfo struct {
	Name   string
	Fields []string
	// FieldsResolved is false when Fields is not a literal slice.
	FieldsResolved bool

	CRUDDisabled bool
	MCP          bool
	Public       bool
	HasAccess    bool
	readGated    bool
	OwnerField   string
	MultiTenant  bool
	// AccessPermissions are the non-blank permission strings the Access block names.
	AccessPermissions []string

	File string
	Line int
	Pos  token.Pos
}

// Scoped reports whether the entity limits which rows a caller can reach.
func (e EntityInfo) Scoped() bool {
	return piicheck.Scoped(e.OwnerField, e.MultiTenant, e.HasAccess)
}

// Exposed reports whether the entity has a generated surface at all.
// CRUD defaults to on, so only an explicit false turns it off.
func (e EntityInfo) Exposed() bool { return !e.CRUDDisabled || e.MCP }

// HookDecl is one statically discovered lifecycle-hook registration.
type HookDecl struct {
	// Entity is the entity name the hook is attached to.
	Entity string
	// Type is the lifecycle point, lower-cased as the manifest spells it ("beforecreate").
	Type string
	File string
	Line int
	Pos  token.Pos
}

var typedHookFuncs = map[string]string{
	"OnBeforeCreate": "beforecreate", "OnAfterCreate": "aftercreate",
	"OnBeforeUpdate": "beforeupdate", "OnAfterUpdate": "afterupdate",
	"OnBeforeDelete": "beforedelete", "OnAfterDelete": "afterdelete",
	"OnBeforeList": "beforelist", "OnAfterList": "afterlist",
	"OnBeforeGet": "beforeget", "OnAfterGet": "afterget",
}

const hookKey = "analyzers.hooks"

// Hooks returns every statically readable lifecycle-hook registration.
func Hooks(p *contracts.Pass) []HookDecl {
	return p.Memo(hookKey, func() any { return collectHooks(p) }).([]HookDecl)
}

func collectHooks(p *contracts.Pass) []HookDecl {
	var out []HookDecl
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		if !importsAny(file, "framework", "framework/hook") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			fn := call.Fun
			if idx, isIndex := fn.(*ast.IndexExpr); isIndex {
				fn = idx.X
			}
			if idx, isIndexList := fn.(*ast.IndexListExpr); isIndexList {
				fn = idx.X
			}
			var name string
			switch v := fn.(type) {
			case *ast.SelectorExpr:
				name = v.Sel.Name
			case *ast.Ident:
				name = v.Name
			default:
				return true
			}
			hookType, known := typedHookFuncs[name]
			if !known || len(call.Args) < 2 {
				return true
			}
			entity, litOK := stringLit(call.Args[1])
			if !litOK || entity == "" {
				return true
			}
			pos := p.Position(call.Pos())
			out = append(out, HookDecl{
				Entity: entity, Type: hookType,
				File: f.Rel, Line: pos.Line, Pos: call.Pos(),
			})
			return true
		})
	}
	return out
}

// EventSub is one statically discovered event subscription.
type EventSub struct {
	// Type is the event type string the handler listens for.
	Type string
	File string
	Line int
	Pos  token.Pos
}

var subscribeMethods = map[string]bool{"On": true, "Subscribe": true}

const eventSubKey = "analyzers.eventsubs"

// EventSubs returns every statically readable event subscription.
func EventSubs(p *contracts.Pass) []EventSub {
	return p.Memo(eventSubKey, func() any { return collectEventSubs(p) }).([]EventSub)
}

func collectEventSubs(p *contracts.Pass) []EventSub {
	var out []EventSub
	seen := map[string]bool{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		if !importsAny(file, "framework/event") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			_, method, call, isCall := selectorCall(n)
			if !isCall || !subscribeMethods[method] || len(call.Args) != 2 {
				return true
			}
			eventType, litOK := stringLit(call.Args[0])
			if !litOK || eventType == "" || seen[eventType] {
				return true
			}
			seen[eventType] = true
			pos := p.Position(call.Pos())
			out = append(out, EventSub{
				Type: eventType, File: f.Rel, Line: pos.Line, Pos: call.Pos(),
			})
			return true
		})
	}
	return out
}

// RoleGrant is one statically discovered role definition.
type RoleGrant struct {
	// Role is the role name granted permissions.
	Role string
	File string
	Line int
	Pos  token.Pos
}

const roleKey = "analyzers.roles"

// Roles returns every role named in a `policy.Grant("role", …)` call.
func Roles(p *contracts.Pass) []RoleGrant {
	return p.Memo(roleKey, func() any { return collectRoles(p) }).([]RoleGrant)
}

func collectRoles(p *contracts.Pass) []RoleGrant {
	var out []RoleGrant
	seen := map[string]bool{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		if !importsAny(file, "framework/access", "framework") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			_, method, call, isCall := selectorCall(n)
			if !isCall || method != "Grant" || len(call.Args) < 2 {
				return true
			}
			role, litOK := stringLit(call.Args[0])
			if !litOK || role == "" || seen[role] {
				return true
			}
			permissionish := false
			for _, arg := range call.Args[1:] {
				if s, ok := stringLit(arg); ok && strings.Contains(s, ":") {
					permissionish = true
					break
				}
			}
			if !permissionish {
				return true
			}
			seen[role] = true
			pos := p.Position(call.Pos())
			out = append(out, RoleGrant{
				Role: role, File: f.Rel, Line: pos.Line, Pos: call.Pos(),
			})
			return true
		})
	}
	return out
}

const entityKey = "analyzers.entities"

// Entities returns every statically readable entity registration.
func Entities(p *contracts.Pass) []EntityInfo {
	return p.Memo(entityKey, func() any { return collectEntities(p) }).([]EntityInfo)
}

func collectEntities(p *contracts.Pass) []EntityInfo {
	var out []EntityInfo
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok || !importsAny(file, "framework", "framework/entity") {
			continue
		}
		aliases := importAliases(file)
		ast.Inspect(file, func(n ast.Node) bool {
			_, method, call, isCall := selectorCall(n)
			if !isCall {
				return true
			}
			var nameArg, configArg ast.Expr
			switch method {
			case "Entity":
				if len(call.Args) != 2 {
					return true
				}
				nameArg, configArg = call.Args[0], call.Args[1]
			case "GroupEntity":
				if len(call.Args) != 3 {
					return true
				}
				nameArg, configArg = call.Args[1], call.Args[2]
			default:
				return true
			}
			name, litOK := stringLit(nameArg)
			if !litOK {
				return true
			}
			lit, isLit := configArg.(*ast.CompositeLit)
			if !isLit {
				return true
			}
			pos := p.Position(call.Pos())
			info := EntityInfo{Name: name, File: f.Rel, Line: pos.Line, Pos: call.Pos()}
			readEntityConfig(lit, &info, aliases)
			out = append(out, info)
			return true
		})
	}
	return out
}

func readEntityConfig(lit *ast.CompositeLit, info *EntityInfo, aliases map[string]string) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Fields":
			info.Fields, info.FieldsResolved = readFieldNames(kv.Value)
		case "Scope":
			readScope(kv.Value, info)
		case "Exposure":
			readExposure(kv.Value, info, aliases)
		}
	}
}

func readScope(e ast.Expr, info *EntityInfo) {
	lit := compositeOf(e)
	if lit == nil {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "OwnerField":
			if s, litOK := stringLit(kv.Value); litOK {
				info.OwnerField = s
			}
		case "MultiTenant":
			info.MultiTenant = isTrue(kv.Value)
		}
	}
}

func readExposure(e ast.Expr, info *EntityInfo, aliases map[string]string) {
	lit := compositeOf(e)
	if lit == nil {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "MCP":
			info.MCP = isTrue(kv.Value)
		case "Public":
			info.Public = isTrue(kv.Value)
		case "CRUD":
			info.CRUDDisabled = pointsToFalse(kv.Value)
		case "Access":
			info.AccessPermissions, info.HasAccess, info.readGated = readAccess(kv.Value, aliases)
		}
	}
}

func readAccess(e ast.Expr, aliases map[string]string) (permissions []string, gated, readGated bool) {
	lit := compositeOf(e)
	if lit == nil {
		return nil, true, true
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		value := unparen(kv.Value)
		gatesOperation := !isAnyoneConstant(value, aliases)
		if s, isString := stringLit(value); isString {
			if strings.TrimSpace(s) != "" {
				permissions = append(permissions, s)
			}
			gatesOperation = piicheck.GatesAccess(s)
		}
		if gatesOperation {
			gated = true
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Read" {
				readGated = true
			}
		}
	}
	return permissions, gated, readGated
}

func isAnyoneConstant(e ast.Expr, aliases map[string]string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Anyone" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && (aliases[pkg.Name] == frameworkPkg || aliases[pkg.Name] == frameworkPkg+"/entity")
}

func compositeOf(e ast.Expr) *ast.CompositeLit {
	switch v := e.(type) {
	case *ast.CompositeLit:
		return v
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return compositeOf(v.X)
		}
	case *ast.ParenExpr:
		return compositeOf(v.X)
	}
	return nil
}

func isTrue(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "true"
}

func pointsToFalse(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return pointsToFalse(v.X)
		}
	case *ast.Ident:
		return v.Name == "false"
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			return pointsToFalse(v.Args[0])
		}
	case *ast.ParenExpr:
		return pointsToFalse(v.X)
	}
	return false
}

func readFieldNames(e ast.Expr) ([]string, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	var names []string
	for _, elt := range lit.Elts {
		fl := compositeOf(elt)
		if fl == nil {
			continue
		}
		var name, fieldType string
		for _, fe := range fl.Elts {
			kv, isKV := fe.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}
			key, isIdent := kv.Key.(*ast.Ident)
			if !isIdent {
				continue
			}
			switch key.Name {
			case "Name":
				name, _ = stringLit(kv.Value)
			case "Type":
				fieldType = exprText(kv.Value)
			}
		}
		if name != "" && !strings.EqualFold(fieldType, "relation") &&
			!strings.HasSuffix(fieldType, ".Relation") {
			names = append(names, name)
		}
	}
	return names, true
}

func runEntities(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	auth := AuthWiring(p)
	noAuth := len(auth.Configured) == 0 && len(auth.Mounted) == 0
	for _, e := range Entities(p) {
		if e.MCP && e.CRUDDisabled {
			d := diag(p, contracts.RuleMCPWithoutCRUD, e.File, e.Pos,
				fmt.Sprintf("entity %q sets MCP with CRUD disabled: its tools dispatch to routes that do not exist", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		if e.Public {
			d := diag(p, contracts.RulePublicEntity, e.File, e.Pos, fmt.Sprintf(
				"entity %q is Public: anonymous callers can create, update, and delete rows, not only read them", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		if noAuth && !e.CRUDDisabled && !e.Public {
			d := diag(p, contracts.RuleCrudWithoutAuth, e.File, e.Pos, fmt.Sprintf(
				"entity %q mounts auto-CRUD routes but the app wires no auth: every operation 401s for every caller; wire battery/auth or mark the entity Public", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		readScoped := piicheck.Scoped(e.OwnerField, e.MultiTenant, e.readGated)
		if !e.Exposed() || readScoped || e.Public || !e.FieldsResolved {
			continue
		}
		var pii []string
		for _, f := range e.Fields {
			if piicheck.Field(f) {
				pii = append(pii, f)
			}
		}
		if len(pii) == 0 {
			continue
		}
		d := diag(p, contracts.RuleUnscopedPII, e.File, e.Pos, fmt.Sprintf(
			"entity %q exposes %s through auto-CRUD/MCP with no owner field, tenant scope, or read permission",
			e.Name, strings.Join(pii, ", ")))
		d.Evidence = map[string]string{"entity": e.Name, "fields": strings.Join(pii, ",")}
		out = append(out, d)
	}
	return out, nil
}

func runPermissions(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	out, err := runAuthWiring(p)
	if err != nil {
		return out, err
	}
	out = append(out, runUnregisteredPermissions(p)...)

	table := Routes(p)
	if !table.Registered {
		return out, nil
	}
	for _, r := range table.Routes {
		if !mutatingMethods[strings.ToUpper(r.Method)] || r.Guarded {
			continue
		}
		d := diag(p, contracts.RuleUnguardedMutation, r.File, r.Pos, fmt.Sprintf(
			"%s %s is registered outside any group carrying access or middleware", r.Method, r.Pattern))
		d.Evidence = map[string]string{
			"method": r.Method, "pattern": r.Pattern, "handler": r.Handler,
		}
		out = append(out, d)
	}
	return out, nil
}

type authWiring struct {
	// Configured is one `auth.New(...)` site per package that calls it.
	Configured []authConfigureSite
	// Mounted is the set of packages installing SessionMiddleware, RequireAuth or BFF.
	Mounted map[string]bool
	// Imports is the module-internal import graph over every app package.
	Imports map[string]map[string]bool
}

type authConfigureSite struct {
	Package string
	File    string
	Line    int
	Pos     token.Pos
}

var authReaders = map[string]bool{
	"SessionMiddleware": true,
	"RequireAuth":       true,
	"BFF":               true,
}

const authWiringKey = "analyzers.authwiring"

// AuthWiring reports whether the module configures auth and whether
// anything reads it.
func AuthWiring(p *contracts.Pass) authWiring {
	return p.Memo(authWiringKey, func() any { return collectAuthWiring(p) }).(authWiring)
}

func collectAuthWiring(p *contracts.Pass) authWiring {
	out := authWiring{
		Mounted: map[string]bool{},
		Imports: map[string]map[string]bool{},
	}
	configured := map[string]bool{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		pkg := f.Package
		if p.ModulePath == "" {
			pkg = ""
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if target, internal := modulePackage(p.ModulePath, path); internal {
				if out.Imports[pkg] == nil {
					out.Imports[pkg] = map[string]bool{}
				}
				out.Imports[pkg][target] = true
			}
		}
		if !importsAny(file, "battery/auth") {
			continue
		}
		if dotImportsAuth(file) {
			shadowed := map[string]bool{}
			skip := map[*ast.Ident]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.FuncDecl:
					shadowed[v.Name.Name] = true
				case *ast.TypeSpec:
					shadowed[v.Name.Name] = true
				case *ast.Field:
					for _, id := range v.Names {
						shadowed[id.Name] = true
					}
				case *ast.ValueSpec:
					for _, id := range v.Names {
						shadowed[id.Name] = true
					}
				case *ast.AssignStmt:
					if v.Tok == token.DEFINE {
						for _, lhs := range v.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								shadowed[id.Name] = true
							}
						}
					}
				case *ast.KeyValueExpr:
					if id, ok := v.Key.(*ast.Ident); ok {
						skip[id] = true
					}
				case *ast.SelectorExpr:
					skip[v.Sel] = true
				}
				return true
			})
			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && authReaders[id.Name] && !skip[id] && !shadowed[id.Name] {
					out.Mounted[pkg] = true
				}
				return true
			})
		}
		aliases := importAliases(file)
		calledSelectors := map[*ast.SelectorExpr]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					calledSelectors[sel] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			sel, isSel := n.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			ident, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			path, known := aliases[ident.Name]
			if !known || !strings.HasSuffix(path, "battery/auth") {
				return true
			}
			method := sel.Sel.Name
			if authReaders[method] {
				out.Mounted[pkg] = true
				return true
			}
			if method == "New" && !configured[pkg] && calledSelectors[sel] {
				pos := p.Position(sel.Pos())
				configured[pkg] = true
				out.Configured = append(out.Configured, authConfigureSite{
					Package: pkg, File: f.Rel, Line: pos.Line, Pos: sel.Pos(),
				})
			}
			return true
		})
	}
	return out
}

func dotImportsAuth(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Name == nil || imp.Name.Name != "." {
			continue
		}
		if p, err := strconv.Unquote(imp.Path.Value); err == nil &&
			(p == "battery/auth" || strings.HasSuffix(p, "/battery/auth")) {
			return true
		}
	}
	return false
}

func modulePackage(module, imp string) (string, bool) {
	if module == "" {
		return "", false
	}
	if imp == module || strings.HasPrefix(imp, module+"/") {
		return imp, true
	}
	return "", false
}

func runAuthWiring(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	w := AuthWiring(p)
	if len(w.Configured) == 0 {
		return nil, nil
	}
	importers := reverseGraph(w.Imports)
	var out []contracts.Diagnostic
	for _, site := range w.Configured {
		if authCovered(site.Package, w, importers) {
			continue
		}
		d := diag(p, contracts.RuleAuthNotWired, site.File, site.Pos,
			"auth.New builds a manager here, but nothing linked with this package installs a middleware that reads the credential")
		d.Evidence = map[string]string{"configured": fmt.Sprintf("%s:%d", site.File, site.Line)}
		out = append(out, d)
	}
	return out, nil
}

func authCovered(pkg string, w authWiring, importers map[string]map[string]bool) bool {
	for linker := range closure(pkg, importers) {
		for linked := range closure(linker, w.Imports) {
			if w.Mounted[linked] {
				return true
			}
		}
	}
	return false
}

func reverseGraph(edges map[string]map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for from, tos := range edges {
		for to := range tos {
			if out[to] == nil {
				out[to] = map[string]bool{}
			}
			out[to][from] = true
		}
	}
	return out
}

func closure(start string, edges map[string]map[string]bool) map[string]bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		q := queue[0]
		queue = queue[1:]
		for next := range edges[q] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}
