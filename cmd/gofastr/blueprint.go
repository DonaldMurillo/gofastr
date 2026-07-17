package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

type Blueprint struct {
	App        BlueprintApp
	Entities   []framework.EntityDeclaration
	Screens    []BlueprintScreen
	Nav        []BlueprintNavItem
	Seed       []BlueprintSeedEntity
	Endpoints  []BlueprintEndpoint
	Middleware []BlueprintNamedStub
	Plugins    []BlueprintNamedStub
	Helpers    []BlueprintNamedStub
}

// BlueprintSeedEntity holds seed data for one entity.
type BlueprintSeedEntity struct {
	Entity string
	Rows   []map[string]any
	// Count, when > 0, auto-generates that many demo rows for the entity
	// (in addition to any explicit Rows) with deterministic, realistic-looking
	// values. Enum columns get a varied (non-uniform) distribution.
	Count int
	// Weights maps an enum column to a value→weight distribution used when
	// auto-generating rows (e.g. {"status": {"open": 5, "closed": 1}}). A
	// column with no weights entry gets a deterministic skew seeded from the
	// entity name so demos never read as a flat round-robin.
	Weights map[string]map[string]int
}

// BlueprintNavItem describes a navigation entry — a link to a screen or URL.
type BlueprintNavItem struct {
	Label string
	Href  string
	Icon  string
	Role  string // optional: only shown to users holding this role
	Items []BlueprintNavItem
}
type BlueprintApp struct {
	Name      string
	Module    string
	DBDriver  string
	DBURL     string
	StaticDir string
	OutputDir string
	APIPrefix string
	Theme     map[string]string
	ThemeDark map[string]string // optional dark-scheme color overrides (app.theme.dark)
	Auth      BlueprintAuth
	Admin     BlueprintAdmin
	PWA       BlueprintPWA
	// LLMMD emits uihost.WithPublicLLMMD() so every registered screen
	// serves its /llm.md document (plus the /llm-pages.md index).
	// Independent of PWA — a screen inventory is schema disclosure, so
	// it never rides along with another feature.
	LLMMD bool
}

// BlueprintPWA configures the generated app's installable-PWA surface
// (app.pwa). Enabled emits uihost.WithPWA(...) plus replaceable 192px,
// 512px, and maskable icons under <static>/icons/. Every other field is
// optional; the framework derives the defaults (name from the app
// title, start_url/scope "/", standalone display).
type BlueprintPWA struct {
	Enabled         bool
	Name            string
	ShortName       string
	Description     string
	StartURL        string
	Scope           string
	Display         string // standalone | fullscreen | minimal-ui | browser
	ThemeColor      string
	BackgroundColor string
}

// BlueprintAdmin configures the auto-generated admin back-office (battery/admin).
type BlueprintAdmin struct {
	Enabled      bool
	Path         string // URL prefix, default "/admin"
	Role         string // required role, default "admin"
	LoginPath    string // where to bounce unauthenticated visitors, e.g. "/login"
	SeedEmail    string // bootstrap admin account email
	SeedPassword string // bootstrap admin account password
}

// BlueprintAuth configures the built-in authentication system.
type BlueprintAuth struct {
	Enabled   bool
	DevMode   bool
	BasePath  string // defaults to "/auth"
	JWTSecret string
}

type BlueprintScreen struct {
	Name        string
	Route       string
	Title       string
	Description string
	Type        string
	Layout      string // "marketing" | "app" | "" (default)
	Access      BlueprintAccess
	Body        []BlueprintBlock
}

// BlueprintAccess gates a screen: require a logged-in user and/or a role.
type BlueprintAccess struct {
	Auth bool
	Role string
}

type BlueprintBlock struct {
	Type        string
	Kind        string
	Text        string
	Level       int
	Class       string
	Href        string
	Entity      string
	Fields      []string
	Limit       int
	EmptyText   string
	Mode        string   // "create", "edit" for entity_form
	Search      string   // entity_list LIKE-search field
	Filters     []string // entity_list: facet-filter columns (enum, bool, or relation)
	Create      bool     // entity_list: show "New" + mount a create form screen
	Props       map[string]any
	Children    []BlueprintBlock
	Actions     []BlueprintAction
	Transitions []BlueprintTransition // entity_detail: status-transition workflow buttons
	Island      string
	Widget      string
}

// BlueprintTransition is a status-change workflow action shown on a detail page:
// a button that sets the entity's status field to Status (e.g. "Mark paid"),
// optionally stamping a date field (Stamp, e.g. paid_on) with today.
type BlueprintTransition struct {
	Label   string
	Status  string
	Variant string
	Stamp   string // optional date field stamped with today on transition
}

type BlueprintAction struct {
	Name     string
	Event    string
	ClientJS string
}

type BlueprintEndpoint struct {
	Name        string
	Method      string
	Path        string
	Entity      string
	Handler     string
	Description string
	MCP         bool
}

type BlueprintNamedStub struct {
	Name        string
	Description string
}

func loadBlueprint(path string) (Blueprint, error) {
	return loadBlueprintPath(path, true)
}

func loadBlueprintPath(path string, validate bool) (Blueprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Blueprint{}, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return Blueprint{}, err
		}
		var merged Blueprint
		foundBlueprint := false
		for _, entry := range entries {
			if entry.IsDir() || !isBlueprintFile(entry.Name()) {
				continue
			}
			foundBlueprint = true
			next, err := loadBlueprintPath(filepath.Join(path, entry.Name()), false)
			if err != nil {
				return Blueprint{}, err
			}
			merged = mergeBlueprints(merged, next)
		}
		if !foundBlueprint {
			return Blueprint{}, fmt.Errorf("blueprint directory %q does not contain any blueprint files", path)
		}
		if validate {
			if err := validateBlueprint(merged); err != nil {
				return Blueprint{}, err
			}
		}
		return merged, nil
	}
	bp, err := decodeBlueprintFile(path)
	if err != nil {
		return Blueprint{}, err
	}
	if validate {
		if err := validateBlueprint(bp); err != nil {
			return Blueprint{}, err
		}
	}
	return bp, nil
}

func decodeBlueprintFile(path string) (Blueprint, error) {
	if !isBlueprintFile(path) {
		return Blueprint{}, fmt.Errorf("blueprint %q must be .yml, .yaml, or .json", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Blueprint{}, err
	}
	var node *coreyaml.Node
	if strings.EqualFold(filepath.Ext(path), ".json") {
		var raw any
		if err := json.Unmarshal(data, &raw); err != nil {
			return Blueprint{}, fmt.Errorf("parse %s: %w", path, err)
		}
		node = yamlNodeFromJSON(raw)
	} else {
		node, err = coreyaml.Parse(string(data))
		if err != nil {
			return Blueprint{}, err
		}
	}
	bp, err := decodeBlueprint(node)
	if err != nil {
		return Blueprint{}, err
	}
	return bp, nil
}

func yamlNodeFromJSON(value any) *coreyaml.Node {
	switch v := value.(type) {
	case map[string]any:
		out := &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}, Line: 1, Column: 1}
		for key, child := range v {
			out.Map[key] = yamlNodeFromJSON(child)
		}
		return out
	case []any:
		out := &coreyaml.Node{Kind: coreyaml.List, Line: 1, Column: 1}
		for _, child := range v {
			out.List = append(out.List, yamlNodeFromJSON(child))
		}
		return out
	default:
		return &coreyaml.Node{Kind: coreyaml.Scalar, Value: v, Line: 1, Column: 1}
	}
}

func isBlueprintFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml" || ext == ".json"
}

func mergeBlueprints(a, b Blueprint) Blueprint {
	if b.App.Name != "" || b.App.Module != "" || b.App.DBDriver != "" || b.App.DBURL != "" || b.App.StaticDir != "" || b.App.OutputDir != "" || len(b.App.Theme) > 0 {
		a.App = b.App
	}
	a.Entities = append(a.Entities, b.Entities...)
	a.Screens = append(a.Screens, b.Screens...)
	a.Nav = append(a.Nav, b.Nav...)
	a.Endpoints = append(a.Endpoints, b.Endpoints...)
	a.Middleware = append(a.Middleware, b.Middleware...)
	a.Plugins = append(a.Plugins, b.Plugins...)
	a.Helpers = append(a.Helpers, b.Helpers...)
	return a
}

func decodeBlueprint(node *coreyaml.Node) (Blueprint, error) {
	m, err := expectMap(node, "blueprint")
	if err != nil {
		return Blueprint{}, err
	}
	allowed := map[string]bool{"app": true, "entities": true, "screens": true, "nav": true, "seed": true, "endpoints": true, "middleware": true, "plugins": true, "helpers": true, "isolation": true}
	if err := rejectUnknownKeys(m, allowed, "blueprint"); err != nil {
		return Blueprint{}, err
	}
	var bp Blueprint
	if child := m["app"]; child != nil {
		app, err := decodeBlueprintApp(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.App = app
	}
	if child := m["entities"]; child != nil {
		entities, endpoints, err := decodeBlueprintEntities(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.Entities = entities
		bp.Endpoints = append(bp.Endpoints, endpoints...)
	}
	if child := m["screens"]; child != nil {
		screens, err := decodeBlueprintScreens(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.Screens = screens
	}
	if child := m["nav"]; child != nil {
		nav, err := decodeBlueprintNav(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.Nav = nav
	}
	if child := m["seed"]; child != nil {
		seed, err := decodeBlueprintSeed(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.Seed = seed
	}
	if child := m["endpoints"]; child != nil {
		endpoints, err := decodeBlueprintEndpoints(child)
		if err != nil {
			return Blueprint{}, err
		}
		bp.Endpoints = append(bp.Endpoints, endpoints...)
	}
	if child := m["middleware"]; child != nil {
		stubs, err := decodeNamedStubs(child, "middleware")
		if err != nil {
			return Blueprint{}, err
		}
		bp.Middleware = stubs
	}
	if child := m["plugins"]; child != nil {
		stubs, err := decodeNamedStubs(child, "plugins")
		if err != nil {
			return Blueprint{}, err
		}
		bp.Plugins = stubs
	}
	if child := m["helpers"]; child != nil {
		stubs, err := decodeNamedStubs(child, "helpers")
		if err != nil {
			return Blueprint{}, err
		}
		bp.Helpers = stubs
	}
	return bp, nil
}

func decodeBlueprintApp(node *coreyaml.Node) (BlueprintApp, error) {
	m, err := expectMap(node, "app")
	if err != nil {
		return BlueprintApp{}, err
	}
	allowed := map[string]bool{"name": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "api_prefix": true, "theme": true, "auth": true, "admin": true, "pwa": true, "llm_md": true}
	if err := rejectUnknownKeys(m, allowed, "app"); err != nil {
		return BlueprintApp{}, err
	}
	app := BlueprintApp{
		Name:      stringValue(m["name"]),
		Module:    stringValue(m["module"]),
		StaticDir: stringValue(m["static_dir"]),
		OutputDir: stringValue(m["output_dir"]),
		// Mount entity JSON CRUD under /api by default so bare /{entity}
		// routes are free for HTML screens. Set api_prefix: "" to opt out
		// (entity APIs mount at bare /{table}, the historical behavior).
		APIPrefix: "api",
	}
	if v, ok := m["api_prefix"]; ok {
		app.APIPrefix = strings.Trim(stringValue(v), "/")
	}
	if dbNode := m["db"]; dbNode != nil {
		db, err := expectMap(dbNode, "app.db")
		if err != nil {
			return BlueprintApp{}, err
		}
		if err := rejectUnknownKeys(db, map[string]bool{"driver": true, "url": true}, "app.db"); err != nil {
			return BlueprintApp{}, err
		}
		app.DBDriver = stringValue(db["driver"])
		app.DBURL = stringValue(db["url"])
	}
	if themeNode := m["theme"]; themeNode != nil {
		theme, dark, err := decodeappTheme(themeNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.Theme = theme
		app.ThemeDark = dark
	}
	if authNode := m["auth"]; authNode != nil {
		auth, err := decodeBlueprintAuth(authNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.Auth = auth
	}
	if adminNode := m["admin"]; adminNode != nil {
		admin, err := decodeBlueprintAdmin(adminNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.Admin = admin
	}
	if pwaNode := m["pwa"]; pwaNode != nil {
		pwa, err := decodeBlueprintPWA(pwaNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.PWA = pwa
	}
	if v, ok := m["llm_md"]; ok {
		app.LLMMD = boolValue(v)
	}
	return app, nil
}

func decodeBlueprintPWA(node *coreyaml.Node) (BlueprintPWA, error) {
	m, err := expectMap(node, "app.pwa")
	if err != nil {
		return BlueprintPWA{}, err
	}
	allowed := map[string]bool{"enabled": true, "name": true, "short_name": true, "description": true, "start_url": true, "scope": true, "display": true, "theme_color": true, "background_color": true}
	if err := rejectUnknownKeys(m, allowed, "app.pwa"); err != nil {
		return BlueprintPWA{}, err
	}
	return BlueprintPWA{
		Enabled:         boolValue(m["enabled"]),
		Name:            stringValue(m["name"]),
		ShortName:       stringValue(m["short_name"]),
		Description:     stringValue(m["description"]),
		StartURL:        stringValue(m["start_url"]),
		Scope:           stringValue(m["scope"]),
		Display:         stringValue(m["display"]),
		ThemeColor:      stringValue(m["theme_color"]),
		BackgroundColor: stringValue(m["background_color"]),
	}, nil
}

func decodeBlueprintAdmin(node *coreyaml.Node) (BlueprintAdmin, error) {
	m, err := expectMap(node, "app.admin")
	if err != nil {
		return BlueprintAdmin{}, err
	}
	allowed := map[string]bool{"enabled": true, "path": true, "role": true, "login_path": true, "seed_email": true, "seed_password": true}
	if err := rejectUnknownKeys(m, allowed, "app.admin"); err != nil {
		return BlueprintAdmin{}, err
	}
	return BlueprintAdmin{
		Enabled:      boolValue(m["enabled"]),
		Path:         stringValue(m["path"]),
		Role:         stringValue(m["role"]),
		LoginPath:    stringValue(m["login_path"]),
		SeedEmail:    stringValue(m["seed_email"]),
		SeedPassword: stringValue(m["seed_password"]),
	}, nil
}

func decodeBlueprintAuth(node *coreyaml.Node) (BlueprintAuth, error) {
	m, err := expectMap(node, "app.auth")
	if err != nil {
		return BlueprintAuth{}, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"enabled": true, "dev_mode": true, "base_path": true, "jwt_secret": true}, "app.auth"); err != nil {
		return BlueprintAuth{}, err
	}
	// dev_mode defaults to true when omitted: a freshly generated app
	// serves plain HTTP, where the production cookie defaults
	// (__Host-session + Secure) never round-trip — login would silently
	// break out of the box. `gofastr generate` warns loudly about the
	// default; set `dev_mode: false` (plus jwt_secret + HTTPS) to deploy.
	devMode := true
	if dm, ok := m["dev_mode"]; ok {
		// Strict bool only: dev_mode is the one blueprint bool whose
		// default is true, so the usual lax "anything-but-true → false"
		// coercion would let YAML-1.1 spellings like `yes` silently flip
		// it to prod cookie mode on plain HTTP — the exact broken-login
		// scenario the default exists to prevent — while also
		// suppressing the dev-mode warning.
		v, err := strictBoolValue(dm)
		if err != nil {
			return BlueprintAuth{}, fmt.Errorf("app.auth.dev_mode %w", err)
		}
		devMode = v
	}
	return BlueprintAuth{
		Enabled:   boolValue(m["enabled"]),
		DevMode:   devMode,
		BasePath:  stringValue(m["base_path"]),
		JWTSecret: stringValue(m["jwt_secret"]),
	}, nil
}

// decodeappTheme returns the light color/font tokens and, from a nested
// `dark:` map, the dark-scheme color overrides.
func decodeappTheme(node *coreyaml.Node) (light, dark map[string]string, err error) {
	m, err := expectMap(node, "app.theme")
	if err != nil {
		return nil, nil, err
	}
	light = make(map[string]string, len(m))
	for key, value := range m {
		if key == "dark" {
			darkMap, derr := expectMap(value, "app.theme.dark")
			if derr != nil {
				return nil, nil, derr
			}
			dark = make(map[string]string, len(darkMap))
			for dk, dv := range darkMap {
				if dv == nil || dv.Kind != coreyaml.Scalar {
					return nil, nil, fmt.Errorf("app.theme.dark.%s must be a scalar CSS color value", dk)
				}
				dark[dk] = stringValue(dv)
			}
			continue
		}
		if value == nil || value.Kind != coreyaml.Scalar {
			return nil, nil, fmt.Errorf("app.theme.%s must be a scalar CSS token value", key)
		}
		light[key] = stringValue(value)
	}
	return light, dark, nil
}

func decodeBlueprintEntities(node *coreyaml.Node) ([]framework.EntityDeclaration, []BlueprintEndpoint, error) {
	list, err := expectList(node, "entities")
	if err != nil {
		return nil, nil, err
	}
	out := make([]framework.EntityDeclaration, 0, len(list))
	var endpointStubs []BlueprintEndpoint
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("entities[%d]", i))
		if err != nil {
			return nil, nil, err
		}
		allowed := map[string]bool{"name": true, "table": true, "fields": true, "relations": true, "endpoints": true, "soft_delete": true, "multi_tenant": true, "owner_field": true, "cross_owner_read": true, "search_fields": true, "access": true, "timestamps": true, "crud": true, "mcp": true, "cursor_field": true, "cursor_fields": true, "indices": true, "properties": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("entities[%d]", i)); err != nil {
			return nil, nil, err
		}
		decl := framework.EntityDeclaration{
			Name:           stringValue(m["name"]),
			Table:          stringValue(m["table"]),
			SoftDelete:     boolValue(m["soft_delete"]),
			MultiTenant:    boolValue(m["multi_tenant"]),
			OwnerField:     stringValue(m["owner_field"]),
			CrossOwnerRead: stringValue(m["cross_owner_read"]),
			SearchFields:   stringListValue(m["search_fields"]),
			MCP:            boolValue(m["mcp"]),
			CursorField:    stringValue(m["cursor_field"]),
			CursorFields:   stringListValue(m["cursor_fields"]),
			Properties:     mapValue(m["properties"]),
		}
		if m["timestamps"] != nil {
			v := boolValue(m["timestamps"])
			decl.Timestamps = &v
		}
		if m["crud"] != nil {
			v := boolValue(m["crud"])
			decl.CRUD = &v
		}
		fields, err := decodeFields(m["fields"])
		if err != nil {
			return nil, nil, err
		}
		decl.Fields = fields
		// owner_field names a per-row owner column. Auto-inject it as a hidden
		// string column so AutoMigrate creates it and auto-CRUD can stamp/scope
		// it, without the author hand-declaring a field they never want shown in
		// a form or table. pack drops this synthesized column on the way back.
		if decl.OwnerField != "" && !blueprintEntityHasField(decl, decl.OwnerField) {
			decl.Fields = append(decl.Fields, framework.FieldDeclaration{
				Name:   decl.OwnerField,
				Type:   "string",
				Hidden: true,
			})
		}
		// cross_owner_read lifts owner scoping for reads only — it only
		// makes sense on an owner-scoped entity. Catch the misconfiguration
		// here with an actionable message rather than letting the knob
		// silently do nothing at runtime.
		if decl.CrossOwnerRead != "" && decl.OwnerField == "" {
			return nil, nil, fmt.Errorf("blueprint: entity %q sets cross_owner_read %q but has no owner_field (cross-owner read only applies to owner-scoped entities)", decl.Name, decl.CrossOwnerRead)
		}
		// search_fields must reference known, non-hidden, string/text
		// columns. entity.Define panics on these too, but the blueprint
		// decoder returns a friendlier error before code generation.
		for _, sf := range decl.SearchFields {
			var found *framework.FieldDeclaration
			for j := range decl.Fields {
				if decl.Fields[j].Name == sf {
					found = &decl.Fields[j]
					break
				}
			}
			if found == nil {
				return nil, nil, fmt.Errorf("blueprint: entity %q search_fields entry %q is not a declared field", decl.Name, sf)
			}
			if found.Hidden {
				return nil, nil, fmt.Errorf("blueprint: entity %q search_fields entry %q is Hidden (search would disclose its values)", decl.Name, sf)
			}
			if found.Type != "string" && found.Type != "text" {
				return nil, nil, fmt.Errorf("blueprint: entity %q search_fields entry %q must be string or text, got %q", decl.Name, sf, found.Type)
			}
		}
		relations, err := decodeRelations(m["relations"])
		if err != nil {
			return nil, nil, err
		}
		decl.Relations = relations
		indices, err := decodeIndices(m["indices"])
		if err != nil {
			return nil, nil, err
		}
		decl.Indices = indices
		access, err := decodeEntityAccess(m["access"], fmt.Sprintf("entities[%d].access", i))
		if err != nil {
			return nil, nil, err
		}
		decl.Access = access
		endpoints, stubs, err := decodeEntityEndpoints(decl.Name, m["endpoints"])
		if err != nil {
			return nil, nil, err
		}
		decl.Endpoints = endpoints
		endpointStubs = append(endpointStubs, stubs...)
		out = append(out, decl)
	}
	return out, endpointStubs, nil
}

// blueprintHasEntityAccess reports whether any entity declares an `access:`
// block — i.e. its auto-CRUD API is permission-gated and therefore needs a
// RolePolicy installed for the signed-in user, or every write 403s.
func blueprintHasEntityAccess(bp Blueprint) bool {
	for _, e := range bp.Entities {
		if e.Access != nil {
			return true
		}
	}
	return false
}

// blueprintHasOwnerScopedEntity reports whether any entity declares an
// owner_field. When it does and an admin account is bootstrapped, the seed
// runs *as* that admin so demo rows belong to them and a fresh signup starts
// with an empty, owner-scoped workspace.
// blueprintLoginRoute returns the route of the screen hosting the login form,
// defaulting to "/login". Used to point the auth battery's form-error redirect
// at the login page (so a failed login lands back on the form, not raw JSON).
func blueprintLoginRoute(bp Blueprint) string {
	for _, s := range bp.Screens {
		for _, b := range s.Body {
			if isLoginFormBlock(b) {
				return s.Route
			}
		}
	}
	return "/login"
}

// blueprintAppHome returns the post-login landing route — the first app-layout
// screen, defaulting to "/". Used for the auth-aware header's "Dashboard" link
// and to bounce already-signed-in visitors off the login/signup screens.
func blueprintAppHome(bp Blueprint) string {
	for _, s := range bp.Screens {
		if s.Layout == "app" {
			return s.Route
		}
	}
	return "/"
}

// screenHasAuthForm reports whether a screen hosts a login or signup form, so
// the generator gates it with a guest policy (signed-in users are redirected
// to the app instead of seeing a sign-in form they don't need).
func screenHasAuthForm(s BlueprintScreen) bool {
	for _, b := range s.Body {
		if isLoginFormBlock(b) || isSignupFormBlock(b) {
			return true
		}
	}
	return false
}

// blueprintHasAuthFormScreen reports whether any screen hosts an auth form.
func blueprintHasAuthFormScreen(bp Blueprint) bool {
	for _, s := range bp.Screens {
		if screenHasAuthForm(s) {
			return true
		}
	}
	return false
}

// blueprintNavHasRoles reports whether any nav item (at any depth) is
// role-restricted, so the generated app wires ui.SetRolesExtractor to filter
// the sidebar/drawer by the signed-in user's roles.
func blueprintNavHasRoles(items []BlueprintNavItem) bool {
	for _, it := range items {
		if it.Role != "" || blueprintNavHasRoles(it.Items) {
			return true
		}
	}
	return false
}

// blueprintEntityHasField reports whether the declaration already lists a field
// of the given name (case-sensitive — column names are verbatim).
func blueprintEntityHasField(decl framework.EntityDeclaration, name string) bool {
	for _, f := range decl.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func blueprintHasOwnerScopedEntity(bp Blueprint) bool {
	for _, e := range bp.Entities {
		if e.OwnerField != "" {
			return true
		}
	}
	return false
}

// decodeEntityAccess decodes an entity's `access:` map — the per-operation
// RBAC permissions mirroring EntityConfig.Access. nil node = no RBAC gating
// (the key is optional and additive; existing blueprints are unaffected).
func decodeEntityAccess(node *coreyaml.Node, context string) (*fwentity.AccessDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"read": true, "create": true, "update": true, "delete": true}, context); err != nil {
		return nil, err
	}
	return &fwentity.AccessDeclaration{
		Read:   stringValue(m["read"]),
		Create: stringValue(m["create"]),
		Update: stringValue(m["update"]),
		Delete: stringValue(m["delete"]),
	}, nil
}

func decodeIndices(node *coreyaml.Node) ([]framework.Index, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "indices")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Index, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("indices[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "columns": true, "unique": true}, fmt.Sprintf("indices[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, framework.Index{
			Name:    stringValue(m["name"]),
			Columns: stringListValue(m["columns"]),
			Unique:  boolValue(m["unique"]),
		})
	}
	return out, nil
}

func decodeFields(node *coreyaml.Node) ([]framework.FieldDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "fields")
	if err != nil {
		return nil, err
	}
	out := make([]framework.FieldDeclaration, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("fields[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "type": true, "required": true, "unique": true, "default": true, "auto_generate": true, "read_only": true, "hidden": true, "max": true, "min": true, "pattern": true, "values": true, "to": true, "many": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("fields[%d]", i)); err != nil {
			return nil, err
		}
		field := framework.FieldDeclaration{
			Name:         stringValue(m["name"]),
			Type:         stringValue(m["type"]),
			Required:     boolValue(m["required"]),
			Unique:       boolValue(m["unique"]),
			Default:      scalarValue(m["default"]),
			AutoGenerate: stringValue(m["auto_generate"]),
			ReadOnly:     boolValue(m["read_only"]),
			Hidden:       boolValue(m["hidden"]),
			Pattern:      stringValue(m["pattern"]),
			Values:       stringListValue(m["values"]),
			To:           stringValue(m["to"]),
			Many:         boolValue(m["many"]),
		}
		if m["max"] != nil {
			v := floatValue(m["max"])
			field.Max = &v
		}
		if m["min"] != nil {
			v := floatValue(m["min"])
			field.Min = &v
		}
		out = append(out, field)
	}
	return out, nil
}

func decodeRelations(node *coreyaml.Node) ([]framework.Relation, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "relations")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Relation, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("relations[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"type": true, "name": true, "entity": true, "foreign_key": true, "through": true, "local_key": true, "foreign_key_target": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("relations[%d]", i)); err != nil {
			return nil, err
		}
		relType, err := relationTypeFromString(stringValue(m["type"]))
		if err != nil {
			return nil, err
		}
		out = append(out, framework.Relation{
			Type:             relType,
			Name:             stringValue(m["name"]),
			Entity:           stringValue(m["entity"]),
			ForeignKey:       stringValue(m["foreign_key"]),
			Through:          stringValue(m["through"]),
			LocalKey:         stringValue(m["local_key"]),
			ForeignKeyTarget: stringValue(m["foreign_key_target"]),
		})
	}
	return out, nil
}

func decodeEntityEndpoints(entityName string, node *coreyaml.Node) ([]framework.Endpoint, []BlueprintEndpoint, error) {
	if node == nil {
		return nil, nil, nil
	}
	list, err := expectList(node, "endpoints")
	if err != nil {
		return nil, nil, err
	}
	out := make([]framework.Endpoint, 0, len(list))
	stubs := make([]BlueprintEndpoint, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("endpoints[%d]", i))
		if err != nil {
			return nil, nil, err
		}
		allowed := map[string]bool{"method": true, "path": true, "name": true, "description": true, "mcp": true, "handler": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("endpoints[%d]", i)); err != nil {
			return nil, nil, err
		}
		handler := stringValue(m["handler"])
		out = append(out, framework.Endpoint{
			Method:      stringValue(m["method"]),
			Path:        stringValue(m["path"]),
			Name:        stringValue(m["name"]),
			Description: stringValue(m["description"]),
			MCP:         false,
		})
		stubs = append(stubs, BlueprintEndpoint{
			Name:        stringValue(m["name"]),
			Method:      stringValue(m["method"]),
			Path:        stringValue(m["path"]),
			Entity:      entityName,
			Handler:     handler,
			Description: stringValue(m["description"]),
			MCP:         boolValue(m["mcp"]),
		})
	}
	return out, stubs, nil
}

func decodeBlueprintScreens(node *coreyaml.Node) ([]BlueprintScreen, error) {
	list, err := expectList(node, "screens")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintScreen, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("screens[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "route": true, "title": true, "description": true, "type": true, "layout": true, "access": true, "body": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("screens[%d]", i)); err != nil {
			return nil, err
		}
		screen := BlueprintScreen{
			Name:        stringValue(m["name"]),
			Route:       stringValue(m["route"]),
			Title:       stringValue(m["title"]),
			Description: stringValue(m["description"]),
			Type:        stringValue(m["type"]),
			Layout:      stringValue(m["layout"]),
		}
		if accNode := m["access"]; accNode != nil {
			accM, err := expectMap(accNode, fmt.Sprintf("screens[%d].access", i))
			if err != nil {
				return nil, err
			}
			if err := rejectUnknownKeys(accM, map[string]bool{"auth": true, "role": true}, fmt.Sprintf("screens[%d].access", i)); err != nil {
				return nil, err
			}
			screen.Access = BlueprintAccess{Auth: boolValue(accM["auth"]), Role: stringValue(accM["role"])}
			if screen.Access.Role != "" {
				screen.Access.Auth = true
			}
		}
		body, err := decodeBlocks(m["body"])
		if err != nil {
			return nil, err
		}
		screen.Body = body
		out = append(out, screen)
	}
	return out, nil
}
func decodeBlueprintNav(node *coreyaml.Node) ([]BlueprintNavItem, error) {
	list, err := expectList(node, "nav")
	if err != nil {
		return nil, err
	}
	return decodeNavItems(list, "nav")
}
func decodeNavItems(list []*coreyaml.Node, label string) ([]BlueprintNavItem, error) {
	out := make([]BlueprintNavItem, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("%s[%d]", label, i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"label": true, "href": true, "icon": true, "role": true, "items": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("%s[%d]", label, i)); err != nil {
			return nil, err
		}
		navItem := BlueprintNavItem{
			Label: stringValue(m["label"]),
			Href:  stringValue(m["href"]),
			Icon:  stringValue(m["icon"]),
			Role:  stringValue(m["role"]),
		}
		if child := m["items"]; child != nil {
			subList, err := expectList(child, fmt.Sprintf("%s[%d].items", label, i))
			if err != nil {
				return nil, err
			}
			items, err := decodeNavItems(subList, fmt.Sprintf("%s[%d].items", label, i))
			if err != nil {
				return nil, err
			}
			navItem.Items = items
		}
		out = append(out, navItem)
	}
	return out, nil
}
func decodeBlueprintSeed(node *coreyaml.Node) ([]BlueprintSeedEntity, error) {
	list, err := expectList(node, "seed")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintSeedEntity, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("seed[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"entity": true, "rows": true, "count": true, "weights": true}, fmt.Sprintf("seed[%d]", i)); err != nil {
			return nil, err
		}
		entity := stringValue(m["entity"])
		if entity == "" {
			return nil, fmt.Errorf("seed[%d]: entity is required", i)
		}
		var rows []map[string]any
		if rowsNode := m["rows"]; rowsNode != nil {
			rowList, err := expectList(rowsNode, fmt.Sprintf("seed[%d].rows", i))
			if err != nil {
				return nil, err
			}
			for j, rowNode := range rowList {
				rowMap, err := expectMap(rowNode, fmt.Sprintf("seed[%d].rows[%d]", i, j))
				if err != nil {
					return nil, err
				}
				row := make(map[string]any, len(rowMap))
				for k, v := range rowMap {
					row[k] = anyValue(v)
				}
				rows = append(rows, row)
			}
		}
		count := 0
		if cn := m["count"]; cn != nil {
			count = intValue(cn)
			if count < 0 {
				return nil, fmt.Errorf("seed[%d].count must be >= 0", i)
			}
		}
		weights, err := decodeSeedWeights(m["weights"], fmt.Sprintf("seed[%d].weights", i))
		if err != nil {
			return nil, err
		}
		out = append(out, BlueprintSeedEntity{Entity: entity, Rows: rows, Count: count, Weights: weights})
	}
	return out, nil
}

// decodeSeedWeights parses the optional per-column weight map used when
// auto-generating rows: `weights: { <column>: { <value>: <weight> } }`.
func decodeSeedWeights(node *coreyaml.Node, label string) (map[string]map[string]int, error) {
	if node == nil {
		return nil, nil
	}
	cols, err := expectMap(node, label)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]int, len(cols))
	for col, child := range cols {
		valMap, err := expectMap(child, label+"."+col)
		if err != nil {
			return nil, err
		}
		vw := make(map[string]int, len(valMap))
		for val, wNode := range valMap {
			w := intValue(wNode)
			if w < 0 {
				return nil, fmt.Errorf("%s.%s.%s must be >= 0", label, col, val)
			}
			vw[val] = w
		}
		out[col] = vw
	}
	return out, nil
}

// blueprintExpandSeed appends Count auto-generated demo rows to each seed
// entity that declares one. Generation is deterministic (no runtime
// randomness), so regeneration is stable and the diff never churns. Enum
// columns get a weighted or deterministically-skewed distribution so demo data
// never reads as a flat 1/N-per-value round-robin.
func blueprintExpandSeed(bp Blueprint) Blueprint {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	for i := range bp.Seed {
		s := &bp.Seed[i]
		if s.Count <= 0 {
			continue
		}
		decl, ok := entityMap[s.Entity]
		if !ok {
			continue
		}
		s.Rows = append(s.Rows, blueprintGenerateSeedRows(decl, s.Count, s.Weights)...)
	}
	return bp
}

