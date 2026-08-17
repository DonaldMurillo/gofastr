package entity

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// EntityConfig holds the declarative configuration for an entity.
// Name is set via Define(); Fields declare the schema.
// Timestamps is nil by default; Define resolves nil to true.
type EntityConfig struct {
	Name       string            // entity name (e.g. "users")
	Table      string            // DB table name (defaults to snake_case of Name)
	Fields     []schema.Field    // typed field definitions
	Relations  []Relation        // entity relationships
	Endpoints  []Endpoint        // custom HTTP endpoints for this entity
	Scope      *ScopeConfig      // ownership, tenancy, and soft-delete behavior
	Pagination *PaginationConfig // list limits and keyset cursor shape
	Exposure   *ExposureConfig   // generated HTTP/MCP and access posture
	Timestamps *bool             // add created_at / updated_at columns; nil defaults to true
	Indices    []Index           // additional CREATE INDEX statements emitted by AutoMigrate
	Unmanaged  bool              // when true, the migration system never emits DDL for this object (it is created elsewhere — e.g. a view, an FTS virtual table, or a legacy/external table). The ORM still queries it.

	// Properties holds caller-owned metadata. The framework does not
	// interpret any key; generators, plugins, and apps define their own keys.
	Properties map[string]any

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
	// Renames declares column renames (old name → new name) so the schema
	// diff emits a non-destructive ALTER TABLE … RENAME COLUMN instead of a
	// data-losing DROP of the old column + ADD of the new one. Rename is
	// otherwise indistinguishable from drop+add, so it requires this explicit
	// declaration; auto-detection is unsafe. A rename only fires when the old
	// column is present in the live schema and the new name is declared on the
	// entity. Declare it in Go or under an entity's `renames:` key in a
	// blueprint.
	Renames map[string]string
}

// ScopeConfig groups the rules that constrain which rows a request can read
// or change. Define always populates EntityConfig.Scope on the resolved entity.
type ScopeConfig struct {
	SoftDelete     bool   // add deleted_at and hide deleted rows by default
	MultiTenant    bool   // scope rows to the tenant in the request context
	TenantField    string // tenant column; empty uses tenant_id
	OwnerField     string // owner column stamped and scoped by auto-CRUD
	CrossOwnerRead string // permission that lifts owner scoping for reads only
}

// PaginationConfig groups list limits and keyset cursor configuration. A
// non-empty CursorFields composite takes precedence over CursorField.
type PaginationConfig struct {
	CursorField  string
	CursorFields []string
	MaxListLimit int
}

// ExposureConfig groups generated routes and their access rules. CRUD is a
// pointer so nil keeps automatic route generation and false disables it.
type ExposureConfig struct {
	CRUD   *bool         // nil or true generates CRUD routes
	MCP    bool          // register MCP CRUD tools
	Public bool          // allow anonymous CRUD when no scope or access rule applies
	Access AccessControl // per-operation permissions
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
// Under framework.WithAPIPrefix a relative path resolves under the prefixed
// table path — WithAPIPrefix("/api") mounts "{id}/publish" on entity "posts"
// at POST /api/posts/{id}/publish, alongside that entity's CRUD routes. An
// absolute path bypasses the prefix entirely; use it to mount outside the
// entity's API namespace.
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

	// MCPGate is an optional per-caller precondition for the MCP twin. It
	// runs before MCPHandler on every tools/call, and decides whether the
	// tool is visible to that caller in tools/list.
	//
	// It exists because the two front doors of one Endpoint do not get the
	// same protection for free: Handler inherits the route's middleware
	// chain, while MCPHandler is registered straight onto the MCP server and
	// sees none of it. An endpoint behind auth.RequireRole("editor") was
	// therefore role-checked over HTTP and ungated over MCP.
	//
	// When unset, the twin defaults to requiring an authenticated caller
	// (framework.MCPRequireUser). Set MCPPublic to opt out of that default;
	// set this to something stricter, e.g. auth.MCPRole("editor").
	MCPGate func(ctx context.Context) error `json:"-"`

	// MCPPublic opts the MCP twin out of the default authenticated-caller
	// gate, for an endpoint that really is anonymous over HTTP too. Ignored
	// when MCPGate is set.
	MCPPublic bool `json:"mcpPublic,omitempty"`
}

