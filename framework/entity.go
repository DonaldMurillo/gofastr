package framework

import (
	"database/sql"
	"fmt"
	"strings"

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
	SoftDelete  bool           // enable soft-delete (deleted_at column)
	MultiTenant bool           // scope queries by tenant_id
	Timestamps  bool           // add created_at / updated_at columns
	CRUD        bool           // auto-generate CRUD routes
	MCP         bool           // auto-generate MCP tools

	// timestampsSet tracks whether Timestamps was explicitly set.
	// When false (zero value), Define defaults Timestamps to true.
	timestampsSet bool
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