// blueprintGenerateSeedRows builds n demo rows for an entity. Each enum column
// is filled from a precomputed distribution; scalar columns get plausible
// deterministic values keyed off the row index. System columns (id/timestamps/
// auto-generated/read-only), hidden columns, and relations are skipped —
// relations can't be safely fabricated, so count-seeding suits scalar/enum
// entities (use explicit rows: for entities with required relations).
func blueprintGenerateSeedRows(decl framework.EntityDeclaration, n int, weights map[string]map[string]int) []map[string]any {
	// Precompute enum distributions once per column.
	dist := map[string][]string{}
	for _, f := range decl.Fields {
		if strings.EqualFold(f.Type, "enum") && len(f.Values) > 0 {
			dist[f.Name] = blueprintEnumDistribution(decl.Name, f.Name, f.Values, weights[f.Name], n)
		}
	}
	singular := singularize(toDisplayName(decl.Name))
	rows := make([]map[string]any, 0, n)
	for r := 0; r < n; r++ {
		row := map[string]any{}
		for _, f := range decl.Fields {
			if blueprintFieldSystem(f.Name) || f.Hidden || f.ReadOnly || f.AutoGenerate != "" {
				continue
			}
			switch strings.ToLower(f.Type) {
			case "enum":
				if seq := dist[f.Name]; len(seq) == n {
					row[f.Name] = seq[r]
				}
			case "string", "text":
				row[f.Name] = blueprintSeedStringValue(f, singular, r)
			case "int", "integer", "bigint":
				// A gently-skewed spread rather than 1..n so charts summing a
				// numeric column show variation.
				row[f.Name] = 10 + int(blueprintSeedHash(decl.Name+f.Name+strconv.Itoa(r))%90)
			case "float", "double", "decimal", "money", "numeric":
				v := 5 + float64(blueprintSeedHash(decl.Name+f.Name+strconv.Itoa(r))%9950)/10.0
				row[f.Name] = math.Round(v*100) / 100
			case "bool", "boolean":
				// Skew towards true so demos look "active".
				row[f.Name] = blueprintSeedHash(decl.Name+f.Name+strconv.Itoa(r))%4 != 0
			default:
				// date/timestamp/uuid/relation/json: leave to auto-generation
				// or nullable columns.
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// blueprintSeedStringValue produces a plausible value for a scalar text column
// based on common naming conventions (name/title/email/slug), falling back to a
// humanized "<Singular> <n>".
func blueprintSeedStringValue(f framework.FieldDeclaration, singular string, r int) string {
	name := strings.ToLower(f.Name)
	switch {
	case strings.Contains(name, "email"):
		return fmt.Sprintf("%s%d@example.com", strings.ToLower(strings.ReplaceAll(singular, " ", "")), r+1)
	case strings.Contains(name, "slug"):
		return fmt.Sprintf("%s-%d", strings.ToLower(strings.ReplaceAll(singular, " ", "-")), r+1)
	case strings.Contains(name, "url"), strings.Contains(name, "link"):
		return fmt.Sprintf("https://example.com/%d", r+1)
	case name == "name" || name == "title" || strings.HasSuffix(name, "_name") || strings.HasSuffix(name, "_title"):
		return fmt.Sprintf("%s %d", singular, r+1)
	case strings.EqualFold(f.Type, "text") || strings.Contains(name, "description") || strings.Contains(name, "body") || strings.Contains(name, "notes"):
		return fmt.Sprintf("Sample %s for %s %d.", strings.ReplaceAll(name, "_", " "), strings.ToLower(singular), r+1)
	default:
		return fmt.Sprintf("%s %d", humanizeFieldLabel(f.Name), r+1)
	}
}

// blueprintSeedHash is a small deterministic FNV-1a hash used to derive stable
// pseudo-random-looking (but reproducible) seed values.
func blueprintSeedHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// blueprintEnumDistribution returns a length-n slice of enum values whose
// frequencies follow `weights` when given, otherwise a deterministic skew
// derived from the entity+column+value names. The result is always non-uniform
// for the default (no-weights) path, so demo data never reads as a flat
// round-robin. Values are interleaved (round-robin over the assigned counts) so
// the first page of a list isn't a single value.
func blueprintEnumDistribution(seedKey, col string, values []string, weights map[string]int, n int) []string {
	if len(values) == 0 || n <= 0 {
		return nil
	}
	w := make([]int, len(values))
	total := 0
	explicit := len(weights) > 0
	for i, v := range values {
		if explicit {
			w[i] = weights[v]
		} else {
			w[i] = 1 + int(blueprintSeedHash(seedKey+"|"+col+"|"+v)%7) // 1..7
		}
		total += w[i]
	}
	if total == 0 {
		for i := range w {
			w[i] = 1
		}
		total = len(w)
	}
	// Guard the default skew against a hash collision leaving every weight
	// equal (which would round-trip to a uniform split).
	if !explicit && len(w) > 1 {
		allEqual := true
		for i := 1; i < len(w); i++ {
			if w[i] != w[0] {
				allEqual = false
				break
			}
		}
		if allEqual {
			w[0]++
			total++
		}
	}
	// Largest-remainder apportionment: exact integer counts summing to n.
	counts := make([]int, len(values))
	type rem struct {
		idx  int
		frac float64
	}
	rems := make([]rem, len(values))
	assigned := 0
	for i := range values {
		q := float64(n) * float64(w[i]) / float64(total)
		counts[i] = int(math.Floor(q))
		rems[i] = rem{i, q - math.Floor(q)}
		assigned += counts[i]
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].frac > rems[b].frac })
	for k := 0; assigned < n && len(rems) > 0; k++ {
		counts[rems[k%len(rems)].idx]++
		assigned++
	}
	// Interleave for readability.
	out := make([]string, 0, n)
	remaining := make([]int, len(counts))
	copy(remaining, counts)
	for len(out) < n {
		progressed := false
		for i := range values {
			if remaining[i] > 0 {
				out = append(out, values[i])
				remaining[i]--
				progressed = true
				if len(out) == n {
					break
				}
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

func decodeBlocks(node *coreyaml.Node) ([]BlueprintBlock, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "body")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintBlock, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("body[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "entity": true, "fields": true, "limit": true, "empty_text": true, "mode": true, "search": true, "filters": true, "create": true, "props": true, "children": true, "actions": true, "transitions": true, "island": true, "widget": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("body[%d]", i)); err != nil {
			return nil, err
		}
		children, err := decodeBlocks(m["children"])
		if err != nil {
			return nil, err
		}
		actions, err := decodeActions(m["actions"])
		if err != nil {
			return nil, err
		}
		transitions, err := decodeTransitions(m["transitions"])
		if err != nil {
			return nil, err
		}
		out = append(out, BlueprintBlock{
			Type:        stringValue(m["type"]),
			Kind:        stringValue(m["kind"]),
			Text:        stringValue(m["text"]),
			Level:       intValue(m["level"]),
			Class:       stringValue(m["class"]),
			Href:        stringValue(m["href"]),
			Entity:      stringValue(m["entity"]),
			Fields:      stringListValue(m["fields"]),
			Limit:       intValue(m["limit"]),
			EmptyText:   stringValue(m["empty_text"]),
			Mode:        stringValue(m["mode"]),
			Search:      stringValue(m["search"]),
			Filters:     stringListValue(m["filters"]),
			Create:      boolValue(m["create"]),
			Props:       mapValue(m["props"]),
			Children:    children,
			Actions:     actions,
			Transitions: transitions,
			Island:      stringValue(m["island"]),
			Widget:      stringValue(m["widget"]),
		})
	}
	return out, nil
}

func decodeTransitions(node *coreyaml.Node) ([]BlueprintTransition, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "transitions")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintTransition, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("transitions[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"label": true, "status": true, "variant": true, "stamp": true}, fmt.Sprintf("transitions[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, BlueprintTransition{
			Label:   stringValue(m["label"]),
			Status:  stringValue(m["status"]),
			Variant: stringValue(m["variant"]),
			Stamp:   stringValue(m["stamp"]),
		})
	}
	return out, nil
}

func decodeActions(node *coreyaml.Node) ([]BlueprintAction, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "actions")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintAction, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("actions[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "event": true, "client_js": true}, fmt.Sprintf("actions[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, BlueprintAction{
			Name:     stringValue(m["name"]),
			Event:    stringValue(m["event"]),
			ClientJS: stringValue(m["client_js"]),
		})
	}
	return out, nil
}

func decodeBlueprintEndpoints(node *coreyaml.Node) ([]BlueprintEndpoint, error) {
	list, err := expectList(node, "endpoints")
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintEndpoint, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("endpoints[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "method": true, "path": true, "entity": true, "handler": true, "description": true, "mcp": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("endpoints[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, BlueprintEndpoint{
			Name:        stringValue(m["name"]),
			Method:      stringValue(m["method"]),
			Path:        stringValue(m["path"]),
			Entity:      stringValue(m["entity"]),
			Handler:     stringValue(m["handler"]),
			Description: stringValue(m["description"]),
			MCP:         boolValue(m["mcp"]),
		})
	}
	return out, nil
}

func decodeNamedStubs(node *coreyaml.Node, label string) ([]BlueprintNamedStub, error) {
	list, err := expectList(node, label)
	if err != nil {
		return nil, err
	}
	out := make([]BlueprintNamedStub, 0, len(list))
	for i, item := range list {
		if item.Kind == coreyaml.Scalar {
			out = append(out, BlueprintNamedStub{Name: fmt.Sprint(item.Value)})
			continue
		}
		m, err := expectMap(item, fmt.Sprintf("%s[%d]", label, i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "description": true}, fmt.Sprintf("%s[%d]", label, i)); err != nil {
			return nil, err
		}
		out = append(out, BlueprintNamedStub{Name: stringValue(m["name"]), Description: stringValue(m["description"])})
	}
	return out, nil
}

func validateBlueprint(bp Blueprint) error {
	// Production auth without a signing key: the generated app's auth
	// battery fails closed at boot (battery/auth Init refuses an empty
	// JWTSecret with DevMode=false). Fail at generate/validate time
	// instead, with the same remedy.
	if bp.App.Auth.Enabled && !bp.App.Auth.DevMode && bp.App.Auth.JWTSecret == "" {
		return fmt.Errorf("blueprint: app.auth has dev_mode: false but no jwt_secret — the generated app would refuse to boot (production auth requires a signing key); set jwt_secret from your secret store, or set dev_mode: true for local development")
	}
	// A multi-tenant entity needs a request-time tenant resolver, and the
	// right one is host-specific (subdomain, JWT claim, user's org). The
	// generator can't emit it, and ApplyTenantScope is fail-closed — so a
	// generated multi_tenant app with no resolver reads empty and stamps an
	// empty tenant on writes: silently broken while looking secure. Refuse
	// to generate rather than ship that.
	for _, decl := range bp.Entities {
		if decl.MultiTenant {
			return fmt.Errorf("blueprint: entity %q sets multi_tenant: true, but the generator cannot emit a tenant resolver (the strategy — subdomain, JWT claim, user's org — is app-specific). A generated app with none reads empty and stamps an empty tenant on every write. Wire tenant.TenantMiddleware + SetTenantID in your own main (see `gofastr docs multi-tenant`) and drop multi_tenant from the blueprint, or use owner_field for per-user scoping", decl.Name)
		}
	}
	if bp.App.PWA.Enabled {
		switch bp.App.PWA.Display {
		case "", "standalone", "fullscreen", "minimal-ui", "browser":
		default:
			return fmt.Errorf("blueprint: app.pwa.display %q is not a web-app-manifest display mode; use standalone, fullscreen, minimal-ui, or browser (or omit it for standalone)", bp.App.PWA.Display)
		}
		if u := bp.App.PWA.StartURL; u != "" && !strings.HasPrefix(u, "/") {
			return fmt.Errorf("blueprint: app.pwa.start_url %q must be a root-relative path (start with /)", u)
		}
		if s := bp.App.PWA.Scope; s != "" && !strings.HasPrefix(s, "/") {
			return fmt.Errorf("blueprint: app.pwa.scope %q must be a root-relative path (start with /)", s)
		}
	}
	for key := range bp.App.Theme {
		if _, ok := blueprintThemeColorPath(key); ok {
			continue
		}
		// Font tokens are valid theme keys too — they drive the theme's
		// font-family tokens and the webfont <link>s, not colors.
		if key == "font_body" || key == "font_heading" || key == "font_display" {
			continue
		}
		return fmt.Errorf("blueprint: app.theme has unsupported token %q", key)
	}
	for key := range bp.App.ThemeDark {
		if _, ok := blueprintThemeColorPath(key); !ok {
			return fmt.Errorf("blueprint: app.theme.dark has unsupported color token %q", key)
		}
	}
	entityNames := map[string]bool{}
	entitiesByName := map[string]framework.EntityDeclaration{}
	for _, decl := range bp.Entities {
		if decl.Name == "" {
			return fmt.Errorf("blueprint: entity name is required")
		}
		if !isGoIdentifier(toCamelCase(decl.Name)) {
			return fmt.Errorf("blueprint: entity %q does not produce a valid Go identifier — the generated code would not compile; rename it to start with a letter (e.g. \"two_fa_tokens\" instead of \"2fa_tokens\")", decl.Name)
		}
		if entityNames[decl.Name] {
			return fmt.Errorf("blueprint: duplicate entity %q", decl.Name)
		}
		entityNames[decl.Name] = true
		entitiesByName[decl.Name] = decl
		if _, err := decl.Config(); err != nil {
			return fmt.Errorf("blueprint: entity %q: %w", decl.Name, err)
		}
		for _, endpoint := range decl.Endpoints {
			if endpoint.MCP {
				return fmt.Errorf("blueprint: entity %q endpoint %q cannot set mcp=true without Go MCP handler", decl.Name, endpoint.Path)
			}
		}
	}
	for _, decl := range bp.Entities {
		// Relation-TYPED FIELDS (`type: relation, to: X`) become BelongsTo
		// relations at runtime. Catch a dangling target here so the failure
		// is a generate-time error, not an auto-migrate crash in the built
		// app ("entity has BelongsTo to unknown entity").
		for _, field := range decl.Fields {
			if !strings.EqualFold(strings.TrimSpace(field.Type), "relation") {
				continue
			}
			if strings.TrimSpace(field.To) == "" {
				return fmt.Errorf("blueprint: entity %q field %q has type \"relation\" but no target — add `to: <entity>` naming a declared entity", decl.Name, field.Name)
			}
			if !entityNames[field.To] {
				return fmt.Errorf("blueprint: entity %q field %q is a relation to unknown entity %q — declare an entity named %q under entities: (or fix the field's to: value)", decl.Name, field.Name, field.To, field.To)
			}
		}
		for _, rel := range decl.Relations {
			if rel.Entity == "" {
				return fmt.Errorf("blueprint: entity %q relation %q target entity is required — add `entity: <name>` referencing a declared entity", decl.Name, rel.Name)
			}
			if !entityNames[rel.Entity] {
				return fmt.Errorf("blueprint: entity %q relation %q targets unknown entity %q — declare an entity named %q under entities: (or fix the relation's entity: value)", decl.Name, rel.Name, rel.Entity, rel.Entity)
			}
		}
	}
	routes := map[string]bool{}
	for _, screen := range bp.Screens {
		if screen.Name == "" {
			return fmt.Errorf("blueprint: screen name is required")
		}
		if !isGoIdentifier(toCamelCase(screen.Name)) {
			return fmt.Errorf("blueprint: screen %q does not produce a valid Go identifier", screen.Name)
		}
		if screen.Route == "" {
			return fmt.Errorf("blueprint: screen %q route is required", screen.Name)
		}
		if routes[screen.Route] {
			return fmt.Errorf("blueprint: duplicate screen route %q", screen.Route)
		}
		routes[screen.Route] = true
		if _, err := screenTypeConst(screen.Type); err != nil {
			return err
		}
		for _, block := range screen.Body {
			if err := validateBlueprintBlock(screen.Name, entitiesByName, block); err != nil {
				return err
			}
		}
		if err := validateBlueprintActions(screen.Name, screen.Body); err != nil {
			return err
		}
	}
	endpointNames := map[string]bool{}
	endpointHandlers := map[string]bool{}
	endpointRoutes := map[string]bool{}
	crudRoutes := blueprintCRUDRoutes(bp.Entities)
	for _, endpoint := range bp.Endpoints {
		if endpoint.Name == "" {
			return fmt.Errorf("blueprint: endpoint name is required")
		}
		if endpointNames[endpoint.Name] {
			return fmt.Errorf("blueprint: duplicate endpoint %q", endpoint.Name)
		}
		endpointNames[endpoint.Name] = true
		if endpoint.Handler == "" {
			return fmt.Errorf("blueprint: endpoint %q handler is required", endpoint.Name)
		}
		handlerName := toCamelCase(endpoint.Handler)
		if !isGoIdentifier(handlerName) {
			return fmt.Errorf("blueprint: endpoint %q handler %q does not produce a valid Go identifier", endpoint.Name, endpoint.Handler)
		}
		if endpointHandlers[handlerName] {
			return fmt.Errorf("blueprint: duplicate endpoint handler %q", endpoint.Handler)
		}
		endpointHandlers[handlerName] = true
		if endpoint.Path == "" {
			return fmt.Errorf("blueprint: endpoint %q path is required", endpoint.Name)
		}
		method := strings.ToUpper(endpoint.Method)
		if method == "" {
			return fmt.Errorf("blueprint: endpoint %q method is required", endpoint.Name)
		}
		if _, ok := validHTTPMethods[method]; !ok {
			return fmt.Errorf("blueprint: endpoint %q method %q is not supported", endpoint.Name, endpoint.Method)
		}
		if endpoint.Entity != "" && !entityNames[endpoint.Entity] {
			return fmt.Errorf("blueprint: endpoint %q targets unknown entity %q", endpoint.Name, endpoint.Entity)
		}
		routeKey := method + " " + blueprintEndpointPath(endpoint)
		if endpointRoutes[routeKey] {
			return fmt.Errorf("blueprint: duplicate endpoint route %q", routeKey)
		}
		endpointRoutes[routeKey] = true
		if crudRoutes[routeKey] {
			return fmt.Errorf("blueprint: endpoint %q collides with generated CRUD route %q", endpoint.Name, routeKey)
		}
		if endpoint.MCP {
			return fmt.Errorf("blueprint: endpoint %q cannot set mcp=true without Go MCP handler", endpoint.Name)
		}
	}
	for _, group := range []struct {
		label string
		items []BlueprintNamedStub
	}{
		{"middleware", bp.Middleware},
		{"plugins", bp.Plugins},
		{"helpers", bp.Helpers},
	} {
		names := map[string]bool{}
		for _, item := range group.items {
			if item.Name == "" {
				return fmt.Errorf("blueprint: %s name is required", group.label)
			}
			if !isGoIdentifier(toCamelCase(item.Name)) {
				return fmt.Errorf("blueprint: %s %q does not produce a valid Go identifier", group.label, item.Name)
			}
			if names[item.Name] {
				return fmt.Errorf("blueprint: duplicate %s %q", group.label, item.Name)
			}
			names[item.Name] = true
		}
	}
	return nil
}

func blueprintCRUDRoutes(entities []framework.EntityDeclaration) map[string]bool {
	out := map[string]bool{}
	for _, decl := range entities {
		if decl.CRUD != nil && !*decl.CRUD {
			continue
		}
		root := "/" + blueprintEntityTable(decl)
		for _, route := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, root},
			{http.MethodGet, root + "/{id}"},
			{http.MethodPost, root},
			{http.MethodPut, root + "/{id}"},
			{http.MethodDelete, root + "/{id}"},
			{http.MethodPost, root + "/_batch"},
			{http.MethodPatch, root + "/_batch"},
			{http.MethodDelete, root + "/_batch"},
			{http.MethodGet, root + "/_events"},
		} {
			out[route.method+" "+route.path] = true
		}
	}
	return out
}

func blueprintEntityTable(decl framework.EntityDeclaration) string {
	if decl.Table != "" {
		return strings.Trim(decl.Table, "/")
	}
	return blueprintDefaultTableName(decl.Name)
}

