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
)

type Blueprint struct {
	App        BlueprintApp
	Entities   []framework.EntityDeclaration
	Screens    []BlueprintScreen
	Endpoints  []BlueprintEndpoint
	Middleware []BlueprintNamedStub
	Plugins    []BlueprintNamedStub
	Helpers    []BlueprintNamedStub
}

type BlueprintApp struct {
	Name      string
	Module    string
	DBDriver  string
	DBURL     string
	StaticDir string
	OutputDir string
	Theme     map[string]string
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
	Type     string
	Kind     string
	Text     string
	Level    int
	Class    string
	Href     string
	Props    map[string]any
	Children []BlueprintBlock
	Actions  []BlueprintAction
	Island   string
	Widget   string
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
	allowed := map[string]bool{"app": true, "entities": true, "screens": true, "endpoints": true, "middleware": true, "plugins": true, "helpers": true}
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
	allowed := map[string]bool{"name": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "theme": true}
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
	return app, nil
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
		allowed := map[string]bool{"name": true, "table": true, "fields": true, "relations": true, "endpoints": true, "soft_delete": true, "multi_tenant": true, "timestamps": true, "crud": true, "mcp": true, "cursor_field": true, "cursor_fields": true, "indices": true, "properties": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("entities[%d]", i)); err != nil {
			return nil, nil, err
		}
		decl := framework.EntityDeclaration{
			Name:         stringValue(m["name"]),
			Table:        stringValue(m["table"]),
			SoftDelete:   boolValue(m["soft_delete"]),
			MultiTenant:  boolValue(m["multi_tenant"]),
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
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "props": true, "children": true, "actions": true, "island": true, "widget": true}
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
			Type:     stringValue(m["type"]),
			Kind:     stringValue(m["kind"]),
			Text:     stringValue(m["text"]),
			Level:    intValue(m["level"]),
			Class:    stringValue(m["class"]),
			Href:     stringValue(m["href"]),
			Props:    mapValue(m["props"]),
			Children: children,
			Actions:  actions,
			Island:   stringValue(m["island"]),
			Widget:   stringValue(m["widget"]),
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
	for key := range bp.App.Theme {
		if _, ok := blueprintThemeColorPath(key); !ok {
			return fmt.Errorf("blueprint: app.theme has unsupported color token %q", key)
		}
	}
	entityNames := map[string]bool{}
	for _, decl := range bp.Entities {
		if decl.Name == "" {
			return fmt.Errorf("blueprint: entity name is required")
		}
		if entityNames[decl.Name] {
			return fmt.Errorf("blueprint: duplicate entity %q", decl.Name)
		}
		entityNames[decl.Name] = true
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
		for _, rel := range decl.Relations {
			if rel.Entity == "" {
				return fmt.Errorf("blueprint: entity %q relation %q target entity is required", decl.Name, rel.Name)
			}
			if !entityNames[rel.Entity] {
				return fmt.Errorf("blueprint: entity %q relation %q targets unknown entity %q", decl.Name, rel.Name, rel.Entity)
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
			if err := validateBlueprintBlock(screen.Name, block); err != nil {
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

func validateBlueprintBlock(screenName string, block BlueprintBlock) error {
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
	default:
		return fmt.Errorf("blueprint: screen %q has unsupported block type %q", screenName, kind)
	}
	for _, child := range block.Children {
		if err := validateBlueprintBlock(screenName, child); err != nil {
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

func renderBlueprintScreens(bp Blueprint) string {
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
		sb.WriteString("\tkilnrender \"github.com/DonaldMurillo/gofastr/kiln/render\"\n")
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
			for _, block := range screen.Body {
				sb.WriteString("\t\t" + renderBlueprintBlock(block) + ",\n")
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
		if len(screen.Body) == 0 {
			needs.html = true
		}
		if screenHasActions(screen) {
			needs.component = true
		}
		var walk func([]BlueprintBlock)
		walk = func(blocks []BlueprintBlock) {
			for _, block := range blocks {
				if block.Widget != "" {
					needs.component = true
				}
				if block.Island != "" {
					needs.island = true
				}
				if blueprintBlockUsesNodeRenderer(block) {
					needs.node = true
				} else {
					needs.html = true
				}
				walk(block.Children)
			}
		}
		walk(screen.Body)
	}
	return needs
}

func blueprintBlockUsesNodeRenderer(block BlueprintBlock) bool {
	return block.Kind != "" || len(block.Props) > 0 || len(block.Children) > 0 || len(block.Actions) > 0 || block.Island != "" || block.Widget != ""
}

func screenHasActions(screen BlueprintScreen) bool {
	return len(screenActions(screen)) > 0
}

func screenActions(screen BlueprintScreen) []BlueprintAction {
	var actions []BlueprintAction
	var walk func([]BlueprintBlock)
	walk = func(blocks []BlueprintBlock) {
		for _, block := range blocks {
			actions = append(actions, block.Actions...)
			walk(block.Children)
		}
	}
	walk(screen.Body)
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

func renderBlueprintBlock(block BlueprintBlock) string {
	if blueprintBlockUsesNodeRenderer(block) {
		expr := renderBlueprintNodeExpression(block)
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
			sb.WriteString(renderBlueprintNodeExpression(child))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

func renderBlueprintStubs(bp Blueprint) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by gofastr. DO NOT EDIT.\npackage blueprint\n\n")
	needsHTTP := len(bp.Endpoints) > 0 || len(bp.Middleware) > 0
	needsFramework := len(bp.Plugins) > 0
	if needsHTTP || needsFramework {
		sb.WriteString("import (\n")
		if needsHTTP {
			sb.WriteString("\t\"net/http\"\n")
		}
		if needsFramework {
			if needsHTTP {
				sb.WriteString("\n")
			}
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
		}
		sb.WriteString(")\n\n")
	}
	for _, endpoint := range bp.Endpoints {
		sb.WriteString(fmt.Sprintf("func %s(w http.ResponseWriter, r *http.Request) {\n", toCamelCase(endpoint.Handler)))
		sb.WriteString(fmt.Sprintf("\thttp.Error(w, %q, http.StatusNotImplemented)\n", "TODO: implement "+endpoint.Handler))
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
	if len(bp.Endpoints) > 0 {
		sb.WriteString("\t\"net/http\"\n\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n")
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
	sb.WriteString("// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.\n")
	sb.WriteString("func RegisterGenerated(fwApp *framework.App, site *app.App) {\n")
	sb.WriteString("\tif site == nil {\n")
	sb.WriteString(fmt.Sprintf("\t\tsite = app.NewApp(%q)\n", name))
	sb.WriteString("\t}\n")
	if len(bp.App.Theme) > 0 {
		sb.WriteString("\tsite.WithTheme(BlueprintTheme())\n")
	}
	for _, screen := range bp.Screens {
		sb.WriteString(fmt.Sprintf("\tsite.Register(%q, &%sScreen{}, nil)\n", screen.Route, toCamelCase(screen.Name)))
	}
	for _, endpoint := range bp.Endpoints {
		sb.WriteString(fmt.Sprintf("\tfwApp.Router.Handle(%q, %q, http.HandlerFunc(%s))\n", strings.ToUpper(endpoint.Method), blueprintEndpointPath(endpoint), toCamelCase(endpoint.Handler)))
	}
	for _, item := range bp.Middleware {
		sb.WriteString(fmt.Sprintf("\tfwApp.Use(%sMiddleware)\n", toCamelCase(item.Name)))
	}
	for _, item := range bp.Plugins {
		sb.WriteString(fmt.Sprintf("\tfwApp.RegisterPlugin(%sPlugin{})\n", toCamelCase(item.Name)))
	}
	sb.WriteString("}\n")
	return sb.String()
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
