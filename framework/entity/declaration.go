package entity

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// EntityDeclaration is the readable, JSON/YAML-friendly shape the gofastr.yml
// blueprint loader decodes entities into before converting them to
// EntityConfig. It mirrors EntityConfig while keeping field types readable.
type EntityDeclaration struct {
	Name        string             `json:"name"`
	Table       string             `json:"table,omitempty"`
	Fields      []FieldDeclaration `json:"fields"`
	Relations   []Relation         `json:"relations,omitempty"`
	Endpoints   []Endpoint         `json:"endpoints,omitempty"`
	SoftDelete  bool               `json:"soft_delete,omitempty"`
	MultiTenant bool               `json:"multi_tenant,omitempty"`
	// OwnerField names the DB column that holds the row's owner id
	// (e.g. "user_id"). When set AND an owner extractor is registered
	// by a battery, auto-CRUD scopes List/Get/Update/Delete by the
	// current request's owner and auto-stamps Create. Mirrors
	// EntityConfig.OwnerField; leave empty to keep pre-existing behaviour.
	OwnerField string `json:"owner_field,omitempty"`
	// CrossOwnerRead optionally names an RBAC permission that lifts owner
	// scoping for READ operations only on this entity, when the permission
	// is held in the request context. Mirrors EntityConfig.CrossOwnerRead;
	// leave empty to keep pre-existing behaviour. Requires OwnerField.
	CrossOwnerRead string `json:"cross_owner_read,omitempty"`
	// SearchFields names the DB columns that ?q= free-text search operates
	// on (e.g. ["title","body"]). Mirrors EntityConfig.SearchFields; leave
	// empty to keep pre-existing behaviour (?q= is ignored).
	SearchFields []string `json:"search_fields,omitempty"`
	// Access declares the RBAC permission required per CRUD operation.
	// Mirrors EntityConfig.Access (AccessControl): each entry is a
	// permission string (e.g. "posts:write"); a blank/omitted entry
	// leaves that operation un-gated by RBAC. nil means no RBAC at all.
	Access *AccessDeclaration `json:"access,omitempty"`
	// Public mirrors EntityConfig.Public: a deliberate, full opt-out of
	// the framework's secure-by-default session requirement (issue #65)
	// — every operation is reachable by an anonymous caller, matching
	// pre-#65 behaviour. Has no effect when OwnerField or Access is set.
	Public       bool           `json:"public,omitempty"`
	Timestamps   *bool          `json:"timestamps,omitempty"`
	CRUD         *bool          `json:"crud,omitempty"`
	MCP          bool           `json:"mcp,omitempty"`
	CursorField  string         `json:"cursor_field,omitempty"`
	CursorFields []string       `json:"cursor_fields,omitempty"`
	Indices      []Index        `json:"indices,omitempty"`
	Properties   map[string]any `json:"properties,omitempty"`
}

// AccessDeclaration is the JSON/YAML-friendly mirror of AccessControl —
// the per-operation RBAC permissions for a blueprint-declared entity.
// "read" covers both List and Get. The CRUD layer enforces these via
// access.Can against the policy + roles in the request context (403 on
// missing permission), exactly like a Go-declared EntityConfig.Access.
type AccessDeclaration struct {
	Read   string `json:"read,omitempty"`
	Create string `json:"create,omitempty"`
	Update string `json:"update,omitempty"`
	Delete string `json:"delete,omitempty"`
}

// FieldDeclaration is a JSON-friendly schema.Field.
type FieldDeclaration struct {
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

// Config converts a declaration into an EntityConfig.
func (d EntityDeclaration) Config() (EntityConfig, error) {
	if d.Name == "" {
		return EntityConfig{}, fmt.Errorf("name is required")
	}
	fields := make([]schema.Field, 0, len(d.Fields))
	for _, fd := range d.Fields {
		field, err := fd.Field()
		if err != nil {
			return EntityConfig{}, fmt.Errorf("field %q: %w", fd.Name, err)
		}
		fields = append(fields, field)
	}
	cfg := EntityConfig{
		Name:           d.Name,
		Table:          d.Table,
		Fields:         fields,
		Relations:      d.Relations,
		Endpoints:      d.Endpoints,
		SoftDelete:     d.SoftDelete,
		MultiTenant:    d.MultiTenant,
		OwnerField:     d.OwnerField,
		CrossOwnerRead: d.CrossOwnerRead,
		SearchFields:   d.SearchFields,
		Public:         d.Public,
		CRUD:           d.CRUD,
		CursorField:    d.CursorField,
		CursorFields:   d.CursorFields,
		Indices:        d.Indices,
		Properties:     d.Properties,
	}
	if d.Access != nil {
		cfg.Access = AccessControl{
			Read:   d.Access.Read,
			Create: d.Access.Create,
			Update: d.Access.Update,
			Delete: d.Access.Delete,
		}
	}
	if d.Timestamps != nil {
		cfg = cfg.WithTimestamps(*d.Timestamps)
	}
	return cfg, nil
}

// Field converts a JSON field declaration into schema.Field.
func (fd FieldDeclaration) Field() (schema.Field, error) {
	if fd.Name == "" {
		return schema.Field{}, fmt.Errorf("name is required")
	}
	fieldType, err := parseFieldType(fd.Type)
	if err != nil {
		return schema.Field{}, err
	}
	auto, err := parseAutoGenerate(fd.AutoGenerate)
	if err != nil {
		return schema.Field{}, err
	}
	return schema.Field{
		Name:         fd.Name,
		Type:         fieldType,
		Required:     fd.Required,
		Unique:       fd.Unique,
		Default:      fd.Default,
		AutoGenerate: auto,
		ReadOnly:     fd.ReadOnly,
		Hidden:       fd.Hidden,
		Max:          fd.Max,
		Min:          fd.Min,
		Pattern:      fd.Pattern,
		Values:       fd.Values,
		To:           fd.To,
		Many:         fd.Many,
	}, nil
}

func parseFieldType(value string) (schema.FieldType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "string":
		return schema.String, nil
	case "text":
		return schema.Text, nil
	case "int", "integer":
		return schema.Int, nil
	case "float", "number":
		return schema.Float, nil
	case "decimal":
		return schema.Decimal, nil
	case "bool", "boolean":
		return schema.Bool, nil
	case "enum":
		return schema.Enum, nil
	case "uuid":
		return schema.UUID, nil
	case "timestamp", "datetime":
		return schema.Timestamp, nil
	case "date":
		return schema.Date, nil
	case "json":
		return schema.JSON, nil
	case "relation":
		return schema.Relation, nil
	case "image":
		return schema.Image, nil
	case "file":
		return schema.File, nil
	default:
		return schema.String, fmt.Errorf("unknown type %q", value)
	}
}

func parseAutoGenerate(value string) (schema.AutoGenerate, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none":
		return schema.AutoNone, nil
	case "uuid":
		return schema.AutoUUID, nil
	case "timestamp":
		return schema.AutoTimestamp, nil
	case "increment", "auto_increment":
		return schema.AutoIncrement, nil
	default:
		return schema.AutoNone, fmt.Errorf("unknown auto_generate %q", value)
	}
}