func blueprintDefaultTableName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	if strings.ToLower(name) == name {
		return name
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func validateBlueprintBlock(screenName string, entities map[string]framework.EntityDeclaration, block BlueprintBlock) error {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "text", "p", "paragraph", "section", "div", "article", "main", "header", "footer", "nav", "aside", "span", "strong", "em", "code", "pre", "small", "blockquote", "button", "input", "label", "form", "select", "option", "textarea", "fieldset", "image", "img", "list", "ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td", "raw":
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6":
		level := block.Level
		if level == 0 {
			level = 1
		}
		if level < 1 || level > 6 {
			return fmt.Errorf("blueprint: screen %q heading level must be 1-6", screenName)
		}
	case "link", "a":
		href := block.Href
		if href == "" {
			href, _ = block.Props["href"].(string)
		}
		if href == "" {
			return fmt.Errorf("blueprint: screen %q link block href is required", screenName)
		}
	case "entity_list":
		if block.Entity == "" {
			return fmt.Errorf("blueprint: screen %q entity_list block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("blueprint: screen %q entity_list targets unknown entity %q", screenName, block.Entity)
		}
		if decl.CRUD != nil && !*decl.CRUD {
			return fmt.Errorf("blueprint: screen %q entity_list target %q must enable crud", screenName, block.Entity)
		}
		if len(block.Fields) == 0 {
			return fmt.Errorf("blueprint: screen %q entity_list block fields are required", screenName)
		}
		validFields := map[string]bool{"id": true}
		for _, field := range decl.Fields {
			validFields[field.Name] = true
			validFields[toCamelJSON(field.Name)] = true
		}
		for _, field := range block.Fields {
			if !validFields[field] {
				return fmt.Errorf("blueprint: screen %q entity_list field %q is not defined on entity %q", screenName, field, block.Entity)
			}
		}
		if block.Limit < 0 {
			return fmt.Errorf("blueprint: screen %q entity_list limit must be >= 0", screenName)
		}
		// filters: each must be a defined column of a facetable type — enum,
		// bool, or relation. Explicit only (no auto-derivation): an omitted
		// filters: list renders exactly as before, no facet toolbar.
		if len(block.Filters) > 0 {
			rels := blueprintEntityRelations(decl) // fk column -> target entity
			typeOf := map[string]string{}
			for _, field := range decl.Fields {
				typeOf[field.Name] = strings.ToLower(strings.TrimSpace(field.Type))
			}
			for _, col := range block.Filters {
				t, defined := typeOf[col]
				_, isRel := rels[col]
				if !defined && !isRel {
					return fmt.Errorf("blueprint: screen %q entity_list filter %q is not defined on entity %q", screenName, col, block.Entity)
				}
				switch {
				case t == "enum", t == "bool", t == "boolean", t == "relation", isRel:
					// facetable
				default:
					return fmt.Errorf("blueprint: screen %q entity_list filter %q has type %q; only enum, bool, and relation columns can be faceted", screenName, col, t)
				}
			}
		}
	case "entity_form":
		if block.Entity == "" {
			return fmt.Errorf("blueprint: screen %q entity_form block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("blueprint: screen %q entity_form targets unknown entity %q", screenName, block.Entity)
		}
		if decl.CRUD != nil && !*decl.CRUD {
			return fmt.Errorf("blueprint: screen %q entity_form target %q must enable crud", screenName, block.Entity)
		}
		mode := strings.ToLower(strings.TrimSpace(block.Mode))
		if mode != "" && mode != "create" && mode != "edit" {
			return fmt.Errorf("blueprint: screen %q entity_form mode must be \"create\" or \"edit\"", screenName)
		}
	case "entity_detail":
		if block.Entity == "" {
			return fmt.Errorf("blueprint: screen %q entity_detail block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("blueprint: screen %q entity_detail targets unknown entity %q", screenName, block.Entity)
		}
		if decl.CRUD != nil && !*decl.CRUD {
			return fmt.Errorf("blueprint: screen %q entity_detail target %q must enable crud", screenName, block.Entity)
		}
	case "login_form", "signup_form":
		// Static HTML auth form posting to the auth battery.
		if v, ok := block.Props["action"]; ok {
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("blueprint: screen %q %s action must be a string", screenName, kind)
			}
		}
	case "bar_chart", "pie_chart", "line_chart":
		// Charts render from grouped entity data; without a valid source
		// the block would silently vanish from the page. Reject upfront.
		src, ok := block.Props["source"].(map[string]any)
		if !ok {
			return fmt.Errorf("blueprint: screen %q %s requires source: {entity: ..., group_by: ...}", screenName, kind)
		}
		srcEntity, _ := src["entity"].(string)
		groupBy, _ := src["group_by"].(string)
		if srcEntity == "" || groupBy == "" {
			return fmt.Errorf("blueprint: screen %q %s source needs both entity and group_by", screenName, kind)
		}
		decl, known := entities[strings.Trim(srcEntity, "/")]
		if !known {
			return fmt.Errorf("blueprint: screen %q %s source targets unknown entity %q", screenName, kind, srcEntity)
		}
		if decl.CRUD != nil && !*decl.CRUD {
			return fmt.Errorf("blueprint: screen %q %s source entity %q must enable crud (the chart reads its rows via the CRUD handler)", screenName, kind, srcEntity)
		}
	case "stat_card":
		// A stat_card with a source binds to live entity data; without a
		// registered CRUD handler statValue would render a silent "—".
		// Reject an unknown or crud-disabled source upfront. (A static
		// value: stat_card has no source and is fine.)
		if src, ok := block.Props["source"].(map[string]any); ok {
			srcEntity, _ := src["entity"].(string)
			if srcEntity == "" {
				return fmt.Errorf("blueprint: screen %q stat_card source needs entity", screenName)
			}
			decl, known := entities[strings.Trim(srcEntity, "/")]
			if !known {
				return fmt.Errorf("blueprint: screen %q stat_card source targets unknown entity %q", screenName, srcEntity)
			}
			if decl.CRUD != nil && !*decl.CRUD {
				return fmt.Errorf("blueprint: screen %q stat_card source entity %q must enable crud", screenName, srcEntity)
			}
		}
	case "stack", "cluster", "grid", "stat_grid":
		if err := validateBlueprintLayoutBlock(screenName, kind, block.Props); err != nil {
			return err
		}
	case "page_header", "hero", "card", "stat_row",
		"link_button", "callout",
		"markdown", "pricing", "divider":
		// framework/ui catalog blocks — props are validated leniently (the
		// generator reads only the props each component understands).
	default:
		return fmt.Errorf("blueprint: screen %q has unsupported block type %q", screenName, kind)
	}
	for _, child := range block.Children {
		if err := validateBlueprintBlock(screenName, entities, child); err != nil {
			return err
		}
	}
	return nil
}

func validateBlueprintLayoutBlock(screenName, kind string, props map[string]any) error {
	allowed := func(value string, values ...string) bool {
		if value == "" {
			return true
		}
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	stringProp := func(name string) (string, error) {
		value, ok := props[name]
		if !ok {
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("blueprint: screen %q %s prop %q must be a string", screenName, kind, name)
		}
		return strings.ToLower(strings.TrimSpace(text)), nil
	}
	gap, err := stringProp("gap")
	if err != nil {
		return err
	}
	if !allowed(gap, "none", "xs", "sm", "md", "lg", "xl", "2xl") {
		return fmt.Errorf("blueprint: screen %q %s gap %q is not a design token", screenName, kind, gap)
	}
	if kind == "stack" || kind == "cluster" {
		align, err := stringProp("align")
		if err != nil {
			return err
		}
		if !allowed(align, "start", "center", "end", "baseline", "stretch") {
			return fmt.Errorf("blueprint: screen %q %s align %q is unsupported", screenName, kind, align)
		}
		justify, err := stringProp("justify")
		if err != nil {
			return err
		}
		if !allowed(justify, "start", "center", "end", "between", "around") {
			return fmt.Errorf("blueprint: screen %q %s justify %q is unsupported", screenName, kind, justify)
		}
	}
	if kind == "cluster" {
		if value, ok := props["no_wrap"]; ok {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("blueprint: screen %q cluster prop %q must be a boolean", screenName, "no_wrap")
			}
		}
	}
	if kind == "grid" || kind == "stat_grid" {
		if _, err := stringProp("min"); err != nil {
			return err
		}
	}
	return nil
}

func validateBlueprintActions(screenName string, blocks []BlueprintBlock) error {
	names := map[string]bool{}
	var walk func(BlueprintBlock) error
	walk = func(block BlueprintBlock) error {
		events := map[string]bool{}
		for i, action := range block.Actions {
			name := strings.TrimSpace(action.Name)
			event := blueprintActionEvent(action)
			if name == "" {
				name = event
			}
			if !isGoFastrActionEvent(event) {
				return fmt.Errorf("blueprint: screen %q action %q event %q is not supported", screenName, name, event)
			}
			if events[event] {
				return fmt.Errorf("blueprint: screen %q duplicate event %q on one block", screenName, event)
			}
			events[event] = true
			if event == "click" && len(block.Actions) > 1 && i != 0 {
				return fmt.Errorf("blueprint: screen %q click action must be first when combined with other actions", screenName)
			}
			if names[name] {
				return fmt.Errorf("blueprint: screen %q duplicate action %q", screenName, name)
			}
			names[name] = true
			if action.ClientJS == "" {
				return fmt.Errorf("blueprint: screen %q action %q client_js is required", screenName, name)
			}
		}
		for _, child := range block.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, block := range blocks {
		if err := walk(block); err != nil {
			return err
		}
	}
	var checkSynthetic func([]BlueprintBlock, []int) error
	checkSynthetic = func(blocks []BlueprintBlock, path []int) error {
		for i, block := range blocks {
			blockPath := append(append([]int(nil), path...), i)
			if isEntityListBlock(block) {
				name := blueprintEntityListActionName(BlueprintScreen{Name: screenName}, block, blockPath)
				if names[name] {
					return fmt.Errorf("blueprint: screen %q duplicate action %q", screenName, name)
				}
				names[name] = true
			}
			if err := checkSynthetic(block.Children, blockPath); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkSynthetic(blocks, nil); err != nil {
		return err
	}
	return nil
}

func isGoFastrActionEvent(event string) bool {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "click", "input", "change", "submit":
		return true
	default:
		return false
	}
}

func isGoIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

var validHTTPMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

// blueprintSynthesizeCRUDScreens appends the form screens that make app-side
// entity screens writable: a /new create form for every entity_list flagged
// `create: true`, and a /{id}/edit form for every entity_detail. The synthesized
// screens render through the resource engine's Form(ctx, id), inherit the source
// screen's layout + access, and are NOT added to nav. List "New" / detail "Edit"
// + "Delete" affordances are wired separately (WithCreate / CanEdit).
func blueprintSynthesizeCRUDScreens(bp Blueprint) Blueprint {
	existing := map[string]bool{}
	for _, s := range bp.Screens {
		existing[s.Route] = true
	}
	var extra []BlueprintScreen
	add := func(s BlueprintScreen) {
		if existing[s.Route] {
			return
		}
		existing[s.Route] = true
		extra = append(extra, s)
	}
	for _, s := range bp.Screens {
		for _, b := range s.Body {
			e := strings.Trim(b.Entity, "/")
			if e == "" {
				continue
			}
			singular := singularize(toDisplayName(e))
			switch {
			case isEntityListBlock(b) && b.Create:
				add(BlueprintScreen{
					Name:   e + "_new",
					Route:  strings.TrimRight(s.Route, "/") + "/new",
					Layout: s.Layout,
					Access: s.Access,
					Title:  "New " + singular,
					Body:   []BlueprintBlock{{Kind: "entity_create", Entity: e}},
				})
			case isEntityDetailBlock(b):
				// Detail route carries the {id}; the edit form sits beneath it.
				add(BlueprintScreen{
					Name:   e + "_edit",
					Route:  strings.TrimRight(s.Route, "/") + "/edit",
					Layout: s.Layout,
					Access: s.Access,
					Title:  "Edit " + singular,
					Body:   []BlueprintBlock{{Kind: "entity_edit", Entity: e}},
				})
			}
		}
	}
	bp.Screens = append(bp.Screens, extra...)
	return bp
}

func renderBlueprintFiles(bp Blueprint) ([]generatedFile, error) {
	return renderBlueprintFilesWithOrder(bp, 0, 0)
}

// renderBlueprintFilesWithOrder is the additive variant: entity declaration
// orders start at entityOrderOffset and authored screen orders start at
// screenOrderOffset instead of 0, so --add entities/screens continue after
// the project's existing set.
func renderBlueprintFilesWithOrder(bp Blueprint, entityOrderOffset, screenOrderOffset int) ([]generatedFile, error) {
	bp = blueprintSynthesizeCRUDScreens(bp)
	bp = blueprintExpandSeed(bp)
	var files []generatedFile
	if bp.App.Module != "" {
		files = append(files, generatedFile{name: "main.go", content: renderBlueprintMain(bp)})
	}
	if len(bp.Entities) > 0 {
		decls := make([]framework.EntityDeclaration, len(bp.Entities))
		copy(decls, bp.Entities)
		for i := range decls {
			decls[i].Endpoints = nil
		}
		entityFiles, err := renderGeneratedProjectWithOrder(decls, entityOrderOffset)
		if err != nil {
			return nil, err
		}
		for _, file := range entityFiles {
			files = append(files, generatedFile{name: filepath.Join("entities", file.name), content: file.content})
		}
	} else if bp.App.Module != "" {
		// No entities yet, but main.go imports the entities package and
		// calls entities.RegisterAll unconditionally — emit the empty seam
		// so the project compiles AND stays additive-ready: a later
		// `--add`/scaffold entity is a new file that self-registers, with
		// no edit to any owned file.
		files = append(files, generatedFile{name: filepath.Join("entities", "register.go"), content: renderRegisterSeam()})
	}
	emitsApp := bp.App.Name != "" || bp.App.Module != "" || bp.App.DBDriver != "" || bp.App.DBURL != "" || bp.App.StaticDir != "" || bp.App.OutputDir != "" || len(bp.App.Theme) > 0 || len(bp.Screens) > 0 || len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0
	// Per-screen layout: the fixed screens_register.go seam + one file per
	// authored screen + one per-entity crud file (screens + appResources).
	files = append(files, blueprintScreenFiles(bp, screenOrderOffset)...)
	if len(bp.Screens) == 0 && emitsApp {
		// Same additive-readiness for screens: app.go calls mountGenerated
		// unconditionally, so a screen-less app still ships the seam.
		files = append(files, generatedFile{name: "screens_register.go", content: blueprintScreensRegisterGo})
	}
	if len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0 || len(bp.Helpers) > 0 || len(bp.Seed) > 0 {
		files = append(files, generatedFile{name: "stubs.go", content: renderBlueprintStubs(bp)})
	}
	if emitsApp {
		files = append(files, generatedFile{name: "app.go", content: renderBlueprintApp(bp)})
	}
	if blueprintUsesEntityScreens(bp) {
		files = append(files, generatedFile{name: "resource.go", content: blueprintResourceGo})
		files = append(files, generatedFile{name: "resource_test.go", content: blueprintResourceTestGo})
	}
	if bp.App.Module != "" && len(bp.Screens) > 0 {
		files = append(files, generatedFile{name: "e2e_test.go", content: renderBlueprintE2ETest(bp)})
	}
	// Secrets never land in committed source: the generated Go reads them
	// via env (JWT_SECRET, DATABASE_URL, ADMIN_SEED_PASSWORD) and the
	// generated .env — excluded by the generated .gitignore — carries the
	// blueprint's values so the app runs out of the box.
	if env := renderBlueprintEnv(bp); env != "" {
		files = append(files, generatedFile{name: ".env", content: env})
		files = append(files, generatedFile{name: ".gitignore", content: blueprintGitignore})
	}
	// Self-hosted webfonts: fetch the theme's families at generate time so a
	// named font WORKS in a fresh app with no manual step. An offline fetch
	// drops the file (generateFromBlueprint warns with the exact path); the
	// generated app boot-checks for it too, so no silent /fonts/*.woff2 404
	// path remains.
	fontFiles, _ := blueprintFontAssets(bp)
	files = append(files, fontFiles...)
	// PWA placeholder icons: generated deterministic PNGs so a fresh
	// app.pwa blueprint is installable immediately. Replaceable — swap
	// the files under <static>/icons/ with real branding.
	files = append(files, blueprintPWAIconAssets(bp)...)
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// blueprintGitignore keeps the generated .env (and local overrides) out of
// source control. TestBlueprintNeverInlinesSecrets pins the contract.
const blueprintGitignore = `# Generated by gofastr.
.env
.env.local
`

// renderBlueprintEnv collects every blueprint value that must not be
// committed as a Go literal. Empty when the blueprint holds no secrets —
// then no .env is emitted at all.
func renderBlueprintEnv(bp Blueprint) string {
	var b strings.Builder
	if bp.App.Auth.Enabled && bp.App.Auth.JWTSecret != "" {
		b.WriteString("JWT_SECRET=" + envQuote(bp.App.Auth.JWTSecret) + "\n")
	}
	if dsnHasSecret(bp.App.DBURL) {
		b.WriteString("DATABASE_URL=" + envQuote(bp.App.DBURL) + "\n")
	}
	if bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != "" {
		b.WriteString("ADMIN_SEED_PASSWORD=" + envQuote(bp.App.Admin.SeedPassword) + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "# Generated by gofastr from your blueprint. Keep out of source\n" +
		"# control (the generated .gitignore already excludes it).\n" +
		"# Regenerating with --force rewrites this file from the blueprint.\n" +
		b.String()
}

// envQuote renders a value for the generated .env so both readers of
// that file — core/dotenv (the generated app's loader and its e2e test)
// and `gofastr pack` — read back exactly the blueprint's value. Bare
// values survive verbatim unless they lead with a quote character or
// carry edge whitespace; those are double-quoted with backslash escapes
// ('$' included, so no ${VAR} expansion fires on the way back in).
func envQuote(v string) string {
	if v == "" || (v == strings.TrimSpace(v) && v[0] != '"' && v[0] != '\'' && !strings.ContainsAny(v, "\n\r")) {
		return v
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '\\', '"', '$':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// dsnHasSecret reports whether a database DSN embeds credentials — a
// URL-form password (postgres://user:pw@host/db) or a key/value
// `password=` pair. SQLite file DSNs return false: nothing to hide.
//
// Fails CLOSED on URL-form DSNs that url.Parse rejects (e.g. a password
// containing a bad % escape): if there is a userinfo section we cannot
// prove holds no credential, treat it as secret-bearing rather than
// committing it verbatim into generated source.
func dsnHasSecret(dsn string) bool {
	if dsn == "" {
		return false
	}
	if strings.Contains(dsn, "password=") {
		return true
	}
	if u, err := url.Parse(dsn); err == nil {
		if u.User != nil {
			if _, has := u.User.Password(); has {
				return true
			}
		}
	} else if i := strings.Index(dsn, "://"); i >= 0 && strings.Contains(dsn[i+3:], "@") {
		return true
	}
	return false
}

// redactDSN strips the password from a DSN so the remainder can appear in
// committed source (host/db name are configuration, not secrets).
func redactDSN(dsn string) string {
	if !dsnHasSecret(dsn) {
		return dsn
	}
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.User(u.User.Username())
			return u.String()
		}
	}
	if i := strings.Index(dsn, "://"); i >= 0 {
		// URL-form DSN that url.Parse rejected (e.g. a bad % escape in
		// the password): strip the userinfo textually. The password may
		// itself contain '@', so cut at the last '@' before the path.
		rest := dsn[i+3:]
		end := len(rest)
		for _, sep := range []byte{'/', '?', '#'} {
			if j := strings.IndexByte(rest, sep); j >= 0 && j < end {
				end = j
			}
		}
		if at := strings.LastIndex(rest[:end], "@"); at >= 0 {
			user := rest[:at]
			if colon := strings.IndexByte(user, ':'); colon >= 0 {
				user = user[:colon]
			}
			return dsn[:i+3] + user + "@" + rest[at+1:]
		}
		return dsn
	}
	// key=value form: drop the password pair. Values may be libpq
	// single-quoted and contain spaces (password='se cret'), so split
	// quote-aware — a bare strings.Fields would leak the quoted tail.
	fields := splitDSNFields(dsn)
	kept := fields[:0]
	for _, f := range fields {
		if strings.HasPrefix(f, "password=") {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

// splitDSNFields splits a libpq key/value DSN on whitespace, keeping
// single-quoted values (with \' escapes) intact so a quoted password is
// dropped whole rather than leaking its tail.
func splitDSNFields(dsn string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for i := 0; i < len(dsn); i++ {
		c := dsn[i]
		switch {
		case escaped:
			escaped = false
			cur.WriteByte(c)
		case c == '\\' && inQuote:
			escaped = true
			cur.WriteByte(c)
		case c == '\'':
			inQuote = !inQuote
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

// blueprintResourceTestGo is the emitted unit test for the resource engine's
// formatting helpers — owned, runs under `go test`.
const blueprintResourceTestGo = `// Code generated by gofastr. Owned — safe to edit.
package main

import (
	"strings"
	"testing"
)

func TestResMoney(t *testing.T) {
	for in, want := range map[string]string{"1234.5": "$1,234.50", "99": "$99.00", "0": "$0.00", "1000000": "$1,000,000.00"} {
		if got := resMoney(in); got != want {
			t.Errorf("resMoney(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResTitle(t *testing.T) {
	if got := resTitle("past_due"); got != "Past Due" {
		t.Errorf("resTitle(past_due) = %q", got)
	}
}

func TestResCamel(t *testing.T) {
	if got := resCamel("generic_name"); got != "genericName" {
		t.Errorf("resCamel(generic_name) = %q", got)
	}
}

func TestResTruthy(t *testing.T) {
	if !resTruthy("true") || !resTruthy("1") || resTruthy("false") || resTruthy("") {
		t.Error("resTruthy mismatch")
	}
}

func TestResInputType(t *testing.T) {
	for in, want := range map[string]string{
		"decimal": "number", "int": "number", "date": "date",
		"timestamp": "datetime-local", "email": "email", "string": "text",
	} {
		if got := resInputType(in); got != want {
			t.Errorf("resInputType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResDate(t *testing.T) {
	if got := resDate(nil, "2025-01-02", "Jan 2, 2006"); got != "Jan 2, 2025" {
		t.Errorf("resDate = %q, want Jan 2, 2025", got)
	}
}

func TestResFormatEnum(t *testing.T) {
	// enum cells render as a status badge containing the humanized value.
	h := resFormat(ResField{Key: "status", Type: "enum"}, "past_due", nil)
	if !strings.Contains(string(h), "Past Due") {
		t.Errorf("enum cell missing humanized label: %s", string(h))
	}
}
`

// blueprintE2EScreenRoutes splits the blueprint's STATIC screen routes (those
// without a `{param}`) into public (anonymous-renderable) and gated (auth) sets.
// Dynamic routes are exercised by the CRUD lifecycle, which has a real id.
func blueprintE2EScreenRoutes(bp Blueprint) (public, gated []string) {
	for _, s := range bp.Screens {
		route := blueprintScreenRoutePath(s.Route)
		if strings.ContainsAny(route, "{:") {
			continue
		}
		if s.Access.Auth {
			gated = append(gated, route)
		} else {
			public = append(public, route)
		}
	}
	return public, gated
}

// blueprintCRUDTarget is the entity the generated e2e exercises with a full
// create→read→update→delete lifecycle.
type blueprintCRUDTarget struct {
	entity      string
	apiPath     string // /api/<entity>
	newRoute    string // /app/<entity>/new
	detailBase  string // /app/<entity>  (detail = detailBase + "/" + id), "" if no detail screen
	createJSON  string // a valid create payload
	updateJSON  string // a payload mutating one field
	probe       string // a value present in the created record (to find in detail/edit HTML)
	accessGated bool   // entity is access- or owner-scoped → anonymous writes must be refused
}

// blueprintE2EWritableTarget picks an entity to lifecycle-test: one whose list
// is `create: true` (so a /new form exists) and whose required fields are all
// simple scalars (no required relation), so a valid body can be synthesized.
func blueprintE2EWritableTarget(bp Blueprint) (blueprintCRUDTarget, bool) {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	createOf := map[string]string{}
	detailOf := map[string]string{}
	for _, s := range bp.Screens {
		for _, b := range s.Body {
			e := strings.Trim(b.Entity, "/")
			if isEntityListBlock(b) && b.Create {
				createOf[e] = s.Route
			}
			if isEntityDetailBlock(b) {
				detailOf[e] = s.Route
			}
		}
	}
	names := make([]string, 0, len(createOf))
	for e := range createOf {
		names = append(names, e)
	}
	sort.Strings(names)
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	for _, e := range names {
		decl, ok := entityMap[e]
		if !ok || blueprintHasRequiredRelation(decl) {
			continue
		}
		createJSON, updateJSON, probe := blueprintE2ECreateBody(decl)
		t := blueprintCRUDTarget{
			entity: e, apiPath: apiBase + "/" + e,
			newRoute:    strings.TrimRight(createOf[e], "/") + "/new",
			createJSON:  createJSON,
			updateJSON:  updateJSON,
			probe:       probe,
			accessGated: decl.Access != nil || decl.OwnerField != "",
		}
		if dr, ok := detailOf[e]; ok {
			r := dr
			if i := strings.Index(r, "/{"); i >= 0 {
				r = r[:i]
			}
			t.detailBase = r
		}
		return t, true
	}
	return blueprintCRUDTarget{}, false
}

func blueprintHasRequiredRelation(decl framework.EntityDeclaration) bool {
	for _, f := range decl.Fields {
		if f.Required && strings.EqualFold(f.Type, "relation") {
			return true
		}
	}
	return false
}

// blueprintE2ECreateBody synthesizes a valid create payload (JSON literal) for
// decl, plus an update payload mutating one string field, and a probe value that
// the created record will contain (for asserting detail/edit HTML). To stay valid
// across any schema it sends only the entity's REQUIRED fields (best-effort per
// type) — optional fields with exotic validators (json, image, file) are left
// out; the screens still exercise all-field rendering.
func blueprintE2ECreateBody(decl framework.EntityDeclaration) (createJSON, updateJSON, probe string) {
	var parts []string
	probeField := ""
	for _, f := range decl.Fields {
		if blueprintFieldSystem(f.Name) || f.Hidden || !f.Required {
			continue
		}
		var v string
		switch strings.ToLower(f.Type) {
		case "enum":
			if len(f.Values) == 0 {
				continue
			}
			v = `"` + f.Values[0] + `"`
		case "relation", "uuid":
			continue // required relations disqualify the entity upstream; uuids auto-fill
		case "bool", "boolean":
			v = "true"
		case "int", "integer":
			v = "1"
		case "float":
			v = "1"
		case "decimal":
			v = `"1"` // decimal is validated as a string
		case "date":
			v = `"2025-01-01"`
		case "timestamp", "datetime":
			v = `"2025-01-01T00:00:00Z"`
		case "json":
			v = "[]"
		case "image", "file", "blob", "binary":
			v = `"https://e2e.test/x.png"`
		default: // string, text, email, ...
			if strings.EqualFold(f.Type, "email") || strings.Contains(f.Name, "email") {
				v = `"e2e-` + f.Name + `@example.com"`
			} else {
				val := "e2e-" + f.Name
				v = `"` + val + `"`
				if probeField == "" {
					probeField, probe = f.Name, val
				}
			}
		}
		parts = append(parts, `"`+f.Name+`": `+v)
	}
	createJSON = "{" + strings.Join(parts, ", ") + "}"
	if probeField != "" {
		updateJSON = `{"` + probeField + `": "e2e-updated"}`
	} else {
		updateJSON = createJSON
	}
	return createJSON, updateJSON, probe
}

// renderBlueprintE2ETest emits an end-to-end test (package main) that builds and
// boots the generated binary, then asserts the real flow: every static public +
// gated screen renders (gated ones redirect anonymous callers and render once
// authed), a full create→read→update→delete lifecycle runs against a writable
// entity through its CRUD API + screens, and anonymous writes to an
// access-gated entity are refused.
func renderBlueprintE2ETest(bp Blueprint) string {
	appName := bp.App.Name
	if appName == "" {
		appName = "GoFastr"
	}
	public, gated := blueprintE2EScreenRoutes(bp)
	adminEmail := bp.App.Admin.SeedEmail
	adminPass := bp.App.Admin.SeedPassword
	// The cookie-jar login client (and its net/url + net/http/cookiejar imports)
	// is only emitted when there's a gated screen AND a seeded admin to log in as.
	needsAuthClient := len(gated) > 0 && adminEmail != "" && adminPass != ""
	target, hasTarget := blueprintE2EWritableTarget(bp)
	crud := hasTarget && needsAuthClient // the lifecycle needs an authed client
	// The generated e2e test boots against the blueprint's declared driver.
	// A postgres blueprint links only lib/pq, so it cannot open a SQLite
	// file DSN — provision a throwaway Postgres database instead, and skip
	// when Postgres is unreachable so driverless CI stays green-by-skip.
	dbDriver := strings.ToLower(strings.TrimSpace(bp.App.DBDriver))
	isPostgres := dbDriver == "postgres" || dbDriver == "postgresql"

	goSlice := func(ss []string) string {
		q := make([]string, len(ss))
		for i, s := range ss {
			q[i] = fmt.Sprintf("%q", s)
		}
		return "[]string{" + strings.Join(q, ", ") + "}"
	}

	var b strings.Builder
	b.WriteString("// Code generated by gofastr. Owned — safe to edit.\n")
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	imports := []string{"io", "net", "net/http", "os", "os/exec", "path/filepath", "runtime", "strings", "testing", "time"}
	if isPostgres {
		imports = append(imports, "context", "crypto/rand", "database/sql", "encoding/hex", "net/url")
	}
	if needsAuthClient {
		imports = append(imports, "net/http/cookiejar", "net/url")
	}
	if crud {
		imports = append(imports, "regexp")
	}
	seen := map[string]bool{}
	for _, imp := range imports {
		if seen[imp] {
			continue
		}
		seen[imp] = true
		b.WriteString("\t\"" + imp + "\"\n")
	}
	if needsAuthClient {
		b.WriteString("\n\t\"github.com/DonaldMurillo/gofastr/core/dotenv\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("func TestE2E(t *testing.T) {\n")
	b.WriteString("\tif testing.Short() { t.Skip(\"builds + boots the binary\") }\n")
	if needsAuthClient {
		b.WriteString("\t// The seeded admin's password lives in the generated .env, not in\n")
		b.WriteString("\t// committed source. Load it before the server child inherits the\n")
		b.WriteString("\t// environment (it needs ADMIN_SEED_PASSWORD to seed the account).\n")
		b.WriteString("\t_ = dotenv.LoadAndApply(\".env.local\", \".env\")\n")
		b.WriteString("\tadminPass := os.Getenv(\"ADMIN_SEED_PASSWORD\")\n")
		b.WriteString("\tif adminPass == \"\" {\n")
		b.WriteString("\t\t// Fresh checkout: the gitignored .env is absent. The child seeds\n")
		b.WriteString("\t\t// the admin from whatever ADMIN_SEED_PASSWORD it inherits, so a\n")
		b.WriteString("\t\t// test-local value keeps the suite self-contained.\n")
		b.WriteString("\t\tadminPass = \"e2e-seed-admin-pw\"\n")
		b.WriteString("\t\tt.Setenv(\"ADMIN_SEED_PASSWORD\", adminPass)\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("\tdir := t.TempDir()\n")
	b.WriteString("\tbin := filepath.Join(dir, \"app\")\n")
	b.WriteString("\tif runtime.GOOS == \"windows\" {\n\t\tbin += \".exe\"\n\t}\n")
	b.WriteString("\tbuild := exec.Command(\"go\", \"build\", \"-o\", bin, \".\")\n")
	b.WriteString("\tbuild.Stderr = os.Stderr\n")
	b.WriteString("\tif err := build.Run(); err != nil { t.Fatalf(\"build: %v\", err) }\n")
	b.WriteString("\taddr := e2eFreeAddr(t)\n")
	b.WriteString("\tsrv := exec.Command(bin)\n")
	b.WriteString("\tsrv.Dir = dir\n")
	if isPostgres {
		b.WriteString("\t// postgres blueprint: carve a throwaway DB from TEST_POSTGRES_DSN\n")
		b.WriteString("\t// (skips when Postgres is unreachable). DATABASE_URL + DB_DRIVER point\n")
		b.WriteString("\t// the child at the carved DB so its postgres driver opens cleanly.\n")
		b.WriteString("\tdbURL := e2ePostgresDSN(t)\n")
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=\"+dbURL, \"DB_DRIVER=postgres\")\n")
	} else {
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=file:\"+filepath.Join(dir, \"e2e.db\"))\n")
	}
	b.WriteString("\tsrv.Stdout, srv.Stderr = io.Discard, io.Discard\n")
	b.WriteString("\tif err := srv.Start(); err != nil { t.Fatalf(\"start: %v\", err) }\n")
	b.WriteString("\tt.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })\n")
	b.WriteString("\tbase := \"http://\" + addr\n")
	b.WriteString("\te2eWaitReady(t, base)\n\n")
	b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, http.DefaultClient, \"GET\", base+\"/\", \"\"); code != http.StatusOK || !strings.Contains(body, %q) {\n\t\tt.Errorf(\"home page = %%d, missing brand? %%v\", code, !strings.Contains(body, %q))\n\t}\n", appName, appName))

	// Every static public screen renders.
	b.WriteString("\n\t// Public screens render for anonymous visitors.\n")
	b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(public)))
	b.WriteString("\t\tif code, body := e2eDo(t, http.DefaultClient, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
	b.WriteString("\t\t\tt.Errorf(\"public screen %s = %d, want 200\", p, code)\n")
	b.WriteString("\t\t} else if len(body) < 120 { t.Errorf(\"public screen %s body suspiciously short (%d bytes)\", p, len(body)) }\n")
	b.WriteString("\t}\n")

	if bp.App.PWA.Enabled {
		b.WriteString("\n\t// Installable-PWA surface (app.pwa).\n")
		b.WriteString("\tif code, body := e2eDo(t, http.DefaultClient, \"GET\", base+\"/manifest.webmanifest\", \"\"); code != http.StatusOK || !strings.Contains(body, `\"name\"`) {\n")
		b.WriteString("\t\tt.Errorf(\"manifest.webmanifest = %d, want 200 with a name\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif code, body := e2eDo(t, http.DefaultClient, \"GET\", base+\"/service-worker.js\", \"\"); code != http.StatusOK || !strings.Contains(body, \"gofastr-pwa-\") {\n")
		b.WriteString("\t\tt.Errorf(\"service-worker.js = %d, want 200 with a versioned cache\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tfor _, p := range []string{\"/icons/icon-192.png\", \"/icons/icon-512.png\", \"/icons/icon-maskable.png\", \"/__gofastr/pwa/register.js\", \"/__gofastr/pwa/offline\"} {\n")
		b.WriteString("\t\tif code, _ := e2eDo(t, http.DefaultClient, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
		b.WriteString("\t\t\tt.Errorf(\"pwa asset %s = %d, want 200\", p, code)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if bp.App.LLMMD {
		b.WriteString("\n\t// Public LLM markdown (app.llm_md): the index and every screen document resolve.\n")
		b.WriteString("\tif code, body := e2eDo(t, http.DefaultClient, \"GET\", base+\"/llm-pages.md\", \"\"); code != http.StatusOK || body == \"\" {\n")
		b.WriteString("\t\tt.Errorf(\"llm-pages.md = %d, want 200\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(public)))
		b.WriteString("\t\tmd := strings.TrimRight(p, \"/\") + \"/llm.md\"\n")
		b.WriteString("\t\tif code, _ := e2eDo(t, http.DefaultClient, \"GET\", base+md, \"\"); code != http.StatusOK {\n")
		b.WriteString("\t\t\tt.Errorf(\"llm.md for %s = %d, want 200\", p, code)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if len(gated) > 0 {
		b.WriteString("\n\t// Gated screens redirect anonymous callers to the login page.\n")
		b.WriteString("\tnoRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}\n")
		b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(gated)))
		b.WriteString("\t\tif r, err := noRedir.Get(base + p); err == nil {\n")
		b.WriteString("\t\t\tr.Body.Close()\n")
		b.WriteString("\t\t\tif r.StatusCode != http.StatusSeeOther { t.Errorf(\"gated %s anon = %d, want 303 redirect\", p, r.StatusCode) }\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if needsAuthClient {
		b.WriteString("\n\t// Sign in, then every gated screen renders.\n")
		b.WriteString("\tjar, _ := cookiejar.New(nil)\n")
		b.WriteString("\tclient := &http.Client{Jar: jar}\n")
		b.WriteString(fmt.Sprintf("\tif _, err := client.PostForm(base+\"/auth/login\", url.Values{\"email\": {%q}, \"password\": {adminPass}}); err != nil { t.Fatalf(\"login: %%v\", err) }\n", adminEmail))
		b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(gated)))
		b.WriteString("\t\tif code, body := e2eDo(t, client, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
		b.WriteString("\t\t\tt.Errorf(\"authed screen %s = %d, want 200\", p, code)\n")
		b.WriteString("\t\t} else if len(body) < 120 { t.Errorf(\"authed screen %s body suspiciously short (%d bytes)\", p, len(body)) }\n")
		b.WriteString("\t}\n")
	}

	if crud {
		b.WriteString(fmt.Sprintf("\n\t// Full CRUD lifecycle for %s through its API + form screens.\n", target.entity))
		b.WriteString(fmt.Sprintf("\tcode, body := e2eDo(t, client, \"POST\", base+%q, `%s`)\n", target.apiPath, target.createJSON))
		b.WriteString(fmt.Sprintf("\tif code != http.StatusCreated { t.Fatalf(\"create %s = %%d, want 201: %%s\", code, body) }\n", target.entity))
		b.WriteString("\tid := e2eExtractID(body)\n")
		b.WriteString(fmt.Sprintf("\tif id == \"\" { t.Fatalf(\"create %s: no id in response: %%s\", body) }\n", target.entity))
		b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"GET\", base+%q, \"\"); code != http.StatusOK || !strings.Contains(body, \"<form\") {\n", target.newRoute))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"new form %s = %%d (has <form>? %%v)\", code, strings.Contains(body, \"<form\"))\n", target.entity))
		b.WriteString("\t}\n")
		if target.detailBase != "" {
			b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"GET\", base+%q+\"/\"+id, \"\"); code != http.StatusOK {\n", target.detailBase))
			b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"detail %s = %%d, want 200\", code)\n", target.entity))
			if target.probe != "" {
				b.WriteString(fmt.Sprintf("\t} else if !strings.Contains(body, %q) { t.Errorf(\"detail %s missing created value\") }\n", target.probe, target.entity))
			} else {
				b.WriteString("\t}\n")
			}
			if target.probe != "" {
				b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"GET\", base+%q+\"/\"+id+\"/edit\", \"\"); code != http.StatusOK || !strings.Contains(body, %q) {\n", target.detailBase, target.probe))
				b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"edit form %s = %%d, prefilled? %%v\", code, strings.Contains(body, %q))\n", target.entity, target.probe))
				b.WriteString("\t}\n")
			}
		}
		b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"PUT\", base+%q+\"/\"+id, `%s`); code/100 != 2 {\n", target.apiPath, target.updateJSON))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"update %s = %%d: %%s\", code, body)\n", target.entity))
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, client, \"DELETE\", base+%q+\"/\"+id, \"\"); code/100 != 2 {\n", target.apiPath))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"delete %s = %%d, want 2xx\", code)\n", target.entity))
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, client, \"GET\", base+%q+\"/\"+id, \"\"); code != http.StatusNotFound {\n", target.apiPath))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"get deleted %s = %%d, want 404\", code)\n", target.entity))
		b.WriteString("\t}\n")
		if target.accessGated {
			b.WriteString(fmt.Sprintf("\n\t// Scoping: an anonymous write to the access-/owner-scoped %s API is refused.\n", target.entity))
			b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, http.DefaultClient, \"POST\", base+%q, `%s`); code != http.StatusUnauthorized && code != http.StatusForbidden {\n", target.apiPath, target.createJSON))
			b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"anonymous write to %s = %%d, want 401/403\", code)\n", target.entity))
			b.WriteString("\t}\n")
		}
	}
	b.WriteString("}\n\n")

	// helpers
	b.WriteString("func e2eFreeAddr(t *testing.T) string {\n\tt.Helper()\n\tl, err := net.Listen(\"tcp\", \"127.0.0.1:0\")\n\tif err != nil { t.Fatal(err) }\n\tdefer l.Close()\n\treturn l.Addr().String()\n}\n\n")
	b.WriteString("func e2eWaitReady(t *testing.T, base string) {\n\tt.Helper()\n\tfor i := 0; i < 100; i++ {\n\t\tif r, err := http.Get(base + \"/\"); err == nil { r.Body.Close(); return }\n\t\ttime.Sleep(100 * time.Millisecond)\n\t}\n\tt.Fatal(\"server did not become ready\")\n}\n\n")
	b.WriteString("// e2eDo runs one request and returns the status code + body. A non-empty\n")
	b.WriteString("// body is sent as JSON. Network errors fail the test.\n")
	b.WriteString("func e2eDo(t *testing.T, client *http.Client, method, u, body string) (int, string) {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\tvar rdr io.Reader\n")
	b.WriteString("\tif body != \"\" { rdr = strings.NewReader(body) }\n")
	b.WriteString("\treq, err := http.NewRequest(method, u, rdr)\n")
	b.WriteString("\tif err != nil { t.Fatalf(\"%s %s: %v\", method, u, err) }\n")
	b.WriteString("\tif body != \"\" { req.Header.Set(\"Content-Type\", \"application/json\") }\n")
	b.WriteString("\tr, err := client.Do(req)\n")
	b.WriteString("\tif err != nil { t.Fatalf(\"%s %s: %v\", method, u, err) }\n")
	b.WriteString("\tdefer r.Body.Close()\n")
	b.WriteString("\tbb, _ := io.ReadAll(r.Body)\n")
	b.WriteString("\treturn r.StatusCode, string(bb)\n}\n")
	if crud {
		b.WriteString("\nvar e2eIDRe = regexp.MustCompile(`\"id\"\\s*:\\s*\"([^\"]+)\"`)\n\n")
		b.WriteString("func e2eExtractID(body string) string {\n\tif m := e2eIDRe.FindStringSubmatch(body); len(m) == 2 { return m[1] }\n\treturn \"\"\n}\n")
	}
	if isPostgres {
		b.WriteString("\n// e2ePostgresDSN provisions a throwaway Postgres database from an\n")
		b.WriteString("// env-provided admin DSN (TEST_POSTGRES_DSN) so the e2e flow exercises the\n")
		b.WriteString("// real postgres driver instead of a SQLite file that driver can't open. It\n")
		b.WriteString("// t.Skips when Postgres is unreachable, keeping driverless CI green-by-skip\n")
		b.WriteString("// instead of timing out with a misleading \"server did not become ready\".\n")
		b.WriteString("func e2ePostgresDSN(t *testing.T) string {\n")
		b.WriteString("\tt.Helper()\n")
		b.WriteString("\tadminDSN := os.Getenv(\"TEST_POSTGRES_DSN\")\n")
		b.WriteString("\tif adminDSN == \"\" {\n\t\tt.Skip(\"TEST_POSTGRES_DSN unset; skipping postgres e2e\")\n\t}\n")
		b.WriteString("\tadmin, err := sql.Open(\"postgres\", adminDSN)\n")
		b.WriteString("\tif err != nil {\n\t\tt.Skipf(\"postgres e2e: open admin: %v\", err)\n\t}\n")
		b.WriteString("\tprobeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)\n")
		b.WriteString("\tdefer probeCancel()\n")
		b.WriteString("\tif err := admin.PingContext(probeCtx); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Skipf(\"postgres e2e: admin unreachable: %v\", err)\n\t}\n")
		b.WriteString("\tbuf := make([]byte, 6)\n")
		b.WriteString("\tif _, err := rand.Read(buf); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Fatalf(\"postgres e2e: rand: %v\", err)\n\t}\n")
		b.WriteString("\tname := \"e2e_\" + hex.EncodeToString(buf)\n")
		b.WriteString("\tif _, err := admin.ExecContext(probeCtx, \"CREATE DATABASE \\\"\"+name+\"\\\"\"); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Skipf(\"postgres e2e: CREATE DATABASE %q failed (role needs CREATEDB): %v\", name, err)\n\t}\n")
		b.WriteString("\tt.Cleanup(func() {\n")
		b.WriteString("\t\tdctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)\n\t\tdefer dcancel()\n")
		b.WriteString("\t\t_, _ = admin.ExecContext(dctx, \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1\", name)\n")
		b.WriteString("\t\tif _, derr := admin.ExecContext(dctx, \"DROP DATABASE IF EXISTS \\\"\"+name+\"\\\"\"); derr != nil {\n\t\t\tt.Errorf(\"postgres e2e: DROP DATABASE %q: %v\", name, derr)\n\t\t}\n")
		b.WriteString("\t\t_ = admin.Close()\n")
		b.WriteString("\t})\n")
		b.WriteString("\tu, err := url.Parse(adminDSN)\n")
		b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"postgres e2e: parse admin DSN: %v\", err)\n\t}\n")
		b.WriteString("\tu.Path = \"/\" + name\n")
		b.WriteString("\treturn u.String()\n")
		b.WriteString("}\n")
	}
	return b.String()
}

// blueprintUsesEntityScreens reports whether any screen renders a data-bound
// entity block (list/detail/form), which need the emitted resource engine.
func blueprintUsesEntityScreens(bp Blueprint) bool {
	var any func([]BlueprintBlock) bool
	any = func(blocks []BlueprintBlock) bool {
		for _, b := range blocks {
			if isEntityListBlock(b) || isEntityDetailBlock(b) || isEntityFormBlock(b) {
				return true
			}
			if any(b.Children) {
				return true
			}
		}
		return false
	}
	for _, s := range bp.Screens {
		if any(s.Body) {
			return true
		}
	}
	return false
}