// TenantColumn returns the tenant-scoping column name for this entity:
// Scope.TenantField when set, otherwise "tenant_id".
func (c EntityConfig) TenantColumn() string {
	if c.Scope != nil && c.Scope.TenantField != "" {
		return c.Scope.TenantField
	}
	return "tenant_id"
}

// WithTimestamps returns a copy with timestamp columns enabled or disabled.
func (c EntityConfig) WithTimestamps(v bool) EntityConfig {
	c.Timestamps = &v
	return c
}

// Entity represents a registered domain entity with its config and DB handle.
type Entity struct {
	Config     EntityConfig
	DB         *sql.DB
	PrimaryKey string // defaults to "id"

	// Version identifies the API version this entity is mounted under, when
	// registered via App.GroupEntity. It is the route group's full prefix
	// (e.g. "/api/v1"). Empty for entities registered via App.Entity — those
	// keep the historical single-version behaviour. The registry keys on
	// (Config.Name, Version) so the same entity name can coexist under
	// different versions; callers that don't care about version resolve the
	// unversioned or sole entity via Registry.Get.
	Version string

	// OpenAPITag is the tag applied to this entity's operations in the
	// generated OpenAPI document. Set from the route group's OpenAPITag
	// when registered via App.GroupEntity; empty for App.Entity (the tag
	// defaults to the entity name in that case).
	OpenAPITag string
}

