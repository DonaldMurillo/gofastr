package entity

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// EntityConfig is an entity's declaration. Define sets Name, defaults Table to
// snake_case(Name) and resolves a nil Timestamps to true.
type EntityConfig struct {
	Name      string         // entity name (e.g. "users")
	Table     string         // DB table name (defaults to snake_case of Name)
	Fields    []schema.Field // typed field definitions
	Relations []Relation     // entity relationships

	// PrimaryKey names the key columns in order; nil means "id", a generated UUID
	// Define injects. Several columns make a composite key.
	PrimaryKey []string

	Endpoints  []Endpoint        // custom HTTP endpoints for this entity
	Scope      *ScopeConfig      // ownership, tenancy, and soft-delete behavior
	Pagination *PaginationConfig // list limits and keyset cursor shape
	Exposure   *ExposureConfig   // generated HTTP/MCP and access posture
	Timestamps *bool             // add created_at / updated_at columns; nil defaults to true
	Indices    []Index           // secondary indices AutoMigrate creates
	Unmanaged  bool              // no DDL is emitted; the object exists elsewhere

	// Properties holds caller-owned metadata the framework never interprets.
	Properties map[string]any

	// SearchFields names the String or Text columns ?q= matches, one LOWER LIKE per
	// term AND-ed. Without them a non-blank ?q= answers 400 search_not_configured.
	SearchFields []string

	// Seed runs once per entity after AutoMigrate creates the table, recorded in
	// the _gofastr_seeded ledger; an error aborts App.Start. Go-only, and not safe
	// across concurrently starting replicas, so keep it idempotent.
	Seed func(ctx context.Context, db *sql.DB) error

	// SeedFS is an optional fs.FS attached to Seed's context. Go-only.
	SeedFS fs.FS

	// SeedPath is the file within SeedFS that Seed reads; ignored when SeedFS is nil.
	SeedPath string

	// LenientFilters stops List from refusing unknown filter keys with 400. Default false (strict).
	LenientFilters bool

	// AllowedFilterParams lists non-column query keys strict filter parsing skips,
	// such as a param a BeforeList hook reads.
	AllowedFilterParams []string

	// Display holds the screen hints; nil means every default. Define copies it.
	Display *DisplayConfig
	// States names the state field and its moves; nil means none. Define copies it.
	States *StatesConfig
	// Renames maps old column names to new ones, so the schema diff renames a
	// column instead of dropping it and adding another.
	Renames map[string]string
}

// ScopeConfig groups the rules that limit which rows a request reads or changes. Define always sets EntityConfig.Scope.
type ScopeConfig struct {
	SoftDelete     bool   // add deleted_at and hide deleted rows by default
	MultiTenant    bool   // scope rows to the tenant in the request context
	TenantField    string // tenant column; empty uses tenant_id
	OwnerField     string // owner column stamped and scoped by auto-CRUD
	CrossOwnerRead string // permission that lifts owner scoping for reads only
}

// PaginationConfig groups list limits and keyset cursor columns; CursorFields beats CursorField.
type PaginationConfig struct {
	CursorField  string
	CursorFields []string
	MaxListLimit int
}

// ExposureConfig groups generated routes and their access rules.
type ExposureConfig struct {
	CRUD      *bool            // nil or true generates CRUD routes
	MCP       bool             // register MCP CRUD tools
	Public    bool             // allow anonymous CRUD when no scope or access rule applies
	Access    AccessControl    // per-operation permissions
	ReadScope *ReadScopeConfig // narrows which rows a caller may read; nil = no row filtering
}

// RowPredicate is one condition on the entity's own columns, for ReadScopeConfig.
type RowPredicate struct {
	Field  string   // column name, exactly as declared on the entity
	Op     string   // "eq", "neq", "in", "not_in"; empty means "eq"
	Value  string   // single-value ops
	Values []string // "in" / "not_in"
}

// ReadScopeConfig narrows which rows a caller may read: Filter's predicates are
// AND-ed into every read of the table. Writes are not filtered.
type ReadScopeConfig struct {
	Filter       []RowPredicate
	Unrestricted string
}

// AccessControl names the permission each CRUD operation requires. Once any is
// set, a blank Create, Update or Delete is refused to every caller; a blank Read
// stays open. [Anyone] opens an operation on purpose.
type AccessControl struct {
	Read   string // List + Get
	Create string
	Update string
	Delete string
}

// Anyone, as an AccessControl operation, opens it to every caller.
const Anyone = "anyone"

