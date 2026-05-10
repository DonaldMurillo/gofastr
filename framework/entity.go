package framework

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/core/mcp"
	"github.com/gofastr/gofastr/core/schema"
)

// EntityConfig holds the declarative configuration for an entity.
// Name is set via Define(); Fields declare the schema.
// Timestamps defaults to true — use WithTimestamps(false) to disable.
type EntityConfig struct {
	Name        string         // entity name (e.g. "users")
	Table       string         // DB table name (defaults to snake_case of Name)
	Fields      []schema.Field // typed field definitions
	Relations   []Relation     // entity relationships
	Endpoints   []Endpoint     // custom HTTP endpoints for this entity
	SoftDelete  bool           // enable soft-delete (deleted_at column)
	MultiTenant bool           // scope queries by tenant_id
	Timestamps  bool           // add created_at / updated_at columns
	CRUD        *bool          // auto-generate CRUD routes. nil=auto(true when DB set), &true=always, &false=never
	MCP         bool           // auto-generate MCP tools
	CursorField  string         // optional: single-field keyset cursor; defaults to PrimaryKey
	CursorFields []string       // optional: composite cursor — ORDER BY each field in order with tuple-compared keyset. Wins over CursorField when non-empty.
	Indices      []Index        // additional CREATE INDEX statements emitted by AutoMigrate

	// timestampsSet tracks whether Timestamps was explicitly set.
	// When false (zero value), Define defaults Timestamps to true.
	timestampsSet bool
}

// Index declares a secondary index on an entity. Both dialects accept the
// same CREATE INDEX syntax; AutoMigrate emits CREATE INDEX IF NOT EXISTS so
// re-runs are safe.
//
// Name is optional — when empty, AutoMigrate synthesises one as
// "idx_<table>_<col1>_<col2>". Unique indices reject duplicate rows for the
// chosen column set; for single-column uniqueness prefer the Field-level
// Unique flag which lives on the column definition.
type Index struct {
	Name    string   `json:"name,omitempty"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique,omitempty"`
}

// Endpoint declares a custom route owned by an entity.
//
// Path may be absolute ("/posts/{id}/publish") or relative to the entity table
// path ("{id}/publish"). Both Go 1.22 "{id}" and older ":id" parameter syntax
// are accepted. Handler is used for HTTP. MCPHandler is optional and is only
// registered when MCP is true.
type Endpoint struct {
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	MCP         bool            `json:"mcp,omitempty"`
	Handler     http.Handler    `json:"-"`
	MCPHandler  mcp.ToolHandler `json:"-"`
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

	e := &Entity{
		Config: config,
	}
	if e.PrimaryKey == "" {
		e.PrimaryKey = "id"
	}
	return e
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