// Define creates a new Entity with the given name and configuration.
// It applies defaults (Table, Timestamps=true) and stores the name.
// It also injects system fields (id, timestamps, and the scope-driven
// tenant/owner/soft-delete columns) with AutoGenerate flags unless the
// user has already defined them.
func Define(name string, config EntityConfig) *Entity {
	config = config.normalizeSubConfigs()
	config.Name = name

	// Apply default table name
	if config.Table == "" {
		config.Table = toSnake(name)
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
	if *config.Timestamps {
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
	if config.Scope.MultiTenant {
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
	if config.Scope.SoftDelete {
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

	// Inject the owner column when an owner scope is configured and no
	// field with that name is declared — symmetric with tenant_id above
	// and matching the blueprint generator's owner_field semantics. The
	// crud layer stamps the owner on writes (InjectOwner) and scopes
	// reads by it (ApplyOwnerScope), so the column MUST exist in the
	// table; without this, AutoMigrate would create a table with no
	// owner column — a create would silently persist an unowned row (the
	// INSERT column list comes from GetFields) and the first scoped read
	// would fail with "no such column". A field the caller DID declare
	// with that name is left untouched.
	if config.Scope.OwnerField != "" {
		// Validate the owner column name once, here, matching the
		// TenantField check above: it is interpolated into owner-scope
		// WHERE clauses, so a bad name should fail loud at definition,
		// not as an opaque panic on the first scoped request.
		if _, err := query.SafeIdent(config.Scope.OwnerField); err != nil {
			panic(fmt.Sprintf("entity %q: OwnerField %q is not a valid SQL identifier: %v", name, config.Scope.OwnerField, err))
		}
		hasOwner := false
		for _, f := range config.Fields {
			if f.Name == config.Scope.OwnerField {
				hasOwner = true
			}
		}
		if !hasOwner {
			config.Fields = append(config.Fields, schema.Field{
				Name:         config.Scope.OwnerField,
				Type:         schema.String,
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
	if config.Scope.CrossOwnerRead != "" && config.Scope.OwnerField == "" {
		panic(fmt.Sprintf("entity %q: CrossOwnerRead %q requires OwnerField (cross-owner read only applies to owner-scoped entities)", name, config.Scope.CrossOwnerRead))
	}

	// SearchFields must reference known, non-Hidden, non-NoQuery, String/Text
	// columns. An unknown name would produce a "no such column" error at query
	// time; a Hidden or NoQuery column would turn ?q= into a value-disclosure
	// oracle (same rationale as ParseFilters' exclusions); a non-text column
	// can't meaningfully participate in LOWER() LIKE matching. Catch all four
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
			if found.NoQuery {
				panic(fmt.Sprintf("entity %q: SearchFields entry %q is NoQuery (?q= would match on the stored value, disclosing what NoQuery keeps off the query surface)", name, sf))
			}
			if found.Type != schema.String && found.Type != schema.Text {
				panic(fmt.Sprintf("entity %q: SearchFields entry %q must be String or Text, got %d", name, sf, found.Type))
			}
		}
	}

	// Cursor columns are a query surface too: they land in ORDER BY and in the
	// keyset WHERE, and the emitted cursor token is base64 JSON of the raw
	// value — reversible, not secret. A Hidden or NoQuery keyset column
	// therefore hands the caller back exactly what those flags withhold, and
	// lets them binary-search it by forging cursors. Fail at definition, the
	// same way SearchFields does.
	//
	// The DEFAULT keyset column (the primary key) is checked too, not just
	// explicitly declared ones. Keyset paging falls back to the PK when no
	// cursor field is configured, so a NoQuery primary key is used exactly
	// as if it had been named — and its stored value ends up in the emitted
	// token. Checking only declared columns left ?cursor= leaking a value
	// that ?sort= and every filter refused, and disagreed with the DSL's
	// after() guard, which resolves the same default before checking.
	// This mirrors CrudHandler.cursorFields() branch for branch. Anything
	// less lets a column reach ORDER BY and the token without being checked:
	// a composite silently gains the primary key as its tiebreak, so
	// validating only the declared members missed a NoQuery `id` entirely.
	// EntityConfig carries no primary key of its own; Entity.PrimaryKey and
	// CrudHandler both default to "id", so that is the effective column.
	// normalizeSubConfigs has populated Pagination by here.
	var cursorCols []string
	switch {
	case len(config.Pagination.CursorFields) > 0:
		cursorCols = append(cursorCols, config.Pagination.CursorFields...)
		cursorCols = append(cursorCols, "id") // auto-appended tiebreak
	case config.Pagination.CursorField != "":
		cursorCols = append(cursorCols, config.Pagination.CursorField)
	default:
		cursorCols = append(cursorCols, "id")
	}
	for _, cf := range cursorCols {
		for i := range config.Fields {
			f := config.Fields[i]
			if f.Name != cf {
				continue
			}
			if f.Hidden {
				panic(fmt.Sprintf("entity %q: cursor field %q is Hidden (the cursor token would carry its stored value back to the caller)", name, cf))
			}
			if f.NoQuery {
				panic(fmt.Sprintf("entity %q: cursor field %q is NoQuery (keyset paging orders and compares on the stored value, and the cursor token carries it verbatim)", name, cf))
			}
			break
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
	if c.Scope == nil {
		c.Scope = &ScopeConfig{}
	} else {
		scope := *c.Scope
		c.Scope = &scope
	}
	if c.Pagination == nil {
		c.Pagination = &PaginationConfig{}
	} else {
		pagination := *c.Pagination
		pagination.CursorFields = append([]string(nil), c.Pagination.CursorFields...)
		c.Pagination = &pagination
	}
	if c.Exposure == nil {
		c.Exposure = &ExposureConfig{}
	} else {
		exposure := *c.Exposure
		if c.Exposure.CRUD != nil {
			crud := *c.Exposure.CRUD
			exposure.CRUD = &crud
		}
		c.Exposure = &exposure
	}
	if c.Timestamps == nil {
		enabled := true
		c.Timestamps = &enabled
	} else {
		timestamps := *c.Timestamps
		c.Timestamps = &timestamps
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
	// wireKeys maps each field's wire key (WireName when set, else the column
	// name) back to the column that claimed it. A wire key addresses exactly
	// one column: JSON has no way to express two.
	//
	// Without this guard the collision is SILENT and splits reads from writes.
	// CRUD's refreshFieldCache keeps whichever field it saw first, so a body
	// posted under the shared key writes to that column, while filters resolve
	// the same key independently and may target the other — the row written is
	// then invisible to the filter, with no error anywhere. crud.go documented
	// this as "a config error that ValidateWireNames catches at Define"; that
	// function never existed, so nothing caught it.
	wireKeys := make(map[string]string, len(e.Config.Fields))
	for _, f := range e.Config.Fields {
		if f.Name == "" {
			return fmt.Errorf("entity %q: field name must not be empty", e.Config.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("entity %q: duplicate field %q", e.Config.Name, f.Name)
		}
		seen[f.Name] = true

		// Hidden fields are included deliberately: they are excluded from
		// responses but still resolve on write and filter paths, so letting a
		// visible field alias a hidden column would be worse, not harmless.
		//
		// A bare column does not claim its literal name on the wire — it
		// claims the CASE-CONVERTED form (crud.wireKeyOfField), camelCase by
		// default. Validate cannot read JSONCase, which App assigns after
		// registration, so a bare column claims BOTH spellings and a clash
		// under either one is refused. Comparing only the literal name let
		// `author_id` and `WireName: "authorId"` through, which is the same
		// silent read/write split this guard exists to stop.
		claims := []string{f.WireName}
		if f.WireName == "" {
			claims = []string{f.Name}
			if camel := casing.ToCamel(f.Name); camel != f.Name {
				claims = append(claims, camel)
			}
		}
		for _, wk := range claims {
			if owner, clash := wireKeys[wk]; clash {
				return fmt.Errorf(
					"entity %q: fields %q and %q both resolve to wire key %q — a wire key addresses exactly one column; change one field's WireName",
					e.Config.Name, owner, f.Name, wk)
			}
		}
		for _, wk := range claims {
			wireKeys[wk] = f.Name
		}

		if f.Type == schema.Relation && f.To == "" {
			return fmt.Errorf("entity %q: relation field %q must specify To", e.Config.Name, f.Name)
		}

		// A Default is the value crud.doCreate substitutes for a field the
		// request body omitted. It reaches the driver through the same column
		// as a client-sent value but, before this check, through none of the
		// same checks: ValidateAll ran over the body and the Default was
		// applied afterwards. A malformed one therefore surfaced as a
		// per-request 500 with nothing actionable in it — or, on a dialect
		// whose column type is looser, as silently wrong data (a non-JSON
		// Default on a schema.JSON field 500s against Postgres JSONB and
		// stores fine in SQLite's TEXT).
		//
		// The value is static, so it is checked once here rather than on
		// every request, and a bad one is a bug in the app's own declaration
		// rather than in a request — so it fails the declaration, naming the
		// field, instead of reporting an app-authored bug as a caller's 400.
		//
		// Auto-generated fields are skipped, exactly as schema.ValidateAll
		// skips them: doCreate overwrites their body slot with the generated
		// value, so their Default is never an insert value. It survives only
		// as the column's DDL DEFAULT (migrate.ColumnDefaultClause), where an
		// explicit Default deliberately outranks gen_random_uuid().
		if f.Default != nil && f.AutoGenerate == schema.AutoNone {
			if err := schema.Validate(f, defaultAsWireValue(f)); err != nil {
				return fmt.Errorf("entity %q: field %q has an invalid Default %#v: %v", e.Config.Name, f.Name, f.Default, err)
			}
		}
	}

	return nil
}

// defaultAsWireValue renders a field's Default in the form schema.Validate
// expects, which is the form a client sends. The two differ because a Default
// is authored in Go, not JSON, and for a few types the natural Go literal is
// not the wire literal. Normalizing here keeps the registration check from
// refusing declarations the write path accepts — a false refusal fires at
// boot, for every caller at once, which is worse than the bug it guards.
//
// The patterns this permits, both of which reach the driver intact today:
//
//   - Decimal spelled as a Go number. Decimal travels as a string on the wire
//     so it keeps exact precision, but `{Type: schema.Decimal, Default: 0}` is
//     what examples/ecommerce writes and what `gofastr generate` emits for a
//     blueprint `decimal` field with `default: 0`. It renders `DEFAULT 0` in
//     DDL and binds as a number on insert; both dialects accept it.
//   - Timestamp/Date spelled as a time.Time. time.Time's String() is not
//     RFC 3339, so schema.Validate's generic fmt.Stringer path would reject a
//     value the driver takes verbatim.
func defaultAsWireValue(f schema.Field) any {
	switch f.Type {
	case schema.Decimal:
		switch v := f.Default.(type) {
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		case float32:
			return strconv.FormatFloat(float64(v), 'f', -1, 32)
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	case schema.Timestamp:
		if t, ok := f.Default.(time.Time); ok {
			return t.Format(time.RFC3339)
		}
	case schema.Date:
		if t, ok := f.Default.(time.Time); ok {
			return t.Format(time.DateOnly)
		}
	}
	return f.Default
}

// toSnake converts CamelCase or kebab-case to snake_case. The camelCase
// conversion is framework/internal/casing's (cached) — only the kebab/space
// normalization is local: casing.ToSnake leaves those bytes untouched, but a
// default table name must be a bare SQL identifier.
func toSnake(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return casing.ToSnake(s)
}