// Declared reports whether any operation's permission is set.
func (a AccessControl) Declared() bool {
	return a.Read != "" || a.Create != "" || a.Update != "" || a.Delete != ""
}

// Index declares a secondary index, emitted as CREATE INDEX IF NOT EXISTS.
type Index struct {
	Name       string   `json:"name,omitempty"`
	Columns    []string `json:"columns,omitempty"`
	Unique     bool     `json:"unique,omitempty"`
	Expression string   `json:"expression,omitempty"`
}

// DefaultIndexName is "idx_<table>_<cols>", the table's schema left off and a hashed tail past 63 bytes.
func DefaultIndexName(table string, cols []string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		table = table[i+1:]
	}
	return query.FitIdent("idx_" + table + "_" + strings.Join(cols, "_"))
}

// UniqueKeyName is a Unique field's constraint name, <table>_<col>_key with the schema left off, hashed past 63 bytes.
func UniqueKeyName(table, col string) string {
	return query.FitObjectName(tableBase(table), col, "key")
}

// LegacyUniqueKeyName is the Unique constraint name of tables built before v0.96.0.
func LegacyUniqueKeyName(table, col string) string {
	return query.ObjectName(tableBase(table), col, "key")
}

func tableBase(table string) string {
	return table[strings.LastIndexByte(table, '.')+1:]
}

// Endpoint declares a custom route owned by an entity. A relative Path resolves
// under the entity's table path (and API prefix). The route answers 401 to an
// anonymous caller unless Public, and 403 to a cross-site unsafe request.
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

	// MCPGate runs before MCPHandler on every call and decides the tool's
	// visibility; nil requires an authenticated caller unless Public.
	MCPGate func(ctx context.Context) error `json:"-"`

	// Public serves both doors to anonymous callers.
	Public bool `json:"public,omitempty"`

	// SDK marks an endpoint the generated clients get a typed call for.
	SDK bool `json:"sdk,omitempty"`

	// Returns names a successful call's answer for the clients: an entity name,
	// "[]"+name for a list, or "" for an empty object.
	Returns string `json:"returns,omitempty"`
}

// TenantColumn returns Scope.TenantField, or "tenant_id".
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

// Entity is a registered entity: its config and DB handle.
type Entity struct {
	Config EntityConfig
	DB     *sql.DB

	// Version is the route group prefix the entity is mounted under (App.GroupEntity), empty for App.Entity.
	Version string

	// OpenAPITag tags the entity's OpenAPI operations; empty uses the entity name.
	OpenAPITag string

	audited int32
}

