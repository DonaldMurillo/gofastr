package protocol

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
		Description: "Replace the app-level configuration (name, json case, debug endpoints).",
		Schema: object(map[string]any{
			"config": object(map[string]any{
				"name":            str(""),
				"json_case":       enum("camel", "snake"),
				"debug_endpoints": boolean(),
			}, []string{"name"}),
		}, []string{"config"}),
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
		Description: "Replace the kiln page theme. Keys are core-ui/widget/theme token names (e.g. \"page-bg\", \"page-primary\", \"page-accent\"); values are CSS color literals. Empty `theme` clears overrides. The next /kiln/theme.css fetch reflects the change and every page re-skins on next load.",
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
		"name":         str("entity name; lowercase, plural by convention"),
		"table":        str("optional table name override"),
		"fields":       list(fieldSchema()),
		"soft_delete":  boolean(),
		"multi_tenant": boolean(),
		"timestamps":   boolean(),
		"crud":         boolean(),
		"mcp":          boolean(),
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
		"max":           map[string]any{"type": "number"},
		"min":           map[string]any{"type": "number"},
		"pattern":       str(""),
		"values":        list(str("enum value")),
		"to":            str("relation target entity"),
		"many":          boolean(),
	}, []string{"name", "type"})
}

func pageSchema() map[string]any {
	return object(map[string]any{
		"path":  str(""),
		"title": str(""),
		"type":  enum("page", "drawer", "sheet", "dialog"),
		"tree":  nodeSchema(),
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
		"entity": str(""),
		"rows":   list(map[string]any{"type": "object"}),
	}, []string{"entity", "rows"})
}
