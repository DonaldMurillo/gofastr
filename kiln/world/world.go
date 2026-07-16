package world

import "github.com/DonaldMurillo/gofastr/core-ui/node"

// The UI node tree (Node, Action, action-kind consts) and its helpers now
// live in the first-party core-ui/node package so the blueprint codegen
// can compose them without importing the Kiln namespace. These aliases +
// re-exports keep every kiln-internal caller (journal, freeze, effect,
// protocol, render, chat, live) compiling unchanged.
type (
	// Node is a single element in a Page tree. See core-ui/node.Node.
	Node = node.Node
	// Action is the canonical declarative effect type. See core-ui/node.Action.
	Action = node.Action
)

// Known Action kinds, re-exported from core-ui/node.
const (
	ActionNoop         = node.ActionNoop
	ActionSetField     = node.ActionSetField
	ActionValidate     = node.ActionValidate
	ActionAudit        = node.ActionAudit
	ActionCreateEntity = node.ActionCreateEntity
	ActionRespondJSON  = node.ActionRespondJSON
	ActionRespondQuery = node.ActionRespondQuery
	ActionEmitEvent    = node.ActionEmitEvent
)

// Node-tree helpers, re-exported from core-ui/node.
var (
	AssignNodeIDs = node.AssignNodeIDs
	NewElementID  = node.NewElementID
	FindNodeByID  = node.FindNodeByID
)

// SchemaVersion identifies the on-disk shape of the World IR. Bump when a
// non-additive change is made so old journals can be migrated explicitly
// rather than silently misinterpreted.
const SchemaVersion = 1

// World is the canonical, JSON-clean representation of a Kiln application.
type World struct {
	SchemaVersion   int                `json:"schema_version"`
	App             AppConfig          `json:"app"`
	Entities        map[string]*Entity `json:"entities,omitempty"`
	Pages           map[string]*Page   `json:"pages,omitempty"`
	Nav             []NavItem          `json:"nav,omitempty"`
	Hooks           []*Hook            `json:"hooks,omitempty"`
	Routes          []*Route           `json:"routes,omitempty"`
	Endpoints       []*EndpointStub    `json:"endpoints,omitempty"`
	Seeds           []*Seed            `json:"seeds,omitempty"`
	Middleware      []*Middleware      `json:"middleware,omitempty"`
	MiddlewareStubs []NamedStub        `json:"middleware_stubs,omitempty"`
	Plugins         []NamedStub        `json:"plugins,omitempty"`
	Helpers         []NamedStub        `json:"helpers,omitempty"`
}

// New returns an empty World at the current SchemaVersion.
func New() *World {
	return &World{
		SchemaVersion: SchemaVersion,
		App:           AppConfig{APIPrefix: "api", Auth: AuthConfig{DevMode: true}},
		Entities:      map[string]*Entity{},
		Pages:         map[string]*Page{},
	}
}

// AppConfig mirrors framework.AppConfig in JSON-only form, plus
// kiln-specific UI configuration (theme overrides) that the agent or
// a host tool may override at runtime.
type AppConfig struct {
	Name           string      `json:"name,omitempty"`
	Module         string      `json:"module,omitempty"`
	JSONCase       string      `json:"json_case,omitempty"` // "camel" | "snake"
	DebugEndpoints bool        `json:"debug_endpoints,omitempty"`
	DBDriver       string      `json:"db_driver,omitempty"`
	DBURL          string      `json:"db_url,omitempty"`
	StaticDir      string      `json:"static_dir,omitempty"`
	OutputDir      string      `json:"output_dir,omitempty"`
	APIPrefix      string      `json:"api_prefix,omitempty"`
	LLMMD          bool        `json:"llm_md,omitempty"`
	Auth           AuthConfig  `json:"auth,omitempty"`
	Admin          AdminConfig `json:"admin,omitempty"`
	PWA            PWAConfig   `json:"pwa,omitempty"`

	// Theme and ThemeDark are optional semantic token overrides applied to
	// the framework theme. Light keys include colors such as "background",
	// "primary", and "text-muted", plus "font_body" and "font_heading";
	// dark overrides accept the semantic color keys. Values are CSS literals.
	Theme     map[string]string `json:"theme,omitempty"`
	ThemeDark map[string]string `json:"theme_dark,omitempty"`
}

// AuthConfig, AdminConfig, and PWAConfig mirror the corresponding current
// gofastr.yml app sections. Kiln previews the surfaces it can render safely;
// freeze preserves the full declaration for the owned-Go scaffold.
type AuthConfig struct {
	Enabled   bool   `json:"enabled,omitempty"`
	DevMode   bool   `json:"dev_mode,omitempty"`
	BasePath  string `json:"base_path,omitempty"`
	JWTSecret string `json:"jwt_secret,omitempty"`
}

type AdminConfig struct {
	Enabled      bool   `json:"enabled,omitempty"`
	Path         string `json:"path,omitempty"`
	Role         string `json:"role,omitempty"`
	LoginPath    string `json:"login_path,omitempty"`
	SeedEmail    string `json:"seed_email,omitempty"`
	SeedPassword string `json:"seed_password,omitempty"`
}

