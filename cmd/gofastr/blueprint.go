package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
}

// BlueprintNavItem describes a navigation entry — a link to a screen or URL.
type BlueprintNavItem struct {
	Label string
	Href  string
	Icon  string
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
	Type      string
	Kind      string
	Text      string
	Level     int
	Class     string
	Href      string
	Entity    string
	Fields    []string
	Limit     int
	EmptyText string
	Mode      string // "create", "edit" for entity_form
	Search    string // entity_list LIKE-search field
	Create    bool   // entity_list: show "New" + mount a create form screen
	Props     map[string]any
	Children  []BlueprintBlock
	Actions   []BlueprintAction
	Transitions []BlueprintTransition // entity_detail: status-transition workflow buttons
	Island    string
	Widget    string
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
	allowed := map[string]bool{"name": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "api_prefix": true, "theme": true, "auth": true, "admin": true}
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
		theme, dark, err := decodeBlueprintTheme(themeNode)
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
	return app, nil
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
// decodeBlueprintTheme returns the light color/font tokens and, from a nested
// `dark:` map, the dark-scheme color overrides.
func decodeBlueprintTheme(node *coreyaml.Node) (light, dark map[string]string, err error) {
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
		allowed := map[string]bool{"name": true, "table": true, "fields": true, "relations": true, "endpoints": true, "soft_delete": true, "multi_tenant": true, "owner_field": true, "access": true, "timestamps": true, "crud": true, "mcp": true, "cursor_field": true, "cursor_fields": true, "indices": true, "properties": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("entities[%d]", i)); err != nil {
			return nil, nil, err
		}
		decl := framework.EntityDeclaration{
			Name:         stringValue(m["name"]),
			Table:        stringValue(m["table"]),
			SoftDelete:   boolValue(m["soft_delete"]),
			MultiTenant:  boolValue(m["multi_tenant"]),
			OwnerField:   stringValue(m["owner_field"]),
			MCP:          boolValue(m["mcp"]),
			CursorField:  stringValue(m["cursor_field"]),
			CursorFields: stringListValue(m["cursor_fields"]),
			Properties:   mapValue(m["properties"]),
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
		allowed := map[string]bool{"label": true, "href": true, "icon": true, "items": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("%s[%d]", label, i)); err != nil {
			return nil, err
		}
		navItem := BlueprintNavItem{
			Label: stringValue(m["label"]),
			Href:  stringValue(m["href"]),
			Icon:  stringValue(m["icon"]),
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
		if err := rejectUnknownKeys(m, map[string]bool{"entity": true, "rows": true}, fmt.Sprintf("seed[%d]", i)); err != nil {
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
		out = append(out, BlueprintSeedEntity{Entity: entity, Rows: rows})
	}
	return out, nil
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
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "entity": true, "fields": true, "limit": true, "empty_text": true, "mode": true, "search": true, "create": true, "props": true, "children": true, "actions": true, "transitions": true, "island": true, "widget": true}
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
	case "page_header", "hero", "card", "stat_row", "stat_card",
		"bar_chart", "pie_chart", "line_chart", "link_button", "callout",
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
	bp = blueprintSynthesizeCRUDScreens(bp)
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
		entityFiles, err := renderGeneratedProject(decls)
		if err != nil {
			return nil, err
		}
		for _, file := range entityFiles {
			files = append(files, generatedFile{name: filepath.Join("entities", file.name), content: file.content})
		}
	}
	if len(bp.Screens) > 0 {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "screens.go"), content: renderBlueprintScreens(bp)})
	}
	if len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0 || len(bp.Helpers) > 0 || len(bp.Seed) > 0 {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "stubs.go"), content: renderBlueprintStubs(bp)})
	}
	if bp.App.Name != "" || bp.App.Module != "" || bp.App.DBDriver != "" || bp.App.DBURL != "" || bp.App.StaticDir != "" || bp.App.OutputDir != "" || len(bp.App.Theme) > 0 || len(bp.Screens) > 0 || len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0 {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "app.go"), content: renderBlueprintApp(bp)})
	}
	if blueprintUsesEntityScreens(bp) {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "resource.go"), content: blueprintResourceGo})
		files = append(files, generatedFile{name: filepath.Join("blueprint", "resource_test.go"), content: blueprintResourceTestGo})
	}
	if bp.App.Module != "" && len(bp.Screens) > 0 {
		files = append(files, generatedFile{name: "e2e_test.go", content: renderBlueprintE2ETest(bp)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// blueprintResourceTestGo is the emitted unit test for the resource engine's
// formatting helpers — owned, runs under `go test`.
const blueprintResourceTestGo = `// Code generated by gofastr. Owned — safe to edit.
package blueprint

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
	accessGated bool   // entity declares `access:` → anonymous writes must be refused
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
			accessGated: decl.Access != nil,
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
	b.WriteString("\t\"io\"\n\t\"net\"\n\t\"net/http\"\n")
	if needsAuthClient {
		b.WriteString("\t\"net/http/cookiejar\"\n\t\"net/url\"\n")
	}
	if crud {
		b.WriteString("\t\"regexp\"\n")
	}
	b.WriteString("\t\"os\"\n\t\"os/exec\"\n\t\"path/filepath\"\n\t\"strings\"\n\t\"testing\"\n\t\"time\"\n)\n\n")
	b.WriteString("func TestBlueprintE2E(t *testing.T) {\n")
	b.WriteString("\tif testing.Short() { t.Skip(\"builds + boots the binary\") }\n")
	b.WriteString("\tdir := t.TempDir()\n")
	b.WriteString("\tbin := filepath.Join(dir, \"app\")\n")
	b.WriteString("\tbuild := exec.Command(\"go\", \"build\", \"-o\", bin, \".\")\n")
	b.WriteString("\tbuild.Stderr = os.Stderr\n")
	b.WriteString("\tif err := build.Run(); err != nil { t.Fatalf(\"build: %v\", err) }\n")
	b.WriteString("\taddr := e2eFreeAddr(t)\n")
	b.WriteString("\tsrv := exec.Command(bin)\n")
	b.WriteString("\tsrv.Dir = dir\n")
	b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=file:\"+filepath.Join(dir, \"e2e.db\"))\n")
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
		b.WriteString(fmt.Sprintf("\tif _, err := client.PostForm(base+\"/auth/login\", url.Values{\"email\": {%q}, \"password\": {%q}}); err != nil { t.Fatalf(\"login: %%v\", err) }\n", adminEmail, adminPass))
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
			b.WriteString(fmt.Sprintf("\n\t// RBAC: an anonymous write to the access-gated %s API is refused.\n", target.entity))
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
package blueprint

import (
	"context"
	"fmt"
	"math"
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

// blueprintResources holds one ResourceConfig per entity, populated by
// RegisterGenerated once the CrudHandlers exist. Screens look entities up by
// name to render their server-side list/detail views.
var blueprintResources = map[string]ResourceConfig{}

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
	if c.Search != "" {
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

	carry := ""
	if search != "" {
		carry = "q=" + search + "&"
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
	return render.Tag("span", map[string]string{"class": "mrd-muted"}, render.Text("—"))
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

// ----- dashboard data binding (stat_card / charts with source) --------------

// blueprintStatValue computes a single metric over an entity for a stat_card:
// agg "count" (optionally filtered "field=value") or "sum" of a numeric field.
func blueprintStatValue(ctx context.Context, entity, agg, field, filterStr, format string) string {
	c, ok := blueprintResources[entity]
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
		return blueprintFmtNum(total)
	}
	n, err := c.Crud.CountAll(ctx, framework.ListOptions{Filters: filters})
	if err != nil {
		return "—"
	}
	return strconv.Itoa(n)
}

type blueprintKV struct {
	k string
	v int
}

func blueprintGroupCounts(ctx context.Context, entity, groupBy string) []blueprintKV {
	c, ok := blueprintResources[entity]
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
	out := make([]blueprintKV, 0, len(order))
	for _, k := range order {
		out = append(out, blueprintKV{k, m[k]})
	}
	return out
}

func blueprintGroupBars(ctx context.Context, entity, groupBy string) []ui.BarChartBar {
	counts := blueprintGroupCounts(ctx, entity, groupBy)
	bars := make([]ui.BarChartBar, 0, len(counts))
	for _, kv := range counts {
		bars = append(bars, ui.BarChartBar{Label: resTitle(kv.k), Value: float64(kv.v)})
	}
	return bars
}

func blueprintGroupSlices(ctx context.Context, entity, groupBy string) []ui.PieSlice {
	counts := blueprintGroupCounts(ctx, entity, groupBy)
	slices := make([]ui.PieSlice, 0, len(counts))
	for _, kv := range counts {
		slices = append(slices, ui.PieSlice{Label: resTitle(kv.k), Value: float64(kv.v)})
	}
	return slices
}

func blueprintFmtNum(f float64) string {
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
	staticDir := bp.App.StaticDir
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
	if bp.App.Admin.Enabled {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/admin\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	if hasSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/filter\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/isolation\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/uihost\"\n")
	if imp := blueprintDriverImport(driver); imp != "" {
		sb.WriteString(fmt.Sprintf("\t_ %q\n", imp))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("\t%q\n", baseImport+"/blueprint"))
	if len(bp.Entities) > 0 {
		sb.WriteString(fmt.Sprintf("\t%q\n", baseImport+"/entities"))
	}
	sb.WriteString(")\n\n")

	sb.WriteString("func main() {\n")
	sb.WriteString("\truntimeIsolation, err := isolation.Resolve(\".\")\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tdb, err := openBlueprintDB(runtimeIsolation)\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tif db != nil {\n\t\tdefer db.Close()\n\t}\n\n")
	sb.WriteString("\toptions := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: blueprint.BlueprintAppName, APIPrefix: blueprint.BlueprintAPIPrefix})}\n")
	sb.WriteString("\tif db != nil {\n\t\toptions = append(options, framework.WithDB(db))\n\t}\n")
	sb.WriteString("\tfwApp := framework.NewApp(options...)\n")
	if len(bp.Entities) > 0 {
		sb.WriteString("\tentities.RegisterAll(fwApp)\n")
	}
	if hasSeed {
		// Apply blueprint seed data after auto-migration, in declared order
		// and idempotently: skip any entity whose table already has rows.
		// Rows go through CreateOne so validation, id generation, and
		// timestamps apply. A row that fails validation is logged and
		// skipped rather than aborting startup — sample seed data shouldn't
		// take the whole app down.
		sb.WriteString("\tfwApp.WithSeed(func(ctx context.Context) error {\n")
		sb.WriteString("\t\tfor _, s := range blueprint.BlueprintSeedData() {\n")
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
	sb.WriteString("\tfwApp.Router().Handle(\"POST\", \"/mcp\", fwApp.MCP)\n")
	sb.WriteString("\tsite := uiapp.NewApp(blueprint.BlueprintAppName)\n")
	sb.WriteString("\tblueprint.RegisterGenerated(fwApp, site, db)\n")
	// BlueprintBaseCSS ships first so the user's static/app.css (loaded
	// after) overrides it; it gives the generated entity blocks modern,
	// responsive defaults out of the box (scrollable tables, form rhythm).
	if staticDir != "" {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithStaticDir(%q), uihost.WithCustomCSS(blueprint.BlueprintFontCSS+blueprint.BlueprintBaseCSS()+uihost.ReadCustomCSSFile(%q))))\n", staticDir, staticDir+"/app.css"))
	} else {
		sb.WriteString("\tfwApp.Mount(uihost.New(site, uihost.WithCustomCSS(blueprint.BlueprintFontCSS+blueprint.BlueprintBaseCSS())))\n")
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
			themeArg = ", Theme: blueprint.BlueprintTheme(), FontFaceCSS: blueprint.BlueprintFontCSS"
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.RegisterBattery(admin.New(admin.Config{PathPrefix: %q, Title: blueprint.BlueprintAppName, AdminRole: %q, LoginPath: %q, DB: db, AuditTable: \"audit_log\"%s}))\n",
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

	sb.WriteString("func openBlueprintDB(runtimeIsolation *isolation.Runtime) (*sql.DB, error) {\n")
	if driver == "" && dbURL == "" {
		sb.WriteString("\treturn nil, nil\n")
	} else {
		sb.WriteString(fmt.Sprintf("\tdriver := getEnv(\"DB_DRIVER\", %q)\n", driver))
		sb.WriteString(fmt.Sprintf("\tdsn := getEnv(\"DATABASE_URL\", %q)\n", dbURL))
		sb.WriteString("\tresolvedDriver, resolvedDSN, err := runtimeIsolation.Database(driver, dsn)\n")
		sb.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		sb.WriteString("\tdriver, dsn = resolvedDriver, resolvedDSN\n")
		sb.WriteString("\tswitch driver {\n")
		sb.WriteString("\tcase \"\", \"none\":\n\t\treturn nil, nil\n")
		sb.WriteString("\tcase \"sqlite\", \"sqlite3\":\n\t\treturn sql.Open(\"sqlite3\", dsn)\n")
		sb.WriteString("\tcase \"postgres\", \"postgresql\":\n\t\treturn sql.Open(\"postgres\", dsn)\n")
		sb.WriteString("\tdefault:\n\t\treturn nil, fmt.Errorf(\"unsupported blueprint db driver %q\", driver)\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("}\n\n")
	sb.WriteString("func getEnv(key, fallback string) string {\n")
	sb.WriteString("\tif v := os.Getenv(key); v != \"\" {\n\t\treturn v\n\t}\n")
	sb.WriteString("\treturn fallback\n")
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
	var sb strings.Builder
	imports := blueprintScreenImports(bp)
	anyCtx := false
	for _, s := range bp.Screens {
		if screenNeedsCtx(s) {
			anyCtx = true
			break
		}
	}
	sb.WriteString("package blueprint\n\n")
	sb.WriteString("import (\n")
	if anyCtx {
		sb.WriteString("\t\"context\"\n\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if imports.component || anyCtx {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/component\"\n")
	}
	if imports.island {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/island\"\n")
	}
	if imports.html {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/html\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	if imports.ui {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if imports.node {
		// core-ui/noderender is the first-party leaf node renderer (core-ui/html
		// + core/render + core-ui/node only). The node IR + renderer are UI
		// primitives, so the generated app depends on core-ui, never on the
		// experimental kiln namespace.
		sb.WriteString("\tnoderender \"github.com/DonaldMurillo/gofastr/core-ui/noderender\"\n")
		sb.WriteString("\tuinode \"github.com/DonaldMurillo/gofastr/core-ui/node\"\n")
	}
	sb.WriteString(")\n\n")
	if imports.node {
		sb.WriteString("type blueprintNodeComponent struct { node uinode.Node }\n\n")
		sb.WriteString("func (c blueprintNodeComponent) Render() render.HTML { return noderender.RenderNode(c.node) }\n\n")
	}
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	for _, screen := range bp.Screens {
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
					expr = blueprintEntityListResourceExpr(block)
				case ctxScreen && isEntityDetailBlock(block):
					expr = blueprintDetailExpr(block)
				case ctxScreen && isEntityCreateBlock(block):
					expr = fmt.Sprintf("blueprintResources[%q].Form(ctx, \"\")", strings.Trim(block.Entity, "/"))
				case ctxScreen && isEntityEditBlock(block):
					expr = fmt.Sprintf("blueprintResources[%q].Form(ctx, s.id)", strings.Trim(block.Entity, "/"))
				default:
					expr = renderBlueprintBlockForScreen(screen, block, []int{i}, entityMap, apiBase)
				}
				sb.WriteString("\t\t" + expr + ",\n")
			}
			sb.WriteString("\t)\n")
		}
		sb.WriteString("}\n\n")
	}
	return sb.String()
}

// blueprintResourceRegistry emits the blueprintResources map population inside
// RegisterGenerated: one ResourceConfig per entity referenced by a server-side
// entity_list/entity_detail screen, wired to its CrudHandler, displayable
// fields, and relation lookups.
func blueprintResourceRegistry(bp Blueprint) string {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	// base path per entity (list route preferred; detail route minus /{id}).
	base := map[string]string{}
	needed := map[string]bool{}
	editable := map[string]bool{} // has a detail screen → Detail shows Edit/Delete
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
	if len(needed) == 0 {
		return ""
	}
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	names := make([]string, 0, len(needed))
	for e := range needed {
		names = append(names, e)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, e := range names {
		decl, ok := entityMap[e]
		if !ok {
			continue
		}
		sb.WriteString("\tblueprintResources[" + fmt.Sprintf("%q", e) + "] = ResourceConfig{\n")
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
	}
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
	expr := fmt.Sprintf("blueprintResources[%q]", entity)
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
// top-level entity_list block: blueprintResources["x"].WithColumns(...).List(ctx).
func blueprintEntityListResourceExpr(block BlueprintBlock) string {
	entity := strings.Trim(block.Entity, "/")
	expr := fmt.Sprintf("blueprintResources[%q]", entity)
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
		"bar_chart", "pie_chart", "line_chart", "link_button", "callout", "divider",
		"markdown", "pricing":
		return true
	}
	return false
}

// blueprintCatalogUsesHTML reports kinds whose emitted code references html.*.
func blueprintCatalogUsesHTML(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "hero", "bar_chart", "pie_chart", "line_chart":
		return true
	}
	return false
}

func blueprintScreenImports(bp Blueprint) screenImportNeeds {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, decl := range bp.Entities {
		entityMap[decl.Name] = decl
	}
	apiBase := blueprintAPIBase(bp.App.APIPrefix)
	var needs screenImportNeeds
	for _, screen := range bp.Screens {
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
					// Server-rendered via the resource engine (blueprintResources,
					// in-package) — no extra import beyond the ctx-screen machinery.
					continue
				}
				if blueprintCatalogKind(kind) {
					needs.ui = true
					if blueprintCatalogUsesHTML(kind) {
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
			return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %q, Value: blueprintStatValue(ctx, %q, %q, %q, %q, %q)})", label, entity, agg, field, filter, format), true
		}
		return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %q, Value: %q})", label, blueprintProp(block, "value")), true
	case "bar_chart", "pie_chart":
		title := blueprintProp(block, "title")
		if src, ok := block.Props["source"].(map[string]any); ok {
			entity, _ := src["entity"].(string)
			groupBy, _ := src["group_by"].(string)
			head := ""
			if title != "" {
				head = fmt.Sprintf("html.Heading(html.HeadingConfig{Level: 2, Class: \"mrd-chart__title\"}, render.Text(%q)), ", title)
			}
			chart := fmt.Sprintf("ui.BarChart(ui.BarChartConfig{Bars: blueprintGroupBars(ctx, %q, %q), ShowLabels: true})", entity, groupBy)
			if kind == "pie_chart" {
				chart = fmt.Sprintf("ui.PieChart(ui.PieChartConfig{Slices: blueprintGroupSlices(ctx, %q, %q)})", entity, groupBy)
			}
			return "render.Tag(\"div\", map[string]string{\"class\": \"mrd-chart\"}, " + head + chart + ")", true
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
		return blueprintEntityListResourceExpr(block)
	}
	if expr, ok := renderBlueprintCatalogBlock(screen, block, path, entityMap, apiBase); ok {
		return expr
	}
	if blueprintBlockUsesNodeRenderer(block) {
		expr := renderBlueprintNodeExpressionForScreen(screen, block, path, entityMap, apiBase)
		if block.Island != "" {
			return fmt.Sprintf("island.NewIsland(%q, blueprintNodeComponent{node: %s}).Render()", block.Island, expr)
		}
		if block.Widget != "" {
			return fmt.Sprintf("component.NewWidget(%q, blueprintNodeComponent{node: %s}).Render()", block.Widget, expr)
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
	return fmt.Sprintf("ui.AuthCard(ui.AuthCardConfig{Title: %q, Body: %s%s})", heading, form, footer)
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
	sb.WriteString("package blueprint\n\n")
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
		sb.WriteString("// BlueprintSeedEntity is one entity's ordered seed rows.\n")
		sb.WriteString("type BlueprintSeedEntity struct {\n\tEntity string\n\tRows   []map[string]any\n}\n\n")
		sb.WriteString("// BlueprintSeedData returns the initial seed data in blueprint-declared\n")
		sb.WriteString("// order (so entities that reference others are inserted after them).\n")
		sb.WriteString("func BlueprintSeedData() []BlueprintSeedEntity {\n")
		sb.WriteString("\treturn []BlueprintSeedEntity{\n")
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
	sb.WriteString("package blueprint\n\n")
	sb.WriteString("import (\n")
	if adminSeed || hasAccess {
		sb.WriteString("\t\"context\"\n")
	}
	sb.WriteString("\t\"database/sql\"\n")
	if len(bp.Endpoints) > 0 {
		sb.WriteString("\t\"net/http\"\n")
	}
	if hasAccess {
		sb.WriteString("\t\"net/url\"\n")
	}
	sb.WriteString("\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if hasAccess {
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
	if hasAccess || rbac {
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
	sb.WriteString(fmt.Sprintf("\tBlueprintAppName = %q\n", name))
	sb.WriteString(fmt.Sprintf("\tBlueprintModule = %q\n", bp.App.Module))
	sb.WriteString(fmt.Sprintf("\tBlueprintDBDriver = %q\n", bp.App.DBDriver))
	sb.WriteString(fmt.Sprintf("\tBlueprintDBURL = %q\n", bp.App.DBURL))
	sb.WriteString(fmt.Sprintf("\tBlueprintStaticDir = %q\n", bp.App.StaticDir))
	sb.WriteString(fmt.Sprintf("\tBlueprintAPIPrefix = %q\n", bp.App.APIPrefix))
	sb.WriteString(")\n\n")
	sb.WriteString(blueprintBaseCSSFunc())
	if hasAccess {
		sb.WriteString("// blueprintAuthPolicy gates a screen: redirect anonymous GETs to the login\n")
		sb.WriteString("// page (with ?next=) and 403 a signed-in user missing the required role.\n")
		sb.WriteString("func blueprintAuthPolicy(loginPath, role string) app.Policy {\n")
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
	if hasMarketing {
		sb.WriteString("// BlueprintMarketingHeader / Footer wrap the public marketing layout.\n")
		sb.WriteString("func BlueprintMarketingHeader() render.HTML {\n")
		sb.WriteString("\treturn ui.SiteHeader(ui.SiteHeaderConfig{\n")
		sb.WriteString("\t\tBrand: ui.Link(ui.LinkConfig{Href: \"/\", Text: BlueprintAppName}),\n")
		sb.WriteString("\t\tNavItems: []ui.SiteHeaderLink{{Label: \"Pricing\", Href: \"/pricing\"}, {Label: \"About\", Href: \"/about\"}, {Label: \"Sign in\", Href: \"/login\"}},\n")
		sb.WriteString("\t\tDrawer: ui.SiteHeaderDrawerSheet,\n")
		if len(bp.App.ThemeDark) > 0 {
			// The app declares a dark scheme — surface the toggle in the header's
			// Actions slot (the component's purpose-built slot) so the toggle is a
			// real ui.ThemeToggle, not hand-rolled markup.
			sb.WriteString("\t\tActions: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),\n")
		}
		sb.WriteString("\t})\n}\n\n")
		sb.WriteString("func BlueprintMarketingFooter() render.HTML {\n")
		sb.WriteString("\treturn ui.SiteFooter(ui.SiteFooterConfig{\n")
		sb.WriteString("\t\tLead: ui.Link(ui.LinkConfig{Href: \"/\", Text: BlueprintAppName}),\n")
		sb.WriteString("\t\tColumns: []ui.SiteFooterColumn{\n")
		sb.WriteString("\t\t\t{Title: \"Product\", Links: []ui.SiteFooterLink{{Label: \"Pricing\", Href: \"/pricing\"}}},\n")
		sb.WriteString("\t\t\t{Title: \"Company\", Links: []ui.SiteFooterLink{{Label: \"About\", Href: \"/about\"}}},\n")
		sb.WriteString("\t\t\t{Title: \"Legal\", Links: []ui.SiteFooterLink{{Label: \"Terms\", Href: \"/terms\"}, {Label: \"Privacy\", Href: \"/privacy\"}}},\n")
		sb.WriteString("\t\t},\n")
		sb.WriteString("\t})\n}\n\n")
	}
	if len(bp.App.Theme) > 0 {
		sb.WriteString("func BlueprintTheme() style.Theme {\n")
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
	// BlueprintFontCSS holds the @font-face rules for the theme's configured
	// fonts (self-hosted from <static>/fonts/<slug>.woff2). It is the single
	// font-loading source, shared verbatim by the UI host and the admin battery
	// so every surface loads identical fonts. Empty when no fonts are declared.
	sb.WriteString(fmt.Sprintf("// BlueprintFontCSS holds the @font-face rules for the app's fonts, shared by\n// the UI host and the admin battery so every surface loads identical fonts.\nconst BlueprintFontCSS = %q\n\n", blueprintFontFaceCSS(bp.App.Theme)))
	if len(bp.Nav) > 0 {
		sb.WriteString("// BlueprintSidebarConfig returns the navigation sidebar configuration.\n")
		sb.WriteString("func BlueprintSidebarConfig() ui.SidebarConfig {\n")
		sb.WriteString(fmt.Sprintf("\treturn ui.SidebarConfig{Title: %q, Items: []ui.SidebarItem{\n", name))
		for _, item := range bp.Nav {
			renderNavItemGo(&sb, item, "\t\t")
		}
		sb.WriteString("\t}}\n}\n\n")
	}
	sb.WriteString("// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.\n")
	sb.WriteString("func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {\n")
	sb.WriteString("\tif site == nil {\n")
	sb.WriteString(fmt.Sprintf("\t\tsite = app.NewApp(%q)\n", name))
	sb.WriteString("\t}\n")
	sb.WriteString(blueprintResourceRegistry(bp))
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\tsite.WithTheme(BlueprintTheme())\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\tsbCfg := BlueprintSidebarConfig()\n")
		sb.WriteString("\tsb := ui.Sidebar(sbCfg)\n")
		// A minimal app top bar carrying the theme toggle (the sidebar shell has
		// no header otherwise, so the app couldn't switch light/dark).
		sb.WriteString("\tappHeader := app.NewStaticComponent(ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyEnd}, ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon})))\n")
		sb.WriteString("\tappLayout := app.NewLayout(\"app\").WithSidebar(sb).WithHeader(appHeader)\n")
		sb.WriteString("\tsite.SetDefaultLayout(appLayout)\n")
		sb.WriteString("\tui.MountSidebar(blueprintRouterMounter{fwApp.Router()}, sbCfg)\n")
	}
	if hasMarketing {
		sb.WriteString("\tmarketingLayout := app.NewLayout(\"marketing\").\n")
		sb.WriteString("\t\tWithContainer().\n")
		sb.WriteString("\t\tWithHeader(app.NewStaticComponent(BlueprintMarketingHeader())).\n")
		sb.WriteString("\t\tWithFooter(app.NewStaticComponent(BlueprintMarketingFooter()))\n")
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
		if bp.App.Auth.JWTSecret != "" {
			sb.WriteString(fmt.Sprintf(", JWTSecret: %q", bp.App.Auth.JWTSecret))
		}
		sb.WriteString("}\n")
		sb.WriteString("\t\tauthCfg.UserStore = auth.NewEntityUserStore(db, \"auth_users\")\n")
		sb.WriteString("\t\tauthCfg.SessionStore = auth.NewEntitySessionStore(db, \"auth_sessions\")\n")
		sb.WriteString("\t\tauthMgr := auth.New(authCfg)\n")
		sb.WriteString("\t\tauthMgr.Use(auth.NewCorePlugin())\n")
		sb.WriteString("\t\t// Auto-create auth tables if they don't exist.\n")
		sb.WriteString("\t\tdb.Exec(`CREATE TABLE IF NOT EXISTS auth_users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set INTEGER NOT NULL DEFAULT 0)`)\n")
		sb.WriteString("\t\tdb.Exec(`CREATE TABLE IF NOT EXISTS auth_sessions (id TEXT NOT NULL, token TEXT UNIQUE NOT NULL, user_id TEXT NOT NULL, created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, two_factor_verified INTEGER NOT NULL DEFAULT 0, pending_two_factor INTEGER NOT NULL DEFAULT 0)`)\n")
		sb.WriteString("\t\tauthMgr.Init(fwApp)\n")
		if bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != "" {
			sb.WriteString("\t\t// Bootstrap admin account so the back-office is reachable on a\n")
			sb.WriteString("\t\t// fresh database. Created only when absent (idempotent).\n")
			sb.WriteString(fmt.Sprintf("\t\tif _, _, err := authCfg.UserStore.FindByEmail(context.Background(), %q); err != nil {\n", bp.App.Admin.SeedEmail))
			sb.WriteString(fmt.Sprintf("\t\t\tif h, herr := auth.HashPassword(%q); herr == nil {\n", bp.App.Admin.SeedPassword))
			adminRole := bp.App.Admin.Role
			if adminRole == "" {
				adminRole = "admin"
			}
			sb.WriteString(fmt.Sprintf("\t\t\t\tauthCfg.UserStore.CreateUser(context.Background(), %q, h, []string{%q, \"user\"})\n", bp.App.Admin.SeedEmail, adminRole))
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t}\n")
		}
		sb.WriteString("\t\t// Resolve the session cookie to a user on every request so\n")
		sb.WriteString("\t\t// owner/access-scoped CRUD sees the logged-in user. Without\n")
		sb.WriteString("\t\t// this, authorized requests fail closed (401) just like\n")
		sb.WriteString("\t\t// anonymous ones.\n")
		sb.WriteString("\t\tfwApp.Use(auth.SessionMiddleware(authMgr))\n")
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
			sb.WriteString("\t\tblueprintRBAC := access.NewRolePolicy()\n")
			sb.WriteString(fmt.Sprintf("\t\tblueprintRBAC.Grant(%q, access.Wildcard)\n", adminRole))
			sb.WriteString("\t\tfwApp.Use(access.Middleware(blueprintRBAC, func(ctx context.Context) []string {\n")
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
	for _, screen := range bp.Screens {
		route := blueprintScreenRoutePath(screen.Route)
		typeName := toCamelCase(screen.Name) + "Screen"
		layoutExpr := "nil"
		switch screen.Layout {
		case "marketing":
			layoutExpr = "marketingLayout"
		case "app":
			if len(bp.Nav) > 0 {
				layoutExpr = "appLayout"
			}
		default:
			if len(bp.Nav) > 0 {
				layoutExpr = "appLayout"
			}
		}
		if screen.Access.Auth {
			sb.WriteString(fmt.Sprintf("\tsite.RegisterScreen(app.NewScreen(%q, &%s{}).WithTitle(%q).WithPolicy(blueprintAuthPolicy(%q, %q)), %s)\n",
				route, typeName, screen.Title, "/login", screen.Access.Role, layoutExpr))
		} else {
			sb.WriteString(fmt.Sprintf("\tsite.Register(%q, &%s{}, %s)\n", route, typeName, layoutExpr))
		}
	}
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
		sb.WriteString("\t_ = blueprintRouterMounter{}\n")
	}
	sb.WriteString("}\n\n")
	if len(bp.Nav) > 0 {
		sb.WriteString("// blueprintRouterMounter adapts framework's *router.Router to ui.WidgetMounter.\n")
		sb.WriteString("type blueprintRouterMounter struct{ r *router.Router }\n\n")
		sb.WriteString("func (m blueprintRouterMounter) MountWidget(def *widget.Definition) {\n")
		sb.WriteString("\twidget.Mount(m.r, def)\n")
		sb.WriteString("}\n")
		return sb.String()
	}
	return sb.String()
}
// blueprintBaseCSSFunc emits the BlueprintBaseCSS() function. It returns the
// empty string: every generated surface — marketing, app, entity list/detail,
// entity forms, auth — composes framework/ui components and core-ui/app layouts
// that ship their own CSS (auto-injected by the UI host). The generator ships
// ZERO bespoke styling, which is the proof the design system is cohesive and
// composable. This owned function stays as an extension point: an app can add
// its own base CSS here (or, preferably, in static/app.css).
func blueprintBaseCSSFunc() string {
	var sb strings.Builder
	sb.WriteString("// BlueprintBaseCSS is an owned extension point for app-specific base CSS.\n")
	sb.WriteString("// It's empty by default: every generated surface composes framework/ui\n")
	sb.WriteString("// components and core-ui/app layouts that ship their own CSS, so the\n")
	sb.WriteString("// generated app ships no bespoke styling. Add app CSS here or in static/app.css.\n")
	sb.WriteString("func BlueprintBaseCSS() string {\n")
	sb.WriteString("\treturn \"\"\n")
	sb.WriteString("}\n\n")
	return sb.String()
}

func renderNavItemGo(sb *strings.Builder, item BlueprintNavItem, indent string) {
	sb.WriteString(fmt.Sprintf("%s{Label: %q, Href: %q", indent, item.Label, item.Href))
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

// blueprintFontHeadHTML returns the webfont <link> tags (preconnect + Google
// blueprintFontSlug turns a font family name into the self-hosted file slug,
// e.g. "Bricolage Grotesque" -> "bricolage-grotesque".
func blueprintFontSlug(family string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(family), " ", "-"))
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
