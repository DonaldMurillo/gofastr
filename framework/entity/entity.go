package entity

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// EntityConfig holds the declarative configuration for an entity.
// Name is set via Define(); Fields declare the schema.
// Timestamps defaults to true — use WithTimestamps(false) to disable.
type EntityConfig struct {
	Name       string            // entity name (e.g. "users")
	Table      string            // DB table name (defaults to snake_case of Name)
	Fields     []schema.Field    // typed field definitions
	Relations  []Relation        // entity relationships
	Endpoints  []Endpoint        // custom HTTP endpoints for this entity
	Scope      *ScopeConfig      // ownership, tenancy, and soft-delete behavior
	Pagination *PaginationConfig // list limits and keyset cursor shape
	Exposure   *ExposureConfig   // generated HTTP/MCP and access posture
	// Deprecated: use Scope.SoftDelete. Supported through the v0.40 line.
	SoftDelete bool
	// Deprecated: use Scope.MultiTenant. Supported through the v0.40 line.
	MultiTenant bool
	// Deprecated: use Scope.TenantField. Supported through the v0.40 line.
	TenantField string
	Timestamps  bool // add created_at / updated_at columns
	// Deprecated: use Exposure.CRUD. Supported through the v0.40 line.
	CRUD *bool
	// Deprecated: use Exposure.MCP. Supported through the v0.40 line.
	MCP bool
	// Deprecated: use Pagination.CursorField. Supported through the v0.40 line.
	CursorField string
	// Deprecated: use Pagination.CursorFields. Supported through the v0.40 line.
	CursorFields []string
	Indices      []Index        // additional CREATE INDEX statements emitted by AutoMigrate
	Unmanaged    bool           // when true, the migration system never emits DDL for this object (it is created elsewhere — e.g. a view, an FTS virtual table, or a legacy/external table). The ORM still queries it.
	Properties   map[string]any // caller-owned metadata for generators, plugins, and app conventions
	// Deprecated: use Pagination.MaxListLimit. Supported through the v0.40 line.
	MaxListLimit int

	// OwnerField names the DB column that holds the row's owner id (e.g.
	// "user_id"). When set AND an owner extractor is registered (typically
	// by battery/auth), auto-CRUD scopes List/Get/Update/Delete by the
	// current request's owner and auto-stamps Create. Leave empty to keep
	// pre-existing behaviour.
	// Deprecated: use Scope.OwnerField. Supported through the v0.40 line.
	OwnerField string

	// CrossOwnerRead names an RBAC permission (e.g. "tickets:read:all")
	// that, when held by the request context (installed via access.Middleware
	// or battery/auth), lifts owner scoping for READ operations only
	// (List/Get/Count/includes — both HTTP and in-process). Writes stay
	// owner-scoped, always. Requires OwnerField; leave empty to keep
	// pre-existing behaviour. Fail-closed: when no access policy is present
	// in the context the scope stays ON (the secure-by-default answer). The
	// admin battery's wildcard grant passes any permission, so an entity
	// opted in here is fully visible in the back office.
	// Deprecated: use Scope.CrossOwnerRead. Supported through the v0.40 line.
	CrossOwnerRead string

	// Access declares the RBAC permission required for each CRUD operation.
	// A blank permission leaves that operation un-gated by RBAC (owner and
	// tenant scoping still apply). When set, auto-CRUD refuses a request
	// whose context lacks the permission with 403. Roles + policy must be
	// present in the request context — wire them once with access.Middleware
	// (or battery/auth). See framework/docs/content/access-control.md.
	// Deprecated: use Exposure.Access. Supported through the v0.40 line.
	Access AccessControl

	// Public opts an entity OUT of the framework's secure-by-default
	// session requirement (see the doc comment on requireAuthenticated in
	// framework/crud/owner.go, and framework/docs/content/security.md
	// "Default CRUD authentication"): every operation — List/Get and
	// Create/Update/Delete — is reachable by an anonymous caller, the
	// pre-#65 behaviour. This is a deliberate declaration ("yes, this is
	// public": a public contact form, a blog's comments, a newsletter
	// signup), not a partial relaxation — an entity that should allow
	// anonymous reads but still gate writes needs a declared Access block
	// instead (blank Access.Read + a real Access.Create permission).
	// Has no effect when OwnerField or Access is set; those mechanisms
	// already govern the entity. Default false: every operation requires
	// a session, matching the blueprint's default (no `public: true`).
	// Deprecated: use Exposure.Public. Supported through the v0.40 line.
	Public bool

	// SearchFields names the DB columns that ?q= free-text search operates
	// on (e.g. []string{"title","body"}). When non-empty, a List request
	// carrying ?q=<term> tokenizes the term on whitespace (deduped, capped
	// at filter.MaxSearchTerms) and AND-composes one LOWER(col) LIKE
	// pattern per token across the listed fields. Matching is
	// ASCII-case-insensitive everywhere (Unicode-folding on Postgres).
	// Leave nil to keep pre-existing behaviour (?q= is ignored). Column
	// names must be known, non-Hidden, and String/Text-typed; Define panics
	// otherwise. An entity WITHOUT SearchFields ignores ?q= exactly as
	// before (back-compat).
	SearchFields []string

	// Seed runs once per entity after AutoMigrate creates the table. The
	// framework tracks completion in the _gofastr_seeded ledger; subsequent
	// App.Start() calls skip the entity. Errors abort App.Start.
	//
	// Go-only: function values cannot be expressed in a blueprint
	// declaration. Apps whose entities come from a gofastr.yml blueprint
	// must wire seeding from Go.
	//
	// Concurrency: RunSeeds is NOT safe for concurrent invocation across
	// multiple processes. The framework assumes serialized startup (one
	// process / replica calls App.Start at a time). For HA setups, gate
	// seeding behind an external mechanism (init container, one-shot
	// job, advisory lock). Seed implementations should be idempotent
	// (INSERT … ON CONFLICT DO NOTHING) so accidental re-runs cannot
	// duplicate data.
	Seed func(ctx context.Context, db *sql.DB) error

	// SeedFS is an optional fs.FS (typically a //go:embed embed.FS) that
	// the framework attaches to the Seed function's context. Use with
	// SeedPath to point at a single file within the FS.
	//
	// Go-only: like Seed itself, an fs.FS cannot be expressed in JSON
	// entity declarations.
	SeedFS fs.FS

	// SeedPath is the path within SeedFS that Seed should consume.
	// Ignored when SeedFS is nil.
	SeedPath string

	// LenientFilters opts the entity's auto-CRUD List endpoint OUT of strict
	// filter parsing. By default an unknown top-level filter key (a typo like
	// ?stauts=active) is REJECTED with a 400 rather than silently dropped —
	// silently dropping it returns an UNFILTERED result set, which is a
	// data-exposure and broken-client hazard. Set true only as a migration
	// escape hatch for an endpoint that must tolerate arbitrary extra query
	// params (e.g. legacy tracking params); prefer fixing the caller.
	// Default false (strict).
	LenientFilters bool

	// AllowedFilterParams declares extra query-param keys that are NOT entity
	// columns but are legitimately consumed elsewhere on the List request —
	// typically read by a BeforeList hook or custom middleware (e.g. a
	// bespoke "?region=eu" scope param). Strict filter parsing skips these
	// instead of rejecting them, so the endpoint keeps typo-protection for
	// real fields without falling back to LenientFilters (which disables it
	// entirely). Reserved list controls are always allowed and need not be
	// listed here.
	AllowedFilterParams []string

	// timestampsSet tracks whether Timestamps was explicitly set.
	// When false (zero value), Define defaults Timestamps to true.
	timestampsSet bool
}