// blueprintResourceGo is the owned, self-contained server-render engine emitted
// into every blueprint app that has entity screens. It renders entity list and
// detail views by composing framework/ui (DataTable, PageHeader, StatusBadge,
// SearchInput, Pagination, EmptyState) over the entity's CrudHandler — humanized
// labels, formatted cells, resolved relations, server-side search/sort/paging.
// It is generated code you OWN: edit freely.
const blueprintResourceGo = `// Code generated by gofastr. Owned — safe to edit.
package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// appResources holds one ResourceConfig per entity, populated by
// RegisterGenerated once the CrudHandlers exist. Screens look entities up by
// name to render their server-side list/detail views.
var appResources = map[string]ResourceConfig{}

// ResField is one displayed entity field.
type ResField struct {
	Key    string
	Label  string
	Type   string   // string,text,int,float,decimal,bool,enum,date,timestamp,uuid,relation,...
	Values []string // enum: the allowed values (drives <option>s on the form)
}

// RelSource resolves a foreign-key column to a related record's label.
type RelSource struct {
	Crud    *framework.CrudHandler
	Display string
}

// ResFilter is one facet-filter dimension on the list screen: a column the
// user can narrow the list by. Type is "enum", "bool", or "relation" — it
// selects both the facet control (pills vs select) and how options are
// sourced (Values for enums, yes/no for bools, related rows for relations).
type ResFilter struct {
	Key    string
	Label  string
	Type   string   // "enum" | "bool" | "relation"
	Values []string // enum: the allowed values
}

// Transition is a status-change workflow action shown on a detail page — a
// button that PUTs {status: Status} to the entity, then refreshes (Mark paid).
type Transition struct {
	Label   string
	Status  string
	Variant string // "primary" | "secondary" | "danger" | "ghost" (default secondary)
	Stamp   string // optional date field stamped with today on transition
}

// RelatedList is a reverse relation surfaced on a detail page: the records of
// another entity that point back at this one via ForeignKey. Turns a detail
// page from a row editor into an account view (a customer + their invoices).
type RelatedList struct {
	Title      string // e.g. "Invoices"
	ForeignKey string // the FK column on the related entity, e.g. "customer_id"
	BasePath   string // the related entity's app route, e.g. "/app/invoices"
	Crud       *framework.CrudHandler
	Fields     []ResField
	Relations  map[string]RelSource // for resolving the related rows' own FKs
}

// ResourceConfig drives the server-rendered list + detail + form screens for
// one entity.
type ResourceConfig struct {
	Title     string
	Singular  string
	BasePath  string // app route, e.g. "/app/customers"
	APIPath   string // auto-CRUD JSON endpoint, e.g. "/api/customers"
	Crud      *framework.CrudHandler
	Fields    []ResField
	Search    string
	Filters   []ResFilter // facet filters rendered as a toolbar above the table
	PageSize  int
	Relations map[string]RelSource
	CanCreate bool // List shows "New"; a /new create form is mounted
	CanEdit   bool   // Detail shows Edit + Delete; a /{id}/edit form is mounted
	Heading     string        // overrides the list's title (the block's text:)
	EmptyText   string        // overrides the empty-state description (the block's empty_text:)
	Related     []RelatedList // reverse relations surfaced on the detail page
	Transitions []Transition  // status-transition workflow buttons on the detail page
}

// WithTransitions sets the detail-page status-transition workflow buttons.
func (c ResourceConfig) WithTransitions(ts ...Transition) ResourceConfig {
	c.Transitions = ts
	return c
}

func (c ResourceConfig) pageSize() int {
	if c.PageSize > 0 {
		return c.PageSize
	}
	return 25
}

func (c ResourceConfig) hasField(k string) bool {
	for _, f := range c.Fields {
		if f.Key == k {
			return true
		}
	}
	return false
}

func (c ResourceConfig) field(k string) (ResField, bool) {
	for _, f := range c.Fields {
		if f.Key == k {
			return f, true
		}
	}
	return ResField{}, false
}

// WithColumns returns a copy showing only the named fields, in the given order.
func (c ResourceConfig) WithColumns(keys ...string) ResourceConfig {
	fields := make([]ResField, 0, len(keys))
	for _, k := range keys {
		if f, ok := c.field(k); ok {
			fields = append(fields, f)
		}
	}
	c.Fields = fields
	return c
}

// WithSearch sets the LIKE-search field. WithLimit sets the page size.
// WithCreate shows a "New" action linking to BasePath/new.
func (c ResourceConfig) WithSearch(field string) ResourceConfig { c.Search = field; return c }
func (c ResourceConfig) WithLimit(n int) ResourceConfig         { c.PageSize = n; return c }
func (c ResourceConfig) WithCreate() ResourceConfig             { c.CanCreate = true; return c }

// WithFilters sets the facet filters shown in the toolbar above the list.
func (c ResourceConfig) WithFilters(fs ...ResFilter) ResourceConfig { c.Filters = fs; return c }

// WithEdit shows Edit + Delete on the detail screen (a /{id}/edit form is mounted).
func (c ResourceConfig) WithEdit() ResourceConfig { c.CanEdit = true; return c }

// WithHeading overrides the list's title; WithEmpty overrides the empty-state text.
func (c ResourceConfig) WithHeading(s string) ResourceConfig { c.Heading = s; return c }
func (c ResourceConfig) WithEmpty(s string) ResourceConfig   { c.EmptyText = s; return c }

func (c ResourceConfig) relationLabels(ctx context.Context) map[string]map[string]string {
	out := map[string]map[string]string{}
	for col, rel := range c.Relations {
		if rel.Crud == nil {
			continue
		}
		rows, err := rel.Crud.ListAll(ctx, framework.ListOptions{Limit: 1000})
		if err != nil {
			continue
		}
		m := map[string]string{}
		for _, r := range rows {
			id := resCell(resGet(r, "id"))
			if id == "" {
				continue
			}
			label := resCell(resGet(r, rel.Display))
			if label == "" {
				label = id
			}
			m[id] = label
		}
		out[col] = m
	}
	return out
}

// List renders the entity list screen.
func (c ResourceConfig) List(ctx context.Context) render.HTML {
	q := appui.QueryFromContext(ctx)
	page := 1
	if n, err := strconv.Atoi(q.Get("p")); err == nil && n > 1 {
		page = n
	}
	limit := c.pageSize()
	search := strings.TrimSpace(q.Get("q"))

	var filters []filter.ParsedFilter
	if search != "" && c.Search != "" {
		filters = append(filters, filter.ParsedFilter{Field: c.Search, Op: filter.OpLike, Value: search})
	}
	// Facet filters: one equality per active facet. Applied to both the count
	// and the page query, so a filtered result set paginates correctly.
	for _, ff := range c.Filters {
		if v := strings.TrimSpace(q.Get(ff.Key)); v != "" {
			filters = append(filters, filter.ParsedFilter{Field: ff.Key, Op: filter.OpEq, Value: v})
		}
	}
	var sorts []filter.ParsedSort
	sortCol := q.Get("sort")
	if sortCol != "" && c.hasField(sortCol) {
		sorts = append(sorts, filter.ParsedSort{Field: sortCol, Desc: q.Get("dir") == "desc"})
	}

	total, _ := c.Crud.CountAll(ctx, framework.ListOptions{Filters: filters})
	rows, err := c.Crud.ListAll(ctx, framework.ListOptions{Filters: filters, Sorts: sorts, Limit: limit, Offset: (page - 1) * limit})

	var actions render.HTML
	if c.CanCreate {
		actions = ui.LinkButton(ui.LinkButtonConfig{Label: "New " + c.Singular, Href: c.BasePath + "/new", Variant: ui.ButtonPrimary})
	}
	title := c.Title
	if c.Heading != "" {
		title = c.Heading
	}
	body := []render.HTML{ui.PageHeader(ui.PageHeaderConfig{Title: title, Subtitle: resCountLabel(total, c.Singular, c.Title), Actions: actions})}
	// When facets are configured, search folds into the one filter toolbar
	// (rendered below, once relation options are resolved) so the screen is a
	// single GET form. Otherwise keep the standalone search box unchanged.
	if len(c.Filters) == 0 && c.Search != "" {
		body = append(body, ui.SearchInput(ui.SearchInputConfig{
			Name: "q", ID: "search-" + c.Title, Action: c.BasePath, Method: "GET",
			Placeholder: "Search " + c.Title, ExtraAttrs: map[string]string{"value": search},
		}))
	}
	if err != nil {
		body = append(body, ui.Callout(ui.CalloutConfig{Title: "Couldn't load " + c.Title, Variant: ui.StatusDanger}, render.Text("See server logs.")))
		return render.Join(body...)
	}

	rel := c.relationLabels(ctx)
	if len(c.Filters) > 0 {
		if tb := c.filterToolbar(q, search, rel); tb != "" {
			body = append(body, tb)
		}
	}
	cols := make([]ui.Column, 0, len(c.Fields)+1)
	for _, f := range c.Fields {
		col := ui.Column{Key: f.Key, Header: f.Label, Sortable: true}
		if resNumeric(f.Type) {
			col.Align = "end"
		}
		cols = append(cols, col)
	}
	cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end"})

	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id := resCell(resGet(row, "id"))
		cells := map[string]render.HTML{}
		for _, f := range c.Fields {
			cells[f.Key] = resFormat(f, resGet(row, f.Key), rel)
		}
		cells["_a"] = ui.Link(ui.LinkConfig{Href: c.BasePath + "/" + id, Text: "View", Variant: ui.LinkAction})
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}

	// carry preserves search + active facets across sort-header and pagination
	// links (which are <a> navigations, not the toolbar form) so those actions
	// never silently drop the current filter set.
	carry := ""
	if search != "" {
		carry += "q=" + url.QueryEscape(search) + "&"
	}
	for _, ff := range c.Filters {
		if v := strings.TrimSpace(q.Get(ff.Key)); v != "" {
			carry += url.QueryEscape(ff.Key) + "=" + url.QueryEscape(v) + "&"
		}
	}
	dt := ui.DataTableConfig{
		Columns: cols, Rows: uiRows, Responsive: ui.ResponsiveCards,
		SortBy: sortCol, SortDir: ui.SortDir(q.Get("dir")),
		SortHrefPattern: "?" + carry + "sort=%s&dir=%s",
		Empty:           ui.EmptyStateConfig{Title: "No " + c.Title + " yet", Description: resEmptyDesc(c.EmptyText)},
	}
	if pages := int(math.Ceil(float64(total) / float64(limit))); pages > 1 {
		dt.Pagination = &pagination.Config{Total: pages, Current: page, HrefPattern: "?" + carry + "p=%d"}
	}
	body = append(body, ui.DataTable(dt))
	return render.Join(body...)
}

// filterToolbar builds the facet + search toolbar shown above the list. Enum
// facets render as pills when they hold a few short choices and as a select
// otherwise; bools render as Yes/No pills; relations render as a select whose
// options are the related records' display labels. Returns nil when there is
// nothing to render (e.g. only an empty relation facet and no search).
func (c ResourceConfig) filterToolbar(q url.Values, search string, rel map[string]map[string]string) render.HTML {
	facets := make([]ui.Facet, 0, len(c.Filters))
	for _, ff := range c.Filters {
		facet := ui.Facet{Name: ff.Key, Label: ff.Label, Value: q.Get(ff.Key)}
		switch ff.Type {
		case "bool", "boolean":
			facet.Options = []ui.FacetOption{{Label: "Yes", Value: "true"}, {Label: "No", Value: "false"}}
			facet.Kind = ui.FacetPills
		case "relation":
			facet.Options = resRelFacetOptions(rel[ff.Key])
			facet.Kind = ui.FacetSelect
		default: // enum
			short := len(ff.Values) > 0 && len(ff.Values) <= 4
			opts := make([]ui.FacetOption, 0, len(ff.Values))
			for _, v := range ff.Values {
				label := resTitle(v)
				if len(label) > 14 {
					short = false
				}
				opts = append(opts, ui.FacetOption{Label: label, Value: v})
			}
			facet.Options = opts
			if short {
				facet.Kind = ui.FacetPills
			} else {
				facet.Kind = ui.FacetSelect
			}
		}
		if len(facet.Options) == 0 {
			continue
		}
		facets = append(facets, facet)
	}
	cfg := ui.FilterToolbarConfig{Action: c.BasePath, Facets: facets}
	if c.Search != "" {
		cfg.Search = &ui.FilterSearch{Name: "q", Value: search, Placeholder: "Search " + c.Title, Label: "Search " + c.Title}
	}
	if len(cfg.Facets) == 0 && cfg.Search == nil {
		return ""
	}
	return ui.FilterToolbar(cfg)
}

// resRelFacetOptions turns a relation's id→label map into select options,
// ordered by label for a stable, glanceable dropdown.
func resRelFacetOptions(m map[string]string) []ui.FacetOption {
	opts := make([]ui.FacetOption, 0, len(m))
	for id, label := range m {
		opts = append(opts, ui.FacetOption{Label: label, Value: id})
	}
	sort.Slice(opts, func(i, j int) bool {
		if opts[i].Label == opts[j].Label {
			return opts[i].Value < opts[j].Value
		}
		return opts[i].Label < opts[j].Label
	})
	return opts
}

// Detail renders the single-record detail screen.
func (c ResourceConfig) Detail(ctx context.Context, id string) render.HTML {
	row, err := c.Crud.GetOne(ctx, id, nil)
	if err != nil || row == nil {
		return ui.EmptyState(ui.EmptyStateConfig{Title: "Not found", Description: "This " + c.Singular + " does not exist."})
	}
	rel := c.relationLabels(ctx)
	title := resCell(resGet(row, "name"))
	if title == "" {
		title = resCell(resGet(row, "title"))
	}
	if title == "" {
		title = c.Singular
	}
	items := make([]ui.DetailItem, 0, len(c.Fields))
	for _, f := range c.Fields {
		items = append(items, ui.DetailItem{Label: f.Label, Value: resFormat(f, resGet(row, f.Key), rel)})
	}
	actions := []render.HTML{}
	for _, t := range c.Transitions {
		body := "{\"status\":\"" + t.Status + "\""
		if t.Stamp != "" {
			body += ",\"" + t.Stamp + "\":\"" + resToday() + "\""
		}
		body += "}"
		actions = append(actions, ui.Button(ui.ButtonConfig{Label: t.Label, Variant: resButtonVariant(t.Variant), ExtraAttrs: html.Attrs{
			"data-fui-rpc":          c.APIPath + "/" + id,
			"data-fui-rpc-method":   "PUT",
			"data-fui-rpc-body":     body,
			"data-fui-rpc-navigate": c.BasePath + "/" + id,
		}}))
	}
	if c.CanEdit {
		actions = append(actions,
			ui.LinkButton(ui.LinkButtonConfig{Label: "Edit", Href: c.BasePath + "/" + id + "/edit", Variant: ui.ButtonSecondary}),
			ui.Button(ui.ButtonConfig{Label: "Delete", Variant: ui.ButtonDanger, ExtraAttrs: html.Attrs{
				"data-fui-rpc":          c.APIPath + "/" + id,
				"data-fui-rpc-method":   "DELETE",
				"data-fui-rpc-navigate": c.BasePath,
				"data-fui-confirm":      "Delete this " + c.Singular + "? This cannot be undone.",
			}}),
		)
	}
	actions = append(actions, ui.Link(ui.LinkConfig{Href: c.BasePath, Text: "← Back", Variant: ui.LinkMuted}))
	body := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{Title: title, Actions: ui.Cluster(ui.ClusterConfig{}, actions...)}),
		ui.DetailList(ui.DetailListConfig{Items: items}),
	}
	for _, rl := range c.Related {
		body = append(body, c.relatedList(ctx, rl, id))
	}
	return render.Join(body...)
}

// relatedList renders one reverse-relation section: the related entity's rows
// where ForeignKey == this record's id, as a compact table under a heading.
func (c ResourceConfig) relatedList(ctx context.Context, rl RelatedList, id string) render.HTML {
	rows, err := rl.Crud.ListAll(ctx, framework.ListOptions{
		Filters: []filter.ParsedFilter{{Field: rl.ForeignKey, Op: filter.OpEq, Value: id}},
		Limit:   10,
	})
	head := ui.PageHeader(ui.PageHeaderConfig{Title: rl.Title, Subtitle: resCountLabel(len(rows), strings.TrimSuffix(rl.Title, "s"), rl.Title)})
	if err != nil {
		return render.Join(head, ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger, Title: "Couldn't load " + rl.Title}, render.Text("See server logs.")))
	}
	if len(rows) == 0 {
		return render.Join(head, ui.EmptyState(ui.EmptyStateConfig{Title: "No " + strings.ToLower(rl.Title) + " yet", Description: "They will appear here once added."}))
	}
	relLabels := relatedRelationLabels(ctx, rl.Relations)
	cols := make([]ui.Column, 0, len(rl.Fields)+1)
	for _, f := range rl.Fields {
		col := ui.Column{Key: f.Key, Header: f.Label}
		if resNumeric(f.Type) {
			col.Align = "end"
		}
		cols = append(cols, col)
	}
	if rl.BasePath != "" {
		cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end"})
	}
	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		rid := resCell(resGet(row, "id"))
		cells := map[string]render.HTML{}
		for _, f := range rl.Fields {
			cells[f.Key] = resFormat(f, resGet(row, f.Key), relLabels)
		}
		if rl.BasePath != "" {
			cells["_a"] = ui.Link(ui.LinkConfig{Href: rl.BasePath + "/" + rid, Text: "View", Variant: ui.LinkAction})
		}
		uiRows = append(uiRows, ui.Row{ID: rid, Cells: cells})
	}
	return render.Join(head, ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: uiRows, Responsive: ui.ResponsiveCards}))
}

// relatedRelationLabels resolves the FK columns of a related entity's rows to
// display names (so an invoice row under a customer still shows plan names etc.).
func relatedRelationLabels(ctx context.Context, rels map[string]RelSource) map[string]map[string]string {
	out := map[string]map[string]string{}
	for col, rel := range rels {
		if rel.Crud == nil {
			continue
		}
		rows, err := rel.Crud.ListAll(ctx, framework.ListOptions{Limit: 1000})
		if err != nil {
			continue
		}
		m := map[string]string{}
		for _, r := range rows {
			rid := resCell(resGet(r, "id"))
			if rid == "" {
				continue
			}
			label := resCell(resGet(r, rel.Display))
			if label == "" {
				label = rid
			}
			m[rid] = label
		}
		out[col] = m
	}
	return out
}

// Form renders the create (id == "") or edit (id != "") form for one record.
// It submits as an island: data-fui-rpc posts/puts JSON to the entity's
// auto-CRUD endpoint, then SPA-navigates back to the list/detail on success.
func (c ResourceConfig) Form(ctx context.Context, id string) render.HTML {
	edit := id != ""
	var row map[string]any
	if edit {
		r, err := c.Crud.GetOne(ctx, id, nil)
		if err != nil || r == nil {
			return ui.EmptyState(ui.EmptyStateConfig{Title: "Not found", Description: "This " + c.Singular + " does not exist."})
		}
		row = r
	}
	rel := c.relationLabels(ctx)

	title, submit := "New "+c.Singular, "Create "+c.Singular
	rpc, method, back := c.APIPath, "POST", c.BasePath
	if edit {
		title, submit = "Edit "+c.Singular, "Save changes"
		rpc, method, back = c.APIPath+"/"+id, "PUT", c.BasePath+"/"+id
	}
	attrs := html.Attrs{
		"data-fui-rpc":          rpc,
		"data-fui-rpc-method":   method,
		"data-fui-rpc-navigate": back,
	}

	fields := make([]render.HTML, 0, len(c.Fields))
	for _, f := range c.Fields {
		cur := ""
		if edit {
			cur = resCell(resGet(row, f.Key))
		}
		fields = append(fields, ui.FormField(ui.FormFieldConfig{
			Label: f.Label, For: "f-" + f.Key, Input: c.formInput(ctx, f, cur, rel),
		}))
	}
	form := ui.Form(ui.FormConfig{Action: rpc, Method: "POST", SubmitLabel: submit, ExtraAttrs: attrs, Ctx: ctx}, fields...)
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: title, Actions: ui.Link(ui.LinkConfig{Href: back, Text: "← Cancel", Variant: ui.LinkMuted})}),
		form,
	)
}

// formInput builds the typed control for one field, prefilled with cur. Enums
// and relations render their options server-side; relations resolve to the same
// human label the list/detail show.
func (c ResourceConfig) formInput(ctx context.Context, f ResField, cur string, rel map[string]map[string]string) render.HTML {
	id := "f-" + f.Key
	if labels, ok := rel[f.Key]; ok {
		opts := []html.SelectOption{{Value: "", Text: "— Select —"}}
		for val, label := range labels {
			opts = append(opts, html.SelectOption{Value: val, Text: label, Selected: val == cur})
		}
		return html.Select(html.SelectConfig{Name: f.Key, ID: id, Options: opts})
	}
	switch f.Type {
	case "enum":
		opts := []html.SelectOption{{Value: "", Text: "— Select —"}}
		for _, v := range f.Values {
			opts = append(opts, html.SelectOption{Value: v, Text: resTitle(v), Selected: v == cur})
		}
		return html.Select(html.SelectConfig{Name: f.Key, ID: id, Options: opts})
	case "text":
		return html.TextArea(html.TextAreaConfig{Name: f.Key, ID: id, Content: cur, Rows: 4})
	case "bool", "boolean":
		attrs := html.Attrs{}
		if resTruthy(cur) {
			attrs["checked"] = "checked"
		}
		return html.Input(html.InputConfig{Type: "checkbox", Name: f.Key, ID: id, ExtraAttrs: attrs})
	default:
		return html.Input(html.InputConfig{Type: resInputType(f.Type), Name: f.Key, ID: id, Value: cur})
	}
}

// resInputType maps a field type to an <input type=...>.
func resInputType(t string) string {
	switch t {
	case "int", "integer", "float", "decimal":
		return "number"
	case "date":
		return "date"
	case "timestamp", "datetime":
		return "datetime-local"
	case "email":
		return "email"
	default:
		return "text"
	}
}

func resToday() string {
	return time.Now().Format("2006-01-02")
}

func resButtonVariant(v string) ui.ButtonVariant {
	switch v {
	case "primary":
		return ui.ButtonPrimary
	case "danger":
		return ui.ButtonDanger
	case "ghost":
		return ui.ButtonGhost
	default:
		return ui.ButtonSecondary
	}
}

// ----- formatting helpers ---------------------------------------------------

func resCell(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

// resGet reads a row value by snake_case key, falling back to the camelCase
// form the JSON API serializes (requires_prescription → requiresPrescription).
func resGet(row map[string]any, key string) any {
	if v, ok := row[key]; ok {
		return v
	}
	return row[resCamel(key)]
}

func resCamel(s string) string {
	var b strings.Builder
	up := false
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up {
			if r >= 'a' && r <= 'z' {
				r = r - 32
			}
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func resMuted() render.HTML {
	return ui.EmptyValue()
}

func resNumeric(t string) bool {
	switch t {
	case "int", "integer", "float", "decimal":
		return true
	}
	return false
}

func resTruthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on", "t":
		return true
	}
	return false
}

func resFormat(f ResField, raw any, rel map[string]map[string]string) render.HTML {
	val := resCell(raw)
	if labels, ok := rel[f.Key]; ok {
		if val == "" {
			return resMuted()
		}
		if l, ok := labels[val]; ok {
			return render.Text(l)
		}
		return render.Text(val)
	}
	switch f.Type {
	case "bool", "boolean":
		if resTruthy(val) {
			return ui.StatusBadge(ui.StatusBadgeConfig{Label: "Yes", Variant: ui.StatusSuccess})
		}
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: "No", Variant: ui.StatusNeutral})
	}
	if val == "" {
		return resMuted()
	}
	switch f.Type {
	case "enum":
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: resTitle(val), Variant: resEnumVariant(val)})
	case "decimal", "float":
		return render.Text(resMoney(val))
	case "date":
		return render.Text(resDate(raw, val, "Jan 2, 2006"))
	case "timestamp", "datetime":
		return render.Text(resDate(raw, val, "Jan 2, 2006 3:04 PM"))
	}
	return render.Text(val)
}

// resDate renders a date/timestamp cleanly. DB drivers hand dates back as
// time.Time (whose default String() is the noisy "2006-01-02 15:04:05 -0700
// MST"), so format those directly; fall back to parsing common string layouts,
// then to trimming the time portion off an ISO-ish string.
func resDate(raw any, val, layout string) string {
	if t, ok := raw.(time.Time); ok {
		if t.IsZero() {
			return "—"
		}
		return t.Format(layout)
	}
	for _, l := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(l, val); err == nil {
			return parsed.Format(layout)
		}
	}
	if i := strings.IndexByte(val, 'T'); i > 0 {
		return val[:i]
	}
	if i := strings.IndexByte(val, ' '); i > 0 {
		return val[:i]
	}
	return val
}

func resTitle(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "_", " "), "-", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func resEnumVariant(v string) ui.StatusVariant {
	switch strings.ToLower(v) {
	case "active", "paid", "succeeded", "completed", "open":
		return ui.StatusSuccess
	case "past_due", "pending", "trialing", "draft":
		return ui.StatusWarning
	case "canceled", "cancelled", "void", "failed", "refunded", "inactive":
		return ui.StatusNeutral
	}
	return ui.StatusInfo
}

func resMoney(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f)
	cents := int64(math.Round((f - float64(whole)) * 100))
	if cents == 100 {
		whole++
		cents = 0
	}
	ws := strconv.FormatInt(whole, 10)
	var grp []string
	for len(ws) > 3 {
		grp = append([]string{ws[len(ws)-3:]}, grp...)
		ws = ws[:len(ws)-3]
	}
	grp = append([]string{ws}, grp...)
	out := "$" + strings.Join(grp, ",") + fmt.Sprintf(".%02d", cents)
	if neg {
		out = "-" + out
	}
	return out
}

func resCountLabel(total int, singular, title string) string {
	if total == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", total, strings.ToLower(title))
}

func resEmptyDesc(custom string) string {
	if custom != "" {
		return custom
	}
	return "They will appear here once created."
}

// authError reads the ?error= code an auth redirect sets and returns a
// human message for an auth card's alert slot, or "" when there's no error. The
// auth battery redirects a failed form login back to the login page with this
// code instead of rendering a raw JSON error body.
func authError(ctx context.Context) render.HTML {
	switch appui.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		return render.Text("Invalid email or password.")
	case "credentials_required":
		return render.Text("Enter your email and password.")
	case "rate_limit":
		return render.Text("Too many attempts — please wait a moment and try again.")
	case "email_taken", "user_exists", "duplicate":
		return render.Text("That email is already registered.")
	default:
		return render.Text("Sorry, something went wrong. Please try again.")
	}
}

// ----- dashboard data binding (stat_card / charts with source) --------------

// statValue computes a single metric over an entity for a stat_card:
// agg "count" (optionally filtered "field=value") or "sum" of a numeric field.
func statValue(ctx context.Context, entity, agg, field, filterStr, format string) string {
	c, ok := appResources[entity]
	if !ok || c.Crud == nil {
		return "—"
	}
	var filters []filter.ParsedFilter
	if filterStr != "" {
		if i := strings.IndexByte(filterStr, '='); i > 0 {
			filters = append(filters, filter.ParsedFilter{Field: filterStr[:i], Op: filter.OpEq, Value: filterStr[i+1:]})
		}
	}
	if agg == "sum" {
		rows, err := c.Crud.ListAll(ctx, framework.ListOptions{Filters: filters, Limit: 100000})
		if err != nil {
			return "—"
		}
		var total float64
		for _, r := range rows {
			f, _ := strconv.ParseFloat(resCell(resGet(r, field)), 64)
			total += f
		}
		if format == "money" {
			return resMoney(strconv.FormatFloat(total, 'f', 2, 64))
		}
		return fmtNum(total)
	}
	n, err := c.Crud.CountAll(ctx, framework.ListOptions{Filters: filters})
	if err != nil {
		return "—"
	}
	return strconv.Itoa(n)
}

type kvPair struct {
	k string
	v int
}

func groupCounts(ctx context.Context, entity, groupBy string) []kvPair {
	c, ok := appResources[entity]
	if !ok || c.Crud == nil {
		return nil
	}
	rows, err := c.Crud.ListAll(ctx, framework.ListOptions{Limit: 100000})
	if err != nil {
		return nil
	}
	order := []string{}
	m := map[string]int{}
	for _, r := range rows {
		key := resCell(resGet(r, groupBy))
		if key == "" {
			key = "—"
		}
		if _, seen := m[key]; !seen {
			order = append(order, key)
		}
		m[key]++
	}
	out := make([]kvPair, 0, len(order))
	for _, k := range order {
		out = append(out, kvPair{k, m[k]})
	}
	return out
}

func groupBars(ctx context.Context, entity, groupBy string) []ui.BarChartBar {
	counts := groupCounts(ctx, entity, groupBy)
	bars := make([]ui.BarChartBar, 0, len(counts))
	for _, kv := range counts {
		bars = append(bars, ui.BarChartBar{Label: resTitle(kv.k), Value: float64(kv.v)})
	}
	return bars
}

func groupSlices(ctx context.Context, entity, groupBy string) []ui.PieSlice {
	counts := groupCounts(ctx, entity, groupBy)
	slices := make([]ui.PieSlice, 0, len(counts))
	for _, kv := range counts {
		slices = append(slices, ui.PieSlice{Label: resTitle(kv.k), Value: float64(kv.v)})
	}
	return slices
}

// lineChart renders a single-series line chart over the grouped
// counts. Fewer than two groups renders ui.LineChart's calm empty state.
func lineChart(ctx context.Context, entity, groupBy string) render.HTML {
	counts := groupCounts(ctx, entity, groupBy)
	labels := make([]string, 0, len(counts))
	values := make([]float64, 0, len(counts))
	for _, kv := range counts {
		labels = append(labels, resTitle(kv.k))
		values = append(values, float64(kv.v))
	}
	return ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{{Name: resTitle(groupBy), Values: values}},
		Labels: labels,
	})
}

func fmtNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}
`

