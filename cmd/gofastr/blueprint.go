package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	Theme     map[string]string
	Auth      BlueprintAuth
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
	Body        []BlueprintBlock
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
	Props     map[string]any
	Children  []BlueprintBlock
	Actions   []BlueprintAction
	Island    string
	Widget    string
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
	allowed := map[string]bool{"name": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "theme": true, "auth": true}
	if err := rejectUnknownKeys(m, allowed, "app"); err != nil {
		return BlueprintApp{}, err
	}
	app := BlueprintApp{
		Name:      stringValue(m["name"]),
		Module:    stringValue(m["module"]),
		StaticDir: stringValue(m["static_dir"]),
		OutputDir: stringValue(m["output_dir"]),
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
		theme, err := decodeBlueprintTheme(themeNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.Theme = theme
	}
	if authNode := m["auth"]; authNode != nil {
		auth, err := decodeBlueprintAuth(authNode)
		if err != nil {
			return BlueprintApp{}, err
		}
		app.Auth = auth
	}
	return app, nil
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
func decodeBlueprintTheme(node *coreyaml.Node) (map[string]string, error) {
	m, err := expectMap(node, "app.theme")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m))
	for key, value := range m {
		if value == nil || value.Kind != coreyaml.Scalar {
			return nil, fmt.Errorf("app.theme.%s must be a scalar CSS token value", key)
		}
		out[key] = stringValue(value)
	}
	return out, nil
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
		allowed := map[string]bool{"name": true, "route": true, "title": true, "description": true, "type": true, "body": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("screens[%d]", i)); err != nil {
			return nil, err
		}
		screen := BlueprintScreen{
			Name:        stringValue(m["name"]),
			Route:       stringValue(m["route"]),
			Title:       stringValue(m["title"]),
			Description: stringValue(m["description"]),
			Type:        stringValue(m["type"]),
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
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "entity": true, "fields": true, "limit": true, "empty_text": true, "mode": true, "props": true, "children": true, "actions": true, "island": true, "widget": true}
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
		out = append(out, BlueprintBlock{
			Type:      stringValue(m["type"]),
			Kind:      stringValue(m["kind"]),
			Text:      stringValue(m["text"]),
			Level:     intValue(m["level"]),
			Class:     stringValue(m["class"]),
			Href:      stringValue(m["href"]),
			Entity:    stringValue(m["entity"]),
			Fields:    stringListValue(m["fields"]),
			Limit:     intValue(m["limit"]),
			EmptyText: stringValue(m["empty_text"]),
			Mode:      stringValue(m["mode"]),
			Props:     mapValue(m["props"]),
			Children:  children,
			Actions:   actions,
			Island:    stringValue(m["island"]),
			Widget:    stringValue(m["widget"]),
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
		if _, ok := blueprintThemeColorPath(key); !ok {
			return fmt.Errorf("blueprint: app.theme has unsupported color token %q", key)
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

func renderBlueprintFiles(bp Blueprint) ([]generatedFile, error) {
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
	if len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0 || len(bp.Helpers) > 0 {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "stubs.go"), content: renderBlueprintStubs(bp)})
	}
	if bp.App.Name != "" || bp.App.Module != "" || bp.App.DBDriver != "" || bp.App.DBURL != "" || bp.App.StaticDir != "" || bp.App.OutputDir != "" || len(bp.App.Theme) > 0 || len(bp.Screens) > 0 || len(bp.Endpoints) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0 {
		files = append(files, generatedFile{name: filepath.Join("blueprint", "app.go"), content: renderBlueprintApp(bp)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

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
	outputDir := bp.App.OutputDir
	if outputDir == "" {
		outputDir = "gen"
	}
	outputDir = strings.TrimPrefix(filepath.ToSlash(outputDir), "./")
	baseImport := strings.TrimSuffix(bp.App.Module, "/") + "/" + strings.TrimSuffix(outputDir, "/")

	var sb strings.Builder
	sb.WriteString("// Code generated by gofastr. DO NOT EDIT.\npackage main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"database/sql\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"log\"\n")
	sb.WriteString("\t\"net/http\"\n")
	sb.WriteString("\t\"os\"\n\n")
	sb.WriteString("\tuiapp \"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
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
	sb.WriteString("\toptions := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: blueprint.BlueprintAppName})}\n")
	sb.WriteString("\tif db != nil {\n\t\toptions = append(options, framework.WithDB(db))\n\t}\n")
	sb.WriteString("\tfwApp := framework.NewApp(options...)\n")
	if len(bp.Entities) > 0 {
		sb.WriteString("\tentities.RegisterAll(fwApp)\n")
	}
	sb.WriteString("\tfwApp.Router().Handle(\"POST\", \"/mcp\", fwApp.MCP)\n")
	sb.WriteString("\tsite := uiapp.NewApp(blueprint.BlueprintAppName)\n")
	sb.WriteString("\tblueprint.RegisterGenerated(fwApp, site, db)\n")
	if staticDir != "" {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithStaticDir(%q)))\n", staticDir))
	} else {
		sb.WriteString("\tfwApp.Mount(uihost.New(site))\n")
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
	sb.WriteString("// Code generated by gofastr. DO NOT EDIT.\npackage blueprint\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if imports.component {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/component\"\n")
	}
	if imports.island {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/island\"\n")
	}
	if imports.html {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/html\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	if imports.node {
		// kiln/noderender is the leaf node renderer (core-ui/html + core/render
		// + kiln/world only). Importing kiln/render here would drag Kiln's
		// authoring engine (kiln/expr, kiln/effect, framework) into the shipped
		// app — see G1.
		sb.WriteString("\tkilnrender \"github.com/DonaldMurillo/gofastr/kiln/noderender\"\n")
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/kiln/world\"\n")
	}
	sb.WriteString(")\n\n")
	if imports.node {
		sb.WriteString("type blueprintNodeComponent struct { node world.Node }\n\n")
		sb.WriteString("func (c blueprintNodeComponent) Render() render.HTML { return kilnrender.RenderNode(c.node) }\n\n")
	}
	for _, screen := range bp.Screens {
		typeName := toCamelCase(screen.Name) + "Screen"
		hasActions := screenHasActions(screen)
		sb.WriteString(fmt.Sprintf("type %s struct{}\n\n", typeName))
		sb.WriteString(fmt.Sprintf("func (s *%s) ScreenTitle() string { return %q }\n", typeName, screen.Title))
		sb.WriteString(fmt.Sprintf("func (s *%s) ScreenDescription() string { return %q }\n", typeName, screen.Description))
		typeConst, _ := screenTypeConst(screen.Type)
		sb.WriteString(fmt.Sprintf("func (s *%s) ScreenType() app.ScreenType { return %s }\n", typeName, typeConst))
		if hasActions {
			sb.WriteString(fmt.Sprintf("func (s *%s) ComponentID() string { return %q }\n", typeName, screenActionComponentID(screen)))
			sb.WriteString(fmt.Sprintf("func (s *%s) Actions() {\n", typeName))
			for _, action := range screenActions(screen) {
				sb.WriteString(fmt.Sprintf("\tcomponent.On(%q, func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS(%q))\n", blueprintActionName(action), action.ClientJS))
			}
			sb.WriteString("}\n")
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("func (s *%s) Render() render.HTML {\n", typeName))
		rootAttrs := "nil"
		if hasActions {
			rootAttrs = fmt.Sprintf("map[string]string{\"data-component\": s.ComponentID()}")
		}
		if len(screen.Body) == 0 {
			sb.WriteString(fmt.Sprintf("\treturn render.Tag(\"div\", %s, html.Heading(html.HeadingConfig{Level: 1}, render.Text(%q)))\n", rootAttrs, screen.TitleOrName()))
		} else {
			sb.WriteString(fmt.Sprintf("\treturn render.Tag(\"div\", %s,\n", rootAttrs))
			for i, block := range screen.Body {
				sb.WriteString("\t\t" + renderBlueprintBlockForScreen(screen, block, []int{i}, entityMap) + ",\n")
			}
			sb.WriteString("\t)\n")
		}
		sb.WriteString("}\n\n")
	}
	return sb.String()
}

type screenImportNeeds struct {
	component bool
	island    bool
	html      bool
	node      bool
}

func blueprintScreenImports(bp Blueprint) screenImportNeeds {
	var needs screenImportNeeds
	for _, screen := range bp.Screens {
		// Empty-body screens emit html.Heading directly (see
		// renderBlueprintStubs / the screen Render() empty path).
		if len(screen.Body) == 0 {
			needs.html = true
		}
		if screenHasActions(screen) {
			needs.component = true
		}
		// Only TOP-LEVEL blocks drive html/island/widget/node imports.
		// A node block's children are rendered as part of the parent's
		// world.Node tree (via the node renderer), never via html.* and
		// never via island.NewIsland / component.NewWidget — so we must
		// not recurse those needs into children.
		for _, block := range screen.Body {
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
			if isEntityListBlock(block) {
				// Rendered via kilnrender.RenderNode(...).
				needs.node = true
				continue
			}
			if blueprintTopLevelBlockEmitsHTML(block) {
				needs.html = true
			}
		}
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

func screenHasActions(screen BlueprintScreen) bool {
	return len(screenActions(screen)) > 0
}

func screenActions(screen BlueprintScreen) []BlueprintAction {
	var actions []BlueprintAction
	var walk func([]BlueprintBlock, []int)
	walk = func(blocks []BlueprintBlock, path []int) {
		for i, block := range blocks {
			blockPath := append(append([]int(nil), path...), i)
			actions = append(actions, block.Actions...)
			if isEntityListBlock(block) {
				actions = append(actions, BlueprintAction{
					Name:     blueprintEntityListActionName(screen, block, blockPath),
					ClientJS: blueprintEntityListClientJS(block),
				})
			}
			walk(block.Children, blockPath)
		}
	}
	walk(screen.Body, nil)
	return actions
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

func renderBlueprintBlockForScreen(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration) string {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "entity_form":
		return renderBlueprintEntityFormExpression(screen, block, path, entityMap)
	case "entity_detail":
		return renderBlueprintEntityDetailExpression(screen, block, path, entityMap)
	}
	if isEntityListBlock(block) {
		return "kilnrender.RenderNode(" + renderBlueprintEntityListNodeExpression(screen, block, path) + ")"
	}
	if blueprintBlockUsesNodeRenderer(block) {
		expr := renderBlueprintNodeExpressionForScreen(screen, block, path)
		if block.Island != "" {
			return fmt.Sprintf("island.NewIsland(%q, blueprintNodeComponent{node: %s}).Render()", block.Island, expr)
		}
		if block.Widget != "" {
			return fmt.Sprintf("component.NewWidget(%q, blueprintNodeComponent{node: %s}).Render()", block.Widget, expr)
		}
		return "kilnrender.RenderNode(" + expr + ")"
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
	return renderBlueprintNodeExpressionForScreen(BlueprintScreen{}, block, nil)
}

func renderBlueprintNodeExpressionForScreen(screen BlueprintScreen, block BlueprintBlock, path []int) string {
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
	sb.WriteString("world.Node{")
	sb.WriteString(fmt.Sprintf("Kind: %q", kind))
	if len(props) > 0 {
		literal, err := renderGoLiteral(props)
		if err == nil {
			sb.WriteString(", Props: " + literal)
		}
	}
	if len(block.Children) > 0 {
		sb.WriteString(", Children: []world.Node{")
		for i, child := range block.Children {
			if i > 0 {
				sb.WriteString(", ")
			}
			childPath := append(append([]int(nil), path...), i)
			sb.WriteString(renderBlueprintNodeExpressionForScreen(screen, child, childPath))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

func isEntityListBlock(block BlueprintBlock) bool {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	return strings.EqualFold(strings.TrimSpace(kind), "entity_list")
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
		"class":            "gofastr-entity-list",
		"data-entity-list": entity,
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
	return "world.Node{Kind: \"section\", Props: " + literal + ", Children: []world.Node{" + strings.Join(children, ", ") + "}}"
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

func blueprintEntityListClientJS(block BlueprintBlock) string {
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
  const entity = %q;
  const fields = %s;
  const root = document.querySelector('[data-entity-list="' + entity + '"]');
  const body = root && root.querySelector('[data-entity-list-body]');
  if (!body) return;
  const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';
  body.innerHTML = table('<tr><td colspan="' + fields.length + '">Loading...</td></tr>');
  try {
    const res = await fetch('/' + entity + '?limit=' + %d, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const payload = await res.json();
    const rows = Array.isArray(payload.data) ? payload.data : [];
    if (!rows.length) {
      body.innerHTML = table('<tr><td colspan="' + fields.length + '">%s</td></tr>');
      return;
    }
    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));
  } catch (err) {
    body.innerHTML = table('<tr><td colspan="' + fields.length + '">Failed to load ' + esc(entity) + '</td></tr>');
  }
})();`, entity, string(fieldsRaw), limit, htmlEscapeJSString(emptyText))
}

func htmlEscapeJSString(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", `'`, "&#39;")
	return replacer.Replace(value)
}

// renderBlueprintEntityFormExpression generates a kiln node that renders a
// create/edit form for the given entity. Fields are generated from the entity
// definition; the form POSTs to the CRUD endpoint. Client JS handles submit.
func renderBlueprintEntityFormExpression(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("render.Tag(\"div\", nil, render.Text(%q))", "entity_form: unknown entity "+entity)
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
	apiPath := "/" + entity
	if mode == "edit" {
		apiPath = "/" + entity + "/{id}"
	}
	props := map[string]any{
		"class":            "gofastr-entity-form",
		"data-entity-form": entity,
		"data-entity-mode": mode,
		"data-form-action": apiPath,
		"data-action":      actionName,
	}
	var children []string
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind:  "heading",
		Props: map[string]any{"level": int64(2), "text": title},
	}))
	// Generate form fields from entity definition
	filterFields := block.Fields
	for _, field := range decl.Fields {
		if field.Name == "id" || field.Name == "created_at" || field.Name == "updated_at" || field.Name == "deleted_at" {
			continue
		}
		if field.Hidden {
			continue
		}
		if field.AutoGenerate != "" {
			continue
		}
		if field.ReadOnly {
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
			inputType = "hidden"
		}
		if inputType == "select" && len(field.Values) > 0 {
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind: "div",
				Props: map[string]any{
					"class": "form-field",
					"text":  label,
				},
				Children: []BlueprintBlock{
					{
						Kind: "select",
						Props: map[string]any{
							"name":     field.Name,
							"id":       "field-" + field.Name,
							"required": field.Required,
						},
					},
				},
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
		if inputType == "hidden" {
			// relation fields get a hidden input
			children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
				Kind: "input",
				Props: map[string]any{
					"type": "hidden",
					"name": field.Name,
					"id":   "field-" + field.Name,
				},
			}))
		}
	}
	// Submit button
	submitLabel := "Create"
	if mode == "edit" {
		submitLabel = "Update"
	}
	children = append(children, renderBlueprintNodeExpression(BlueprintBlock{
		Kind: "button",
		Props: map[string]any{
			"type":             "submit",
			"text":             submitLabel,
			"data-action":      actionName + "_submit",
			"data-form-submit": entity,
		},
	}))
	literal, err := renderGoLiteral(props)
	if err != nil {
		literal = "nil"
	}
	return "kilnrender.RenderNode(world.Node{Kind: \"form\", Props: " + literal + ", Children: []world.Node{" + strings.Join(children, ", ") + "}})"
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

// renderBlueprintEntityDetailExpression generates a kiln node that renders
// a detail view for a single entity record. Client JS fetches the entity
// by ID and populates the field values.
func renderBlueprintEntityDetailExpression(screen BlueprintScreen, block BlueprintBlock, path []int, entityMap map[string]framework.EntityDeclaration) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("render.Tag(\"div\", nil, render.Text(%q))", "entity_detail: unknown entity "+entity)
	}
	actionName := blueprintEntityDetailActionName(screen, block, path)
	title := block.Text
	if title == "" {
		title = toDisplayName(entity) + " Details"
	}
	props := map[string]any{
		"class":              "gofastr-entity-detail",
		"data-entity-detail": entity,
		"data-action":        actionName,
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
	return "kilnrender.RenderNode(world.Node{Kind: \"section\", Props: " + literal + ", Children: []world.Node{" + strings.Join(children, ", ") + "}})"
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
func toDisplayName(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
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

func renderBlueprintStubs(bp Blueprint) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by gofastr. DO NOT EDIT.\npackage blueprint\n\n")
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
		sb.WriteString("// BlueprintSeedData returns the initial seed data for the application.\n")
		sb.WriteString("// Call this from main.go to populate empty tables on first boot.\n")
		sb.WriteString("func BlueprintSeedData() map[string][]map[string]any {\n")
		sb.WriteString("\treturn map[string][]map[string]any{\n")
		for _, seed := range bp.Seed {
			sb.WriteString(fmt.Sprintf("\t\t%q: {\n", seed.Entity))
			for _, row := range seed.Rows {
				sb.WriteString("\t\t\t{")
				first := true
				for k, v := range row {
					if !first {
						sb.WriteString(", ")
					}
					first = false
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
			sb.WriteString("\t\t},\n")
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
	sb.WriteString("// Code generated by gofastr. DO NOT EDIT.\npackage blueprint\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"database/sql\"\n")
	if len(bp.Endpoints) > 0 {
		sb.WriteString("\t\"net/http\"\n\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget\"\n")
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/router\"\n")
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if blueprintNeedsToasts(bp) {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget/preset\"\n")
		if len(bp.Nav) == 0 {
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget\"\n")
		}
	}
	if bp.App.Auth.Enabled {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n")
	}
	if blueprintHasSoftDelete(bp) {
		if len(bp.Nav) == 0 {
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
		}
		if len(bp.Nav) == 0 && !blueprintNeedsToasts(bp) {
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget\"\n")
		}
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n)\n\n")
	sb.WriteString("const (\n")
	sb.WriteString(fmt.Sprintf("\tBlueprintAppName = %q\n", name))
	sb.WriteString(fmt.Sprintf("\tBlueprintModule = %q\n", bp.App.Module))
	sb.WriteString(fmt.Sprintf("\tBlueprintDBDriver = %q\n", bp.App.DBDriver))
	sb.WriteString(fmt.Sprintf("\tBlueprintDBURL = %q\n", bp.App.DBURL))
	sb.WriteString(fmt.Sprintf("\tBlueprintStaticDir = %q\n", bp.App.StaticDir))
	sb.WriteString(")\n\n")
	if len(bp.App.Theme) > 0 {
		sb.WriteString("func BlueprintTheme() style.Theme {\n")
		sb.WriteString("\ttheme := style.DefaultTheme()\n")
		for _, key := range sortedStringMapKeys(bp.App.Theme) {
			if path, ok := blueprintThemeColorPath(key); ok {
				sb.WriteString(fmt.Sprintf("\ttheme.Colors.%s.Value = %q\n", path, bp.App.Theme[key]))
			}
		}
		sb.WriteString("\treturn theme\n")
		sb.WriteString("}\n\n")
	}
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
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\tsite.WithTheme(BlueprintTheme())\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\tsbCfg := BlueprintSidebarConfig()\n")
		sb.WriteString("\tsb := ui.Sidebar(sbCfg)\n")
		sb.WriteString("\tlayout := app.NewLayout(\"blueprint\").WithSidebar(sb)\n")
		sb.WriteString("\tsite.SetDefaultLayout(layout)\n")
		sb.WriteString("\tui.MountSidebar(blueprintRouterMounter{fwApp.Router()}, sbCfg)\n")
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
		sb.WriteString("\t\t// Resolve the session cookie to a user on every request so\n")
		sb.WriteString("\t\t// owner/access-scoped CRUD sees the logged-in user. Without\n")
		sb.WriteString("\t\t// this, authorized requests fail closed (401) just like\n")
		sb.WriteString("\t\t// anonymous ones.\n")
		sb.WriteString("\t\tfwApp.Use(auth.SessionMiddleware(authMgr))\n")
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
		sb.WriteString(fmt.Sprintf("\tsite.Register(%q, &%sScreen{}, nil)\n", screen.Route, toCamelCase(screen.Name)))
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
