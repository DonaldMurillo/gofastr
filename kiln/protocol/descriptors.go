package protocol

import "sort"

// Descriptor is the public schema of one tool. Transports (MCP, ACP)
// expose Descriptor.Schema as the tool's input schema and Description
// as its description.
type Descriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	Destructive bool           `json:"destructive,omitempty"`
}

// List returns every tool the protocol exposes.
func (t *Tools) List() []Descriptor {
	out := make([]Descriptor, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Describe returns the descriptor for a given tool name.
func (t *Tools) Describe(name string) (Descriptor, bool) {
	d, ok := descriptors[name]
	return d, ok
}

var descriptors = map[string]Descriptor{
	"world_get": {
		Name:        "world_get",
		Description: "Read the current Kiln world IR. Empty path returns the full world. Use entities.<name>, pages.<path>, _chat, or _plans to fetch a sub-tree.",
		Schema: object(map[string]any{
			"path": str("subpath; empty for full world"),
		}, nil),
	},
	"set_app_config": {
		Name:        "set_app_config",
		Description: "Replace the app-level configuration. This is the same app surface accepted by the current gofastr.yml blueprint; include api_prefix (normally api) so UI and CRUD routes stay separate.",
		Schema: object(map[string]any{
			"config": object(map[string]any{
				"name":            str(""),
				"module":          str("Go module path for the frozen scaffold"),
				"json_case":       enum("camel", "snake"),
				"debug_endpoints": boolean(),
				"db_driver":       str("database driver for the frozen scaffold"),
				"db_url":          str("database URL for the frozen scaffold"),
				"static_dir":      str("static asset directory"),
				"output_dir":      str("optional scaffold output directory"),
				"api_prefix":      str("CRUD prefix without leading slash; use api unless there is a deliberate reason not to"),
				"llm_md":          boolean(),
				"theme":           stringMap("light theme token overrides"),
				"theme_dark":      stringMap("dark theme color overrides"),
				"auth": object(map[string]any{
					"enabled": boolean(), "dev_mode": boolean(), "base_path": str(""), "jwt_secret": str(""),
				}, nil),
				"admin": object(map[string]any{
					"enabled": boolean(), "path": str(""), "role": str(""), "login_path": str(""), "seed_email": str(""), "seed_password": str(""),
				}, nil),
				"pwa": object(map[string]any{
					"enabled": boolean(), "name": str(""), "short_name": str(""), "description": str(""),
					"start_url": str(""), "scope": str(""), "display": enum("standalone", "fullscreen", "minimal-ui", "browser"),
					"theme_color": str(""), "background_color": str(""),
				}, nil),
			}, []string{"name"}),
		}, []string{"config"}),
	},
	"set_scaffold": {
		Name:        "set_scaffold",
		Description: "Replace navigation and the owned-Go endpoint, middleware, plugin, and helper stubs preserved by freeze into the current gofastr.yml blueprint. Navigation is rendered live; stubs become editable Go in the generated scaffold.",
		Schema: object(map[string]any{
			"nav": list(navItemSchema()),
			"endpoints": list(object(map[string]any{
				"name": str(""), "method": enum("GET", "POST", "PUT", "PATCH", "DELETE"),
				"path": str(""), "entity": str(""), "handler": str("owned-Go handler name"),
				"description": str(""), "mcp": boolean(),
			}, []string{"method", "path"})),
			"middleware": list(object(map[string]any{
				"name": str(""), "description": str(""),
			}, []string{"name"})),
			"plugins": list(namedStubSchema()),
			"helpers": list(namedStubSchema()),
		}, nil),
	},
	"add_entity": {
		Name:        "add_entity",
		Description: "Add a new entity. Provides CRUD endpoints, OpenAPI, and (when MCP is true) MCP tools automatically.",
		Schema:      object(map[string]any{"entity": entitySchema()}, []string{"entity"}),
	},
	"update_entity": {
		Name:        "update_entity",
		Description: "Replace an existing entity in full. Prefer add_field for additive changes.",
		Schema:      object(map[string]any{"entity": entitySchema()}, []string{"entity"}),
	},
	"delete_entity": {
		Name:        "delete_entity",
		Description: "Drop an entity. Destructive: requires plan_id of an approved propose_plan whose targets include {op:\"delete_entity\",name:<entity>}.",
		Destructive: true,
		Schema: object(map[string]any{
			"name":    str(""),
			"plan_id": str("approved plan authorizing this delete"),
		}, []string{"name", "plan_id"}),
	},
	"add_field": {
		Name:        "add_field",
		Description: "Append a field to an existing entity.",
		Schema: object(map[string]any{
			"entity": str(""),
			"field":  fieldSchema(),
		}, []string{"entity", "field"}),
	},
	"delete_field": {
		Name:        "delete_field",
		Description: "Remove a field from an entity. Destructive: requires plan_id of an approved propose_plan whose targets include {op:\"delete_field\",name:\"<entity>.<field>\"}.",
		Destructive: true,
		Schema: object(map[string]any{
			"entity":  str(""),
			"field":   str(""),
			"plan_id": str("approved plan authorizing this delete"),
		}, []string{"entity", "field", "plan_id"}),
	},
	"add_page": {
		Name:        "add_page",
		Description: "Register a UI page (a tree of declarative elements).",
		Schema:      object(map[string]any{"page": pageSchema()}, []string{"page"}),
	},
	"delete_page": {
		Name:        "delete_page",
		Description: "Remove a page by path. Destructive: requires plan_id of an approved propose_plan whose targets include {op:\"delete_page\",name:\"<path>\"}.",
		Destructive: true,
		Schema: object(map[string]any{
			"path":    str(""),
			"plan_id": str("approved plan authorizing this delete"),
		}, []string{"path", "plan_id"}),
	},
	"update_page_element": {
		Name: "update_page_element",
		Description: "Patch one element inside a page tree without re-sending the whole page. " +
			"Address the element by its stable _id (read it from /kiln/world/pages.<path>). " +
			"Non-destructive — page edits don't lose persisted data, no plan required. " +
			"patch.op selects the operation: set_props (merge), replace_props, replace_subtree, " +
			"remove, insert_before, insert_after, append_child. " +
			"Pass if_match=<page.version> for optimistic concurrency; mismatch returns a conflict so you can refetch.",
		Schema: object(map[string]any{
			"path":       str("page path, e.g. /dashboard"),
			"element_id": str("the _id of the target element from /kiln/world/pages.<path>"),
			"if_match":   map[string]any{"type": "integer", "description": "expected page.version; if mismatched, conflict"},
			"patch": object(map[string]any{
				"op": enum("set_props", "replace_props", "replace_subtree",
					"remove", "insert_before", "insert_after", "append_child"),
				"set_props": map[string]any{"type": "object",
					"description": "for set_props (merge) or replace_props (full replace)"},
				"element": object(map[string]any{
					"kind": str("element kind"),
				}, nil),
			}, []string{"op"}),
		}, []string{"path", "element_id", "patch"}),
	},
	"add_hook": {
		Name:        "add_hook",
		Description: "Add a declarative entity hook. Action runs when the entity event fires; condition gates it.",
		Schema:      object(map[string]any{"hook": hookSchema()}, []string{"hook"}),
	},
	"delete_hook": {
		Name:        "delete_hook",
		Description: "Remove a hook by ID. Destructive: requires plan_id of an approved propose_plan whose targets include {op:\"delete_hook\",name:\"<id>\"}.",
		Destructive: true,
		Schema: object(map[string]any{
			"id":      str(""),
			"plan_id": str("approved plan authorizing this delete"),
		}, []string{"id", "plan_id"}),
	},
	"add_route": {
		Name:        "add_route",
		Description: "Add a custom HTTP route with a declarative action (e.g., respond_json).",
		Schema:      object(map[string]any{"route": routeSchema()}, []string{"route"}),
	},
	"delete_route": {
		Name:        "delete_route",
		Description: "Remove a route by method+path. Destructive: requires plan_id of an approved propose_plan whose targets include {op:\"delete_route\",name:\"<METHOD> <path>\"}.",
		Destructive: true,
		Schema: object(map[string]any{
			"method":  str(""),
			"path":    str(""),
			"plan_id": str("approved plan authorizing this delete"),
		}, []string{"method", "path", "plan_id"}),
	},
	"add_seed": {
		Name:        "add_seed",
		Description: "Add seed data rows for an entity. Apply migrations first.",
		Schema:      object(map[string]any{"seed": seedSchema()}, []string{"seed"}),
	},
	"propose_plan": {
		Name:        "propose_plan",
		Description: "Submit a multi-step plan for the user to approve. Required before any destructive op. List each destructive op in `targets` so the protocol can authorize the matching delete_*. The user clicks Approve in the panel; you then call delete_* with `plan_id` set.",
		Schema: object(map[string]any{
			"plan_id": str("stable id for this plan"),
			"steps":   list(str("one short step description")),
			"reason":  str("optional justification"),
			"targets": list(object(map[string]any{
				"op":   str("destructive op key, e.g. \"delete_entity\""),
				"name": str("target name, e.g. \"posts\" or \"posts.title\""),
			}, []string{"op", "name"})),
		}, []string{"plan_id", "steps"}),
	},
	"approve_plan": {
		Name:        "approve_plan",
		Description: "Approve a previously proposed plan. Typically invoked by the panel on user click.",
		Schema: object(map[string]any{
			"plan_id":  str(""),
			"modified": boolean(),
		}, []string{"plan_id"}),
	},
	"reject_plan": {
		Name:        "reject_plan",
		Description: "Reject a proposed plan. Rejected plans cannot later be approved — propose a new one. Typically invoked by the panel on user click.",
		Schema: object(map[string]any{
			"plan_id": str(""),
			"reason":  str("optional explanation"),
		}, []string{"plan_id"}),
	},
	"undo": {
		Name:        "undo",
		Description: "Truncate the journal by one entry, reverting the most recent change.",
		Schema:      object(nil, nil),
	},
	"reset_session": {
		Name:        "reset_session",
		Description: "Wipe the entire journal and reload to an empty world. Used by the panel's Reset button. Destructive in scope but does not require a plan — it's a user-initiated start-over.",
		Destructive: true,
		Schema:      object(nil, nil),
	},
	"set_theme": {
		Name:        "set_theme",
		Description: "Replace the kiln page theme. Keys are current semantic app-theme names (for example \"background\", \"primary\", \"accent\", \"text-muted\", \"font_body\", and \"font_heading\"); values are CSS literals. Empty `theme` clears overrides. Current pages consume the palette through UIHost's /__gofastr/app.css.",
		Schema: object(map[string]any{
			"theme": object(nil, nil),
		}, []string{"theme"}),
	},
	"chat": {
		Name:        "chat",
		Description: "Record a chat message in the session journal.",
		Schema: object(map[string]any{
			"role": enum("user", "assistant"),
			"text": str(""),
		}, []string{"role", "text"}),
	},
}

// --- JSON-Schema helpers ----------------------------------------------

func object(props map[string]any, required []string) map[string]any {
	out := map[string]any{"type": "object"}
	if props != nil {
		out["properties"] = props
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func str(desc string) map[string]any {
	out := map[string]any{"type": "string"}
	if desc != "" {
		out["description"] = desc
	}
	return out
}

func boolean() map[string]any { return map[string]any{"type": "boolean"} }

func enum(values ...string) map[string]any {
	vals := make([]any, len(values))
	for i, v := range values {
		vals[i] = v
	}
	return map[string]any{"type": "string", "enum": vals}
}

func list(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

func entitySchema() map[string]any {
	return object(map[string]any{
		"name":             str("entity name; lowercase, plural by convention"),
		"table":            str("optional table name override"),
		"fields":           list(fieldSchema()),
		"relations":        list(relationSchema()),
		"endpoints":        list(entityEndpointSchema()),
		"soft_delete":      boolean(),
		"multi_tenant":     map[string]any{"type": "boolean", "description": "legacy journals only; Kiln rejects true because tenant resolution must be wired in owned Go"},
		"owner_field":      str("row owner column, e.g. user_id; required for per-user auto-CRUD data"),
		"cross_owner_read": str("optional permission that lifts owner scoping for reads"),
		"search_fields":    list(str("searchable field")),
		"access": object(map[string]any{
			"read": str(""), "create": str(""), "update": str(""), "delete": str(""),
		}, nil),
		"timestamps":    boolean(),
		"crud":          boolean(),
		"mcp":           boolean(),
		"cursor_field":  str("legacy single cursor field"),
		"cursor_fields": list(str("ordered cursor field")),
		"indices":       list(indexSchema()),
		"properties":    map[string]any{"type": "object"},
	}, []string{"name", "fields"})
}

func fieldSchema() map[string]any {
	return object(map[string]any{
		"name":          str(""),
		"type":          enum("string", "text", "int", "float", "decimal", "bool", "enum", "uuid", "timestamp", "date", "json", "relation", "image", "file"),
		"required":      boolean(),
		"unique":        boolean(),
		"default":       map[string]any{},
		"auto_generate": enum("uuid", "timestamp", "increment"),
		"read_only":     boolean(),
		"hidden":        boolean(),
		"max":           map[string]any{"type": "number"},
		"min":           map[string]any{"type": "number"},
		"pattern":       str(""),
		"values":        list(str("enum value")),
		"to":            str("relation target entity"),
		"many":          boolean(),
	}, []string{"name", "type"})
}

func relationSchema() map[string]any {
	return object(map[string]any{
		"type":               enum("has_one", "has_many", "belongs_to", "many_to_many"),
		"name":               str("logical relation name"),
		"entity":             str("target entity"),
		"foreign_key":        str("foreign-key column"),
		"through":            str("many-to-many pivot table"),
		"local_key":          str("many-to-many source key"),
		"foreign_key_target": str("many-to-many target key"),
	}, []string{"type", "name", "entity"})
}

func entityEndpointSchema() map[string]any {
	return object(map[string]any{
		"method": enum("GET", "POST", "PUT", "DELETE", "PATCH"),
		"path":   str(""), "name": str(""), "description": str(""), "mcp": boolean(), "action": actionSchema(),
	}, []string{"method", "path", "action"})
}

func indexSchema() map[string]any {
	return object(map[string]any{
		"name": str(""), "columns": list(str("indexed column")), "unique": boolean(),
	}, nil)
}

func pageSchema() map[string]any {
	return object(map[string]any{
		"path":        str(""),
		"name":        str(""),
		"title":       str(""),
		"description": str(""),
		"type":        enum("page", "drawer", "sheet", "dialog"),
		"layout":      object(map[string]any{"name": str("")}, nil),
		"access":      object(map[string]any{"auth": boolean(), "role": str("")}, nil),
		"tree":        nodeSchema(),
	}, []string{"path", "tree"})
}

func nodeSchema() map[string]any {
	return object(map[string]any{
		"kind":     str("element kind: div, heading, button, …"),
		"props":    map[string]any{"type": "object"},
		"bindings": map[string]any{"type": "object"},
		"actions":  map[string]any{"type": "object"},
		"children": map[string]any{"type": "array"},
	}, []string{"kind"})
}

func hookSchema() map[string]any {
	return object(map[string]any{
		"id":        str(""),
		"entity":    str(""),
		"when":      enum("before_create", "after_create", "before_update", "after_update", "before_delete", "after_delete", "before_list", "after_list"),
		"condition": str("optional expression; hook runs only if true"),
		"action":    actionSchema(),
	}, []string{"id", "entity", "when", "action"})
}

func routeSchema() map[string]any {
	return object(map[string]any{
		"method": enum("GET", "POST", "PUT", "DELETE", "PATCH"),
		"path":   str(""),
		"action": actionSchema(),
	}, []string{"method", "path", "action"})
}

func actionSchema() map[string]any {
	return object(map[string]any{
		"kind":   enum("noop", "validate", "set_field", "audit", "create_entity", "respond_json", "respond_query", "emit_event"),
		"params": map[string]any{"type": "object"},
	}, []string{"kind"})
}

func seedSchema() map[string]any {
	return object(map[string]any{
		"entity":  str(""),
		"rows":    list(map[string]any{"type": "object"}),
		"count":   map[string]any{"type": "integer", "minimum": 0},
		"weights": map[string]any{"type": "object"},
	}, []string{"entity"})
}

func stringMap(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": map[string]any{"type": "string"}}
}

func namedStubSchema() map[string]any {
	return object(map[string]any{"name": str(""), "description": str("")}, []string{"name"})
}

func navItemSchema() map[string]any {
	return object(map[string]any{
		"label": str(""), "href": str(""), "icon": str(""), "role": str(""),
		// Keep the recursive child shape intentionally shallow in JSON Schema;
		// nested values use the same runtime world.NavItem validation.
		"items": list(object(map[string]any{
			"label": str(""), "href": str(""), "icon": str(""), "role": str(""),
		}, []string{"label", "href"})),
	}, []string{"label", "href"})
}