func renderBlueprintMain(bp Blueprint) string {
	name := bp.App.Name
	if name == "" {
		name = "GoFastr"
	}
	staticDir := blueprintEffectiveStaticDir(bp)
	dbURL := bp.App.DBURL
	if dbURL == "" && len(bp.Entities) > 0 {
		dbURL = "file:gofastr.db"
	}
	driver := bp.App.DBDriver
	if driver == "" && (len(bp.Entities) > 0 || dbURL != "") {
		driver = "sqlite"
	}
	// Owned layout: module root by default (baseImport == module),
	// or a subpackage when app.output_dir / --out names one.
	outputDir := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(bp.App.OutputDir), "./"), "/")
	baseImport := strings.TrimSuffix(bp.App.Module, "/")
	if outputDir != "" && outputDir != "." {
		baseImport += "/" + outputDir
	}

	hasSeed := len(bp.Seed) > 0
	// ownerSeed: any owner_field entity + a bootstrapped admin → seed runs as
	// that admin so demo rows are owned and a fresh signup starts empty.
	ownerSeed := hasSeed && bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" &&
		bp.App.Admin.SeedPassword != "" && blueprintHasOwnerScopedEntity(bp)
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	if hasSeed {
		sb.WriteString("\t\"context\"\n")
	}
	sb.WriteString("\t\"database/sql\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"log\"\n")
	sb.WriteString("\t\"net/http\"\n")
	sb.WriteString("\t\"os\"\n")
	if hasSeed {
		sb.WriteString("\t\"strings\"\n")
	}
	sb.WriteString("\n")
	sb.WriteString("\tuiapp \"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if ownerSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n")
	}
	if bp.App.Admin.Enabled {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/admin\"\n")
	}
	sb.WriteString("\tgflog \"github.com/DonaldMurillo/gofastr/battery/log\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/dotenv\"\n")
	if ownerSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/handler\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	if hasSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/filter\"\n")
	}
	sb.WriteString("\tfwimage \"github.com/DonaldMurillo/gofastr/framework/image\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/isolation\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/uihost\"\n")
	if imp := blueprintDriverImport(driver); imp != "" {
		sb.WriteString(fmt.Sprintf("\t_ %q\n", imp))
	}
	// Unconditional: the entities seam ships even with zero entities, so a
	// later `--add`/scaffold entity registers without editing this owned file.
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("\t%q\n", baseImport+"/entities"))
	sb.WriteString(")\n\n")

	sb.WriteString("func main() {\n")
	sb.WriteString("\t// Load .env before anything reads the environment — the DB (and\n")
	sb.WriteString("\t// its DATABASE_URL) opens before NewApp's own dotenv auto-load\n")
	sb.WriteString("\t// would run. Existing process env always wins over the files.\n")
	sb.WriteString("\t_ = dotenv.LoadAndApply(\".env.local\", \".env\")\n")
	sb.WriteString("\truntimeIsolation, err := isolation.Resolve(\".\")\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tdb, err := openDB(runtimeIsolation)\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tif db != nil {\n\t\tdefer db.Close()\n\t}\n\n")
	sb.WriteString("\toptions := []framework.AppOption{\n")
	sb.WriteString("\t\tframework.WithConfig(framework.AppConfig{Name: appName, APIPrefix: apiPrefix}),\n")
	sb.WriteString("\t\t// Agent-ready MCP surface: WithMCP mounts /mcp (POST JSON-RPC +\n")
	sb.WriteString("\t\t// GET SSE) plus the discovery well-knowns (/.well-known/mcp/*);\n")
	sb.WriteString("\t\t// WithMCPIntrospection adds read-only orientation tools\n")
	sb.WriteString("\t\t// (app_routes, app_readiness, framework_docs_search, …). The\n")
	sb.WriteString("\t\t// introspection tools reveal the app's shape — remove the option\n")
	sb.WriteString("\t\t// if /mcp is reachable by untrusted callers in production.\n")
	sb.WriteString("\t\t// Under `gofastr dev` the framework additionally auto-enables the\n")
	sb.WriteString("\t\t// mutating control tools + log debug tools (opt-out:\n")
	sb.WriteString("\t\t// GOFASTR_DEV_MCP=0); add framework.WithMCPControl() here to opt a\n")
	sb.WriteString("\t\t// trusted production /mcp into runtime control.\n")
	sb.WriteString("\t\tframework.WithMCP(),\n")
	sb.WriteString("\t\tframework.WithMCPIntrospection(),\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tif db != nil {\n\t\toptions = append(options, framework.WithDB(db))\n\t}\n")
	sb.WriteString("\tfwApp := framework.NewApp(options...)\n")
	sb.WriteString("\t// Structured logging (battery/log zero-value canon): per-app file\n")
	sb.WriteString("\t// sink, access log, panic recovery, colorized dev console. Under\n")
	sb.WriteString("\t// `gofastr dev` its MCP debug tools (log_recent, log_filter,\n")
	sb.WriteString("\t// log_metrics, log_set_level) auto-register so a connected agent\n")
	sb.WriteString("\t// can read recent requests and errors; they stay OFF outside dev —\n")
	sb.WriteString("\t// access logs carry client IPs. Set EnableMCP: true here only when\n")
	sb.WriteString("\t// a production /mcp is reachable solely by trusted callers.\n")
	sb.WriteString("\tfwApp.RegisterPlugin(gflog.New(gflog.Config{}))\n")
	if fams := blueprintConfiguredFontFamilies(bp.App.Theme); len(fams) > 0 {
		// Boot-time guard: a self-hosted webfont whose file is missing from
		// the static dir would 404 and silently fall back to system fonts.
		// Warn loudly (once at startup) with the exact path so it never fails
		// silently — the generate-time fetch normally supplies these.
		sb.WriteString("\tfor _, fontFile := range []string{\n")
		for _, fam := range fams {
			sb.WriteString(fmt.Sprintf("\t\t%q,\n", blueprintFontRelPath(staticDir, fam)))
		}
		sb.WriteString("\t} {\n")
		sb.WriteString("\t\tif _, statErr := os.Stat(fontFile); statErr != nil {\n")
		sb.WriteString("\t\t\tlog.Printf(\"gofastr: webfont %s is missing — falling back to system fonts. Add the .woff2 there (the strict CSP blocks the Google CDN, so fonts must be self-hosted). See `gofastr docs blueprints`.\", fontFile)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("\tentities.RegisterAll(fwApp)\n")
	if hasSeed {
		// Apply blueprint seed data after auto-migration, in declared order
		// and idempotently: skip any entity whose table already has rows.
		// Rows go through CreateOne so validation, id generation, and
		// timestamps apply. A row that fails validation is logged and
		// skipped rather than aborting startup — sample seed data shouldn't
		// take the whole app down.
		sb.WriteString("\tfwApp.WithSeed(func(ctx context.Context) error {\n")
		if ownerSeed {
			sb.WriteString("\t\t// Seed owner-scoped rows as the bootstrap admin so the demo data\n")
			sb.WriteString("\t\t// belongs to them; a fresh signup then starts with an empty\n")
			sb.WriteString("\t\t// workspace and adds its own. CreateOne stamps the owner column\n")
			sb.WriteString("\t\t// from the user on the context.\n")
			sb.WriteString(fmt.Sprintf("\t\tif u, _, err := auth.NewEntityUserStore(db, \"auth_users\").FindByEmail(ctx, %q); err == nil && u != nil {\n", bp.App.Admin.SeedEmail))
			sb.WriteString("\t\t\tctx = handler.SetUser(ctx, u)\n")
			sb.WriteString("\t\t}\n")
		}
		sb.WriteString("\t\tfor _, s := range seedData() {\n")
		sb.WriteString("\t\t\tch, err := fwApp.CrudHandler(s.Entity)\n")
		sb.WriteString("\t\t\tif err != nil {\n\t\t\t\tcontinue\n\t\t\t}\n")
		sb.WriteString("\t\t\tif n, err := ch.CountAll(ctx, framework.ListOptions{}); err == nil && n > 0 {\n\t\t\t\tcontinue\n\t\t\t}\n")
		sb.WriteString("\t\t\tfor _, row := range s.Rows {\n")
		sb.WriteString("\t\t\t\tresolveSeedRefs(ctx, fwApp, row)\n")
		sb.WriteString("\t\t\t\tif _, err := ch.CreateOne(ctx, row); err != nil {\n")
		sb.WriteString("\t\t\t\t\tlog.Printf(\"seed %s: skipping row: %v\", s.Entity, err)\n")
		sb.WriteString("\t\t\t\t}\n")
		sb.WriteString("\t\t\t}\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\treturn nil\n")
		sb.WriteString("\t})\n")
	}
	sb.WriteString("\tsite := uiapp.NewApp(appName)\n")
	sb.WriteString("\tRegisterGenerated(fwApp, site, db)\n")
	// appBaseCSS ships first so the user's static/app.css (loaded
	// after) overrides it; it gives the generated entity blocks modern,
	// responsive defaults out of the box (scrollable tables, form rhythm).
	// Opt-in host surfaces (PWA, public LLM markdown) append to the same
	// uihost.New call. Independent by design: enabling one never emits
	// the other.
	// Always-on SEO/icon defaults: a generated app ships a favicon (derived
	// at startup from the theme's primary color — replace appIconPNG with
	// real logo bytes when there is one) and a robots.txt that keeps the
	// framework's internal endpoints out of the index while allowing
	// everything else. Explicit PWA icons (app.pwa) still win the manifest.
	extraUIOpts := ", uihost.WithAppIcon(appIconPNG())"
	extraUIOpts += `, uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}})`
	if bp.App.PWA.Enabled {
		extraUIOpts += ", " + blueprintPWAOptionLiteral(bp)
	}
	if bp.App.LLMMD {
		extraUIOpts += ", uihost.WithPublicLLMMD()"
	}
	if staticDir != "" {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithStaticDir(%q), uihost.WithCustomCSS(fontFaceCSS+appBaseCSS()+uihost.ReadCustomCSSFile(%q))%s))\n", staticDir, staticDir+"/app.css", extraUIOpts))
	} else {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithCustomCSS(fontFaceCSS+appBaseCSS())%s))\n", extraUIOpts))
	}
	if bp.App.Admin.Enabled {
		// Auto-generated back-office over every CRUD entity. Registered as a
		// battery so it Inits after Mount and discovers the UI host. Gated by
		// the admin role; unauthenticated GETs bounce to the login page.
		adminPath := bp.App.Admin.Path
		if adminPath == "" {
			adminPath = "/admin"
		}
		adminRole := bp.App.Admin.Role
		if adminRole == "" {
			adminRole = "admin"
		}
		themeArg := ""
		if len(bp.App.Theme) > 0 {
			// Hand the admin back-office the same theme tokens AND @font-face
			// rules the UI host uses, so the back-office renders coherently —
			// same colors, same fonts — with the rest of the app.
			themeArg = ", Theme: appTheme(), FontFaceCSS: fontFaceCSS"
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.RegisterBattery(admin.New(admin.Config{PathPrefix: %q, Title: appName, AdminRole: %q, LoginPath: %q, DB: db, AuditTable: \"audit_log\", AllEntities: true%s}))\n",
			adminPath, adminRole, bp.App.Admin.LoginPath, themeArg))
	}
	sb.WriteString("\taddr, err := runtimeIsolation.Addr(getEnv(\"PORT\", \"localhost:8080\"))\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\t// Banner fires via OnReady — only after auto-migrate, hooks, and the\n")
	sb.WriteString("\t// port bind all succeeded. Printing before Start would announce a\n")
	sb.WriteString("\t// server that may never come up.\n")
	sb.WriteString("\tfwApp.OnReady(func(boundAddr string) {\n\t\tfmt.Printf(\"Server running at http://%s\\n\", boundAddr)\n\t})\n")
	sb.WriteString("\tif err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("}\n\n")

	sb.WriteString("func openDB(runtimeIsolation *isolation.Runtime) (*sql.DB, error) {\n")
	if driver == "" && dbURL == "" {
		sb.WriteString("\treturn nil, nil\n")
	} else {
		sb.WriteString(fmt.Sprintf("\tdriver := getEnv(\"DB_DRIVER\", %q)\n", driver))
		// A credentialed DSN never appears as a committed fallback literal —
		// the generated .env supplies DATABASE_URL instead.
		fallbackDSN := dbURL
		if dsnHasSecret(dbURL) {
			fallbackDSN = ""
		}
		sb.WriteString(fmt.Sprintf("\tdsn := getEnv(\"DATABASE_URL\", %q)\n", fallbackDSN))
		if fallbackDSN == "" {
			sb.WriteString("\tif dsn == \"\" {\n\t\treturn nil, fmt.Errorf(\"DATABASE_URL is not set — the generated .env supplies it; copy it next to the binary or export the variable\")\n\t}\n")
		}
		sb.WriteString("\tresolvedDriver, resolvedDSN, err := runtimeIsolation.Database(driver, dsn)\n")
		sb.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		sb.WriteString("\tdriver, dsn = resolvedDriver, resolvedDSN\n")
		sb.WriteString("\tswitch driver {\n")
		sb.WriteString("\tcase \"\", \"none\":\n\t\treturn nil, nil\n")
		sb.WriteString("\tcase \"sqlite\", \"sqlite3\":\n\t\treturn sql.Open(\"sqlite3\", dsn)\n")
		sb.WriteString("\tcase \"postgres\", \"postgresql\":\n\t\treturn sql.Open(\"postgres\", dsn)\n")
		sb.WriteString("\tdefault:\n\t\treturn nil, fmt.Errorf(\"unsupported db driver %q\", driver)\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("}\n\n")
	sb.WriteString("func getEnv(key, fallback string) string {\n")
	sb.WriteString("\tif v := os.Getenv(key); v != \"\" {\n\t\treturn v\n\t}\n")
	sb.WriteString("\treturn fallback\n")
	sb.WriteString("}\n\n")
	iconFrom, iconTo := blueprintIconStops(bp)
	sb.WriteString("// appIconPNG generates the app's icon source at startup — a diagonal\n")
	sb.WriteString("// gradient in the blueprint's primary color. uihost.WithAppIcon derives\n")
	sb.WriteString("// /favicon.ico, the sized PNGs, and the head links from this one image.\n")
	sb.WriteString("// Have a real logo? Embed it and return those bytes instead:\n")
	sb.WriteString("//\n")
	sb.WriteString("//\t//go:embed logo.png\n")
	sb.WriteString("//\tvar logo []byte\n")
	sb.WriteString("func appIconPNG() []byte {\n")
	sb.WriteString(fmt.Sprintf("\timg, err := fwimage.NewGradient(512, 512, %q, %q)\n", iconFrom, iconTo))
	sb.WriteString("\tif err != nil {\n\t\treturn nil // WithAppIcon warns and skips on undecodable input\n\t}\n")
	sb.WriteString("\tb, err := img.PNG().Bytes()\n")
	sb.WriteString("\tif err != nil {\n\t\treturn nil\n\t}\n")
	sb.WriteString("\treturn b\n")
	sb.WriteString("}\n")
	if hasSeed {
		sb.WriteString("\n// resolveSeedRefs rewrites \"@entity.field=value\" reference strings in a\n")
		sb.WriteString("// seed row into the resolved primary-key id of the matching row. This lets\n")
		sb.WriteString("// relational seed data point at rows created earlier in the same pass\n")
		sb.WriteString("// (e.g. a subscription's customer_id: \"@customers.email=ada@acme.io\").\n")
		sb.WriteString("// Unresolvable references are left as-is so the create fails loudly.\n")
		sb.WriteString("func resolveSeedRefs(ctx context.Context, fwApp *framework.App, row map[string]any) {\n")
		sb.WriteString("\tfor k, v := range row {\n")
		sb.WriteString("\t\ts, ok := v.(string)\n")
		sb.WriteString("\t\tif !ok || !strings.HasPrefix(s, \"@\") {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\trest := s[1:]\n")
		sb.WriteString("\t\tdot := strings.IndexByte(rest, '.')\n")
		sb.WriteString("\t\teq := strings.IndexByte(rest, '=')\n")
		sb.WriteString("\t\tif dot < 1 || eq <= dot+1 {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\tent, field, val := rest[:dot], rest[dot+1:eq], rest[eq+1:]\n")
		sb.WriteString("\t\tch, err := fwApp.CrudHandler(ent)\n")
		sb.WriteString("\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\trows, err := ch.ListAll(ctx, framework.ListOptions{Filters: []filter.ParsedFilter{{Field: field, Op: filter.OpEq, Value: val}}, Limit: 1})\n")
		sb.WriteString("\t\tif err != nil || len(rows) == 0 {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\tif id, ok := rows[0][\"id\"]; ok {\n\t\t\trow[k] = id\n\t\t}\n")
		sb.WriteString("\t}\n")
		sb.WriteString("}\n")
	}
	return sb.String()
}

// blueprintIconStops picks the generated app icon's gradient stops from
// the blueprint theme: the primary color when it's a plain #RRGGBB hex
// (theme values may be any CSS color — oklch etc. can't feed the PNG
// generator, so those fall back to the framework's indigo), and a 45%-
// darkened variant of the from-stop for depth.
func blueprintIconStops(bp Blueprint) (string, string) {
	from := "#4338CA"
	if v, ok := bp.App.Theme["primary"]; ok && isHexRGB(v) {
		from = strings.ToUpper(v)
	}
	var r, g, b int
	fmt.Sscanf(from[1:], "%02X%02X%02X", &r, &g, &b)
	to := fmt.Sprintf("#%02X%02X%02X", r*55/100, g*55/100, b*55/100)
	return from, to
}

func isHexRGB(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func blueprintDriverImport(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "sqlite", "sqlite3":
		return "github.com/mattn/go-sqlite3"
	case "postgres", "postgresql":
		return "github.com/lib/pq"
	default:
		return ""
	}
}

func renderBlueprintScreens(bp Blueprint) string {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, decl := range bp.Entities {
		entityMap[decl.Name] = decl
	}
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	needs := blueprintScreenImports(bp)
	anyCtx := screensNeedCtx(bp.Screens)
	writeScreenImportBlock(&sb, needs, anyCtx, false, true)
	if needs.node {
		sb.WriteString("type nodeComponent struct { node uinode.Node }\n\n")
		sb.WriteString("func (c nodeComponent) Render() render.HTML { return noderender.RenderNode(c.node) }\n\n")
	}
	for _, screen := range bp.Screens {
		sb.WriteString(blueprintScreenBody(screen, entityMap, apiBase))
	}
	return sb.String()
}

// screensNeedCtx reports whether any screen in the set needs a request
// context (RenderCtx) — drives the context + component.ContextOnly imports.
func screensNeedCtx(screens []BlueprintScreen) bool {
	for _, s := range screens {
		if screenNeedsCtx(s) {
			return true
		}
	}
	return false
}

// writeScreenImportBlock writes the shared import block for a set of screens.
// When withMount is true, database/sql + framework are added (per-screen files
// carry a mount func with that signature); the aggregated test-facing emitter
// passes false to stay byte-compatible with the pre-per-screen screens.go.
func writeScreenImportBlock(sb *strings.Builder, needs screenImportNeeds, anyCtx, withMount, hasScreens bool) {
	sb.WriteString("import (\n")
	if hasScreens && anyCtx {
		sb.WriteString("\t\"context\"\n\n")
	}
	if withMount {
		sb.WriteString("\t\"database/sql\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if hasScreens && (needs.component || anyCtx) {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/component\"\n")
	}
	if hasScreens && needs.island {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/island\"\n")
	}
	if hasScreens && needs.html {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/html\"\n")
	}
	if hasScreens {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	}
	if hasScreens && needs.ui {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if hasScreens && needs.node {
		// core-ui/noderender is the first-party leaf node renderer (core-ui/html
		// + core/render + core-ui/node only). The node IR + renderer are UI
		// primitives, so the generated app depends on core-ui, never on the
		// experimental kiln namespace.
		sb.WriteString("\tnoderender \"github.com/DonaldMurillo/gofastr/core-ui/noderender\"\n")
		sb.WriteString("\tuinode \"github.com/DonaldMurillo/gofastr/core-ui/node\"\n")
	}
	if withMount {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	}
	sb.WriteString(")\n\n")
}

// blueprintScreenBody emits one screen's type declaration, Screen* methods,
// and Render/RenderCtx — the screen content that is identical whether it
// lands in its own file or in a per-entity crud file.
func blueprintScreenBody(screen BlueprintScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	var sb strings.Builder
	typeName := toCamelCase(screen.Name) + "Screen"
	ctxScreen := screenNeedsCtx(screen)
	needParams := screenNeedsParams(screen)
	hasActions := screenHasActions(screen, entityMap, apiBase)
	if ctxScreen {
		if needParams {
			sb.WriteString(fmt.Sprintf("type %s struct{ component.ContextOnly; id string }\n\n", typeName))
			sb.WriteString(fmt.Sprintf("func (s *%s) SetParams(p map[string]string) { s.id = p[\"id\"] }\n", typeName))
		} else {
			sb.WriteString(fmt.Sprintf("type %s struct{ component.ContextOnly }\n\n", typeName))
		}
	} else {
		sb.WriteString(fmt.Sprintf("type %s struct{}\n\n", typeName))
	}
	sb.WriteString(fmt.Sprintf("func (s *%s) ScreenTitle() string { return %q }\n", typeName, screen.Title))
	sb.WriteString(fmt.Sprintf("func (s *%s) ScreenDescription() string { return %q }\n", typeName, screen.Description))
	typeConst, _ := screenTypeConst(screen.Type)
	sb.WriteString(fmt.Sprintf("func (s *%s) ScreenType() app.ScreenType { return %s }\n", typeName, typeConst))
	if hasActions {
		sb.WriteString(fmt.Sprintf("func (s *%s) ComponentID() string { return %q }\n", typeName, screenActionComponentID(screen)))
		sb.WriteString(fmt.Sprintf("func (s *%s) Actions() {\n", typeName))
		for _, action := range screenActions(screen, entityMap, apiBase) {
			sb.WriteString(fmt.Sprintf("\tcomponent.On(%q, func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS(%q))\n", blueprintActionName(action), action.ClientJS))
		}
		sb.WriteString("}\n")
	}
	sb.WriteString("\n")
	renderMethod := "Render() render.HTML"
	if ctxScreen {
		renderMethod = "RenderCtx(ctx context.Context) render.HTML"
	}
	sb.WriteString(fmt.Sprintf("func (s *%s) %s {\n", typeName, renderMethod))
	rootAttrs := "nil"
	if hasActions {
		rootAttrs = fmt.Sprintf("map[string]string{\"data-component\": s.ComponentID()}")
	}
	if len(screen.Body) == 0 {
		sb.WriteString(fmt.Sprintf("\treturn render.Tag(\"div\", %s, html.Heading(html.HeadingConfig{Level: 1}, render.Text(%q)))\n", rootAttrs, screen.TitleOrName()))
	} else {
		sb.WriteString(fmt.Sprintf("\treturn render.Tag(\"div\", %s,\n", rootAttrs))
		for i, block := range screen.Body {
			var expr string
			switch {
			case ctxScreen && isEntityListBlock(block):
				expr = blueprintEntityListResourceExpr(block, entityMap)
			case ctxScreen && isEntityDetailBlock(block):
				expr = blueprintDetailExpr(block)
			case ctxScreen && isEntityCreateBlock(block):
				expr = fmt.Sprintf("appResources[%q].Form(ctx, \"\")", strings.Trim(block.Entity, "/"))
			case ctxScreen && isEntityEditBlock(block):
				expr = fmt.Sprintf("appResources[%q].Form(ctx, s.id)", strings.Trim(block.Entity, "/"))
			default:
				expr = renderBlueprintBlockForScreen(screen, block, []int{i}, entityMap, apiBase)
			}
			sb.WriteString("\t\t" + expr + ",\n")
		}
		sb.WriteString("\t)\n")
	}
	sb.WriteString("}\n\n")
	return sb.String()
}

// blueprintScreensRegisterGo is the fixed mount seam. It is byte-identical
// for any screen or entity count: it never carries a screen, entity, or app
// name. Add a screen by dropping in a new screen_<name>.go whose init()
// appends to screenRegistrars; RegisterGenerated (app.go) just calls
// mountGenerated.
const blueprintScreensRegisterGo = `package main

import (
	"database/sql"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

// screenRegistrar pairs a screen's mount func with its declaration order.
// Each screen file appends one or more screenRegistrars in init();
// mountGenerated runs them in declaration order, so the package behaves
// identically regardless of the lexical order Go runs each file's init()
// in. The order field is also how gofastr pack recovers the blueprint's
// authored screen order from source.
type screenRegistrar struct {
	order int
	fn    func(fwApp *framework.App, site *app.App, db *sql.DB)
}

var screenRegistrars []screenRegistrar

// mountGenerated mounts every generated screen with site, in declaration
// order. This file never holds a screen or entity name: add a screen by
// dropping in a new screen_<name>.go that appends to screenRegistrars in
// init(). Entity resource wiring (appResources) lives in the per-entity
// screen_<entity>_crud.go files, never here.
func mountGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	sort.SliceStable(screenRegistrars, func(i, j int) bool {
		return screenRegistrars[i].order < screenRegistrars[j].order
	})
	for _, r := range screenRegistrars {
		r.fn(fwApp, site, db)
	}
}
`

// blueprintScreenSharedGo holds nodeComponent, the shared adapter that lets a
// raw uinode.Node render as a component.Component. Emitted once (in
// screen_shared.go) when any screen renders a node tree, so no per-screen
// file redefines the type. Like the seam, it carries no screen/entity name.
const blueprintScreenSharedGo = `package main

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	noderender "github.com/DonaldMurillo/gofastr/core-ui/noderender"
	uinode "github.com/DonaldMurillo/gofastr/core-ui/node"
)

// nodeComponent adapts a raw uinode.Node so it renders as a component.
// Shared by every screen that mounts a node tree inside an island/widget.
type nodeComponent struct{ node uinode.Node }

func (c nodeComponent) Render() render.HTML { return noderender.RenderNode(c.node) }
`

// screenFileName maps a screen (or entity-crud) base name to its generated
// filename. The screen_ prefix avoids the fixed package files (main.go,
// app.go, stubs.go, resource.go, e2e_test.go, screens_register.go,
// screen_shared.go) by construction; the guard below additionally prefixes a
// name that would otherwise collide with the seam or shared helper.
func screenFileName(base string) string {
	snake := toSnakeCase(base)
	name := "screen_" + snake + ".go"
	// Guard the fixed helper files. A screen named "shared" would shadow
	// screen_shared.go (the nodeComponent adapter); re-prefix it. ("register"
	// cannot collide: the seam is screens_register.go, a different prefix.)
	if name == "screen_shared.go" {
		return "screen_screen_shared.go"
	}
	return name
}

// screenEntityRef returns the entity a CRUD screen renders (its
// entity_list/entity_detail/entity_create/entity_edit block target) and
// whether the screen is a CRUD screen at all. Non-CRUD authored screens
// (marketing, dashboards, auth forms) return ("", false) and land in their
// own screen_<snake>.go.
func screenEntityRef(s BlueprintScreen) (entity string, isCrud bool) {
	for _, b := range s.Body {
		e := strings.Trim(b.Entity, "/")
		if e == "" {
			continue
		}
		if isEntityListBlock(b) || isEntityDetailBlock(b) || isEntityCreateBlock(b) || isEntityEditBlock(b) {
			return e, true
		}
	}
	return "", false
}

// blueprintScreenLayoutExpr resolves a screen's Layout field to the
// package-level layout identifier app.go declares (appLayout / marketingLayout)
// or "nil". Layouts are package-level vars so per-screen mount funcs can
// reference them without app.go naming any screen.
func blueprintScreenLayoutExpr(screen BlueprintScreen, bp Blueprint) string {
	switch screen.Layout {
	case "marketing":
		return "marketingLayout"
	case "app":
		if len(bp.Nav) > 0 {
			return "appLayout"
		}
		return "nil"
	default:
		if len(bp.Nav) > 0 {
			return "appLayout"
		}
		return "nil"
	}
}

// blueprintScreenMountStmt emits the site.Register / site.RegisterScreen call
// for one screen. Policies (authPolicy/guestPolicy) and layouts
// (appLayout/marketingLayout) are package-level identifiers declared in app.go.
func blueprintScreenMountStmt(screen BlueprintScreen, bp Blueprint) string {
	route := blueprintScreenRoutePath(screen.Route)
	typeName := toCamelCase(screen.Name) + "Screen"
	layoutExpr := blueprintScreenLayoutExpr(screen, bp)
	guestRedirect := bp.App.Auth.Enabled && blueprintHasAuthFormScreen(bp)
	switch {
	case screen.Access.Auth:
		return fmt.Sprintf("\tsite.RegisterScreen(app.NewScreen(%q, &%s{}).WithTitle(%q).WithPolicy(authPolicy(%q, %q)), %s)", route, typeName, screen.Title, "/login", screen.Access.Role, layoutExpr)
	case guestRedirect && screenHasAuthForm(screen):
		return fmt.Sprintf("\tsite.RegisterScreen(app.NewScreen(%q, &%s{}).WithTitle(%q).WithPolicy(guestPolicy(%q)), %s)", route, typeName, screen.Title, blueprintAppHome(bp), layoutExpr)
	default:
		return fmt.Sprintf("\tsite.Register(%q, &%s{}, %s)", route, typeName, layoutExpr)
	}
}

// blueprintScreenFiles partitions bp's (already-synthesized) screens into the
// per-file generated layout: the fixed screens_register.go seam, one
// screen_<snake>.go per non-CRUD authored screen, and one
// screen_<entity>_crud.go per entity holding that entity's list/detail/form
// screens AND its appResources wiring. screenOrderOffset continues authored
// screen orders after an existing project's set (--add).
func blueprintScreenFiles(bp Blueprint, screenOrderOffset int) []generatedFile {
	if len(bp.Screens) == 0 {
		return nil
	}
	entityMap, base, needed, editable := blueprintResourceIndex(bp)
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	var files []generatedFile
	files = append(files, generatedFile{name: "screens_register.go", content: blueprintScreensRegisterGo})
	if blueprintScreenImports(bp).node {
		files = append(files, generatedFile{name: "screen_shared.go", content: blueprintScreenSharedGo})
	}
	// Authored screen order = post-synthesis index (+ offset for --add).
	screenOrder := make(map[string]int, len(bp.Screens))
	for i, s := range bp.Screens {
		screenOrder[s.Name] = i + screenOrderOffset
	}
	// Partition: CRUD screens grouped per entity (declaration order); the
	// rest are standalone.
	crudByEntity := map[string][]BlueprintScreen{}
	var crudOrder []string // entities that have ≥1 CRUD screen, first-seen order
	var standalone []BlueprintScreen
	for _, s := range bp.Screens {
		if e, ok := screenEntityRef(s); ok {
			if _, seen := crudByEntity[e]; !seen {
				crudOrder = append(crudOrder, e)
			}
			crudByEntity[e] = append(crudByEntity[e], s)
		} else {
			standalone = append(standalone, s)
		}
	}
	for _, s := range standalone {
		files = append(files, renderBlueprintStandaloneScreenFile(s, bp, entityMap, apiBase, screenOrder[s.Name]))
	}
	// One crud file per entity that needs an appResources entry: entities
	// with CRUD screens first (crudOrder), then resource-only entities
	// (sourced but screen-less) in sorted order for stable output.
	handled := map[string]bool{}
	for _, e := range crudOrder {
		files = append(files, renderBlueprintCrudFile(e, crudByEntity[e], bp, entityMap, base, editable, apiBase, screenOrder))
		handled[e] = true
	}
	var resourceOnly []string
	for e := range needed {
		if !handled[e] {
			resourceOnly = append(resourceOnly, e)
		}
	}
	sort.Strings(resourceOnly)
	for _, e := range resourceOnly {
		files = append(files, renderBlueprintCrudFile(e, nil, bp, entityMap, base, editable, apiBase, screenOrder))
	}
	return files
}

func renderBlueprintStandaloneScreenFile(screen BlueprintScreen, bp Blueprint, entityMap map[string]framework.EntityDeclaration, apiBase string, order int) generatedFile {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	needs := blueprintScreensImportNeeds([]BlueprintScreen{screen}, entityMap, apiBase)
	writeScreenImportBlock(&sb, needs, screenNeedsCtx(screen), true, true)
	sb.WriteString(blueprintScreenBody(screen, entityMap, apiBase))
	mountName := "mount" + toCamelCase(screen.Name) + "Screen"
	sb.WriteString(fmt.Sprintf("// %s mounts the %s screen with site.\nfunc %s(fwApp *framework.App, site *app.App, db *sql.DB) {\n%s\n}\n\n", mountName, screen.Name, mountName, blueprintScreenMountStmt(screen, bp)))
	sb.WriteString(fmt.Sprintf("func init() {\n\tscreenRegistrars = append(screenRegistrars, screenRegistrar{order: %d, fn: %s})\n}\n", order, mountName))
	return generatedFile{name: screenFileName(screen.Name), content: sb.String()}
}

// renderBlueprintCrudFile emits screen_<entity>_crud.go: the entity's
// list/detail/form screens (in declaration order), a mount func per screen,
// the entity's appResources wiring (inside the primary mount func — it needs
// fwApp), and one init() registering every mount func at its authored order.
// screens may be nil for a resource-only entity (sourced but screen-less):
// then the file carries just the resource wiring.
func renderBlueprintCrudFile(entity string, screens []BlueprintScreen, bp Blueprint, entityMap map[string]framework.EntityDeclaration, base map[string]string, editable map[string]bool, apiBase string, screenOrder map[string]int) generatedFile {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	needs := blueprintScreensImportNeeds(screens, entityMap, apiBase)
	writeScreenImportBlock(&sb, needs, screensNeedCtx(screens), true, len(screens) > 0)
	for _, s := range screens {
		sb.WriteString(blueprintScreenBody(s, entityMap, apiBase))
	}
	resourceStmt := blueprintResourceRegistryOne(bp, entity, entityMap, base, editable)
	// Mount funcs in authored (declaration) order; the resource wiring lands
	// in the primary (first) mount func so it runs before any screen renders.
	ordered := append([]BlueprintScreen(nil), screens...)
	sort.SliceStable(ordered, func(i, j int) bool { return screenOrder[ordered[i].Name] < screenOrder[ordered[j].Name] })
	var initLines []string
	for i, s := range ordered {
		mountName := "mount" + toCamelCase(s.Name) + "Screen"
		sb.WriteString(fmt.Sprintf("func %s(fwApp *framework.App, site *app.App, db *sql.DB) {\n", mountName))
		if i == 0 && resourceStmt != "" {
			sb.WriteString(resourceStmt)
		}
		sb.WriteString(blueprintScreenMountStmt(s, bp))
		sb.WriteString("\n}\n\n")
		initLines = append(initLines, fmt.Sprintf("\t\tscreenRegistrar{order: %d, fn: %s},", screenOrder[s.Name], mountName))
	}
	if len(ordered) == 0 && resourceStmt != "" {
		mountName := "mount" + toCamelCase(entity) + "Resource"
		sb.WriteString(fmt.Sprintf("// %s wires the %s resource (no screen mounts it; it is looked up by\n// data sources / relation labels elsewhere).\nfunc %s(fwApp *framework.App, site *app.App, db *sql.DB) {\n%s}\n\n", mountName, entity, mountName, resourceStmt))
		initLines = append(initLines, fmt.Sprintf("\t\tscreenRegistrar{order: 0, fn: %s},", mountName))
	}
	sb.WriteString("func init() {\n\tscreenRegistrars = append(screenRegistrars,\n" + strings.Join(initLines, "\n") + "\n\t)\n}\n")
	return generatedFile{name: screenFileName(entity + "_crud"), content: sb.String()}
}

// blueprintResourceRegistry emits the appResources map population inside
// RegisterGenerated: one ResourceConfig per entity referenced by a server-side
// entity_list/entity_detail screen, wired to its CrudHandler, displayable
// fields, and relation lookups.
func blueprintResourceRegistry(bp Blueprint) string {
	entityMap, base, needed, editable := blueprintResourceIndex(bp)
	if len(needed) == 0 {
		return ""
	}
	names := make([]string, 0, len(needed))
	for e := range needed {
		names = append(names, e)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, e := range names {
		sb.WriteString(blueprintResourceRegistryOne(bp, e, entityMap, base, editable))
	}
	return sb.String()
}

// blueprintResourceIndex computes the shared analysis backing the appResources
// emission: the entity map, the base route per entity, the set of entities
// that need a ResourceConfig, and which have an editable detail screen.
func blueprintResourceIndex(bp Blueprint) (entityMap map[string]framework.EntityDeclaration, base map[string]string, needed map[string]bool, editable map[string]bool) {
	entityMap = make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	// base path per entity (list route preferred; detail route minus /{id}).
	base = map[string]string{}
	needed = map[string]bool{}
	editable = map[string]bool{} // has a detail screen → Detail shows Edit/Delete
	for _, s := range bp.Screens {
		for _, b := range s.Body {
			e := strings.Trim(b.Entity, "/")
			if isEntityListBlock(b) {
				needed[e] = true
				base[e] = s.Route
			} else if isEntityDetailBlock(b) {
				needed[e] = true
				editable[e] = true
				if base[e] == "" {
					r := s.Route
					if i := strings.Index(r, "/{"); i >= 0 {
						r = r[:i]
					}
					base[e] = r
				}
			}
		}
	}
	// Entities referenced only by a data source (stat_card / *_chart
	// `source: {entity: X}`) need a ResourceConfig too, even without a
	// list/detail screen — otherwise statValue/groupCounts look X up in
	// appResources, miss, and render a silent "—". Registering a config is
	// pure lookup-map population (no route is mounted here), so this is safe.
	for src := range blueprintSourceEntities(bp) {
		if _, ok := entityMap[src]; ok {
			needed[src] = true
		}
	}
	return entityMap, base, needed, editable
}

// blueprintResourceRegistryOne emits the `appResources["E"] = ResourceConfig{…}`
// block for a single entity. Shared by the (legacy) aggregated app.go emitter
// and the per-entity screen_<entity>_crud.go file, so the wiring is identical
// regardless of where it lands. Returns "" when the entity is unknown.
func blueprintResourceRegistryOne(bp Blueprint, e string, entityMap map[string]framework.EntityDeclaration, base map[string]string, editable map[string]bool) string {
	decl, ok := entityMap[e]
	if !ok {
		return ""
	}
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	var sb strings.Builder
	sb.WriteString("\tappResources[" + fmt.Sprintf("%q", e) + "] = ResourceConfig{\n")
	sb.WriteString(fmt.Sprintf("\t\tTitle: %q, Singular: %q, BasePath: %q, APIPath: %q,\n", toDisplayName(e), singularize(toDisplayName(e)), base[e], apiBase+"/"+e))
	sb.WriteString(fmt.Sprintf("\t\tCrud: fwApp.MustCrudHandler(%q),\n", e))
	if editable[e] {
		sb.WriteString("\t\tCanEdit: true,\n")
	}
	// Fields: displayable columns (skip system + hidden).
	sb.WriteString("\t\tFields: []ResField{\n")
	for _, f := range decl.Fields {
		if blueprintFieldSystem(f.Name) || f.Hidden {
			continue
		}
		values := ""
		if strings.EqualFold(f.Type, "enum") && len(f.Values) > 0 {
			quoted := make([]string, len(f.Values))
			for i, v := range f.Values {
				quoted[i] = fmt.Sprintf("%q", v)
			}
			values = ", Values: []string{" + strings.Join(quoted, ", ") + "}"
		}
		sb.WriteString(fmt.Sprintf("\t\t\t{Key: %q, Label: %q, Type: %q%s},\n", f.Name, humanizeFieldLabel(f.Name), f.Type, values))
	}
	sb.WriteString("\t\t},\n")
	// Relations: FK column -> related crud + display field.
	rels := blueprintEntityRelations(decl)
	if len(rels) > 0 {
		sb.WriteString("\t\tRelations: map[string]RelSource{\n")
		relCols := make([]string, 0, len(rels))
		for c := range rels {
			relCols = append(relCols, c)
		}
		sort.Strings(relCols)
		for _, col := range relCols {
			target := rels[col]
			disp := "id"
			if td, ok := entityMap[target]; ok {
				disp = blueprintDisplayField(td)
			}
			sb.WriteString(fmt.Sprintf("\t\t\t%q: {Crud: fwApp.MustCrudHandler(%q), Display: %q},\n", col, target, disp))
		}
		sb.WriteString("\t\t},\n")
	}
	// Related: reverse relations — other entities that point back at this
	// one via a FK. Surfaced as tables on the detail page (account view).
	if rel := blueprintRelatedEmit(e, entityMap, base); rel != "" {
		sb.WriteString(rel)
	}
	sb.WriteString("\t}\n")
	return sb.String()
}

// blueprintFieldSystem reports auto/system columns hidden from generated UIs.
func blueprintFieldSystem(name string) bool {
	switch name {
	case "id", "created_at", "updated_at", "deleted_at", "tenant_id":
		return true
	}
	return false
}

// humanizeFieldLabel turns a column name into a header: "customer_id" →
// "Customer", "due_on" → "Due", "generic_name" → "Generic Name".
func humanizeFieldLabel(name string) string {
	s := strings.TrimSuffix(name, "_id")
	s = strings.TrimSuffix(s, "_on")
	return toDisplayName(s)
}

// blueprintEntityRelations maps each belongs_to FK column → target entity.
func blueprintEntityRelations(decl framework.EntityDeclaration) map[string]string {
	out := map[string]string{}
	for _, f := range decl.Fields {
		if strings.EqualFold(f.Type, "relation") && f.To != "" {
			out[f.Name] = f.To
		}
	}
	for _, r := range decl.Relations {
		if r.Type == fwentity.RelManyToOne && r.ForeignKey != "" && r.Entity != "" {
			out[r.ForeignKey] = r.Entity
		}
	}
	return out
}

// blueprintRelatedEmit emits the Related []RelatedList field for entity e: one
// entry per (otherEntity, fkColumn) where otherEntity.fkColumn targets e — i.e.
// the records that should appear on e's detail page as an account view.
func blueprintRelatedEmit(e string, entityMap map[string]framework.EntityDeclaration, base map[string]string) string {
	names := make([]string, 0, len(entityMap))
	for n := range entityMap {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, other := range names {
		if other == e {
			continue
		}
		od := entityMap[other]
		orels := blueprintEntityRelations(od) // fkCol -> target
		fkCols := make([]string, 0)
		for col, target := range orels {
			if target == e {
				fkCols = append(fkCols, col)
			}
		}
		sort.Strings(fkCols)
		for _, fk := range fkCols {
			b.WriteString("\t\t{\n")
			b.WriteString(fmt.Sprintf("\t\t\tTitle: %q, ForeignKey: %q, BasePath: %q,\n", toDisplayName(other), fk, base[other]))
			b.WriteString(fmt.Sprintf("\t\t\tCrud: fwApp.MustCrudHandler(%q),\n", other))
			b.WriteString("\t\t\tFields: []ResField{\n")
			shown := 0
			for _, f := range od.Fields {
				if blueprintFieldSystem(f.Name) || f.Hidden || f.Name == fk || shown >= 4 {
					continue
				}
				b.WriteString(fmt.Sprintf("\t\t\t\t{Key: %q, Label: %q, Type: %q},\n", f.Name, humanizeFieldLabel(f.Name), f.Type))
				shown++
			}
			b.WriteString("\t\t\t},\n")
			if len(orels) > 0 {
				b.WriteString("\t\t\tRelations: map[string]RelSource{\n")
				cols := make([]string, 0, len(orels))
				for c := range orels {
					cols = append(cols, c)
				}
				sort.Strings(cols)
				for _, col := range cols {
					target := orels[col]
					disp := "id"
					if td, ok := entityMap[target]; ok {
						disp = blueprintDisplayField(td)
					}
					b.WriteString(fmt.Sprintf("\t\t\t\t%q: {Crud: fwApp.MustCrudHandler(%q), Display: %q},\n", col, target, disp))
				}
				b.WriteString("\t\t\t},\n")
			}
			b.WriteString("\t\t},\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\t\tRelated: []RelatedList{\n" + b.String() + "\t\t},\n"
}

// blueprintDisplayField picks the human label column for an entity.
func blueprintDisplayField(decl framework.EntityDeclaration) string {
	for _, pref := range []string{"name", "title", "label", "email", "slug", "number", "code"} {
		for _, f := range decl.Fields {
			if f.Name == pref {
				return pref
			}
		}
	}
	for _, f := range decl.Fields {
		if !blueprintFieldSystem(f.Name) && (f.Type == "string" || f.Type == "text") {
			return f.Name
		}
	}
	return "id"
}

// singularize is a naive English singularizer for entity display names.
func singularize(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	}
	return s
}

// screenNeedsCtx reports whether a screen renders any request-time, server-side
// data block (top-level entity_list/entity_detail, or any data-bound widget with
// a `source:` anywhere in the tree) and so must implement RenderCtx.
func screenNeedsCtx(screen BlueprintScreen) bool {
	var walk func([]BlueprintBlock, bool) bool
	walk = func(blocks []BlueprintBlock, top bool) bool {
		for _, b := range blocks {
			// Any entity list/detail/form — at any nesting level — is
			// server-rendered via the resource engine, which needs the request ctx.
			if isEntityListBlock(b) || isEntityDetailBlock(b) || isEntityCreateBlock(b) || isEntityEditBlock(b) {
				return true
			}
			// Auth forms read ?error= from the request to surface a failed-login
			// message inline, so they render with the request ctx too.
			if isLoginFormBlock(b) || isSignupFormBlock(b) {
				return true
			}
			if blueprintBlockHasSource(b) {
				return true
			}
			if walk(b.Children, false) {
				return true
			}
		}
		return false
	}
	return walk(screen.Body, true)
}

// blueprintBlockHasSource reports whether a block binds to live entity data.
func blueprintBlockHasSource(b BlueprintBlock) bool {
	_, ok := b.Props["source"].(map[string]any)
	return ok
}

// blueprintSourceEntities collects every entity named by a block's
// `source: {entity: X}` across all screens (recursing into children). These
// are the entities a stat_card/chart reads live data from — they must be
// registered in appResources even when no list/detail screen exists for them.
func blueprintSourceEntities(bp Blueprint) map[string]bool {
	out := map[string]bool{}
	var walk func(blocks []BlueprintBlock)
	walk = func(blocks []BlueprintBlock) {
		for _, b := range blocks {
			if src, ok := b.Props["source"].(map[string]any); ok {
				if e, _ := src["entity"].(string); e != "" {
					out[strings.Trim(e, "/")] = true
				}
			}
			walk(b.Children)
		}
	}
	for _, s := range bp.Screens {
		walk(s.Body)
	}
	return out
}

// screenNeedsParams reports whether a screen reads a route {id} param.
func screenNeedsParams(screen BlueprintScreen) bool {
	if strings.Contains(screen.Route, "{") {
		return true
	}
	for _, b := range screen.Body {
		if isEntityDetailBlock(b) || isEntityEditBlock(b) {
			return true
		}
	}
	return false
}

// blueprintDetailExpr emits the server-side detail render call, with any
// status-transition workflow buttons chained in via WithTransitions.
func blueprintDetailExpr(block BlueprintBlock) string {
	entity := strings.Trim(block.Entity, "/")
	expr := fmt.Sprintf("appResources[%q]", entity)
	if len(block.Transitions) > 0 {
		parts := make([]string, len(block.Transitions))
		for i, t := range block.Transitions {
			parts[i] = fmt.Sprintf("Transition{Label: %q, Status: %q, Variant: %q, Stamp: %q}", t.Label, t.Status, t.Variant, t.Stamp)
		}
		expr += ".WithTransitions(" + strings.Join(parts, ", ") + ")"
	}
	return expr + ".Detail(ctx, s.id)"
}

// blueprintEntityListResourceExpr emits the server-side list render call for a
// top-level entity_list block: appResources["x"].WithColumns(...).List(ctx).
func blueprintEntityListResourceExpr(block BlueprintBlock, entityMap map[string]framework.EntityDeclaration) string {
	entity := strings.Trim(block.Entity, "/")
	expr := fmt.Sprintf("appResources[%q]", entity)
	if len(block.Fields) > 0 {
		args := make([]string, len(block.Fields))
		for i, f := range block.Fields {
			args[i] = fmt.Sprintf("%q", f)
		}
		expr += ".WithColumns(" + strings.Join(args, ", ") + ")"
	}
	if block.Search != "" {
		expr += fmt.Sprintf(".WithSearch(%q)", block.Search)
	}
	if f := blueprintEntityListFiltersExpr(block, entityMap); f != "" {
		expr += f
	}
	if block.Limit > 0 {
		expr += fmt.Sprintf(".WithLimit(%d)", block.Limit)
	}
	if block.Create {
		expr += ".WithCreate()"
	}
	if block.Text != "" {
		expr += fmt.Sprintf(".WithHeading(%q)", block.Text)
	}
	if block.EmptyText != "" {
		expr += fmt.Sprintf(".WithEmpty(%q)", block.EmptyText)
	}
	expr += ".List(ctx)"
	return expr
}

// blueprintEntityListFiltersExpr emits the `.WithFilters(ResFilter{...}, …)`
// call for a top-level entity_list block's `filters:` list. Each column is
// resolved against the entity declaration to its facet type (enum/bool/
// relation) and, for enums, its allowed values — so the emitted engine can
// render the right facet control and apply the right equality filter without
// re-reading the schema. Returns "" when the block declares no filters.
func blueprintEntityListFiltersExpr(block BlueprintBlock, entityMap map[string]framework.EntityDeclaration) string {
	if len(block.Filters) == 0 {
		return ""
	}
	decl, ok := entityMap[strings.Trim(block.Entity, "/")]
	if !ok {
		return ""
	}
	byName := map[string]framework.FieldDeclaration{}
	for _, f := range decl.Fields {
		byName[f.Name] = f
	}
	rels := blueprintEntityRelations(decl) // fk column -> target entity
	parts := make([]string, 0, len(block.Filters))
	for _, col := range block.Filters {
		f, defined := byName[col]
		ft := ""
		if defined {
			ft = strings.ToLower(strings.TrimSpace(f.Type))
		}
		kind := ""
		values := ""
		switch {
		case ft == "relation", !defined && rels[col] != "":
			kind = "relation"
		case ft == "bool" || ft == "boolean":
			kind = "bool"
		case ft == "enum":
			kind = "enum"
			if len(f.Values) > 0 {
				quoted := make([]string, len(f.Values))
				for i, v := range f.Values {
					quoted[i] = fmt.Sprintf("%q", v)
				}
				values = ", Values: []string{" + strings.Join(quoted, ", ") + "}"
			}
		default:
			continue
		}
		parts = append(parts, fmt.Sprintf("ResFilter{Key: %q, Label: %q, Type: %q%s}", col, humanizeFieldLabel(col), kind, values))
	}
	if len(parts) == 0 {
		return ""
	}
	return ".WithFilters(" + strings.Join(parts, ", ") + ")"
}

type screenImportNeeds struct {
	component bool
	island    bool
	html      bool
	node      bool
	ui        bool
}

// blueprintCatalogKind reports whether a block kind maps to a framework/ui
// catalog component (handled by renderBlueprintCatalogBlock).
func blueprintCatalogKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "page_header", "hero", "section", "card", "stat_row", "stat_card",
		"stack", "cluster", "grid", "stat_grid",
		"bar_chart", "pie_chart", "line_chart", "link_button", "callout", "divider",
		"markdown", "pricing":
		return true
	}
	return false
}

// blueprintCatalogUsesHTML reports whether a catalog block's emitted code
// references html.*. Only a hero WITH media (image/media prop) emits an
// html.Image call; a media-less hero composes ui.* only.
func blueprintCatalogUsesHTML(block BlueprintBlock) bool {
	if strings.ToLower(strings.TrimSpace(block.Kind)) != "hero" {
		return false
	}
	return blueprintProp(block, "image") != "" || blueprintProp(block, "media") != ""
}

func blueprintScreenImports(bp Blueprint) screenImportNeeds {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, decl := range bp.Entities {
		entityMap[decl.Name] = decl
	}
	return blueprintScreensImportNeeds(bp.Screens, entityMap, blueprintAPIBase(bp.App.APIPrefix))
}

// blueprintScreensImportNeeds computes the import set for an arbitrary
// subset of screens (one standalone file, one crud file's screens, or the
// whole project). It is the per-file analogue of blueprintScreenImports.
func blueprintScreensImportNeeds(screens []BlueprintScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) screenImportNeeds {
	var needs screenImportNeeds
	for _, screen := range screens {
		// Empty-body screens emit html.Heading directly (see
		// renderBlueprintStubs / the screen Render() empty path).
		if len(screen.Body) == 0 {
			needs.html = true
		}
		if screenHasActions(screen, entityMap, apiBase) {
			needs.component = true
		}
		// Walk the body: catalog blocks (framework/ui) and their children are
		// rendered via ui.*/html.*; node blocks render as a uinode tree (don't
		// recurse their needs into children); server-side entity blocks need
		// nothing from the screen.
		var scan func([]BlueprintBlock, bool)
		scan = func(blocks []BlueprintBlock, top bool) {
			for _, block := range blocks {
				kind := block.Kind
				if kind == "" {
					kind = block.Type
				}
				if isLoginFormBlock(block) || isSignupFormBlock(block) {
					// Auth forms compose ui.AuthCard + ui.Form + ui.FormField.
					needs.ui = true
					continue
				}
				if isEntityFormBlock(block) {
					// Entity forms compose ui.Form + ui.FormField + html.Attrs.
					needs.ui = true
					needs.html = true
					continue
				}
				if isEntityListBlock(block) || isEntityDetailBlock(block) || isEntityCreateBlock(block) || isEntityEditBlock(block) {
					// Server-rendered via the resource engine (appResources,
					// in-package) — no extra import beyond the ctx-screen machinery.
					continue
				}
				if blueprintCatalogKind(kind) {
					needs.ui = true
					if blueprintCatalogUsesHTML(block) {
						needs.html = true
					}
					scan(block.Children, false)
					continue
				}
				if blueprintBlockUsesNodeRenderer(block) {
					needs.node = true
					if block.Island != "" {
						needs.island = true
					}
					if block.Widget != "" {
						needs.component = true
					}
					continue
				}
				if blueprintTopLevelBlockEmitsHTML(block) {
					needs.html = true
				}
			}
		}
		scan(screen.Body, true)
	}
	return needs
}

// blueprintTopLevelBlockEmitsHTML reports whether a non-node top-level
// block is rendered via an html.* call by renderBlueprintBlockForScreen
// (heading/link types). All other plain types use render.Tag/render.Text
// and do not need the html import.
func blueprintTopLevelBlockEmitsHTML(block BlueprintBlock) bool {
	switch strings.ToLower(strings.TrimSpace(block.Type)) {
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6", "link":
		return true
	default:
		return false
	}
}

func blueprintBlockUsesNodeRenderer(block BlueprintBlock) bool {
	return block.Kind != "" || len(block.Props) > 0 || len(block.Children) > 0 || len(block.Actions) > 0 || block.Island != "" || block.Widget != ""
}

func screenHasActions(screen BlueprintScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) bool {
	return len(screenActions(screen, entityMap, apiBase)) > 0
}

func screenActions(screen BlueprintScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) []BlueprintAction {
	var actions []BlueprintAction
	var walk func([]BlueprintBlock, []int)
	walk = func(blocks []BlueprintBlock, path []int) {
		for i, block := range blocks {
			blockPath := append(append([]int(nil), path...), i)
			actions = append(actions, block.Actions...)
			switch {
			case isEntityListBlock(block), isEntityDetailBlock(block):
				// Entity lists/details are server-rendered via the resource
				// engine at every nesting level — no client island, no action.
			case isEntityFormBlock(block):
				// Only forms with relation fields need a mount action to
				// fetch <select> options; submission itself is wired via
				// data-fui-rpc on the form element (no component action).
				if blueprintFormHasRelation(block, entityMap) {
					actions = append(actions, BlueprintAction{
						Name:     blueprintEntityFormActionName(screen, block, blockPath),
						ClientJS: blueprintEntityFormClientJS(block, apiBase),
					})
				}
			}
			walk(block.Children, blockPath)
		}
	}
	walk(screen.Body, nil)
	return actions
}

// blueprintFormHasRelation reports whether the entity behind an entity_form
// block has at least one editable relation field (which renders as a
// client-populated <select>).
func blueprintFormHasRelation(block BlueprintBlock, entityMap map[string]framework.EntityDeclaration) bool {
	decl, ok := entityMap[strings.Trim(block.Entity, "/")]
	if !ok {
		return false
	}
	for _, field := range decl.Fields {
		if blueprintFormFieldSkipped(field, block.Fields) {
			continue
		}
		if strings.EqualFold(field.Type, "relation") && field.To != "" {
			return true
		}
	}
	return false
}

func screenActionComponentID(screen BlueprintScreen) string {
	id := strings.ToLower(toCamelJSON(screen.Name))
	id = strings.NewReplacer("_", "-", " ", "-", "/", "-", ":", "").Replace(id)
	id = strings.Trim(id, "-")
	if id == "" {
		id = "screen"
	}
	return "screen-" + id
}

func blueprintActionName(action BlueprintAction) string {
	name := strings.TrimSpace(action.Name)
	if name != "" {
		return name
	}
	return blueprintActionEvent(action)
}

func blueprintActionEvent(action BlueprintAction) string {
	event := strings.ToLower(strings.TrimSpace(action.Event))
	if event != "" {
		return event
	}
	return "click"
}

func (s BlueprintScreen) TitleOrName() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}

// blueprintProp reads a string prop from a block.
func blueprintProp(b BlueprintBlock, key string) string {
	if b.Props == nil {
		return ""
	}
	s, _ := b.Props[key].(string)
	return s
}

func blueprintBoolProp(b BlueprintBlock, key string) bool {
	if b.Props == nil {
		return false
	}
	value, _ := b.Props[key].(bool)
	return value
}

func blueprintGapExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return "ui.GapNone"
	case "xs":
		return "ui.GapXS"
	case "sm":
		return "ui.GapSM"
	case "lg":
		return "ui.GapLG"
	case "xl":
		return "ui.GapXL"
	case "2xl":
		return "ui.Gap2XL"
	default:
		return "ui.GapMD"
	}
}

func blueprintAlignExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "center":
		return "ui.AlignCenter"
	case "end":
		return "ui.AlignEnd"
	case "baseline":
		return "ui.AlignBaseline"
	case "stretch":
		return "ui.AlignStretch"
	default:
		return "ui.AlignStart"
	}
}

func blueprintJustifyExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "center":
		return "ui.JustifyCenter"
	case "end":
		return "ui.JustifyEnd"
	case "between":
		return "ui.JustifyBetween"
	case "around":
		return "ui.JustifyAround"
	default:
		return "ui.JustifyStart"
	}
}

// blueprintSectionChildrenAreCards reports whether every child of a section is
// a card/stat_card — the only kinds that belong in the responsive card grid.
// Mixed or non-card children flow in a left-aligned stack instead so a single
// CTA button doesn't stretch the full column width.
func blueprintSectionChildrenAreCards(children []BlueprintBlock) bool {
	if len(children) == 0 {
		return false
	}
	for _, c := range children {
		switch strings.ToLower(strings.TrimSpace(c.Kind)) {
		case "card", "stat_card":
		default:
			return false
		}
	}
	return true
}

// renderBlueprintCatalogBlock emits a framework/ui component for a catalog block
// kind (page_header, hero, section, card, stat_card, charts, …). Returns
// (expr, true) when handled; ("", false) to fall through to other paths.
func renderBlueprintCatalogBlock(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) (string, bool) {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	childExprs := func() string {
		parts := make([]string, 0, len(block.Children))
		for i, ch := range block.Children {
			cp := append(append([]int(nil), path...), i)
			parts = append(parts, renderBlueprintBlockForScreen(screen, ch, cp, entityMap, apiBase))
		}
		return strings.Join(parts, ", ")
	}
	switch kind {
	case "stack":
		cfg := fmt.Sprintf("ui.StackConfig{Gap: %s, Align: %s, Justify: %s}", blueprintGapExpr(blueprintProp(block, "gap")), blueprintAlignExpr(blueprintProp(block, "align")), blueprintJustifyExpr(blueprintProp(block, "justify")))
		children := childExprs()
		if children == "" {
			return "ui.Stack(" + cfg + ")", true
		}
		return "ui.Stack(" + cfg + ", " + children + ")", true
	case "cluster":
		cfg := fmt.Sprintf("ui.ClusterConfig{Gap: %s, Align: %s, Justify: %s, NoWrap: %t}", blueprintGapExpr(blueprintProp(block, "gap")), blueprintAlignExpr(blueprintProp(block, "align")), blueprintJustifyExpr(blueprintProp(block, "justify")), blueprintBoolProp(block, "no_wrap"))
		children := childExprs()
		if children == "" {
			return "ui.Cluster(" + cfg + ")", true
		}
		return "ui.Cluster(" + cfg + ", " + children + ")", true
	case "grid", "stat_grid":
		min := blueprintProp(block, "min")
		if min == "" {
			if kind == "stat_grid" {
				min = "12rem"
			} else {
				min = "16rem"
			}
		}
		cfg := fmt.Sprintf("ui.GridConfig{Min: %q, Gap: %s}", min, blueprintGapExpr(blueprintProp(block, "gap")))
		children := childExprs()
		if children == "" {
			return "ui.Grid(" + cfg + ")", true
		}
		return "ui.Grid(" + cfg + ", " + children + ")", true
	case "page_header":
		cfg := fmt.Sprintf("ui.PageHeaderConfig{Title: %q, Subtitle: %q, Eyebrow: %q}", blueprintProp(block, "title"), blueprintProp(block, "subtitle"), blueprintProp(block, "eyebrow"))
		return "ui.PageHeader(" + cfg + ")", true
	case "hero":
		title := blueprintProp(block, "title")
		var ctas []string
		if t := blueprintProp(block, "cta_text"); t != "" {
			ctas = append(ctas, fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %q, Href: %q, Variant: ui.ButtonPrimary})", t, blueprintProp(block, "cta_href")))
		}
		if t := blueprintProp(block, "secondary_text"); t != "" {
			ctas = append(ctas, fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %q, Href: %q, Variant: ui.ButtonSecondary})", t, blueprintProp(block, "secondary_href")))
		}
		actions := ""
		if len(ctas) > 0 {
			actions = ", Actions: []render.HTML{" + strings.Join(ctas, ", ") + "}"
		}
		image := blueprintProp(block, "image")
		if image == "" {
			image = blueprintProp(block, "media")
		}
		media := ""
		if image != "" {
			media = fmt.Sprintf(", Media: html.Image(html.ImageConfig{Src: %q, Alt: %q})", image, title)
		}
		return fmt.Sprintf("ui.Hero(ui.HeroConfig{Eyebrow: %q, Title: %q, Subtitle: %q%s%s})", blueprintProp(block, "eyebrow"), title, blueprintProp(block, "subtitle"), actions, media), true
	case "section":
		cfg := fmt.Sprintf("ui.SectionConfig{Heading: %q, Eyebrow: %q, Description: %q, Label: %q, Class: %q, ID: %q}", blueprintProp(block, "heading"), blueprintProp(block, "eyebrow"), blueprintProp(block, "description"), blueprintProp(block, "label"), blueprintProp(block, "class"), blueprintProp(block, "id"))
		if len(block.Children) > 0 {
			// Card-like children flow in a responsive ui.Grid; anything else
			// (a CTA button, prose) flows in a left-aligned ui.Stack so it keeps
			// its natural width instead of stretching across the column.
			wrap := "ui.Stack(ui.StackConfig{Align: ui.AlignStart}, " + childExprs() + ")"
			if blueprintSectionChildrenAreCards(block.Children) {
				wrap = "ui.Grid(ui.GridConfig{Min: \"16rem\"}, " + childExprs() + ")"
			}
			return "ui.Section(" + cfg + ", " + wrap + ")", true
		}
		return "ui.Section(" + cfg + ")", true
	case "card":
		cfg := fmt.Sprintf("ui.CardConfig{Heading: %q, Description: %q}", blueprintProp(block, "heading"), blueprintProp(block, "text"))
		if len(block.Children) > 0 {
			return "ui.Card(" + cfg + ", " + childExprs() + ")", true
		}
		return "ui.Card(" + cfg + ")", true
	case "stat_row":
		return "ui.Grid(ui.GridConfig{Min: \"12rem\"}, " + childExprs() + ")", true
	case "stat_card":
		label := blueprintProp(block, "label")
		if src, ok := block.Props["source"].(map[string]any); ok {
			entity, _ := src["entity"].(string)
			agg, _ := src["agg"].(string)
			field, _ := src["field"].(string)
			filter, _ := src["filter"].(string)
			format := blueprintProp(block, "format")
			return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %q, Value: statValue(ctx, %q, %q, %q, %q, %q)})", label, entity, agg, field, filter, format), true
		}
		return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %q, Value: %q})", label, blueprintProp(block, "value")), true
	case "bar_chart", "pie_chart", "line_chart":
		title := blueprintProp(block, "title")
		if src, ok := block.Props["source"].(map[string]any); ok {
			entity, _ := src["entity"].(string)
			groupBy, _ := src["group_by"].(string)
			var chart string
			switch kind {
			case "pie_chart":
				chart = fmt.Sprintf("ui.PieChart(ui.PieChartConfig{Slices: groupSlices(ctx, %q, %q)})", entity, groupBy)
			case "line_chart":
				chart = fmt.Sprintf("lineChart(ctx, %q, %q)", entity, groupBy)
			default:
				chart = fmt.Sprintf("ui.BarChart(ui.BarChartConfig{Bars: groupBars(ctx, %q, %q), ShowLabels: true})", entity, groupBy)
			}
			// A titled chart is a Card with a heading — design-system
			// composition, zero bespoke classes (Hard rule 7).
			if title != "" {
				return fmt.Sprintf("ui.Card(ui.CardConfig{Heading: %q}, %s)", title, chart), true
			}
			return chart, true
		}
		return "", false
	case "link_button":
		v := "ui.ButtonPrimary"
		switch blueprintProp(block, "variant") {
		case "secondary":
			v = "ui.ButtonSecondary"
		case "ghost":
			v = "ui.ButtonGhost"
		}
		return fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %q, Href: %q, Variant: %s})", blueprintProp(block, "label"), blueprintProp(block, "href"), v), true
	case "callout":
		return fmt.Sprintf("ui.Callout(ui.CalloutConfig{Title: %q}, render.Text(%q))", blueprintProp(block, "title"), block.Text), true
	case "divider":
		return "ui.Divider(ui.DividerConfig{})", true
	case "markdown":
		return fmt.Sprintf("ui.Markdown(ui.MarkdownConfig{Source: %q})", block.Text), true
	case "pricing":
		plans, _ := block.Props["plans"].([]any)
		cards := make([]string, 0, len(plans))
		for _, p := range plans {
			if pm, ok := p.(map[string]any); ok {
				cards = append(cards, blueprintPricingCardExpr(pm))
			}
		}
		return "ui.Grid(ui.GridConfig{Min: \"16rem\"}, " + strings.Join(cards, ", ") + ")", true
	}
	return "", false
}

// blueprintPricingCardExpr emits a ui.PricingCard call from a plan map.
func blueprintPricingCardExpr(p map[string]any) string {
	s := func(k string) string { v, _ := p[k].(string); return v }
	feats := ""
	if fl, ok := p["features"].([]any); ok && len(fl) > 0 {
		qs := make([]string, 0, len(fl))
		for _, f := range fl {
			if fs, ok := f.(string); ok {
				qs = append(qs, fmt.Sprintf("%q", fs))
			}
		}
		feats = ", Features: []string{" + strings.Join(qs, ", ") + "}"
	}
	featured := ""
	if b, _ := p["featured"].(bool); b {
		featured = ", Featured: true"
	}
	return fmt.Sprintf("ui.PricingCard(ui.PricingCardConfig{Name: %q, Price: %q, Period: %q, Description: %q%s, CTALabel: %q, CTAHref: %q%s})",
		s("name"), s("price"), s("period"), s("description"), feats, s("cta_text"), s("cta_href"), featured)
}

func renderBlueprintBlockForScreen(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "login_form":
		return renderBlueprintLoginFormExpr(block)
	case "signup_form":
		return renderBlueprintSignupFormExpr(block)
	case "entity_form":
		// Island form composed from framework components (no ctx needed).
		return blueprintEntityFormExpr(screen, block, path, entityMap, apiBase)
	case "entity_detail":
		// Server-rendered via the resource engine (s.id from the route param).
		return blueprintDetailExpr(block)
	}
	if isEntityListBlock(block) {
		// Server-rendered via the resource engine (ui.DataTable).
		return blueprintEntityListResourceExpr(block, entityMap)
	}
	if expr, ok := renderBlueprintCatalogBlock(screen, block, path, entityMap, apiBase); ok {
		return expr
	}
	if blueprintBlockUsesNodeRenderer(block) {
		expr := renderBlueprintNodeExpressionForScreen(screen, block, path, entityMap, apiBase)
		if block.Island != "" {
			return fmt.Sprintf("island.NewIsland(%q, nodeComponent{node: %s}).Render()", block.Island, expr)
		}
		if block.Widget != "" {
			return fmt.Sprintf("component.NewWidget(%q, nodeComponent{node: %s}).Render()", block.Widget, expr)
		}
		return "noderender.RenderNode(" + expr + ")"
	}
	attrs := "nil"
	if block.Class != "" {
		attrs = fmt.Sprintf("map[string]string{\"class\": %q}", block.Class)
	}
	switch strings.ToLower(block.Type) {
	case "", "text", "p", "paragraph":
		return fmt.Sprintf("render.Tag(\"p\", %s, render.Text(%q))", attrs, block.Text)
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6":
		level := block.Level
		if level == 0 {
			switch strings.ToLower(block.Type) {
			case "h2":
				level = 2
			case "h3":
				level = 3
			case "h4":
				level = 4
			case "h5":
				level = 5
			case "h6":
				level = 6
			default:
				level = 1
			}
		}
		return fmt.Sprintf("html.Heading(html.HeadingConfig{Level: %d, Class: %q}, render.Text(%q))", level, block.Class, block.Text)
	case "link":
		return fmt.Sprintf("html.Link(html.LinkConfig{Href: %q, Text: %q, Class: %q})", block.Href, block.Text, block.Class)
	case "section":
		return fmt.Sprintf("render.Tag(\"section\", %s, render.Text(%q))", attrs, block.Text)
	default:
		return fmt.Sprintf("render.Tag(\"div\", %s, render.Text(%q))", attrs, block.Text)
	}
}

func renderBlueprintNodeExpression(block BlueprintBlock) string {
	return renderBlueprintNodeExpressionForScreen(BlueprintScreen{}, block, nil, nil, "")
}

// blueprintChildNodeExpr returns the uinode.Node literal for a child block.
// Entity blocks (list/detail/form) are framework UI components, not nodes, so
// they're rendered by renderBlueprintBlockForScreen (in ui-component containers
// like ui.Section), never inside a raw node tree.
func blueprintChildNodeExpr(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	return renderBlueprintNodeExpressionForScreen(screen, block, path, entityMap, apiBase)
}

func renderBlueprintNodeExpressionForScreen(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	if kind == "" {
		kind = "div"
	}
	props := map[string]any{}
	for k, v := range block.Props {
		props[k] = v
	}
	if block.Text != "" {
		props["text"] = block.Text
	}
	if block.Class != "" {
		props["class"] = block.Class
	}
	if block.Href != "" {
		props["href"] = block.Href
	}
	if block.Level != 0 {
		props["level"] = int64(block.Level)
	}
	if len(block.Actions) > 0 {
		for i, action := range block.Actions {
			event := blueprintActionEvent(action)
			name := blueprintActionName(action)
			if i == 0 {
				if _, ok := props["data-action"]; !ok {
					props["data-action"] = name
				}
				switch event {
				case "input", "change", "submit":
					if _, ok := props["data-action-type"]; !ok {
						props["data-action-type"] = event
					}
				}
				continue
			}
			if _, ok := props["data-action-"+event]; !ok {
				props["data-action-"+event] = name
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("uinode.Node{")
	sb.WriteString(fmt.Sprintf("Kind: %q", kind))
	if len(props) > 0 {
		literal, err := renderGoLiteral(props)
		if err == nil {
			sb.WriteString(", Props: " + literal)
		}
	}
	if len(block.Children) > 0 {
		sb.WriteString(", Children: []uinode.Node{")
		for i, child := range block.Children {
			if i > 0 {
				sb.WriteString(", ")
			}
			childPath := append(append([]int(nil), path...), i)
			sb.WriteString(blueprintChildNodeExpr(screen, child, childPath, entityMap, apiBase))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

func isEntityListBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "entity_list")
}

func isEntityFormBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "entity_form")
}

func isEntityDetailBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "entity_detail")
}

// entity_create / entity_edit are synthesized (never authored) by
// blueprintSynthesizeCRUDScreens for the /new and /{id}/edit form screens.
// They render via the resource engine's Form(ctx, id).
func isEntityCreateBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "entity_create")
}

func isEntityEditBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "entity_edit")
}

func isLoginFormBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "login_form")
}

func isSignupFormBlock(block BlueprintBlock) bool {
	return blueprintBlockKindIs(block, "signup_form")
}

// blueprintAuthFormExpr composes a ui.AuthCard around a ui.Form for the auth
// surfaces — the framework owns the card/centering/field CSS, so this emits
// zero bespoke styling. The form posts urlencoded to the auth battery's POST
// <action> handler, which sets the session cookie and 303-redirects to ?next=,
// so it works with no JavaScript.
func blueprintAuthFormExpr(heading, action, next, submitLabel, pwAutocomplete, pwExtra, footerHref, footerText string) string {
	e := htmlEscapeJSString
	hidden := `<input type="hidden" name="next" value="` + e(next) + `">`
	emailInput := `<input id="auth-email" name="email" type="email" autocomplete="email" required>`
	pwInput := `<input id="auth-password" name="password" type="password" autocomplete="` + pwAutocomplete + `" required` + pwExtra + `>`
	form := fmt.Sprintf(
		"ui.Form(ui.FormConfig{Action: %q, Method: \"POST\", SubmitLabel: %q}, render.Raw(%q), "+
			"ui.FormField(ui.FormFieldConfig{Label: \"Email\", For: \"auth-email\", Required: true, Input: render.Raw(%q)}), "+
			"ui.FormField(ui.FormFieldConfig{Label: \"Password\", For: \"auth-password\", Required: true, Input: render.Raw(%q)}))",
		action, submitLabel, hidden, emailInput, pwInput)
	footer := ""
	if footerHref != "" {
		footer = fmt.Sprintf(", Footer: render.Raw(%q)", `<a href="`+e(footerHref)+`">`+e(footerText)+`</a>`)
	}
	// Auth screens render with the request ctx (screenNeedsCtx), so the card
	// surfaces a failed-login / duplicate-email message inline from ?error=.
	return fmt.Sprintf("ui.AuthCard(ui.AuthCardConfig{Title: %q, Alert: authError(ctx), Body: %s%s})", heading, form, footer)
}

// renderBlueprintSignupFormExpr emits the registration form (posts to the auth
// battery's register endpoint, 303-redirects to ?next= on success).
func renderBlueprintSignupFormExpr(block BlueprintBlock) string {
	propStr := func(k string) string { s, _ := block.Props[k].(string); return s }
	action := propStr("action")
	if action == "" {
		action = "/auth/register"
	}
	next := propStr("next")
	if next == "" {
		next = "/"
	}
	heading := block.Text
	if heading == "" {
		heading = "Create your account"
	}
	return blueprintAuthFormExpr(heading, action, next, "Create account", "new-password", ` minlength="8"`, propStr("login_href"), "Already have an account? Sign in")
}

// renderBlueprintLoginFormExpr emits the login form. props: action (default
// /auth/login), next (post-login redirect, default /), register_href (optional).
func renderBlueprintLoginFormExpr(block BlueprintBlock) string {
	propStr := func(k string) string { s, _ := block.Props[k].(string); return s }
	action := propStr("action")
	if action == "" {
		action = "/auth/login"
	}
	next := propStr("next")
	if next == "" {
		next = "/"
	}
	heading := block.Text
	if heading == "" {
		heading = "Sign in"
	}
	return blueprintAuthFormExpr(heading, action, next, "Sign in", "current-password", "", propStr("register_href"), "Create an account")
}

func blueprintBlockKindIs(block BlueprintBlock, want string) bool {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	return strings.EqualFold(strings.TrimSpace(kind), want)
}

// blueprintScreenRoutePath converts a blueprint screen route's framework-style
// path params ("/patients/{id}") to the colon style the core-ui screen router
// matches ("/patients/:id"). Static routes pass through unchanged.
func blueprintScreenRoutePath(route string) string {
	if !strings.Contains(route, "{") {
		return route
	}
	segs := strings.Split(route, "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			segs[i] = ":" + strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
		}
	}
	return strings.Join(segs, "/")
}

// blueprintAPIBase returns the URL path prefix entity CRUD mounts at —
// "/api" for prefix "api", or "" for an empty prefix (bare /{table}).
// Mirrors framework (*App).apiPrefix so generated client fetches/posts hit
// the same path the entity routes are registered at.
func blueprintAPIBase(apiPrefix string) string {
	p := strings.Trim(apiPrefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

func renderBlueprintEntityListNodeExpression(screen BlueprintScreen, block BlueprintBlock, path []int) string {
	entity := strings.Trim(block.Entity, "/")
	limit := block.Limit
	if limit == 0 {
		limit = 20
	}
	emptyText := block.EmptyText
	if emptyText == "" {
		emptyText = "No records."
	}
	actionName := blueprintEntityListActionName(screen, block, path)
	props := map[string]any{
		"class":             "gofastr-entity-list",
		"data-entity-list":  entity,
		"data-action-mount": actionName, // auto-fetch rows on hydration
	}
	var children []string
	title := block.Text
	if title == "" {
		title = entity
	}
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind: "heading",
		Props: map[string]any{
			"level": int64(2),
			"text":  title,
		},
	}))
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind: "button",
		Props: map[string]any{
			"type":                     "button",
			"text":                     "Refresh",
			"data-action":              actionName,
			"data-entity-list-refresh": entity,
			"data-param-entity":        entity,
			"data-param-limit":         int64(limit),
			"data-param-empty-text":    emptyText,
			"aria-label":               "Refresh " + entity,
		},
	}))
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind: "div",
		Props: map[string]any{
			"data-entity-list-body": true,
			"text":                  emptyText,
		},
	}))
	literal, err := renderGoLiteral(props)
	if err != nil {
		literal = "nil"
	}
	return "uinode.Node{Kind: \"section\", Props: " + literal + ", Children: []uinode.Node{" + strings.Join(children, ", ") + "}}"
}