type PWAConfig struct {
	Enabled         bool   `json:"enabled,omitempty"`
	Name            string `json:"name,omitempty"`
	ShortName       string `json:"short_name,omitempty"`
	Description     string `json:"description,omitempty"`
	StartURL        string `json:"start_url,omitempty"`
	Scope           string `json:"scope,omitempty"`
	Display         string `json:"display,omitempty"`
	ThemeColor      string `json:"theme_color,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"`
}

// Entity is the JSON-clean entity declaration. It tracks framework
// EntityDeclaration plus declarative hooks and endpoints.
type Entity struct {
	Name           string             `json:"name"`
	Table          string             `json:"table,omitempty"`
	Fields         []Field            `json:"fields"`
	Relations      []Relation         `json:"relations,omitempty"`
	Endpoints      []EntityEndpoint   `json:"endpoints,omitempty"`
	SoftDelete     bool               `json:"soft_delete,omitempty"`
	MultiTenant    bool               `json:"multi_tenant,omitempty"`
	OwnerField     string             `json:"owner_field,omitempty"`
	CrossOwnerRead string             `json:"cross_owner_read,omitempty"`
	SearchFields   []string           `json:"search_fields,omitempty"`
	Access         *AccessDeclaration `json:"access,omitempty"`
	Timestamps     *bool              `json:"timestamps,omitempty"`
	CRUD           *bool              `json:"crud,omitempty"`
	MCP            bool               `json:"mcp,omitempty"`
	CursorField    string             `json:"cursor_field,omitempty"`
	CursorFields   []string           `json:"cursor_fields,omitempty"`
	Indices        []Index            `json:"indices,omitempty"`
	Properties     map[string]any     `json:"properties,omitempty"`
}

type AccessDeclaration struct {
	Read   string `json:"read,omitempty"`
	Create string `json:"create,omitempty"`
	Update string `json:"update,omitempty"`
	Delete string `json:"delete,omitempty"`
}

type Index struct {
	Name    string   `json:"name,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Unique  bool     `json:"unique,omitempty"`
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
	Type             string `json:"type,omitempty"` // "has_one" | "has_many" | "belongs_to" | "many_to_many"
	Name             string `json:"name"`
	Entity           string `json:"entity,omitempty"`
	ForeignKey       string `json:"foreign_key,omitempty"`
	Through          string `json:"through,omitempty"`
	LocalKey         string `json:"local_key,omitempty"`
	ForeignKeyTarget string `json:"foreign_key_target,omitempty"`
	// To is the pre-parity target key. It remains readable so existing
	// journals replay; new clients should use Entity.
	To string `json:"to,omitempty"`
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
	Entity  string                    `json:"entity"`
	Rows    []map[string]any          `json:"rows,omitempty"`
	Count   int                       `json:"count,omitempty"`
	Weights map[string]map[string]int `json:"weights,omitempty"`
}

// Middleware selects from a built-in catalog by name and supplies optional
// declarative configuration. The catalog is closed; the agent cannot author
// new middleware without escalating beyond Kiln's declarative surface.
type Middleware struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Cfg         map[string]any `json:"cfg,omitempty"`
}

// EndpointStub, NamedStub, and NavItem are the scaffold-only surfaces from
// the current blueprint contract. They do not invent live handler bodies;
// freeze emits owned-Go stubs while world.json retains the exact live IR.
type EndpointStub struct {
	Name        string `json:"name,omitempty"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Entity      string `json:"entity,omitempty"`
	Handler     string `json:"handler,omitempty"`
	Description string `json:"description,omitempty"`
	MCP         bool   `json:"mcp,omitempty"`
}

type NamedStub struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type NavItem struct {
	Label string    `json:"label"`
	Href  string    `json:"href"`
	Icon  string    `json:"icon,omitempty"`
	Role  string    `json:"role,omitempty"`
	Items []NavItem `json:"items,omitempty"`
}

// Page is a UI screen described as an element tree.
//
// Version is an optimistic-concurrency etag. It starts at 1 when the
// page is added and is bumped on every successful mutation (add,
// update_page_element, etc). update_page_element accepts an optional
// IfMatch so an agent can verify the page hasn't shifted under it
// between fetch and patch.
type Page struct {
	Path        string     `json:"path"`
	Name        string     `json:"name,omitempty"`
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	Type        string     `json:"type,omitempty"` // "page" | "drawer" | "sheet" | "dialog"
	Version     int        `json:"version,omitempty"`
	Layout      *Layout    `json:"layout,omitempty"`
	Access      PageAccess `json:"access,omitempty"`
	Tree        Node       `json:"tree"`
}

type PageAccess struct {
	Auth bool   `json:"auth,omitempty"`
	Role string `json:"role,omitempty"`
}

// Layout is a placeholder mirroring core-ui/app.Layout. v1 stores the layout
// reference by name; the renderer resolves it from the host registry.
type Layout struct {
	Name string `json:"name,omitempty"`
}

// Node and Action are declared above as aliases to core-ui/node.
