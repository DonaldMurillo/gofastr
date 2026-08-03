package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "entities",
		Doc:  "Entity declarations: exposure surface and per-user scoping.",
		Rules: []string{
			contracts.RuleMCPWithoutCRUD,
			contracts.RulePublicEntity,
			contracts.RuleUnscopedPII,
		},
		Run: runEntities,
	})
	contracts.Register(&contracts.Analyzer{
		Name: "permissions",
		Doc:  "Access declarations on mutating routes, and whether configured auth is actually mounted.",
		Rules: []string{
			contracts.RuleUnguardedMutation,
			contracts.RuleAuthNotWired,
		},
		Run: runPermissions,
	})
}

// EntityInfo is what the static pass can recover from an entity
// registration. It is a subset of framework/entity.EntityConfig — only
// the fields that decide exposure and scoping — read straight off the
// composite literal.
type EntityInfo struct {
	Name   string
	Fields []string
	// FieldsResolved is false when Fields came from a helper call rather
	// than a literal slice. The scoping rules stay quiet in that case:
	// they cannot see the columns, and guessing would mean either false
	// findings or a false all-clear.
	FieldsResolved bool

	CRUDDisabled bool
	MCP          bool
	Public       bool
	HasAccess    bool
	OwnerField   string
	MultiTenant  bool
	// AccessPermissions are the permission strings the entity's Access
	// block names. These are the permissions least likely to be exercised
	// by accident: nothing in the app calls them by name, so only a real
	// request through the CRUD layer can prove them.
	AccessPermissions []string

	File string
	Line int
	Pos  token.Pos
}

// Scoped reports whether the entity limits which rows a caller can reach.
func (e EntityInfo) Scoped() bool {
	return e.OwnerField != "" || e.MultiTenant || e.HasAccess
}

// Exposed reports whether the entity has a generated surface at all.
// CRUD defaults to on, so only an explicit false turns it off.
func (e EntityInfo) Exposed() bool { return !e.CRUDDisabled || e.MCP }

// HookDecl is one statically discovered lifecycle-hook registration.
type HookDecl struct {
	// Entity is the entity name the hook is attached to.
	Entity string
	// Type is the lifecycle point, lower-cased to match the manifest
	// ("beforecreate", "afterupdate", …).
	Type string
	File string
	Line int
	Pos  token.Pos
}

// typedHookFuncs maps the framework's typed hook constructors onto the
// lifecycle point they register. These take the entity name as a string
// argument, which is what makes them statically readable:
//
//	framework.OnBeforeCreate[Post](app, "posts", stampSlug)
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
		// Same guard the event, role, entity, and auth collectors carry.
		// `OnBeforeCreate(db, "orders", fn)` is a perfectly ordinary
		// trigger helper, and its second argument is a table name — the
		// exact shape this reads a hook from. Without the import there is
		// nothing to distinguish the two.
		if !importsAny(file, "framework", "framework/hook") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			// The typed constructors are generic, so the callee is either
			// `framework.OnBeforeCreate[T]` (an IndexExpr around a
			// selector) or a bare selector when the type is inferred.
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

// subscribeMethods are the EventBus registration calls. Both take the
// event type as the first argument, which is what makes a subscription
// statically readable where the handler itself is not.
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
		// `On` and `Subscribe` are common method names. Requiring the file
		// to import the event package keeps a cache's Subscribe or a
		// signal store's On out of the results.
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
		// `Grant` is generic enough to appear on unrelated types; the
		// import keeps a grants ledger or an OAuth client out of this.
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
			// Every remaining argument should look like a permission —
			// that shape is what distinguishes RolePolicy.Grant from some
			// other two-argument Grant.
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
		// Guard on the import, the way collectAuthWiring, the event
		// subscriber walk, and the role-grant walk all do. Without it any
		// two-argument `x.Entity("name", SomeConfig{})` reads as a GoFastr
		// entity — a host app with its own registry type got phantom
		// findings from five rules at once, and a false positive in
		// someone else's code is how a linter loses its audience.
		if !ok || !importsAny(file, "framework", "framework/entity") {
			continue
		}
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
				// The config is bound to a variable or returned by a
				// helper. Nothing readable here — record nothing rather
				// than record an entity with every flag defaulted, which
				// would make an unscoped-looking finding out of thin air.
				return true
			}
			pos := p.Position(call.Pos())
			info := EntityInfo{Name: name, File: f.Rel, Line: pos.Line, Pos: call.Pos()}
			readEntityConfig(lit, &info)
			out = append(out, info)
			return true
		})
	}
	return out
}