func blueprintEntityListActionName(screen BlueprintScreen, block BlueprintBlock, path []int) string {
	parts := []string{"entity_list"}
	if screen.Name != "" {
		parts = append(parts, toCamelJSON(screen.Name))
	}
	if block.Entity != "" {
		parts = append(parts, toCamelJSON(block.Entity))
	}
	if len(path) > 0 {
		pathParts := make([]string, len(path))
		for i, part := range path {
			pathParts[i] = fmt.Sprint(part)
		}
		parts = append(parts, strings.Join(pathParts, "_"))
	}
	return strings.NewReplacer("-", "_", " ", "_", "/", "_").Replace(strings.Join(parts, "_"))
}

func blueprintEntityListClientJS(block BlueprintBlock, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	limit := block.Limit
	if limit == 0 {
		limit = 20
	}
	emptyText := block.EmptyText
	if emptyText == "" {
		emptyText = "No records."
	}
	fieldsRaw, _ := json.Marshal(block.Fields)
	return fmt.Sprintf(`(async () => {
  const apiBase = %q;
  const entity = %q;
  const fields = %s;
  const root = document.querySelector('[data-entity-list="' + entity + '"]');
  const body = root && root.querySelector('[data-entity-list-body]');
  if (!body) return;
  const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
  // The JSON API serializes column names in camelCase; the blueprint fields
  // are snake_case. Look up the snake name first, then its camelCase form.
  const cell = (row, field) => {
    if (row[field] !== undefined) return row[field];
    const camel = field.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase());
    return row[camel];
  };
  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';
  body.innerHTML = table('<tr><td colspan="' + fields.length + '">Loading...</td></tr>');
  try {
    const res = await fetch(apiBase + '/' + entity + '?limit=' + %d, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const payload = await res.json();
    const rows = Array.isArray(payload.data) ? payload.data : [];
    if (!rows.length) {
      body.innerHTML = table('<tr><td colspan="' + fields.length + '">%s</td></tr>');
      return;
    }
    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(cell(row, field)) + '</td>').join('') + '</tr>').join(''));
  } catch (err) {
    body.innerHTML = table('<tr><td colspan="' + fields.length + '">Failed to load ' + esc(entity) + '</td></tr>');
  }
})();`, apiBase, entity, string(fieldsRaw), limit, htmlEscapeJSString(emptyText))
}

func htmlEscapeJSString(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", `'`, "&#39;")
	return replacer.Replace(value)
}

// blueprintFormInputType maps an entity field type to an <input type=…>.
func blueprintFormInputType(fieldType string) string {
	switch fieldType {
	case "int", "integer", "float", "decimal":
		return "number"
	case "date":
		return "date"
	case "timestamp", "datetime":
		return "datetime-local"
	case "image":
		return "file"
	default:
		return "text"
	}
}

// blueprintEntityFormExpr emits a ui.Form for a create/edit form, composing
// ui.FormField per entity field — the framework owns the form/field CSS, so this
// ships no bespoke styling. The form is an island: data-fui-rpc-* (on the form
// via ExtraAttrs) makes the runtime JSON-encode the body to the CRUD endpoint;
// relation <select>s are populated on mount via data-action-mount.
func blueprintEntityFormExpr(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger, Title: \"Form error\"}, render.Text(%q))", "unknown entity "+entity)
	}
	title := block.Text
	if title == "" {
		title = "New " + toDisplayName(entity)
	}
	mode := strings.ToLower(strings.TrimSpace(block.Mode))
	if mode == "" {
		mode = "create"
	}
	rpcPath := apiBase + "/" + entity
	rpcMethod := "POST"
	submitLabel := "Create"
	if mode == "edit" {
		rpcPath = apiBase + "/" + entity + "/{id}"
		rpcMethod = "PUT"
		submitLabel = "Update"
	}
	attrParts := []string{
		fmt.Sprintf("\"data-entity-form\": %q", entity),
		fmt.Sprintf("\"data-entity-mode\": %q", mode),
		fmt.Sprintf("\"data-fui-rpc\": %q", rpcPath),
		fmt.Sprintf("\"data-fui-rpc-method\": %q", rpcMethod),
	}
	if mode != "edit" {
		attrParts = append(attrParts, "\"data-fui-rpc-reset\": \"true\"")
	}
	if blueprintFormHasRelation(block, entityMap) {
		attrParts = append(attrParts, fmt.Sprintf("\"data-action-mount\": %q", blueprintEntityFormActionName(screen, block, path)))
	}
	extraAttrs := "html.Attrs{" + strings.Join(attrParts, ", ") + "}"

	e := htmlEscapeJSString
	var fields []string
	for _, field := range decl.Fields {
		if blueprintFormFieldSkipped(field, block.Fields) {
			continue
		}
		label := toDisplayName(field.Name)
		fieldID := "field-" + field.Name
		req := ""
		if field.Required {
			req = " required"
		}
		var input string
		switch field.Type {
		case "enum":
			opts := `<option value="">— Select —</option>`
			for _, v := range field.Values {
				opts += `<option value="` + e(v) + `">` + e(toDisplayName(v)) + `</option>`
			}
			input = `<select name="` + e(field.Name) + `" id="` + fieldID + `"` + req + `>` + opts + `</select>`
		case "relation":
			input = `<select name="` + e(field.Name) + `" id="` + fieldID + `"` + req + ` data-rel-entity="` + e(field.To) + `"><option value="">— Select —</option></select>`
		case "text":
			input = `<textarea name="` + e(field.Name) + `" id="` + fieldID + `"` + req + `></textarea>`
		case "bool", "boolean":
			input = `<input type="checkbox" name="` + e(field.Name) + `" id="` + fieldID + `">`
		default:
			input = `<input type="` + blueprintFormInputType(field.Type) + `" name="` + e(field.Name) + `" id="` + fieldID + `"` + req + `>`
		}
		fields = append(fields, fmt.Sprintf("ui.FormField(ui.FormFieldConfig{Label: %q, For: %q, Required: %t, Input: render.Raw(%q)})", label, fieldID, field.Required, input))
	}
	form := fmt.Sprintf("ui.Form(ui.FormConfig{Action: %q, Method: \"POST\", SubmitLabel: %q, ExtraAttrs: %s}, %s)", rpcPath, submitLabel, extraAttrs, strings.Join(fields, ", "))
	return fmt.Sprintf("render.Join(ui.PageHeader(ui.PageHeaderConfig{Title: %q}), %s)", title, form)
}

// renderBlueprintEntityFormNode — DEAD, replaced by blueprintEntityFormExpr.
func renderBlueprintEntityFormNode(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("uinode.Node{Kind: \"div\", Props: map[string]any{\"text\": %q}}", "entity_form: unknown entity "+entity)
	}
	actionName := blueprintEntityFormActionName(screen, block, path)
	title := block.Text
	if title == "" {
		title = "New " + toDisplayName(entity)
	}
	mode := strings.ToLower(strings.TrimSpace(block.Mode))
	if mode == "" {
		mode = "create"
	}
	rpcPath := apiBase + "/" + entity
	rpcMethod := "POST"
	if mode == "edit" {
		rpcPath = apiBase + "/" + entity + "/{id}"
		rpcMethod = "PUT"
	}
	props := map[string]any{
		"class":               "gofastr-entity-form",
		"data-entity-form":    entity,
		"data-entity-mode":    mode,
		"data-fui-rpc":        rpcPath,
		"data-fui-rpc-method": rpcMethod,
	}
	if mode != "edit" {
		props["data-fui-rpc-reset"] = true
	}
	if blueprintFormHasRelation(block, entityMap) {
		// Populate relation <select>s on hydration.
		props["data-action-mount"] = actionName
	}
	var children []string
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind:  "heading",
		Props: map[string]any{"level": int64(2), "text": title},
	}))
	// Generate form fields from entity definition
	filterFields := block.Fields
	for _, field := range decl.Fields {
		if blueprintFormFieldSkipped(field, filterFields) {
			continue
		}
		label := toDisplayName(field.Name)
		inputType := "text"
		switch field.Type {
		case "int", "integer":
			inputType = "number"
		case "float", "decimal":
			inputType = "number"
		case "bool", "boolean":
			inputType = "checkbox"
		case "date":
			inputType = "date"
		case "timestamp", "datetime":
			inputType = "datetime-local"
		case "text":
			inputType = "textarea"
		case "image":
			inputType = "file"
		case "enum":
			inputType = "select"
		case "relation":
			inputType = "relation"
		}
		if inputType == "select" {
			// Enum: render <select> with an empty placeholder + one option
			// per declared value.
			optionChildren := []BlueprintBlock{{
				Kind:  "option",
				Props: map[string]any{"value": "", "text": "— Select —"},
			}}
			for _, v := range field.Values {
				optionChildren = append(optionChildren, BlueprintBlock{
					Kind:  "option",
					Props: map[string]any{"value": v, "text": toDisplayName(v)},
				})
			}
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind:  "div",
				Props: map[string]any{"class": "form-field", "text": label},
				Children: []BlueprintBlock{{
					Kind: "select",
					Props: map[string]any{
						"name":     field.Name,
						"id":       "field-" + field.Name,
						"required": field.Required,
					},
					Children: optionChildren,
				}},
			}))
		} else if inputType == "relation" {
			// Relation: <select> populated client-side from the related
			// entity. data-rel-entity tells the mount handler what to fetch.
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind:  "div",
				Props: map[string]any{"class": "form-field", "text": label},
				Children: []BlueprintBlock{{
					Kind: "select",
					Props: map[string]any{
						"name":            field.Name,
						"id":              "field-" + field.Name,
						"required":        field.Required,
						"data-rel-entity": field.To,
					},
					Children: []BlueprintBlock{{
						Kind:  "option",
						Props: map[string]any{"value": "", "text": "— Select —"},
					}},
				}},
			}))
		} else if inputType == "textarea" {
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind: "div",
				Props: map[string]any{
					"class": "form-field",
					"text":  label,
				},
				Children: []BlueprintBlock{
					{
						Kind: "textarea",
						Props: map[string]any{
							"name":     field.Name,
							"id":       "field-" + field.Name,
							"required": field.Required,
						},
					},
				},
			}))
		} else if inputType == "checkbox" {
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind: "div",
				Props: map[string]any{
					"class": "form-field form-field-checkbox",
				},
				Children: []BlueprintBlock{
					{
						Kind: "input",
						Props: map[string]any{
							"type": "checkbox",
							"name": field.Name,
							"id":   "field-" + field.Name,
						},
					},
					{
						Kind: "label",
						Props: map[string]any{
							"for":  "field-" + field.Name,
							"text": label,
						},
					},
				},
			}))
		} else {
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind: "div",
				Props: map[string]any{
					"class": "form-field",
				},
				Children: []BlueprintBlock{
					{
						Kind: "label",
						Props: map[string]any{
							"for":  "field-" + field.Name,
							"text": label,
						},
					},
					{
						Kind: "input",
						Props: map[string]any{
							"type":     inputType,
							"name":     field.Name,
							"id":       "field-" + field.Name,
							"required": field.Required,
						},
					},
				},
			}))
		}
	}
	// Submit button — submission is handled by the form's data-fui-rpc.
	submitLabel := "Create"
	if mode == "edit" {
		submitLabel = "Update"
	}
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind: "button",
		Props: map[string]any{
			"type": "submit",
			"text": submitLabel,
		},
	}))
	literal, err := renderGoLiteral(props)
	if err != nil {
		literal = "nil"
	}
	return "uinode.Node{Kind: \"form\", Props: " + literal + ", Children: []uinode.Node{" + strings.Join(children, ", ") + "}}"
}

// blueprintFormFieldSkipped reports whether a field is omitted from a
// generated entity_form (system columns, hidden/auto/readonly fields, or
// fields excluded by an explicit block.fields allowlist).
func blueprintFormFieldSkipped(field framework.FieldDeclaration, filterFields []string) bool {
	switch field.Name {
	case "id", "created_at", "updated_at", "deleted_at":
		return true
	}
	if field.Hidden || field.AutoGenerate != "" || field.ReadOnly {
		return true
	}
	if len(filterFields) > 0 {
		for _, f := range filterFields {
			if f == field.Name {
				return false
			}
		}
		return true
	}
	return false
}

// blueprintEntityFormClientJS populates the form's relation <select>s on
// mount by fetching each related entity's collection.
func blueprintEntityFormClientJS(block BlueprintBlock, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	return fmt.Sprintf(`(async () => {
  const apiBase = %q;
  const form = document.querySelector('[data-entity-form="%s"]');
  if (!form) return;
  const labelFor = (row) => String(row.name ?? row.title ?? row.label ?? row.email ?? row.code ?? row.id ?? '');
  for (const sel of form.querySelectorAll('select[data-rel-entity]')) {
    const target = sel.getAttribute('data-rel-entity');
    if (!target || sel.dataset.relLoaded) continue;
    try {
      const res = await fetch(apiBase + '/' + target + '?limit=100', { headers: { 'Accept': 'application/json' } });
      if (!res.ok) continue;
      const payload = await res.json();
      const rows = Array.isArray(payload.data) ? payload.data : [];
      for (const row of rows) {
        const opt = document.createElement('option');
        opt.value = row.id;
        opt.textContent = labelFor(row);
        sel.appendChild(opt);
      }
      sel.dataset.relLoaded = '1';
    } catch (_) {}
  }
})();`, apiBase, entity)
}

func blueprintEntityFormActionName(screen BlueprintScreen, block BlueprintBlock, path []int) string {
	parts := []string{"entity_form"}
	if screen.Name != "" {
		parts = append(parts, toCamelJSON(screen.Name))
	}
	if block.Entity != "" {
		parts = append(parts, toCamelJSON(block.Entity))
	}
	return strings.NewReplacer("-", "_", " ", "_", "/", "_").Replace(strings.Join(parts, "_"))
}

// renderBlueprintEntityDetailNode generates the uinode.Node for a detail view
// of a single entity record. The fields render server-side as placeholders;
// the mount handler (blueprintEntityDetailClientJS) reads the record id from
// the URL, fetches it, and fills the values.
func renderBlueprintEntityDetailNode(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("uinode.Node{Kind: \"div\", Props: map[string]any{\"text\": %q}}", "entity_detail: unknown entity "+entity)
	}
	actionName := blueprintEntityDetailActionName(screen, block, path)
	title := block.Text
	if title == "" {
		title = toDisplayName(entity) + " Details"
	}
	props := map[string]any{
		"class":              "gofastr-entity-detail",
		"data-entity-detail": entity,
		"data-action-mount":  actionName, // fetch + fill on hydration
	}
	var children []string
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind:  "heading",
		Props: map[string]any{"level": int64(2), "text": title},
	}))
	filterFields := block.Fields
	for _, field := range decl.Fields {
		if field.Name == "deleted_at" {
			continue
		}
		if field.Hidden {
			continue
		}
		if len(filterFields) > 0 {
			found := false
			for _, f := range filterFields {
				if f == field.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		label := toDisplayName(field.Name)
		children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
			Kind: "div",
			Props: map[string]any{
				"class":            "detail-field",
				"data-field":       field.Name,
				"data-field-label": label,
			},
			Children: []BlueprintBlock{
				{
					Kind: "span",
					Props: map[string]any{
						"class": "detail-label",
						"text":  label,
					},
				},
				{
					Kind: "span",
					Props: map[string]any{
						"class":            "detail-value",
						"data-field-value": field.Name,
						"text":             "—",
					},
				},
			},
		}))
	}
	literal, err := renderGoLiteral(props)
	if err != nil {
		literal = "nil"
	}
	return "uinode.Node{Kind: \"section\", Props: " + literal + ", Children: []uinode.Node{" + strings.Join(children, ", ") + "}}"
}

// blueprintEntityDetailClientJS fetches one record (id taken from the last
// path segment of the URL) and fills the [data-field-value] spans.
func blueprintEntityDetailClientJS(block BlueprintBlock, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	return fmt.Sprintf(`(async () => {
  const apiBase = %q;
  const entity = %q;
  const root = document.querySelector('[data-entity-detail="' + entity + '"]');
  if (!root) return;
  const id = location.pathname.split('/').filter(Boolean).pop();
  if (!id) return;
  try {
    const res = await fetch(apiBase + '/' + entity + '/' + encodeURIComponent(id), { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const payload = await res.json();
    const row = payload && payload.data ? payload.data : payload;
    if (!row) return;
    // API keys are camelCase; field markers are snake_case — try both.
    const cell = (key) => {
      if (row[key] !== undefined) return row[key];
      return row[key.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase())];
    };
    for (const span of root.querySelectorAll('[data-field-value]')) {
      const val = cell(span.getAttribute('data-field-value'));
      span.textContent = (val === null || val === undefined || val === '') ? '—' : String(val);
    }
  } catch (_) {}
})();`, apiBase, entity)
}

func blueprintEntityDetailActionName(screen BlueprintScreen, block BlueprintBlock, path []int) string {
	parts := []string{"entity_detail"}
	if screen.Name != "" {
		parts = append(parts, toCamelJSON(screen.Name))
	}
	if block.Entity != "" {
		parts = append(parts, toCamelJSON(block.Entity))
	}
	return strings.NewReplacer("-", "_", " ", "_", "/", "_").Replace(strings.Join(parts, "_"))
}

// toDisplayName converts a snake_case field/entity name to a human-readable Title Case display name.
// displayAcronyms are words that read better fully uppercased in a label
// ("mrr" → "MRR", not "Mrr"). Lowercase keys; matched case-insensitively per word.
var displayAcronyms = map[string]bool{
	"id": true, "url": true, "uri": true, "api": true, "mrr": true, "arr": true,
	"ltv": true, "sku": true, "kpi": true, "roi": true, "sla": true, "ui": true,
	"ux": true, "css": true, "html": true, "json": true, "csv": true, "pdf": true,
	"http": true, "https": true, "ip": true, "dns": true, "cdn": true, "sso": true,
	"faq": true, "seo": true, "cta": true, "vat": true, "ein": true, "ssn": true,
}