// Define resolves config into an Entity, injecting system columns; it panics on an invalid declaration.
func Define(name string, config EntityConfig) *Entity {
	config = config.normalizeSubConfigs()
	config.Name = name

	if config.Table == "" {
		config.Table = toSnake(name)
	}

	config.PrimaryKey = slices.Clone(config.PrimaryKey)
	keyCols := config.keyColumns()

	if config.Scope.MultiTenant {
		if _, err := query.SafeIdent(config.TenantColumn()); err != nil {
			panic(fmt.Sprintf("entity %q: TenantField %q is not a valid SQL identifier: %v", name, config.TenantColumn(), err))
		}
	}
	if config.Scope.OwnerField != "" {
		if _, err := query.SafeIdent(config.Scope.OwnerField); err != nil {
			panic(fmt.Sprintf("entity %q: OwnerField %q is not a valid SQL identifier: %v", name, config.Scope.OwnerField, err))
		}
	}
	config.Fields = config.systemFields()
	for i := range config.Fields {
		if config.Fields[i].Mask != nil {
			config.Fields[i].NoQuery = true
		}
	}

	if config.Scope.CrossOwnerRead != "" && config.Scope.OwnerField == "" {
		panic(fmt.Sprintf("entity %q: CrossOwnerRead %q requires OwnerField (cross-owner read only applies to owner-scoped entities)", name, config.Scope.CrossOwnerRead))
	}

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

	if rs := config.Exposure.ReadScope; rs != nil && len(rs.Filter) > 0 {
		seenIn := map[string]bool{}
		for _, p := range rs.Filter {
			if p.Op != "in" {
				continue
			}
			if seenIn[p.Field] {
				panic(fmt.Sprintf("entity %q: ReadScope declares two %q predicates on field %q; they AND to nothing and render as a single widened IN list. Combine the values into one predicate", name, "in", p.Field))
			}
			seenIn[p.Field] = true
		}
		for _, p := range rs.Filter {
			var found *schema.Field
			for i := range config.Fields {
				if config.Fields[i].Name == p.Field {
					found = &config.Fields[i]
					break
				}
			}
			if found == nil {
				panic(fmt.Sprintf("entity %q: ReadScope field %q is not a declared field", name, p.Field))
			}
			if found.Hidden {
				panic(fmt.Sprintf("entity %q: ReadScope field %q is Hidden (a predicate on a masked column leaks its values through the row set)", name, p.Field))
			}
			switch p.Op {
			case "", "eq", "neq":
				if len(p.Values) > 0 {
					panic(fmt.Sprintf("entity %q: ReadScope op %q on field %q must leave Values empty (only in/not_in take Values)", name, p.Op, p.Field))
				}
			case "in", "not_in":
				if len(p.Values) == 0 {
					panic(fmt.Sprintf("entity %q: ReadScope op %q on field %q requires a non-empty Values", name, p.Op, p.Field))
				}
				if p.Value != "" {
					panic(fmt.Sprintf("entity %q: ReadScope op %q on field %q must leave Value empty (it takes Values); the stray Value %q would be silently ignored", name, p.Op, p.Field, p.Value))
				}
			default:
				panic(fmt.Sprintf("entity %q: ReadScope op %q on field %q must be one of eq, neq, in, not_in (empty means eq)", name, p.Op, p.Field))
			}
		}
	}

	config.checkKeyColumns()

	for i, cf := range config.Pagination.CursorFields {
		if slices.Contains(config.Pagination.CursorFields[:i], cf) {
			panic(fmt.Sprintf("entity %q: cursor field %q is listed twice", name, cf))
		}
	}
	var cursorCols []string
	switch {
	case len(config.Pagination.CursorFields) > 0:
		cursorCols = append(cursorCols, config.Pagination.CursorFields...)
		cursorCols = append(cursorCols, keyCols...) // auto-appended tiebreak
	case config.Pagination.CursorField != "":
		cursorCols = append(cursorCols, config.Pagination.CursorField)
		cursorCols = append(cursorCols, keyCols...)
	default:
		cursorCols = append(cursorCols, keyCols...)
	}
	for _, cf := range cursorCols {
		declared := false
		for i := range config.Fields {
			f := config.Fields[i]
			if f.Name != cf {
				continue
			}
			declared = true
			if f.Hidden {
				panic(fmt.Sprintf("entity %q: cursor field %q is Hidden (the cursor token would carry its stored value back to the caller)", name, cf))
			}
			if f.NoQuery {
				panic(fmt.Sprintf("entity %q: cursor field %q is NoQuery (keyset paging orders and compares on the stored value, and the cursor token carries it verbatim)", name, cf))
			}
			break
		}
		if !declared {
			panic(fmt.Sprintf("entity %q: cursor field %q is not a declared field", name, cf))
		}
	}
	for _, cf := range cursorCols {
		for _, f := range config.Fields {
			if f.Name == cf && CanHoldNull(f, keyCols) {
				panic(fmt.Sprintf("entity %q: cursor field %q can hold NULL (keyset paging skips NULL rows); make it Required, or page on a non-null column", name, cf))
			}
		}
	}
	config.checkKeyServed()

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
	return &Entity{Config: config}
}

// WithSystemFields returns a copy with the system columns Define would inject added to Fields.
func (c EntityConfig) WithSystemFields() EntityConfig {
	c.Fields = c.normalizeSubConfigs().systemFields()
	return c
}