// readEntityConfig walks an `EntityConfig{...}` literal. Only the keyed
// form is read: the struct has thirty-odd fields, so nobody writes it
// positionally, and a positional read would silently mis-assign.
func readEntityConfig(lit *ast.CompositeLit, info *EntityInfo) {
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
			readExposure(kv.Value, info)
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

func readExposure(e ast.Expr, info *EntityInfo) {
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
			// CRUD is a *bool: nil or true keeps routes, false removes
			// them. Only a literal false (however it is addressed) counts.
			info.CRUDDisabled = pointsToFalse(kv.Value)
		case "Access":
			info.AccessPermissions = accessPermissions(kv.Value)
			info.HasAccess = accessDeclared(kv.Value)
		}
	}
}

// accessDeclared reports whether an AccessControl literal gates at least
// one operation. A block of empty strings gates nothing and must not
// count as scoping — that is the difference between "reviewed and
// declared open" and "reviewed and forgot".
func accessDeclared(e ast.Expr) bool {
	lit := compositeOf(e)
	if lit == nil {
		// A non-literal Access value (a variable, a helper) is assumed to
		// gate something. Treating it as no-gate would report an entity
		// whose author did the work.
		return true
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if s, litOK := stringLit(kv.Value); !litOK || strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// accessPermissions lists the non-blank permission strings an
// AccessControl literal names.
func accessPermissions(e ast.Expr) []string {
	lit := compositeOf(e)
	if lit == nil {
		return nil
	}
	var out []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if s, litOK := stringLit(kv.Value); litOK && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
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

// pointsToFalse recognises the ways a *bool is set to false:
// `entity.Ptr(false)`, `&falseVar` where the literal is inline, or a
// helper call whose only argument is the false literal.
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

// readFieldNames pulls the Name of each schema.Field in a literal slice.
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
		// A relation column holds a foreign key, not the PII itself; the
		// entity it points at is judged on its own terms.
		if name != "" && !strings.EqualFold(fieldType, "relation") &&
			!strings.HasSuffix(fieldType, ".Relation") {
			names = append(names, name)
		}
	}
	return names, true
}

// ----------------------------------------------------------------------
// PII heuristic
// ----------------------------------------------------------------------

// piiTokens are field-name tokens suggesting personally identifiable or
// secret data. Matching is per-token — split on separators and camelCase
// boundaries — so "cardinality" does not trip "card".
var piiTokens = map[string]bool{
	"email": true, "phone": true, "mobile": true, "address": true,
	"street": true, "ssn": true, "password": true, "passwd": true,
	"token": true, "secret": true, "card": true, "iban": true,
	"dob": true, "birthday": true, "birthdate": true, "passport": true,
	"salary": true,
}

func fieldLooksPII(name string) bool {
	for _, tok := range splitIdentifier(name) {
		if piiTokens[tok] {
			return true
		}
	}
	return false
}

// splitIdentifier breaks a column name into lowercase tokens on
// separators, digits, and camelCase boundaries.
func splitIdentifier(s string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r):
			// Split before an uppercase run that starts a new word, but
			// keep acronyms together (SSNValue → ssn, value).
			if i > 0 && (unicode.IsLower(runes[i-1]) ||
				(i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				flush()
			}
			cur.WriteRune(unicode.ToLower(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// ----------------------------------------------------------------------
// Analyzers
// ----------------------------------------------------------------------

func runEntities(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, e := range Entities(p) {
		if e.MCP && e.CRUDDisabled {
			d := diag(p, contracts.RuleMCPWithoutCRUD, e.File, e.Pos,
				fmt.Sprintf("entity %q sets MCP with CRUD disabled — its tools dispatch to routes that do not exist", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		if e.Public {
			d := diag(p, contracts.RulePublicEntity, e.File, e.Pos, fmt.Sprintf(
				"entity %q is Public — anonymous callers can create, update, and delete rows, not only read them", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		if !e.Exposed() || e.Scoped() || e.Public || !e.FieldsResolved {
			continue
		}
		var pii []string
		for _, f := range e.Fields {
			if fieldLooksPII(f) {
				pii = append(pii, f)
			}
		}
		if len(pii) == 0 {
			continue
		}
		d := diag(p, contracts.RuleUnscopedPII, e.File, e.Pos, fmt.Sprintf(
			"entity %q exposes %s through auto-CRUD with no owner field, tenant, or access rule — every signed-in user can read and write every other user's row",
			e.Name, strings.Join(pii, ", ")))
		d.Evidence = map[string]string{"entity": e.Name, "fields": strings.Join(pii, ",")}
		out = append(out, d)
	}
	return out, nil
}

func runPermissions(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	// The auth-wiring check runs first and unconditionally: it is about
	// whether a configured credential is ever read, which has nothing to
	// do with whether this pass could discover any routes. Gating it on
	// the route table meant it never fired for the shape it exists to
	// catch — an app whose routes are wired somewhere the static pass
	// cannot see.
	out, err := runAuthWiring(p)
	if err != nil {
		return out, err
	}

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

// ----------------------------------------------------------------------
// GOFASTR1903 — auth configured but never mounted.
// ----------------------------------------------------------------------

// authWiring is what the module says about authentication, attributed by
// package. One module-global Mounted flag let the first binary that
// wired auth correctly silence the rule for every other binary in the
// module — a mount can only cover a configure the compiler could link it
// with.
type authWiring struct {
	// Configured is one `auth.New(...)` site per package that calls it.
	Configured []authConfigureSite
	// Mounted is the set of packages installing a reader for the
	// credential: SessionMiddleware (cookies), RequireAuth (bearer), or
	// BFF (which mounts the session middleware itself).
	Mounted map[string]bool
	// Imports is the module-internal import graph over app packages —
	// including packages that have nothing to do with auth, because
	// coverage has to flow through them.
	Imports map[string]map[string]bool
}

// authConfigureSite is where a package builds its auth manager.
type authConfigureSite struct {
	Package string
	File    string
	Line    int
	Pos     token.Pos
}

// authReaders are the calls that put a user on the request context. Any
// one of them means the manager is actually consulted.
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
		// Without a go.mod the packages cannot be told apart — files get
		// relative-directory names while the import matcher can accept
		// nothing, which would leave a graph with nodes and no edges
		// where a mount can never cover a configure. Collapse every file
		// to one node instead: module-global attribution, the pre-graph
		// behaviour, which is the honest fallback when linkage is
		// unknowable.
		pkg := f.Package
		if p.ModulePath == "" {
			pkg = ""
		}
		// Every app file feeds the import graph, auth-related or not.
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
		// A dot import erases the selector: the mount is a bare
		// `RequireAuth`, invisible to the selector walk below. The reader
		// names are specific enough to trust as bare identifiers; `New`
		// is not, so a dot-imported configure stays uncollected — a
		// missed configure can only under-report, never false-positive.
		// Only USES count: an app is free to declare its own method or
		// field named RequireAuth (method names do not collide with
		// file-block dot imports), and a selector's `x.RequireAuth` names
		// something on x, never the dot-imported package function.
		if dotImportsAuth(file) {
			// Exclusion is by NAME, not by declaration site: a use of a
			// shadowed name is a different ident node from its
			// declaration, so a pointer-keyed exclusion let the app's
			// own `Opts{RequireAuth: true}` field key count as a mount.
			// If the file declares the name anywhere, none of its bare
			// occurrences can be trusted to mean the auth package — the
			// residual under-report (a file that both shadows AND
			// genuinely mounts) is the direction this walk accepts.
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
					// A composite literal's field key is not a use of
					// the dot-imported name (map-literal keys can be,
					// and under-reporting those is accepted).
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
		// Which selectors are the callee of a call expression.
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
			// Matched on the SELECTOR, not on a call: middleware is
			// routinely passed as a value rather than invoked here —
			// `app.Group("/x", auth.RequireAuth)` mounts it just as
			// surely as `Use(auth.SessionMiddleware(mgr))` does.
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
			// `auth.New(cfg)` is the manager constructor. Other New*
			// helpers in the package build stores and plugins, not the
			// manager, so the name is matched exactly — and only when it
			// is genuinely invoked, since a bare reference to the
			// constructor configures nothing. One site per package:
			// several auth.New calls in one package are one wiring
			// decision, not several findings.
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

// dotImportsAuth reports whether the file dot-imports the auth battery,
// putting its names directly into file scope.
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

// modulePackage reports whether an import path is inside the analysed
// module, returning it unchanged as the graph node when it is.
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

// authCovered reports whether some compilation unit could contain both
// this configure site and a mount. Walk UP to everything that
// transitively imports the configuring package — the binaries that link
// it — then DOWN through everything those importers link, and look for a
// mount anywhere in that closure. Package identity alone is too narrow
// (main configures, an imported routes package mounts); the whole module
// is too wide (appA's mount says nothing about appB, which never links
// it).
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

// reverseGraph flips edge direction: imports become importers.
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

// closure is start plus everything transitively reachable from it.
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