// ScopeConfig groups entity behavior that constrains which rows a request can
// see or mutate. When EntityConfig.Scope is non-nil it is authoritative over
// the compatibility flat fields.
type ScopeConfig struct {
	SoftDelete     bool
	MultiTenant    bool
	TenantField    string
	OwnerField     string
	CrossOwnerRead string
}

// PaginationConfig groups list limits and keyset cursor configuration. A
// non-empty CursorFields composite takes precedence over CursorField.
type PaginationConfig struct {
	CursorField  string
	CursorFields []string
	MaxListLimit int
}

// ExposureConfig groups generated surfaces and their access posture. CRUD is a
// pointer so nil retains auto mode, while false explicitly disables routes.
type ExposureConfig struct {
	CRUD   *bool
	MCP    bool
	Public bool
	Access AccessControl
}

// AccessControl declares the RBAC permission required for each CRUD operation
// on an entity. Each field holds a permission string (e.g. "posts:write");
// blank means that operation is not RBAC-gated. Read covers both List and Get.
//
// Permissions are plain strings here so the entity package stays decoupled
// from framework/access; the CRUD layer converts them to access.Permission and
// enforces them via access.Can against the policy + roles in the request
// context.
type AccessControl struct {
	Read   string // List + Get
	Create string
	Update string
	Delete string
}