func toDisplayName(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		if displayAcronyms[strings.ToLower(w)] {
			words[i] = strings.ToUpper(w)
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

// blueprintEndpointHandlerName returns the Go identifier for an
// endpoint's handler. It prefers the explicit Handler, falling back to
// the endpoint Name when Handler is empty. When both are empty it
// returns "" — there is no identifier to emit, and callers skip the
// endpoint entirely (no stub, no route registration).
func blueprintEndpointHandlerName(endpoint BlueprintEndpoint) string {
	source := strings.TrimSpace(endpoint.Handler)
	if source == "" {
		source = strings.TrimSpace(endpoint.Name)
	}
	if source == "" {
		return ""
	}
	return toCamelCase(source)
}

// blueprintSeedFieldIsDecimal reports whether the named field on the given
// entity is a decimal-typed column (which CRUD validates as a decimal string).
func blueprintSeedFieldIsDecimal(bp Blueprint, entity, field string) bool {
	for _, decl := range bp.Entities {
		if decl.Name != entity {
			continue
		}
		for _, f := range decl.Fields {
			if f.Name == field {
				return strings.EqualFold(f.Type, "decimal")
			}
		}
	}
	return false
}

// blueprintDecimalLiteral renders a seed value as the decimal string the
// validator expects. Already-string values pass through; numbers are
// formatted without scientific notation or trailing-zero noise.
func blueprintDecimalLiteral(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprint(val)
	}
}

func renderBlueprintStubs(bp Blueprint) string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	needsHTTP := len(bp.Endpoints) > 0 || len(bp.Middleware) > 0
	needsFramework := len(bp.Plugins) > 0
	needsJSON := false
	if needsHTTP || needsFramework || needsJSON {
		sb.WriteString("import (\n")
		if needsHTTP {
			sb.WriteString("\t\"net/http\"\n")
		}
		if needsJSON {
			sb.WriteString("\t\"encoding/json\"\n")
		}
		if needsFramework {
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
		}
		sb.WriteString(")\n\n")
	}
	for _, endpoint := range bp.Endpoints {
		handler := blueprintEndpointHandlerName(endpoint)
		if handler == "" {
			// No Handler and no Name: nothing to derive a Go identifier
			// from. Skip emitting a stub; registration skips it too.
			continue
		}
		label := strings.TrimSpace(endpoint.Handler)
		if label == "" {
			label = strings.TrimSpace(endpoint.Name)
		}
		sb.WriteString(fmt.Sprintf("func %s(w http.ResponseWriter, r *http.Request) {\n", handler))
		sb.WriteString(fmt.Sprintf("\thttp.Error(w, %q, http.StatusNotImplemented)\n", "TODO: implement "+label))
		sb.WriteString("}\n\n")
	}
	for _, item := range bp.Middleware {
		name := toCamelCase(item.Name)
		sb.WriteString(fmt.Sprintf("func %sMiddleware(next http.Handler) http.Handler {\n", name))
		sb.WriteString("\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tnext.ServeHTTP(w, r)\n\t})\n")
		sb.WriteString("}\n\n")
	}
	for _, item := range bp.Plugins {
		name := toCamelCase(item.Name) + "Plugin"
		sb.WriteString(fmt.Sprintf("type %s struct{}\n\n", name))
		sb.WriteString(fmt.Sprintf("func (%s) Name() string { return %q }\n", name, item.Name))
		sb.WriteString(fmt.Sprintf("func (%s) Init(app *framework.App) error { return nil }\n\n", name))
	}
	for _, item := range bp.Helpers {
		name := toCamelCase(item.Name)
		sb.WriteString(fmt.Sprintf("func %s() {\n\t// TODO: implement helper %q.\n}\n\n", name, item.Name))
	}
	if len(bp.Seed) > 0 {
		sb.WriteString("// seedEntity is one entity's ordered seed rows.\n")
		sb.WriteString("type seedEntity struct {\n\tEntity string\n\tRows   []map[string]any\n}\n\n")
		sb.WriteString("// seedData returns the initial seed data in blueprint-declared\n")
		sb.WriteString("// order (so entities that reference others are inserted after them).\n")
		sb.WriteString("func seedData() []seedEntity {\n")
		sb.WriteString("\treturn []seedEntity{\n")
		for _, seed := range bp.Seed {
			sb.WriteString(fmt.Sprintf("\t\t{Entity: %q, Rows: []map[string]any{\n", seed.Entity))
			for _, row := range seed.Rows {
				sb.WriteString("\t\t\t{")
				// Emit keys in sorted order so regeneration is deterministic
				// (map iteration order is random and would otherwise churn the diff).
				keys := make([]string, 0, len(row))
				for k := range row {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				first := true
				for _, k := range keys {
					v := row[k]
					if !first {
						sb.WriteString(", ")
					}
					first = false
					// Decimal columns validate as decimal strings, so a YAML
					// number (12.5) must be emitted as a quoted string.
					if blueprintSeedFieldIsDecimal(bp, seed.Entity, k) {
						sb.WriteString(fmt.Sprintf("%q: %q", k, blueprintDecimalLiteral(v)))
						continue
					}
					switch val := v.(type) {
					case string:
						sb.WriteString(fmt.Sprintf("%q: %q", k, val))
					case int:
						sb.WriteString(fmt.Sprintf("%q: %d", k, val))
					case int64:
						sb.WriteString(fmt.Sprintf("%q: %d", k, val))
					case float64:
						sb.WriteString(fmt.Sprintf("%q: %v", k, val))
					case bool:
						sb.WriteString(fmt.Sprintf("%q: %t", k, val))
					case []any:
						sb.WriteString(fmt.Sprintf("%q: []any{", k))
						for i, item := range val {
							if i > 0 {
								sb.WriteString(", ")
							}
							switch itemVal := item.(type) {
							case string:
								sb.WriteString(fmt.Sprintf("%q", itemVal))
							case int, int64, float64, bool:
								sb.WriteString(fmt.Sprintf("%v", itemVal))
							default:
								sb.WriteString("nil")
							}
						}
						sb.WriteString("}")
					case nil:
						sb.WriteString(fmt.Sprintf("%q: nil", k))
					default:
						sb.WriteString(fmt.Sprintf("%q: nil // unsupported type %T", k, val))
					}
				}
				sb.WriteString("},\n")
			}
			sb.WriteString("\t\t}},\n")
		}
		sb.WriteString("\t}\n}\n\n")
		// Add json import if needed
	}
	return sb.String()
}

func renderBlueprintApp(bp Blueprint) string {
	name := bp.App.Name
	if name == "" {
		name = "GoFastr"
	}
	var sb strings.Builder
	adminSeed := bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != ""
	hasMarketing := false
	hasAccess := false
	for _, s := range bp.Screens {
		if s.Layout == "marketing" {
			hasMarketing = true
		}
		if s.Access.Auth {
			hasAccess = true
		}
	}
	needWidget := len(bp.Nav) > 0 || blueprintNeedsToasts(bp) || blueprintHasSoftDelete(bp)
	needUI := len(bp.Nav) > 0 || hasMarketing || blueprintHasSoftDelete(bp)
	roleNav := bp.App.Auth.Enabled && blueprintNavHasRoles(bp.Nav)
	// authHeader: an auth-aware marketing header (Sign in ↔ account + Sign out).
	// guestRedirect: bounce already-signed-in visitors off the auth screens.
	authHeader := hasMarketing && bp.App.Auth.Enabled
	guestRedirect := bp.App.Auth.Enabled && blueprintHasAuthFormScreen(bp)
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	if adminSeed || hasAccess || roleNav || authHeader || guestRedirect {
		sb.WriteString("\t\"context\"\n")
	}
	sb.WriteString("\t\"database/sql\"\n")
	if adminSeed {
		sb.WriteString("\t\"log\"\n")
	}
	if len(bp.Endpoints) > 0 {
		sb.WriteString("\t\"net/http\"\n")
	}
	if hasAccess {
		sb.WriteString("\t\"net/url\"\n")
	}
	if bp.App.Auth.Enabled {
		// JWT_SECRET + ADMIN_SEED_PASSWORD are read from the environment.
		sb.WriteString("\t\"os\"\n")
	}
	sb.WriteString("\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if hasAccess || guestRedirect {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app/decide\"\n")
	}
	if hasMarketing {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	}
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n")
	}
	if needWidget {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget\"\n")
	}
	if blueprintNeedsToasts(bp) {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget/preset\"\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/router\"\n")
	}
	rbac := bp.App.Auth.Enabled && blueprintHasEntityAccess(bp)
	if hasAccess || rbac || roleNav || authHeader || guestRedirect {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/handler\"\n")
	}
	if needUI {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if rbac {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/access\"\n")
	}
	if bp.App.Auth.Enabled {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n)\n\n")
	sb.WriteString("const (\n")
	sb.WriteString(fmt.Sprintf("\tappName = %q\n", name))
	sb.WriteString(fmt.Sprintf("\tappModule = %q\n", bp.App.Module))
	sb.WriteString(fmt.Sprintf("\tdbDriver = %q\n", bp.App.DBDriver))
	// Credentials never land in committed source: the DSN is emitted
	// password-stripped; the runtime reads the full one from
	// DATABASE_URL (see the generated .env).
	sb.WriteString(fmt.Sprintf("\tdbURL = %q\n", redactDSN(bp.App.DBURL)))
	sb.WriteString(fmt.Sprintf("\tstaticDir = %q\n", blueprintEffectiveStaticDir(bp)))
	sb.WriteString(fmt.Sprintf("\tapiPrefix = %q\n", bp.App.APIPrefix))
	sb.WriteString(")\n\n")
	sb.WriteString(blueprintBaseCSSFunc())
	if hasAccess {
		sb.WriteString("// authPolicy gates a screen: redirect anonymous GETs to the login\n")
		sb.WriteString("// page (with ?next=) and 403 a signed-in user missing the required role.\n")
		sb.WriteString("func authPolicy(loginPath, role string) app.Policy {\n")
		sb.WriteString("\treturn app.PolicyFunc(func(ctx context.Context) app.Decision {\n")
		sb.WriteString("\t\tu, ok := handler.GetUser(ctx)\n")
		sb.WriteString("\t\tif !ok || u == nil {\n")
		sb.WriteString("\t\t\tnext := \"/\"\n")
		sb.WriteString("\t\t\tif r := app.RequestFromContext(ctx); r != nil { next = r.URL.Path }\n")
		sb.WriteString("\t\t\treturn decide.Redirect(loginPath + \"?next=\" + url.QueryEscape(next))\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\tif role != \"\" {\n")
		sb.WriteString("\t\t\tif rh, ok := u.(interface{ GetRoles() []string }); ok {\n")
		sb.WriteString("\t\t\t\tfor _, r := range rh.GetRoles() { if r == role { return decide.Allow() } }\n")
		sb.WriteString("\t\t\t}\n")
		sb.WriteString("\t\t\treturn decide.Block(403, \"Forbidden\")\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\treturn decide.Allow()\n")
		sb.WriteString("\t})\n")
		sb.WriteString("}\n\n")
	}
	if guestRedirect {
		sb.WriteString("// guestPolicy gates a guest-only screen (login / signup): a\n")
		sb.WriteString("// signed-in visitor is redirected to the app instead of seeing a sign-in\n")
		sb.WriteString("// form they're already past.\n")
		sb.WriteString("func guestPolicy(appHome string) app.Policy {\n")
		sb.WriteString("\treturn app.PolicyFunc(func(ctx context.Context) app.Decision {\n")
		sb.WriteString("\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
		sb.WriteString("\t\t\treturn decide.Redirect(appHome)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\treturn decide.Allow()\n")
		sb.WriteString("\t})\n")
		sb.WriteString("}\n\n")
	}
	if hasMarketing {
		sb.WriteString("// marketingHeader / Footer wrap the public marketing layout.\n")
		toggleArg := ""
		if len(bp.App.ThemeDark) > 0 {
			// The app declares a dark scheme — surface the toggle as a real
			// ui.ThemeToggle in the header's Actions slot, never hand-rolled.
			toggleArg = ", ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon})"
		}
		if authHeader {
			// Auth-aware header. Dashboard is a plain nav link (cohesive with
			// Pricing/About, and it collapses into the mobile drawer like them); the
			// single auth action — Sign out when signed in, Sign in when not — is the
			// one button, sized to match. No "Sign in" loop for users already past it.
			appHome := blueprintAppHome(bp)
			sb.WriteString("func marketingHeader(ctx context.Context) render.HTML {\n")
			sb.WriteString("\tnav := []ui.SiteHeaderLink{{Label: \"Pricing\", Href: \"/pricing\"}, {Label: \"About\", Href: \"/about\"}}\n")
			sb.WriteString("\tvar actions render.HTML\n")
			sb.WriteString("\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString(fmt.Sprintf("\t\tnav = append(nav, ui.SiteHeaderLink{Label: \"Dashboard\", Href: %q})\n", appHome))
			sb.WriteString(fmt.Sprintf("\t\tactions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true}, ui.SignOut(ui.SignOutConfig{Next: \"/\"})%s)\n", toggleArg))
			sb.WriteString("\t} else {\n")
			sb.WriteString(fmt.Sprintf("\t\tactions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true}, ui.LinkButton(ui.LinkButtonConfig{Label: \"Sign in\", Href: \"/login\", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall})%s)\n", toggleArg))
			sb.WriteString("\t}\n")
			sb.WriteString("\treturn ui.SiteHeader(ui.SiteHeaderConfig{\n")
			sb.WriteString("\t\tBrand: ui.Link(ui.LinkConfig{Href: \"/\", Text: appName}),\n")
			sb.WriteString("\t\tNavItems: nav,\n")
			sb.WriteString("\t\tDrawer: ui.SiteHeaderDrawerSheet,\n")
			sb.WriteString("\t\tActions: actions,\n")
			sb.WriteString("\t})\n}\n\n")
		} else {
			sb.WriteString("func marketingHeader() render.HTML {\n")
			sb.WriteString("\treturn ui.SiteHeader(ui.SiteHeaderConfig{\n")
			sb.WriteString("\t\tBrand: ui.Link(ui.LinkConfig{Href: \"/\", Text: appName}),\n")
			sb.WriteString("\t\tNavItems: []ui.SiteHeaderLink{{Label: \"Pricing\", Href: \"/pricing\"}, {Label: \"About\", Href: \"/about\"}},\n")
			sb.WriteString("\t\tDrawer: ui.SiteHeaderDrawerSheet,\n")
			if toggleArg != "" {
				sb.WriteString("\t\tActions: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),\n")
			}
			sb.WriteString("\t})\n}\n\n")
		}
		sb.WriteString("func marketingFooter() render.HTML {\n")
		sb.WriteString("\treturn ui.SiteFooter(ui.SiteFooterConfig{\n")
		sb.WriteString("\t\tLead: ui.Link(ui.LinkConfig{Href: \"/\", Text: appName}),\n")
		sb.WriteString("\t\tColumns: []ui.SiteFooterColumn{\n")
		sb.WriteString("\t\t\t{Title: \"Product\", Links: []ui.SiteFooterLink{{Label: \"Pricing\", Href: \"/pricing\"}}},\n")
		sb.WriteString("\t\t\t{Title: \"Company\", Links: []ui.SiteFooterLink{{Label: \"About\", Href: \"/about\"}}},\n")
		sb.WriteString("\t\t\t{Title: \"Legal\", Links: []ui.SiteFooterLink{{Label: \"Terms\", Href: \"/terms\"}, {Label: \"Privacy\", Href: \"/privacy\"}}},\n")
		sb.WriteString("\t\t},\n")
		sb.WriteString("\t})\n}\n\n")
	}
	if len(bp.App.Theme) > 0 {
		sb.WriteString("func appTheme() style.Theme {\n")
		sb.WriteString("\ttheme := style.DefaultTheme()\n")
		for _, key := range sortedStringMapKeys(bp.App.Theme) {
			if path, ok := blueprintThemeColorPath(key); ok {
				sb.WriteString(fmt.Sprintf("\ttheme.Colors.%s.Value = %q\n", path, bp.App.Theme[key]))
			}
		}
		if body, heading := blueprintFontStacks(bp.App.Theme); body != "" || heading != "" {
			if body != "" {
				sb.WriteString(fmt.Sprintf("\ttheme.Fonts.Body.Value = %q\n", body))
			}
			if heading != "" {
				sb.WriteString(fmt.Sprintf("\ttheme.Fonts.Heading.Value = %q\n", heading))
			}
		}
		if len(bp.App.ThemeDark) > 0 {
			// Dark-scheme palette — emitted as a [data-color-scheme="dark"] token
			// block so the header's ui.ThemeToggle recolors the whole app.
			sb.WriteString("\ttheme.DarkColors = map[string]string{\n")
			for _, key := range sortedStringMapKeys(bp.App.ThemeDark) {
				sb.WriteString(fmt.Sprintf("\t\t%q: %q,\n", key, bp.App.ThemeDark[key]))
			}
			sb.WriteString("\t}\n")
		}
		sb.WriteString("\treturn theme\n")
		sb.WriteString("}\n\n")
	}
	// fontFaceCSS holds the @font-face rules for the theme's configured
	// fonts (self-hosted from <static>/fonts/<slug>.woff2). It is the single
	// font-loading source, shared verbatim by the UI host and the admin battery
	// so every surface loads identical fonts. Empty when no fonts are declared.
	sb.WriteString(fmt.Sprintf("// fontFaceCSS holds the @font-face rules for the app's fonts, shared by\n// the UI host and the admin battery so every surface loads identical fonts.\nconst fontFaceCSS = %q\n\n", blueprintFontFaceCSS(bp.App.Theme)))
	if len(bp.Nav) > 0 {
		sb.WriteString("// sidebarConfig returns the navigation sidebar configuration.\n")
		sb.WriteString("func sidebarConfig() ui.SidebarConfig {\n")
		sb.WriteString(fmt.Sprintf("\treturn ui.SidebarConfig{Title: %q, Items: []ui.SidebarItem{\n", name))
		for _, item := range bp.Nav {
			renderNavItemGo(&sb, item, "\t\t")
		}
		sb.WriteString("\t}")
		// The app shell has no top bar, so account/appearance controls live in
		// the sidebar footer (visible on desktop, in the drawer on mobile) — the
		// theme toggle and, when auth is on, a Sign out. Kept inside the returned
		// literal so pack's AST nav reader stays simple.
		var footerParts []string
		if len(bp.App.ThemeDark) > 0 {
			footerParts = append(footerParts, "ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleLabel})")
		}
		if bp.App.Auth.Enabled {
			footerParts = append(footerParts, `ui.SignOut(ui.SignOutConfig{Next: "/"})`)
		}
		switch len(footerParts) {
		case 1:
			sb.WriteString(", Footer: " + footerParts[0])
		case 2:
			sb.WriteString(", Footer: ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart}, " + strings.Join(footerParts, ", ") + ")")
		}
		sb.WriteString("}\n}\n\n")
	}
	// Layouts are package-level so per-screen mount funcs (screen_<name>.go) can
	// reference them without app.go naming any screen. RegisterGenerated assigns
	// them before calling mountGenerated.
	if len(bp.Nav) > 0 || hasMarketing {
		sb.WriteString("var (\n")
		if len(bp.Nav) > 0 {
			sb.WriteString("\tappLayout *app.Layout\n")
		}
		if hasMarketing {
			sb.WriteString("\tmarketingLayout *app.Layout\n")
		}
		sb.WriteString(")\n\n")
	}
	sb.WriteString("// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.\n")
	sb.WriteString("func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {\n")
	sb.WriteString("\tif site == nil {\n")
	sb.WriteString(fmt.Sprintf("\t\tsite = app.NewApp(%q)\n", name))
	sb.WriteString("\t}\n")
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\tsite.WithTheme(appTheme())\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\tsbCfg := sidebarConfig()\n")
		sb.WriteString("\tsb := ui.Sidebar(sbCfg)\n")
		// No standalone app top bar: the sidebar owns brand + nav + the theme
		// toggle (in its footer), so content fills from the top with no empty
		// header band — the way real app shells (Linear/Notion/Stripe) read.
		sb.WriteString("\tappLayout = app.NewLayout(\"app\").WithSidebar(sb)\n")
		sb.WriteString("\tsite.SetDefaultLayout(appLayout)\n")
		sb.WriteString("\tui.MountSidebar(routerMounter{fwApp.Router()}, sbCfg)\n")
	}
	if hasMarketing {
		headerExpr := "app.NewStaticComponent(marketingHeader())"
		if authHeader {
			// Render per-request so the header reflects the live session.
			headerExpr = "app.NewContextComponent(marketingHeader)"
		}
		sb.WriteString("\tmarketingLayout = app.NewLayout(\"marketing\").\n")
		sb.WriteString("\t\tWithContainer().\n")
		sb.WriteString(fmt.Sprintf("\t\tWithHeader(%s).\n", headerExpr))
		sb.WriteString("\t\tWithFooter(app.NewStaticComponent(marketingFooter()))\n")
	}
	// Toast stack — mount a global toast stack so server-side handlers
	// and client-side JS can fire toasts via X-Gofastr-Toast header or
	// window.__gofastr.toast().
	if blueprintNeedsToasts(bp) {
		sb.WriteString("\t{\n")
		sb.WriteString("\t\tstack := preset.ToastStack(\"blueprint-toasts\").Build()\n")
		sb.WriteString("\t\twidget.Mount(fwApp.Router(), &stack)\n")
		sb.WriteString("\t}\n")
	}
	// Auth — wire up the built-in auth system with login, register, logout.
	if bp.App.Auth.Enabled {
		sb.WriteString("\t{\n")
		if bp.App.Auth.DevMode {
			sb.WriteString("\t\t// WARNING: auth runs in DEV MODE — HTTP-friendly cookies (no\n")
			sb.WriteString("\t\t// Secure flag, plain session_id name) and a per-process JWT\n")
			sb.WriteString("\t\t// secret minted at startup. Do NOT deploy like this: set\n")
			sb.WriteString("\t\t// `dev_mode: false` and `jwt_secret` under app.auth in the\n")
			sb.WriteString("\t\t// blueprint, serve over HTTPS, then regenerate.\n")
			sb.WriteString("\t\tauthCfg := auth.AuthConfig{DevMode: true")
		} else {
			sb.WriteString("\t\t// Production auth defaults: Secure __Host-session cookie.\n")
			sb.WriteString("\t\t// Requires HTTPS end-to-end — over plain HTTP the browser\n")
			sb.WriteString("\t\t// never echoes the cookie back and login silently breaks.\n")
			sb.WriteString("\t\tauthCfg := auth.AuthConfig{DevMode: false")
		}
		if bp.App.Auth.BasePath != "" {
			sb.WriteString(fmt.Sprintf(", BasePath: %q", bp.App.Auth.BasePath))
		}
		// The signing key is never a committed literal: it comes from
		// JWT_SECRET (the generated .env carries the blueprint's value).
		// Empty in DevMode mints a per-process secret; empty in
		// production fails closed at boot.
		sb.WriteString(", JWTSecret: os.Getenv(\"JWT_SECRET\")")
		sb.WriteString("}\n")
		sb.WriteString("\t\tauthCfg.UserStore = auth.NewEntityUserStore(db, \"auth_users\")\n")
		sb.WriteString("\t\tauthCfg.SessionStore = auth.NewEntitySessionStore(db, \"auth_sessions\")\n")
		sb.WriteString("\t\tauthMgr := auth.New(authCfg)\n")
		sb.WriteString("\t\tauthMgr.Use(auth.NewCorePlugin())\n")
		// auth_users / auth_sessions are the auth battery's own tables; it
		// creates them in Init (EnsureSchema). The generated app ships no DDL.
		sb.WriteString("\t\tauthMgr.Init(fwApp)\n")
		// Point the auth battery's form-error redirect at the login page, so a
		// failed form login (wrong password, no Referer) lands back on the login
		// form with ?error=… instead of rendering the raw JSON error body.
		sb.WriteString(fmt.Sprintf("\t\tauth.SetDefaultLoginErrorPath(%q)\n", blueprintLoginRoute(bp)))
		if bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != "" {
			sb.WriteString("\t\t// Bootstrap admin account so the back-office is reachable on a\n")
			sb.WriteString("\t\t// fresh database. Created only when absent (idempotent). The\n")
			sb.WriteString("\t\t// password comes from ADMIN_SEED_PASSWORD (see the generated\n")
			sb.WriteString("\t\t// .env — gitignored, so a deploy must export the variable\n")
			sb.WriteString("\t\t// itself), never from committed source; without it no admin\n")
			sb.WriteString("\t\t// is seeded and the skip is logged loudly.\n")
			sb.WriteString("\t\tif seedPw := os.Getenv(\"ADMIN_SEED_PASSWORD\"); seedPw != \"\" {\n")
			sb.WriteString(fmt.Sprintf("\t\t\tif _, _, err := authCfg.UserStore.FindByEmail(context.Background(), %q); err != nil {\n", bp.App.Admin.SeedEmail))
			sb.WriteString("\t\t\t\tif h, herr := auth.HashPassword(seedPw); herr == nil {\n")
			adminRole := bp.App.Admin.Role
			if adminRole == "" {
				adminRole = "admin"
			}
			sb.WriteString(fmt.Sprintf("\t\t\t\t\tauthCfg.UserStore.CreateUser(context.Background(), %q, h, []string{%q, \"user\"})\n", bp.App.Admin.SeedEmail, adminRole))
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t} else {\n")
			sb.WriteString(fmt.Sprintf("\t\t\tlog.Printf(\"WARN: ADMIN_SEED_PASSWORD is not set — admin %%q was NOT seeded; on a fresh database the back-office login will fail\", %q)\n", bp.App.Admin.SeedEmail))
			sb.WriteString("\t\t}\n")
		}
		sb.WriteString("\t\t// Resolve the session cookie to a user on every request so\n")
		sb.WriteString("\t\t// owner/access-scoped CRUD sees the logged-in user. Without\n")
		sb.WriteString("\t\t// this, authorized requests fail closed (401) just like\n")
		sb.WriteString("\t\t// anonymous ones.\n")
		sb.WriteString("\t\tfwApp.Use(auth.SessionMiddleware(authMgr))\n")
		if roleNav {
			// Role-aware nav: the sidebar/drawer filter role-restricted items
			// (e.g. an admin-only link) by the signed-in user's roles, so a
			// link the user can't use never appears.
			sb.WriteString("\t\tui.SetRolesExtractor(func(ctx context.Context) []string {\n")
			sb.WriteString("\t\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString("\t\t\t\tif rh, ok := u.(interface{ GetRoles() []string }); ok {\n")
			sb.WriteString("\t\t\t\t\treturn rh.GetRoles()\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\treturn nil\n")
			sb.WriteString("\t\t})\n")
		}
		if blueprintHasEntityAccess(bp) {
			adminRole := bp.App.Admin.Role
			if adminRole == "" {
				adminRole = "admin"
			}
			sb.WriteString("\t\t// Entities declare `access:` permissions; install a RolePolicy so the\n")
			sb.WriteString("\t\t// signed-in user's roles resolve to those permissions on the gated\n")
			sb.WriteString("\t\t// CRUD API. The admin role holds the wildcard (full access, the same\n")
			sb.WriteString("\t\t// surface the back-office manages); add finer per-role Grants here as\n")
			sb.WriteString("\t\t// you define more roles. Without this, every write 403s.\n")
			sb.WriteString("\t\trbac := access.NewRolePolicy()\n")
			sb.WriteString(fmt.Sprintf("\t\trbac.Grant(%q, access.Wildcard)\n", adminRole))
			sb.WriteString("\t\tfwApp.Use(access.Middleware(rbac, func(ctx context.Context) []string {\n")
			sb.WriteString("\t\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString("\t\t\t\tif rh, ok := u.(interface{ GetRoles() []string }); ok {\n")
			sb.WriteString("\t\t\t\t\treturn rh.GetRoles()\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\treturn nil\n")
			sb.WriteString("\t\t}))\n")
		}
		sb.WriteString("\t\t// auth.CSRF is intentionally NOT mounted: this generated surface\n")
		sb.WriteString("\t\t// is JSON-first (REST CRUD + /mcp), and the CSRF middleware 403s\n")
		sb.WriteString("\t\t// any unsafe-method request that doesn't echo the csrf cookie as\n")
		sb.WriteString("\t\t// an X-CSRF-Token header — which plain JSON/MCP clients don't.\n")
		sb.WriteString("\t\t// Session cookies are SameSite=Strict, so cross-site form posts\n")
		sb.WriteString("\t\t// don't carry the session in modern browsers. If you add browser\n")
		sb.WriteString("\t\t// HTML forms, mount auth.CSRF — see `gofastr docs blueprints`\n")
		sb.WriteString("\t\t// (Auth section) and `gofastr docs auth`.\n")
		sb.WriteString("\t}\n")
	}
	// ConfirmAction dialogs — mount a delete confirmation modal for each
	// soft-delete entity so entity_detail screens can render a trigger.
	for _, decl := range bp.Entities {
		if decl.SoftDelete {
			sb.WriteString("\t{\n")
			sb.WriteString(fmt.Sprintf("\t\t_, b := ui.ConfirmAction(ui.ConfirmActionConfig{Name: %q, TriggerLabel: \"Delete\", Title: %q, Body: %q, RPCPath: %q})\n",
				"delete-"+decl.Name,
				"Delete this "+toDisplayName(decl.Name)+"?",
				"This action will soft-delete the record. It can be restored later.",
				"/"+decl.Name+"/{id}",
			))
			sb.WriteString("\t\td := b.Build()\n")
			sb.WriteString("\t\twidget.Mount(fwApp.Router(), &d)\n")
			sb.WriteString("\t}\n")
		}
	}
	// Screens (authored + synthesized CRUD) mount themselves: each screen_*.go
	// self-registers a mount func, and mountGenerated runs them in declaration
	// order. app.go names no screen type or entity resource — adding a screen
	// or an entity fragment is a new file, never an edit here. The call is
	// unconditional (the seam ships even with zero screens) so a later
	// `--add`/scaffold screen mounts without editing this owned file.
	sb.WriteString("\tmountGenerated(fwApp, site, db)\n")
	for _, endpoint := range bp.Endpoints {
		handler := blueprintEndpointHandlerName(endpoint)
		if handler == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.Router().Handle(%q, %q, http.HandlerFunc(%s))\n", strings.ToUpper(endpoint.Method), blueprintEndpointPath(endpoint), handler))
	}
	for _, item := range bp.Middleware {
		sb.WriteString(fmt.Sprintf("\tfwApp.Use(%sMiddleware)\n", toCamelCase(item.Name)))
	}
	for _, item := range bp.Plugins {
		sb.WriteString(fmt.Sprintf("\tfwApp.RegisterPlugin(%sPlugin{})\n", toCamelCase(item.Name)))
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\t_ = routerMounter{}\n")
	}
	sb.WriteString("}\n\n")
	if len(bp.Nav) > 0 {
		sb.WriteString("// routerMounter adapts framework's *router.Router to ui.WidgetMounter.\n")
		sb.WriteString("type routerMounter struct{ r *router.Router }\n\n")
		sb.WriteString("func (m routerMounter) MountWidget(def *widget.Definition) {\n")
		sb.WriteString("\twidget.Mount(m.r, def)\n")
		sb.WriteString("}\n")
		return sb.String()
	}
	return sb.String()
}

// blueprintBaseCSSFunc emits the appBaseCSS() function. It returns the
// empty string: every generated surface — marketing, app, entity list/detail,
// entity forms, auth — composes framework/ui components and core-ui/app layouts
// that ship their own CSS (auto-injected by the UI host). The generator ships
// ZERO bespoke styling, which is the proof the design system is cohesive and
// composable. This owned function stays as an extension point: an app can add
// its own base CSS here (or, preferably, in static/app.css).
func blueprintBaseCSSFunc() string {
	var sb strings.Builder
	sb.WriteString("// appBaseCSS is an owned extension point for app-specific base CSS.\n")
	sb.WriteString("// It's empty by default: every generated surface composes framework/ui\n")
	sb.WriteString("// components and core-ui/app layouts that ship their own CSS, so the\n")
	sb.WriteString("// generated app ships no bespoke styling. Add app CSS here or in static/app.css.\n")
	sb.WriteString("func appBaseCSS() string {\n")
	sb.WriteString("\treturn \"\"\n")
	sb.WriteString("}\n\n")
	return sb.String()
}

func renderNavItemGo(sb *strings.Builder, item BlueprintNavItem, indent string) {
	sb.WriteString(fmt.Sprintf("%s{Label: %q, Href: %q", indent, item.Label, item.Href))
	if item.Role != "" {
		sb.WriteString(fmt.Sprintf(", Roles: []string{%q}", item.Role))
	}
	if len(item.Items) > 0 {
		sb.WriteString(", Children: []ui.SidebarItem{\n")
		for _, child := range item.Items {
			renderNavItemGo(sb, child, indent+"\t")
		}
		sb.WriteString(indent + "}")
	}
	sb.WriteString("},\n")
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// blueprintFontFamilyName extracts the primary family name from a theme font
// value like "Hanken Grotesk" (or a full stack the author wrote), stripping
// quotes and any trailing fallback so it can be turned into a Google Fonts query.
func blueprintFontFamilyName(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.Trim(strings.TrimSpace(v), "'\"")
}

// blueprintConfiguredFonts returns the heading + body family names declared in
// the blueprint theme (font_heading / font_display alias, font_body).
func blueprintConfiguredFonts(theme map[string]string) (body, heading string) {
	body = blueprintFontFamilyName(theme["font_body"])
	heading = blueprintFontFamilyName(theme["font_heading"])
	if heading == "" {
		heading = blueprintFontFamilyName(theme["font_display"])
	}
	return body, heading
}

const blueprintFontSysStack = "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"

// blueprintFontStacks builds the full CSS font-family stacks for the theme's
// body and heading tokens, appending a robust system fallback (and letting the
// heading fall back through the body family first).
func blueprintFontStacks(theme map[string]string) (bodyStack, headingStack string) {
	body, heading := blueprintConfiguredFonts(theme)
	if body != "" {
		bodyStack = fmt.Sprintf("'%s', %s", body, blueprintFontSysStack)
	}
	if heading != "" {
		if body != "" {
			headingStack = fmt.Sprintf("'%s', '%s', %s", heading, body, blueprintFontSysStack)
		} else {
			headingStack = fmt.Sprintf("'%s', %s", heading, blueprintFontSysStack)
		}
	}
	return bodyStack, headingStack
}

// blueprintFontSlug turns a font family name into the self-hosted file slug,
// e.g. "Bricolage Grotesque" -> "bricolage-grotesque".
func blueprintFontSlug(family string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(family), " ", "-"))
}

// blueprintConfiguredFontFamilies returns the theme's declared font families in
// @font-face order (heading first, then body), de-duplicated. Empty when the
// theme names no fonts.
func blueprintConfiguredFontFamilies(theme map[string]string) []string {
	body, heading := blueprintConfiguredFonts(theme)
	var out []string
	seen := map[string]bool{}
	for _, f := range []string{heading, body} {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// blueprintEffectiveStaticDir is the directory the generated app serves static
// files from. It's the blueprint's static_dir when set; otherwise "static" when
// the theme declares self-hosted fonts (so the emitted woff2 files have a home
// and a serving route). Empty only when neither applies.
func blueprintEffectiveStaticDir(bp Blueprint) string {
	if bp.App.StaticDir != "" {
		return bp.App.StaticDir
	}
	if len(blueprintConfiguredFontFamilies(bp.App.Theme)) > 0 {
		return "static"
	}
	// A PWA needs a home for its scaffolded icons (and a serving route
	// for them), same as self-hosted fonts.
	if bp.App.PWA.Enabled {
		return "static"
	}
	return ""
}

// fontFetcher resolves a Google Fonts family name to a self-hostable woff2 file
// (the regular-weight latin subset). It's a package var so the test suite can
// stub it — the real implementation makes network calls, which tests never do.
var fontFetcher = fetchGoogleFontWoff2

// blueprintFontHTTPClient bounds the generate-time font fetch so an unreachable
// or slow CDN degrades to the offline warning path instead of hanging generate.
var blueprintFontHTTPClient = &http.Client{Timeout: 20 * time.Second}

// fetchGoogleFontWoff2 downloads the latin woff2 subset for a Google Fonts
// family. It queries the Google Fonts CSS API with a modern User-Agent (so the
// API returns woff2, not ttf), extracts the first gstatic .woff2 URL (preferring
// the latin subset), and downloads it. Any network/parse failure returns an
// error; generation then falls back to a loud warning rather than a silent 404.
func fetchGoogleFontWoff2(family string) ([]byte, error) {
	q := url.Values{}
	q.Set("family", family+":wght@400;700")
	q.Set("display", "swap")
	cssURL := "https://fonts.googleapis.com/css2?" + q.Encode()
	css, err := blueprintFontHTTPGet(cssURL)
	if err != nil {
		return nil, err
	}
	woffURL := blueprintFirstWoff2URL(string(css))
	if woffURL == "" {
		return nil, fmt.Errorf("no woff2 url in Google Fonts response for %q", family)
	}
	return blueprintFontHTTPGet(woffURL)
}

// blueprintFontHTTPGet fetches a URL with a browser-like User-Agent and returns
// its body, erroring on any non-2xx status.
func blueprintFontHTTPGet(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// The CSS API serves woff2 only to modern browsers; identify as one.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	resp, err := blueprintFontHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// blueprintFirstWoff2URL pulls the first gstatic .woff2 URL out of a Google
// Fonts css2 response, preferring the block tagged `/* latin */`.
func blueprintFirstWoff2URL(css string) string {
	extract := func(s string) string {
		i := strings.Index(s, "url(")
		if i < 0 {
			return ""
		}
		s = s[i+len("url("):]
		j := strings.Index(s, ")")
		if j < 0 {
			return ""
		}
		u := strings.Trim(strings.TrimSpace(s[:j]), "'\"")
		if strings.Contains(u, ".woff2") {
			return u
		}
		return ""
	}
	if i := strings.Index(css, "/* latin */"); i >= 0 {
		if u := extract(css[i:]); u != "" {
			return u
		}
	}
	// Fall back to the first woff2 anywhere in the response.
	for rest := css; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return ""
		}
		if u := extract(rest[i:]); u != "" {
			return u
		}
		rest = rest[i+len("url("):]
	}
}

// blueprintFontAssets fetches every configured font family and returns the
// woff2 files to emit under <static>/fonts/. Families whose fetch fails are
// reported in `missing` (as their emitted relative path) so the caller can warn
// — generation still proceeds so an offline run produces a working app minus the
// custom fonts.
func blueprintFontAssets(bp Blueprint) (files []generatedFile, missing []string) {
	dir := blueprintEffectiveStaticDir(bp)
	for _, fam := range blueprintConfiguredFontFamilies(bp.App.Theme) {
		rel := blueprintFontRelPath(dir, fam)
		data, err := fontFetcher(fam)
		if err != nil || len(data) == 0 {
			missing = append(missing, rel)
			continue
		}
		files = append(files, generatedFile{name: rel, content: string(data)})
	}
	return files, missing
}

// blueprintPWAOptionLiteral renders the uihost.WithPWA(...) call for the
// generated main.go. Only author-declared fields are emitted (so pack
// recovers exactly the authored blueprint); the framework applies the
// defaults at serve time. The icon set always points at the scaffolded
// placeholder files.
func blueprintPWAOptionLiteral(bp Blueprint) string {
	p := bp.App.PWA
	var fields []string
	if p.Name != "" {
		fields = append(fields, fmt.Sprintf("Name: %q", p.Name))
	}
	if p.ShortName != "" {
		fields = append(fields, fmt.Sprintf("ShortName: %q", p.ShortName))
	}
	if p.Description != "" {
		fields = append(fields, fmt.Sprintf("Description: %q", p.Description))
	}
	if p.StartURL != "" {
		fields = append(fields, fmt.Sprintf("StartURL: %q", p.StartURL))
	}
	if p.Scope != "" {
		fields = append(fields, fmt.Sprintf("Scope: %q", p.Scope))
	}
	if p.Display != "" {
		fields = append(fields, "Display: "+blueprintPWADisplayConst(p.Display))
	}
	if p.ThemeColor != "" {
		fields = append(fields, fmt.Sprintf("ThemeColor: %q", p.ThemeColor))
	}
	if p.BackgroundColor != "" {
		fields = append(fields, fmt.Sprintf("BackgroundColor: %q", p.BackgroundColor))
	}
	// The framework's built-in deny list covers the default /api and
	// /auth mounts; a custom api_prefix or auth base_path must follow
	// its app's real mounts or the never-precache/never-intercept
	// guarantee silently stops covering the API.
	var deny []string
	if prefix := strings.Trim(bp.App.APIPrefix, "/"); prefix != "" && prefix != "api" {
		deny = append(deny, "/"+prefix)
	}
	if bp.App.Auth.Enabled {
		if base := strings.TrimRight(bp.App.Auth.BasePath, "/"); base != "" && base != "/auth" {
			deny = append(deny, base)
		}
	}
	if len(deny) > 0 {
		quoted := make([]string, len(deny))
		for i, d := range deny {
			quoted[i] = fmt.Sprintf("%q", d)
		}
		fields = append(fields, "DenyPaths: []string{"+strings.Join(quoted, ", ")+"}")
	}
	fields = append(fields, `Icons: []uihost.PWAIcon{{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"}, {Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png"}, {Src: "/icons/icon-maskable.png", Sizes: "512x512", Type: "image/png", Purpose: uihost.PWAIconPurposeMaskable}}`)
	return "uihost.WithPWA(uihost.PWAConfig{" + strings.Join(fields, ", ") + "})"
}

// blueprintPWADisplayConst maps a validated blueprint display mode to
// the typed uihost constant emitted into generated code.
func blueprintPWADisplayConst(display string) string {
	switch display {
	case "fullscreen":
		return "uihost.PWADisplayFullscreen"
	case "minimal-ui":
		return "uihost.PWADisplayMinimalUI"
	case "browser":
		return "uihost.PWADisplayBrowser"
	default:
		return "uihost.PWADisplayStandalone"
	}
}

// blueprintPWAIconAssets emits the replaceable placeholder icons an
// app.pwa blueprint scaffolds: 192px and 512px "any" icons plus a 512px
// maskable variant, colored from the declared theme_color (falling back
// to the theme's primary token, then the framework indigo). Generated
// in-process — deterministic PNG bytes, no network fetch.
func blueprintPWAIconAssets(bp Blueprint) []generatedFile {
	if !bp.App.PWA.Enabled {
		return nil
	}
	dir := blueprintEffectiveStaticDir(bp)
	fill := bp.App.PWA.ThemeColor
	if fill == "" {
		fill = bp.App.Theme["primary"]
	}
	bg, ok := blueprintParseHexColor(fill)
	if !ok {
		bg = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff} // indigo fallback
	}
	rel := func(name string) string {
		return filepath.ToSlash(filepath.Join(dir, "icons", name))
	}
	return []generatedFile{
		{name: rel("icon-192.png"), content: string(blueprintPWAIconPNG(192, bg))},
		{name: rel("icon-512.png"), content: string(blueprintPWAIconPNG(512, bg))},
		// The maskable icon uses the same full-bleed art: a solid
		// background with a centered dot keeps all content inside the
		// mask safe zone regardless of the platform's crop shape.
		{name: rel("icon-maskable.png"), content: string(blueprintPWAIconPNG(512, bg))},
	}
}

// blueprintPWAIconPNG draws the placeholder icon: a solid brand-color
// square with a centered translucent-white dot, well inside the
// maskable safe zone.
func blueprintPWAIconPNG(size int, bg color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	dot := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xd9}
	c := float64(size) / 2
	r := float64(size) * 0.22
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, dot)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	// Encode cannot fail on an in-memory buffer with a valid image.
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// blueprintParseHexColor parses #rgb / #rrggbb into an opaque color.
func blueprintParseHexColor(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// blueprintFontRelPath is the emitted path for a family's woff2 file, matching
// the /fonts/<slug>.woff2 URL in fontFaceCSS (the static dir is served at root).
func blueprintFontRelPath(staticDir, family string) string {
	return filepath.ToSlash(filepath.Join(staticDir, "fonts", blueprintFontSlug(family)+".woff2"))
}

// blueprintMissingFontSlugs reports which configured font files are absent from
// the rendered file set (e.g. an offline fetch dropped them) so generate can
// warn with the exact paths the user must supply.
func blueprintMissingFontSlugs(bp Blueprint, files []generatedFile) []string {
	fams := blueprintConfiguredFontFamilies(bp.App.Theme)
	if len(fams) == 0 {
		return nil
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f.name] = true
	}
	dir := blueprintEffectiveStaticDir(bp)
	var missing []string
	for _, fam := range fams {
		if rel := blueprintFontRelPath(dir, fam); !present[rel] {
			missing = append(missing, rel)
		}
	}
	return missing
}

// blueprintFontFaceCSS returns the @font-face rules for the theme's declared
// families, self-hosted from <static>/fonts/<slug>.woff2. This is the SINGLE
// font-loading source — it's prepended to the UI host's stylesheet AND handed to
// the admin battery, so every surface (marketing, app, back-office) loads the
// exact same fonts with no external CDN dependency. Drop the matching woff2 into
// <static_dir>/fonts/. Returns "" when the theme declares no fonts.
func blueprintFontFaceCSS(theme map[string]string) string {
	body, heading := blueprintConfiguredFonts(theme)
	var b strings.Builder
	seen := map[string]bool{}
	for _, f := range []string{heading, body} {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		fmt.Fprintf(&b, "@font-face { font-family: '%s'; font-style: normal; font-weight: 400 700; font-display: swap; src: url('/fonts/%s.woff2') format('woff2'); }\n", f, blueprintFontSlug(f))
	}
	return b.String()
}

func blueprintThemeColorPath(key string) (string, bool) {
	switch key {
	case "primary":
		return "Primary", true
	case "primary-fg":
		return "PrimaryFg", true
	case "secondary":
		return "Secondary", true
	case "background":
		return "Background", true
	case "surface":
		return "Surface", true
	case "surface-soft":
		return "SurfaceSoft", true
	case "text":
		return "Text", true
	case "text-muted":
		return "TextMuted", true
	case "text-subtle":
		return "TextSubtle", true
	case "border":
		return "Border", true
	case "border-strong":
		return "BorderStrong", true
	case "accent":
		return "Accent", true
	case "success":
		return "Success", true
	case "warning":
		return "Warning", true
	case "danger":
		return "Danger", true
	case "info":
		return "Info", true
	default:
		return "", false
	}
}

func blueprintEndpointPath(endpoint BlueprintEndpoint) string {
	path := strings.TrimSpace(endpoint.Path)
	if path == "" || strings.HasPrefix(path, "/") || endpoint.Entity == "" {
		return path
	}
	return "/" + strings.Trim(strings.TrimSpace(endpoint.Entity), "/") + "/" + strings.TrimLeft(path, "/")
}

func relationTypeFromString(value string) (framework.RelationType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "many_to_one", "belongs_to", "belongsto":
		return framework.RelManyToOne, nil
	case "has_one", "hasone":
		return framework.RelHasOne, nil
	case "has_many", "hasmany":
		return framework.RelHasMany, nil
	case "many_to_many", "manytomany":
		return framework.RelManyToMany, nil
	default:
		return framework.RelManyToOne, fmt.Errorf("blueprint: unknown relation type %q", value)
	}
}

func screenTypeConst(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "page":
		return "app.ScreenPage", nil
	case "drawer":
		return "app.ScreenDrawer", nil
	case "sheet":
		return "app.ScreenSheet", nil
	case "dialog", "modal":
		return "app.ScreenDialog", nil
	default:
		return "", fmt.Errorf("blueprint: unknown screen type %q", value)
	}
}

func expectMap(node *coreyaml.Node, label string) (map[string]*coreyaml.Node, error) {
	if node == nil {
		return nil, fmt.Errorf("blueprint: %s is required", label)
	}
	if node.Kind != coreyaml.Map {
		return nil, fmt.Errorf("blueprint: %s must be a map at line %d", label, node.Line)
	}
	return node.Map, nil
}

func expectList(node *coreyaml.Node, label string) ([]*coreyaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != coreyaml.List {
		return nil, fmt.Errorf("blueprint: %s must be a list at line %d", label, node.Line)
	}
	return node.List, nil
}

func rejectUnknownKeys(m map[string]*coreyaml.Node, allowed map[string]bool, label string) error {
	for key, node := range m {
		if !allowed[key] && !strings.HasPrefix(key, "x_") && !strings.HasPrefix(key, "x-") {
			return fmt.Errorf("blueprint: unknown key %q in %s at line %d", key, label, node.Line)
		}
	}
	return nil
}

func stringValue(node *coreyaml.Node) string {
	if node == nil || node.Kind != coreyaml.Scalar || node.Value == nil {
		return ""
	}
	return fmt.Sprint(node.Value)
}

func boolValue(node *coreyaml.Node) bool {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false
	}
	if v, ok := node.Value.(bool); ok {
		return v
	}
	return strings.EqualFold(fmt.Sprint(node.Value), "true")
}

// strictBoolValue accepts only a genuine bool node (or the literal
// strings "true"/"false") and errors on anything else — for keys where
// lax coercion would silently invert a safe default.
func strictBoolValue(node *coreyaml.Node) (bool, error) {
	if node != nil && node.Kind == coreyaml.Scalar {
		if v, ok := node.Value.(bool); ok {
			return v, nil
		}
		switch fmt.Sprint(node.Value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	got := "missing"
	line := 0
	if node != nil {
		got = fmt.Sprintf("%q", fmt.Sprint(node.Value))
		line = node.Line
	}
	return false, fmt.Errorf("must be true or false (got %s) at line %d", got, line)
}

func intValue(node *coreyaml.Node) int {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func floatValue(node *coreyaml.Node) float64 {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

func scalarValue(node *coreyaml.Node) any {
	if node == nil || node.Kind != coreyaml.Scalar {
		return nil
	}
	return node.Value
}

func stringListValue(node *coreyaml.Node) []string {
	if node == nil || node.Kind != coreyaml.List {
		return nil
	}
	out := make([]string, 0, len(node.List))
	for _, item := range node.List {
		out = append(out, stringValue(item))
	}
	return out
}

func mapValue(node *coreyaml.Node) map[string]any {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := make(map[string]any, len(node.Map))
	for key, child := range node.Map {
		out[key] = anyValue(child)
	}
	return out
}

func anyValue(node *coreyaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case coreyaml.Scalar:
		return node.Value
	case coreyaml.List:
		out := make([]any, 0, len(node.List))
		for _, child := range node.List {
			out = append(out, anyValue(child))
		}
		return out
	case coreyaml.Map:
		return mapValue(node)
	default:
		return nil
	}
}

// blueprintNeedsToasts returns true when any screen uses entity_form or
// entity_list — these blocks benefit from toast feedback on CRUD actions.
func blueprintNeedsToasts(bp Blueprint) bool {
	for _, screen := range bp.Screens {
		for _, block := range screen.Body {
			if blueprintBlockNeedsToasts(block) {
				return true
			}
		}
	}
	return false
}
func blueprintBlockNeedsToasts(block BlueprintBlock) bool {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(block.Type))
	}
	if kind == "entity_form" || kind == "entity_list" || kind == "entity_detail" {
		return true
	}
	for _, child := range block.Children {
		if blueprintBlockNeedsToasts(child) {
			return true
		}
	}
	return false
}
func blueprintHasSoftDelete(bp Blueprint) bool {
	for _, decl := range bp.Entities {
		if decl.SoftDelete {
			return true
		}
	}
	return false
}
func blueprintSeedHasComplexValues(bp Blueprint) bool {
	for _, seed := range bp.Seed {
		for _, row := range seed.Rows {
			for _, v := range row {
				switch v.(type) {
				case string, int, int64, float64, bool, nil:
				default:
					return true
				}
			}
		}
	}
	return false
}
