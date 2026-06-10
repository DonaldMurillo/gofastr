package world

// SchemaVersion identifies the on-disk shape of the World IR. Bump when a
// non-additive change is made so old journals can be migrated explicitly
// rather than silently misinterpreted.
const SchemaVersion = 1

// World is the canonical, JSON-clean representation of a Kiln application.
type World struct {
	SchemaVersion int                `json:"schema_version"`
	App           AppConfig          `json:"app"`
	Entities      map[string]*Entity `json:"entities,omitempty"`
	Pages         map[string]*Page   `json:"pages,omitempty"`
	Hooks         []*Hook            `json:"hooks,omitempty"`
	Routes        []*Route           `json:"routes,omitempty"`
	Seeds         []*Seed            `json:"seeds,omitempty"`
	Middleware    []*Middleware      `json:"middleware,omitempty"`
}

// New returns an empty World at the current SchemaVersion.
func New() *World {
	return &World{
		SchemaVersion: SchemaVersion,
		Entities:      map[string]*Entity{},
		Pages:         map[string]*Page{},
	}
}

// AppConfig mirrors framework.AppConfig in JSON-only form, plus
// kiln-specific UI configuration (theme overrides) that the agent or
// a host tool may override at runtime.
type AppConfig struct {
	Name           string `json:"name,omitempty"`
	JSONCase       string `json:"json_case,omitempty"` // "camel" | "snake"
	DebugEndpoints bool   `json:"debug_endpoints,omitempty"`

	// Theme is an optional set of token overrides applied to the
	// framework's default page theme. Keys are token names (e.g.
	// "page-bg", "page-primary", "page-fg-soft"); values are CSS
	// color literals. The renderer merges these on top of
	// core-ui/widget/theme.PageTheme(), so a single set_theme call
	// re-skins every page.
	Theme map[string]string `json:"theme,omitempty"`
}

// Entity is the JSON-clean entity declaration. It tracks framework
// EntityDeclaration plus declarative hooks and endpoints.
type Entity struct {
	Name        string           `json:"name"`
	Table       string           `json:"table,omitempty"`
	Fields      []Field          `json:"fields"`
	Relations   []Relation       `json:"relations,omitempty"`
	Endpoints   []EntityEndpoint `json:"endpoints,omitempty"`
	SoftDelete  bool             `json:"soft_delete,omitempty"`
	MultiTenant bool             `json:"multi_tenant,omitempty"`
	Timestamps  *bool            `json:"timestamps,omitempty"`
	CRUD        *bool            `json:"crud,omitempty"`
	MCP         bool             `json:"mcp,omitempty"`
}

// Field mirrors framework.FieldDeclaration verbatim.
type Field struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Required     bool     `json:"required,omitempty"`
	Unique       bool     `json:"unique,omitempty"`
	Default      any      `json:"default,omitempty"`
	AutoGenerate string   `json:"auto_generate,omitempty"`
	ReadOnly     bool     `json:"read_only,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	Values       []string `json:"values,omitempty"`
	To           string   `json:"to,omitempty"`
	Many         bool     `json:"many,omitempty"`
}

// Relation matches the framework's relation shape.
type Relation struct {
	Name string `json:"name"`
	To   string `json:"to"`
	Type string `json:"type,omitempty"` // "belongs_to" | "has_many" | "many_to_many"
}

// EntityEndpoint is a custom HTTP endpoint attached to an entity. Unlike
// framework.Endpoint it carries no Go handler — the behavior is described
// declaratively via Action.
type EntityEndpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MCP         bool   `json:"mcp,omitempty"`
	Action      Action `json:"action"`
}

// Hook is a declarative lifecycle hook keyed to an entity at a given event.
type Hook struct {
	ID        string `json:"id"`
	Entity    string `json:"entity"`
	When      string `json:"when"` // "before_create" | "after_create" | "before_update" | "after_update" | "before_delete" | "after_delete" | "before_list" | "after_list"
	Condition string `json:"condition,omitempty"`
	Action    Action `json:"action"`
}

// Route is a custom HTTP route not bound to an entity.
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Action Action `json:"action"`
}

// Seed is initial data for an entity, applied after migrations.
type Seed struct {
	Entity string           `json:"entity"`
	Rows   []map[string]any `json:"rows"`
}

// Middleware selects from a built-in catalog by name and supplies optional
// declarative configuration. The catalog is closed; the agent cannot author
// new middleware without escalating beyond Kiln's declarative surface.
type Middleware struct {
	Name string         `json:"name"`
	Cfg  map[string]any `json:"cfg,omitempty"`
}

// Page is a UI screen described as an element tree.
//
// Version is an optimistic-concurrency etag. It starts at 1 when the
// page is added and is bumped on every successful mutation (add,
// update_page_element, etc). update_page_element accepts an optional
// IfMatch so an agent can verify the page hasn't shifted under it
// between fetch and patch.
type Page struct {
	Path        string  `json:"path"`
	Name        string  `json:"name,omitempty"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Type        string  `json:"type,omitempty"` // "page" | "drawer" | "sheet" | "dialog"
	Version     int     `json:"version,omitempty"`
	Layout      *Layout `json:"layout,omitempty"`
	Tree        Node    `json:"tree"`
}

// Layout is a placeholder mirroring core-ui/app.Layout. v1 stores the layout
// reference by name; the renderer resolves it from the host registry.
type Layout struct {
	Name string `json:"name,omitempty"`
}

// Node is a single element in a Page tree. The Kind discriminates between
// built-in elements ("div", "button", "heading", …) and named components
// ("component:<name>"). Props feed element configuration; Bindings express
// signal-driven values via expressions; Actions wire events to declarative
// effects evaluated by kiln/expr.
//
// ID is a stable per-element handle assigned by kiln when the page is
// added. Agents reference it from update_page_element to address the
// exact element they want to mutate, rather than positional tree
// paths (which break when siblings shift) or selector queries (which
// can be ambiguous). The renderer ignores ID — it's pure metadata.
type Node struct {
	ID       string            `json:"_id,omitempty"`
	Kind     string            `json:"kind"`
	Props    map[string]any    `json:"props,omitempty"`
	Bindings map[string]string `json:"bindings,omitempty"`
	Actions  map[string]Action `json:"actions,omitempty"`
	Children []Node            `json:"children,omitempty"`
}

// Action is the canonical declarative effect type. The Kind selects from a
// closed verb catalog evaluated by kiln/expr (Phase 3). Params is verb-
// specific. Treating actions as data — never Go source — is what lets every
// other Kiln subsystem (journal, freeze, MCP tool surface) round-trip them
// losslessly.
type Action struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

// Known Action kinds. The Phase 3 evaluator validates Kind+Params shape;
// Phase 0 only cares that they round-trip through JSON unchanged.
const (
	ActionNoop         = "noop"
	ActionSetField     = "set_field"     // params: {field, value}
	ActionValidate     = "validate"      // params: {expression, message}
	ActionAudit        = "audit"         // params: {channel, message}
	ActionCreateEntity = "create_entity" // params: {entity, data}
	ActionRespondJSON  = "respond_json"  // params: {status, body}
	ActionRespondQuery = "respond_query" // params: {query}
	ActionEmitEvent    = "emit_event"    // params: {topic, data}
)