func (c EntityConfig) systemFields() []schema.Field {
	c.Fields = slices.Clone(c.Fields)
	hasID := false
	for _, f := range c.Fields {
		if f.Name == "id" {
			hasID = true
			break
		}
	}
	if !hasID && slices.Contains(c.keyColumns(), "id") {
		idField := schema.Field{
			Name:         "id",
			Type:         schema.UUID,
			AutoGenerate: schema.AutoUUID,
			ReadOnly:     true,
		}
		c.Fields = append([]schema.Field{idField}, c.Fields...)
	}

	if *c.Timestamps {
		hasCreatedAt := false
		hasUpdatedAt := false
		for _, f := range c.Fields {
			if f.Name == "created_at" {
				hasCreatedAt = true
			}
			if f.Name == "updated_at" {
				hasUpdatedAt = true
			}
		}
		if !hasCreatedAt {
			c.Fields = append(c.Fields, schema.Field{
				Name:         "created_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoTimestamp,
				ReadOnly:     true,
			})
		}
		if !hasUpdatedAt {
			c.Fields = append(c.Fields, schema.Field{
				Name:         "updated_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoTimestamp,
				ReadOnly:     true,
			})
		}
	}

	if c.Scope.MultiTenant {
		tenantCol := c.TenantColumn()
		hasTenantID := false
		for _, f := range c.Fields {
			if f.Name == tenantCol {
				hasTenantID = true
			}
		}
		if !hasTenantID {
			c.Fields = append(c.Fields, schema.Field{
				Name:         tenantCol,
				Type:         schema.String,
				AutoGenerate: schema.AutoNone,
				ReadOnly:     true,
				Hidden:       true,
			})
		}
	}

	if c.Scope.SoftDelete {
		hasDeletedAt := false
		for _, f := range c.Fields {
			if f.Name == "deleted_at" {
				hasDeletedAt = true
			}
		}
		if !hasDeletedAt {
			c.Fields = append(c.Fields, schema.Field{
				Name:         "deleted_at",
				Type:         schema.Timestamp,
				AutoGenerate: schema.AutoNone,
				ReadOnly:     true,
				Hidden:       true,
			})
		}
	}

	if c.Scope.OwnerField != "" {
		hasOwner := false
		for _, f := range c.Fields {
			if f.Name == c.Scope.OwnerField {
				hasOwner = true
			}
		}
		if !hasOwner {
			c.Fields = append(c.Fields, schema.Field{
				Name:         c.Scope.OwnerField,
				Type:         schema.String,
				AutoGenerate: schema.AutoNone,
				ReadOnly:     true,
				Hidden:       true,
			})
		}
	}

	return c.Fields
}

func (c EntityConfig) keyColumns() []string {
	if c.PrimaryKey == nil {
		return []string{"id"}
	}
	return c.PrimaryKey
}

func (c EntityConfig) checkKeyColumns() {
	if err := c.keyColumnsErr(); err != nil {
		panic(err.Error())
	}
}

func (c EntityConfig) keyColumnsErr() error {
	if c.PrimaryKey != nil && len(c.PrimaryKey) == 0 {
		return fmt.Errorf("entity %q: PrimaryKey names no column; leave it nil for \"id\"", c.Name)
	}
	seen := make(map[string]bool, len(c.PrimaryKey))
	for _, col := range c.keyColumns() {
		if seen[col] {
			return fmt.Errorf("entity %q: primary key names %q twice", c.Name, col)
		}
		seen[col] = true
		if !slices.ContainsFunc(c.Fields, func(f schema.Field) bool { return f.Name == col }) {
			return fmt.Errorf("entity %q: primary key column %q is not a declared field", c.Name, col)
		}
	}
	return c.checkRecordKey()
}

// CheckRecordKey reports whether the key can name a record by id; Define panics when it cannot.
func (e *Entity) CheckRecordKey() error { return e.Config.checkRecordKey() }

func (c EntityConfig) checkRecordKey() error {
	keys := c.keyColumns()
	if len(keys) == 0 {
		return fmt.Errorf("entity %q has no primary key, so no id names one of its records", c.Name)
	}
	for _, col := range keys {
		i := slices.IndexFunc(c.Fields, func(f schema.Field) bool { return f.Name == col })
		if i >= 0 && !keyable(c.Fields[i]) {
			return fmt.Errorf("entity %q: primary key column %q has a type whose values have more than one spelling, so one record would answer to several ids; key on a string, text, image, file, integer, enum, UUID or relation column with no RawType (an integer column may set an integer RawType)", c.Name, col)
		}
	}
	return nil
}

// keyable reports whether f's values have one spelling each, so an id names one record.
func keyable(f schema.Field) bool {
	switch f.Type {
	case schema.String, schema.Text, schema.Int, schema.Enum, schema.UUID, schema.Relation, schema.Image, schema.File:
	case schema.Float, schema.Decimal, schema.Bool, schema.Date, schema.Timestamp, schema.JSON:
		return false
	default:
		return false
	}
	if f.RawType == "" {
		return true
	}
	raw := strings.TrimSpace(f.RawType)
	if f.Type != schema.Int || strings.ContainsFunc(raw, func(r rune) bool { return r > unicode.MaxASCII }) {
		return false
	}
	switch strings.ToUpper(raw) {
	case "INTEGER", "INT", "BIGINT", "SMALLINT", "INT2", "INT4", "INT8", "SERIAL", "BIGSERIAL", "SMALLSERIAL":
		return true
	}
	return false
}