// Declared reports whether any per-operation permission is set — i.e.
// whether the entity opted into RBAC gating at all. Used by
// framework/crud's secure-by-default session gate to tell "this entity
// declared an (possibly partial) access: block, defer to it as today"
// apart from "this entity declared nothing".
func (a AccessControl) Declared() bool {
	return a.Read != "" || a.Create != "" || a.Update != "" || a.Delete != ""
}

// Index declares a secondary index on an entity. Both dialects accept the
// same CREATE INDEX syntax; AutoMigrate emits CREATE INDEX IF NOT EXISTS so
// re-runs are safe.
//
// Name is optional — when empty, AutoMigrate synthesises one as
// "idx_<table>_<col1>_<col2>". Unique indices reject duplicate rows for the
// chosen column set; for single-column uniqueness prefer the Field-level
// Unique flag which lives on the column definition.
//
// Expression covers the case the column-list form can't express: a
// functional or partial index, e.g. `UNIQUE(user_id, lower(food))` to
// dedupe case-insensitively. When non-empty, Expression is rendered
// verbatim inside the index body (replacing Columns) — Name is REQUIRED
// in that case because there's no safe deterministic slug for an
// arbitrary expression. Use Columns for plain identifier indices;
// reach for Expression when SQL functions or constants need to
// participate in the indexed key.
type Index struct {
	Name       string   `json:"name,omitempty"`
	Columns    []string `json:"columns,omitempty"`
	Unique     bool     `json:"unique,omitempty"`
	Expression string   `json:"expression,omitempty"`
}

// Endpoint declares a custom route owned by an entity.
//
// Path may be absolute ("/posts/{id}/publish") or relative to the entity table
// path ("{id}/publish"). Both Go 1.22 "{id}" and older ":id" parameter syntax
// are accepted. Handler is used for HTTP. MCPHandler is optional and is only
// registered when MCP is true.
//
// InputSchema and OutputSchema are OPTIONAL typed descriptions of the request
// body and the success (200) response, expressed as []schema.Field — the same
// representation the entity's own CRUD schema is built from, so OpenAPI and the
// generated MCP tool both consume one source. When unset (nil), the endpoint
// renders exactly as before: a shapeless {type:object} request/response in
// OpenAPI and a {type:object} MCP tool input schema. InputSchema is ignored for
// GET endpoints (which carry no request body).
type Endpoint struct {
	Method       string          `json:"method"`
	Path         string          `json:"path"`
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	MCP          bool            `json:"mcp,omitempty"`
	InputSchema  []schema.Field  `json:"inputSchema,omitempty"`
	OutputSchema []schema.Field  `json:"outputSchema,omitempty"`
	Handler      http.Handler    `json:"-"`
	MCPHandler   mcp.ToolHandler `json:"-"`
}

// TenantColumn returns the tenant-scoping column name for this entity:
// TenantField when set, otherwise the framework default "tenant_id". This is
// the single source of the column name across injection, auto-migrate, and the
// CRUD insert/scope/filter paths.
func (c EntityConfig) TenantColumn() string {
	if c.TenantField != "" {
		return c.TenantField
	}
	return "tenant_id"
}

// WithTimestamps returns a copy of the config with Timestamps set to the
// given value. Use this to opt out of the default (true).
func (c EntityConfig) WithTimestamps(v bool) EntityConfig {
	c.Timestamps = v
	c.timestampsSet = true
	return c
}

// Entity represents a registered domain entity with its config and DB handle.
type Entity struct {
	Config     EntityConfig
	DB         *sql.DB
	PrimaryKey string // defaults to "id"
}