func (c EntityConfig) checkKeyServed() {
	if err := c.keyServedErr(); err != nil {
		panic(err.Error())
	}
}

func (c EntityConfig) keyServedErr() error {
	keys := c.keyColumns()
	if len(keys) < 2 {
		return nil
	}
	for _, f := range c.Fields {
		if (f.Hidden || f.NoQuery) && slices.Contains(keys, f.Name) {
			return fmt.Errorf("entity %q: composite key column %q is Hidden or NoQuery, so the API cannot name its records; key on columns the API returns and filters, and enforce other uniqueness with a unique index", c.Name, f.Name)
		}
	}
	return nil
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
		if c.Exposure.ReadScope != nil {
			readScope := *c.Exposure.ReadScope
			readScope.Filter = append([]RowPredicate(nil), c.Exposure.ReadScope.Filter...)
			exposure.ReadScope = &readScope
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
	c.Display = copyDisplayConfig(c.Display)
	c.States = copyStatesConfig(c.States)
	c.Fields = c.States.withStateDefault(c.Fields)
	c.Relations = slices.Clone(c.Relations)
	for index := range c.Relations {
		c.Relations[index].ForeignKey = slices.Clone(c.Relations[index].ForeignKey)
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

// Validate checks the resolved config, its Display and States blocks included.
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
	wireKeys := make(map[string]string, len(e.Config.Fields))
	for _, f := range e.Config.Fields {
		if f.Name == "" {
			return fmt.Errorf("entity %q: field name must not be empty", e.Config.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("entity %q: duplicate field %q", e.Config.Name, f.Name)
		}
		seen[f.Name] = true

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
					"entity %q: fields %q and %q both resolve to wire key %q: a wire key addresses exactly one column; change one field's WireName",
					e.Config.Name, owner, f.Name, wk)
			}
		}
		for _, wk := range claims {
			if wk != f.Name && seen[wk] {
				return fmt.Errorf(
					"entity %q: field %q's wire key %q is field %q's column name: a body naming that column would write %q; change one of them",
					e.Config.Name, f.Name, wk, wk, f.Name)
			}
		}
		if owner, clash := wireKeys[f.Name]; clash && owner != f.Name {
			return fmt.Errorf(
				"entity %q: field %q's wire key %q is field %q's column name: a body naming that column would write %q; change one of them",
				e.Config.Name, owner, f.Name, f.Name, owner)
		}
		for _, wk := range claims {
			wireKeys[wk] = f.Name
		}

		if f.Type == schema.Relation && f.To == "" {
			return fmt.Errorf("entity %q: relation field %q must specify To", e.Config.Name, f.Name)
		}
		if f.Type != schema.Relation {
			for _, relation := range e.Config.Relations {
				if relation.Name == f.Name {
					if relation.Type == RelManyToOne && !relation.CascadeWrite &&
						len(relation.ForeignKey) == 1 && relation.ForeignKey[0] == f.Name {
						continue
					}
					return fmt.Errorf("entity %q: relation %q conflicts with scalar field %q", e.Config.Name, relation.Name, f.Name)
				}
			}
		}
		if f.Type == schema.Enum && len(f.Values) == 0 {
			return fmt.Errorf("entity %q: enum field %q must specify non-empty Values", e.Config.Name, f.Name)
		}

		if err := checkDefault(e.Config.Name, f); err != nil {
			return err
		}
	}

	if e.Config.Display != nil {
		if err := e.Config.Display.validate(e.Config.Name, e.Config.Fields, e.Config.Pagination); err != nil {
			return err
		}
	}
	if e.Config.States != nil {
		if err := e.Config.States.validate(e.Config); err != nil {
			return err
		}
	}
	return nil
}

func checkDefault(entity string, f schema.Field) error {
	if f.Default == nil || f.AutoGenerate != schema.AutoNone {
		return nil
	}
	if err := schema.Validate(f, defaultAsWireValue(f)); err != nil {
		return fmt.Errorf("entity %q: field %q has an invalid Default %#v: %v", entity, f.Name, f.Default, err)
	}
	return nil
}

// ValidateDefaults runs the Default check over cfg after Define, returning a Define panic as the error.
func ValidateDefaults(name string, cfg EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	e := Define(name, cfg)
	for _, f := range e.Config.Fields {
		if err := checkDefault(e.Config.Name, f); err != nil {
			return err
		}
	}
	return nil
}

// defaultAsWireValue renders a Go-authored Default in the form a client sends.
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
	default:
	}
	return f.Default
}

func toSnake(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return casing.ToSnake(s)
}