// Define creates a new Entity with the given name and configuration.
// It applies defaults (Table, Timestamps=true) and stores the name.
// It also injects system fields (id, timestamps) with AutoGenerate flags
// unless the user has already defined them.
func Define(name string, config EntityConfig) *Entity {
	config = config.normalizeSubConfigs()
	config.Name = name

	// Apply default table name
	if config.Table == "" {
		config.Table = toSnake(name)
	}

	// Timestamps defaults to true unless explicitly set via WithTimestamps
	if !config.timestampsSet {
		config.Timestamps = true
	}

	// Inject id field if not already defined by user
	hasID := false
	for _, f := range config.Fields {
		if f.Name == "id" {
			hasID = true
			break
		}
	}
	if !hasID {
		idField := schema.Field{
			Name:         "id",
			Type:         schema.UUID,
			AutoGenerate: schema.AutoUUID,
			ReadOnly:     true,
		}
		config.Fields = append([]schema.Field{idField}, config.Fields...)
	}

	// Inject timestamp fields if enabled and not already defined
	if config.Timestamps {
		hasCreatedAt := false
		hasUpdatedAt := false
		for _, f := range config.Fields {
			if f.Name == "created_at" {
				hasCreatedAt = true
			}
			if f.Name == "updated_at" {
				hasUpdatedAt = true
			}
		}
		if !hasCreatedAt {
			config.Fields = append(config.Fields, schema.Field{
				Name:         "created_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoTimestamp,
				ReadOnly:     true,
			})
		}
		if !hasUpdatedAt {
			config.Fields = append(config.Fields, schema.Field{
				Name:         "updated_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoTimestamp,
				ReadOnly:     true,
			})
		}
	}

	// Inject tenant_id field if multi-tenancy is enabled and not already
	// declared. Symmetric with deleted_at: the crud layer injects tenant_id
	// on writes and scopes reads by it, so the column MUST exist in the
	// table. Without this, AutoMigrate would create a table with no
	// tenant_id column and the first create request would fail with a
	// "no such column" error. Hidden + ReadOnly keeps it out of request
	// bodies and API responses — the framework manages its value.
	if config.MultiTenant {
		tenantCol := config.TenantColumn()
		// Validate the tenant column name once, here, so a misconfigured
		// TenantField fails loud at definition with an actionable message —
		// rather than as an opaque "unsafe SQL identifier" panic on the first
		// tenant-scoped request, where the column name is interpolated into the
		// WHERE clause.
		if _, err := query.SafeIdent(tenantCol); err != nil {
			panic(fmt.Sprintf("entity %q: TenantField %q is not a valid SQL identifier: %v", name, tenantCol, err))
		}
		hasTenantID := false
		for _, f := range config.Fields {
			if f.Name == tenantCol {
				hasTenantID = true
			}
		}
		if !hasTenantID {
			config.Fields = append(config.Fields, schema.Field{
				Name:         tenantCol,
				Type:         schema.String,
				AutoGenerate: schema.AutoNone,
				ReadOnly:     true,
				Hidden:       true,
			})
		}
	}

	// Inject soft delete field if enabled
	if config.SoftDelete {
		hasDeletedAt := false
		for _, f := range config.Fields {
			if f.Name == "deleted_at" {
				hasDeletedAt = true
			}
		}
		if !hasDeletedAt {
			config.Fields = append(config.Fields, schema.Field{
				Name:         "deleted_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoNone,
				ReadOnly:     true,
				Hidden:       true,
			})
		}
	}

	// CrossOwnerRead lifts owner scoping for reads only, so it only makes
	// sense on an entity that is owner-scoped to begin with. Catch the
	// misconfiguration here, at definition, with an actionable message —
	// otherwise the knob silently does nothing.
	if config.CrossOwnerRead != "" && config.OwnerField == "" {
		panic(fmt.Sprintf("entity %q: CrossOwnerRead %q requires OwnerField (cross-owner read only applies to owner-scoped entities)", name, config.CrossOwnerRead))
	}

	// SearchFields must reference known, non-Hidden, String/Text columns.
	// An unknown name would produce a "no such column" error at query time;
	// a Hidden column would turn ?q= into a value-disclosure oracle (same
	// rationale as ParseFilters' hidden stripping); a non-text column can't
	// meaningfully participate in LOWER() LIKE matching. Catch all three
	// here, at definition, with an actionable message.
	if len(config.SearchFields) > 0 {
		for _, sf := range config.SearchFields {
			var found *schema.Field
			for i := range config.Fields {
				if config.Fields[i].Name == sf {
					found = &config.Fields[i]
					break
				}
			}
			if found == nil {
				panic(fmt.Sprintf("entity %q: SearchFields entry %q is not a declared field", name, sf))
			}
			if found.Hidden {
				panic(fmt.Sprintf("entity %q: SearchFields entry %q is Hidden (search would disclose its values)", name, sf))
			}
			if found.Type != schema.String && found.Type != schema.Text {
				panic(fmt.Sprintf("entity %q: SearchFields entry %q must be String or Text, got %d", name, sf, found.Type))
			}
		}
	}

	// Derive a BelongsTo relation for every {Type: Relation} field that points
	// at a target entity. Without this the field is only consumed for an
	// OpenAPI x-relation annotation: migrate emits a plain column with no FK
	// constraint and ?include= (which resolves against Config.Relations) cannot
	// find the join. Deriving the relation here makes both work from the
	// single field declaration. The FK column for a BelongsTo lives on the
	// local table, so it IS the relation field's own column (Name). An
	// explicit relation already declared for the same name wins — we never
	// clobber caller-declared relations.
	for _, f := range config.Fields {
		if f.Type != schema.Relation || f.To == "" || f.Many {
			continue
		}
		exists := false
		for _, r := range config.Relations {
			if r.Name == f.Name {
				exists = true
				break
			}
		}
		if !exists {
			config.Relations = append(config.Relations, BelongsTo(f.Name, f.To, f.Name))
		}
	}

	e := &Entity{
		Config: config,
	}
	if e.PrimaryKey == "" {
		e.PrimaryKey = "id"
	}
	return e
}

func (c EntityConfig) normalizeSubConfigs() EntityConfig {
	if c.Scope != nil {
		c.SoftDelete = c.Scope.SoftDelete
		c.MultiTenant = c.Scope.MultiTenant
		c.TenantField = c.Scope.TenantField
		c.OwnerField = c.Scope.OwnerField
		c.CrossOwnerRead = c.Scope.CrossOwnerRead
	}
	if c.Pagination != nil {
		c.CursorField = c.Pagination.CursorField
		c.CursorFields = append([]string(nil), c.Pagination.CursorFields...)
		c.MaxListLimit = c.Pagination.MaxListLimit
	}
	if c.Exposure != nil {
		c.CRUD = c.Exposure.CRUD
		c.MCP = c.Exposure.MCP
		c.Public = c.Exposure.Public
		c.Access = c.Exposure.Access
	}
	return c
}

// SetDB sets the database connection for this entity.
func (e *Entity) SetDB(db *sql.DB) {
	e.DB = db
}

// GetName returns the entity name.
func (e *Entity) GetName() string {
	return e.Config.Name
}

// GetTable returns the DB table name.
func (e *Entity) GetTable() string {
	return e.Config.Table
}

// GetFields returns the entity's field definitions.
func (e *Entity) GetFields() []schema.Field {
	return e.Config.Fields
}

// Schema returns a core/schema.Schema built from the entity's fields.
func (e *Entity) Schema() schema.Schema {
	return schema.Schema{Fields: e.Config.Fields}
}

// String implements fmt.Stringer.
func (e *Entity) String() string {
	return fmt.Sprintf("Entity(%s/%s)", e.Config.Name, e.Config.Table)
}

// Validate checks that the entity config is well-formed.
func (e *Entity) Validate() error {
	if e.Config.Name == "" {
		return fmt.Errorf("entity: name must not be empty")
	}
	if e.Config.Table == "" {
		return fmt.Errorf("entity: table must not be empty")
	}
	if len(e.Config.Fields) == 0 {
		return fmt.Errorf("entity %q: must have at least one field", e.Config.Name)
	}

	seen := make(map[string]bool, len(e.Config.Fields))
	for _, f := range e.Config.Fields {
		if f.Name == "" {
			return fmt.Errorf("entity %q: field name must not be empty", e.Config.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("entity %q: duplicate field %q", e.Config.Name, f.Name)
		}
		seen[f.Name] = true

		if f.Type == schema.Relation && f.To == "" {
			return fmt.Errorf("entity %q: relation field %q must specify To", e.Config.Name, f.Name)
		}
	}

	return nil
}

// toSnake converts CamelCase or kebab-case to snake_case.
func toSnake(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if strings.ToLower(s) == s {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
