package entity

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// EntityDeclaration is the grouped JSON/YAML shape of an entity.
type EntityDeclaration struct {
	Name   string             `json:"name"`
	Table  string             `json:"table,omitempty"`
	Fields []FieldDeclaration `json:"fields"`
	// PrimaryKey mirrors EntityConfig.PrimaryKey as primary_key.
	PrimaryKey   []string               `json:"primary_key,omitempty"`
	Relations    []Relation             `json:"relations,omitempty"`
	Endpoints    []Endpoint             `json:"endpoints,omitempty"`
	Scope        *ScopeDeclaration      `json:"scope,omitempty"`
	Pagination   *PaginationDeclaration `json:"pagination,omitempty"`
	Exposure     *ExposureDeclaration   `json:"exposure,omitempty"`
	SearchFields []string               `json:"search_fields,omitempty"`
	Timestamps   *bool                  `json:"timestamps,omitempty"`
	Indices      []Index                `json:"indices,omitempty"`
	Properties   map[string]any         `json:"properties,omitempty"`
	Renames      map[string]string      `json:"renames,omitempty"`
	// Display mirrors EntityConfig.Display; it has no flat shorthand.
	Display *DisplayConfig `json:"display,omitempty"`
	// States mirrors EntityConfig.States.
	States *StatesConfig `json:"states,omitempty"`
}

// UnmarshalJSON accepts grouped declarations and the flat shorthand; a flat key
// and its grouped key must agree. Numbers keep their exact digits.
func (d *EntityDeclaration) UnmarshalJSON(data []byte) error {
	type grouped EntityDeclaration
	var decoded grouped
	if err := jsonexact.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	scopeJSON, err := declarationGroupJSON(root, "scope")
	if err != nil {
		return err
	}
	paginationJSON, err := declarationGroupJSON(root, "pagination")
	if err != nil {
		return err
	}
	exposureJSON, err := declarationGroupJSON(root, "exposure")
	if err != nil {
		return err
	}

	*d = EntityDeclaration(decoded)
	if d.Scope != nil || declarationHasAny(root, "soft_delete", "multi_tenant", "tenant_field", "owner_field", "cross_owner_read") {
		if d.Scope == nil {
			d.Scope = &ScopeDeclaration{}
		}
		if err := mergeDeclarationFields(d.Name, root, scopeJSON,
			declFieldOf("soft_delete", &d.Scope.SoftDelete),
			declFieldOf("multi_tenant", &d.Scope.MultiTenant),
			declFieldOf("tenant_field", &d.Scope.TenantField),
			declFieldOf("owner_field", &d.Scope.OwnerField),
			declFieldOf("cross_owner_read", &d.Scope.CrossOwnerRead),
		); err != nil {
			return err
		}
	}
	if d.Pagination != nil || declarationHasAny(root, "cursor_field", "cursor_fields", "max_list_limit") {
		if d.Pagination == nil {
			d.Pagination = &PaginationDeclaration{}
		}
		if err := mergeDeclarationFields(d.Name, root, paginationJSON,
			declFieldOf("cursor_field", &d.Pagination.CursorField),
			declFieldOf("cursor_fields", &d.Pagination.CursorFields),
			declFieldOf("max_list_limit", &d.Pagination.MaxListLimit),
		); err != nil {
			return err
		}
	}
	if d.Exposure != nil || declarationHasAny(root, "crud", "mcp", "public", "access", "read_scope") {
		if d.Exposure == nil {
			d.Exposure = &ExposureDeclaration{}
		}
		if err := mergeDeclarationFields(d.Name, root, exposureJSON,
			declFieldOf("crud", &d.Exposure.CRUD),
			declFieldOf("mcp", &d.Exposure.MCP),
			declFieldOf("public", &d.Exposure.Public),
			declFieldOf("access", &d.Exposure.Access),
			declFieldOf("read_scope", &d.Exposure.ReadScope),
		); err != nil {
			return err
		}
	}
	return nil
}

// declField merges one JSON key from its group into a declaration field.
type declField func(name string, root, group map[string]json.RawMessage) error

// declFieldOf binds key to dst, keeping dst's concrete type so the merge
// decodes into it directly.
func declFieldOf[T any](key string, dst *T) declField {
	return func(name string, root, group map[string]json.RawMessage) error {
		return mergeDeclarationJSONField(name, root, group, key, dst)
	}
}

// mergeDeclarationFields runs each field's merge, stopping at the first error.
func mergeDeclarationFields(name string, root, group map[string]json.RawMessage, fields ...declField) error {
	for _, f := range fields {
		if err := f(name, root, group); err != nil {
			return err
		}
	}
	return nil
}

func declarationGroupJSON(root map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, ok := root[key]
	if !ok {
		return nil, nil
	}
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, fmt.Errorf("%s must be an object: %w", key, err)
	}
	return group, nil
}

func declarationHasAny(root map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if _, ok := root[key]; ok {
			return true
		}
	}
	return false
}

func mergeDeclarationJSONField[T any](
	entityName string,
	root, group map[string]json.RawMessage,
	key string,
	target *T,
) error {
	flatRaw, flatSet := root[key]
	if !flatSet {
		return nil
	}
	var flat T
	//gofastr:allow(floatnum) T is a scope/pagination/exposure key type (bool, *bool, string, []string, int, *AccessDeclaration, *ReadScopeDeclaration): none holds an interface value
	if err := json.Unmarshal(flatRaw, &flat); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if groupedRaw, groupedSet := group[key]; groupedSet {
		var grouped T
		//gofastr:allow(floatnum) same T as the flat decode above: no interface value to round
		if err := json.Unmarshal(groupedRaw, &grouped); err != nil {
			return fmt.Errorf("%s.%s: %w", declarationGroupForKey(key), key, err)
		}
		if !reflect.DeepEqual(flat, grouped) {
			return fmt.Errorf(
				"entity %q: conflicting declaration values for %s and %s.%s (%v != %v)",
				entityName, key, declarationGroupForKey(key), key, flat, grouped,
			)
		}
	}
	*target = flat
	return nil
}

func declarationGroupForKey(key string) string {
	switch key {
	case "soft_delete", "multi_tenant", "tenant_field", "owner_field", "cross_owner_read":
		return "scope"
	case "cursor_field", "cursor_fields", "max_list_limit":
		return "pagination"
	default:
		return "exposure"
	}
}

// ScopeDeclaration is the JSON/YAML shape of ScopeConfig.
type ScopeDeclaration struct {
	SoftDelete     bool   `json:"soft_delete,omitempty"`
	MultiTenant    bool   `json:"multi_tenant,omitempty"`
	TenantField    string `json:"tenant_field,omitempty"`
	OwnerField     string `json:"owner_field,omitempty"`
	CrossOwnerRead string `json:"cross_owner_read,omitempty"`
}

// PaginationDeclaration is the JSON/YAML shape of PaginationConfig.
type PaginationDeclaration struct {
	CursorField  string   `json:"cursor_field,omitempty"`
	CursorFields []string `json:"cursor_fields,omitempty"`
	MaxListLimit int      `json:"max_list_limit,omitempty"`
}

// ExposureDeclaration is the JSON/YAML shape of ExposureConfig.
type ExposureDeclaration struct {
	CRUD      *bool                 `json:"crud,omitempty"`
	MCP       bool                  `json:"mcp,omitempty"`
	Public    bool                  `json:"public,omitempty"`
	Access    *AccessDeclaration    `json:"access,omitempty"`
	ReadScope *ReadScopeDeclaration `json:"read_scope,omitempty"`
}

// AccessDeclaration is the JSON/YAML shape of AccessControl.
type AccessDeclaration struct {
	Read   string `json:"read,omitempty"`
	Create string `json:"create,omitempty"`
	Update string `json:"update,omitempty"`
	Delete string `json:"delete,omitempty"`
}

// ReadScopeDeclaration is the JSON/YAML shape of ReadScopeConfig.
type ReadScopeDeclaration struct {
	Filter       []RowPredicateDeclaration `json:"filter,omitempty"`
	Unrestricted string                    `json:"unrestricted,omitempty"`
}

// RowPredicateDeclaration is the JSON/YAML shape of RowPredicate.
type RowPredicateDeclaration struct {
	Field  string   `json:"field"`
	Op     string   `json:"op,omitempty"`
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`
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
	NoQuery      bool     `json:"no_query,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	Values       []string `json:"values,omitempty"`
	To           string   `json:"to,omitempty"`
	Many         bool     `json:"many,omitempty"`
}

// Config converts d into an EntityConfig sharing no slices, checking a declared primary key.
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

	scope := &ScopeConfig{}
	if d.Scope != nil {
		*scope = ScopeConfig{
			SoftDelete: d.Scope.SoftDelete, MultiTenant: d.Scope.MultiTenant,
			TenantField: d.Scope.TenantField, OwnerField: d.Scope.OwnerField,
			CrossOwnerRead: d.Scope.CrossOwnerRead,
		}
	}
	pagination := &PaginationConfig{}
	if d.Pagination != nil {
		*pagination = PaginationConfig{
			CursorField:  d.Pagination.CursorField,
			CursorFields: append([]string(nil), d.Pagination.CursorFields...),
			MaxListLimit: d.Pagination.MaxListLimit,
		}
	}
	exposure := &ExposureConfig{}
	if d.Exposure != nil {
		exposure.CRUD = d.Exposure.CRUD
		exposure.MCP = d.Exposure.MCP
		exposure.Public = d.Exposure.Public
		if d.Exposure.Access != nil {
			exposure.Access = AccessControl{
				Read: d.Exposure.Access.Read, Create: d.Exposure.Access.Create,
				Update: d.Exposure.Access.Update, Delete: d.Exposure.Access.Delete,
			}
		}
		if d.Exposure.ReadScope != nil {
			rs := &ReadScopeConfig{Unrestricted: d.Exposure.ReadScope.Unrestricted}
			rs.Filter = append([]RowPredicate(nil), readScopePredicates(d.Exposure.ReadScope.Filter)...)
			exposure.ReadScope = rs
		}
	}
	var primaryKey []string
	if d.PrimaryKey != nil {
		primaryKey = append([]string{}, d.PrimaryKey...)
	}
	cfg := EntityConfig{
		Name:         d.Name,
		Table:        d.Table,
		Fields:       fields,
		PrimaryKey:   primaryKey,
		Relations:    d.Relations,
		Endpoints:    d.Endpoints,
		Scope:        scope,
		Pagination:   pagination,
		Exposure:     exposure,
		SearchFields: d.SearchFields,
		Timestamps:   d.Timestamps,
		Indices:      d.Indices,
		Properties:   d.Properties,
		Renames:      d.Renames,
		Display:      d.Display,
		States:       d.States,
	}
	if primaryKey != nil {
		if err := cfg.keyColumnsErr(); err != nil {
			return EntityConfig{}, err
		}
		if err := cfg.keyServedErr(); err != nil {
			return EntityConfig{}, err
		}
	}
	return cfg, nil
}

func readScopePredicates(preds []RowPredicateDeclaration) []RowPredicate {
	if len(preds) == 0 {
		return nil
	}
	out := make([]RowPredicate, 0, len(preds))
	for _, p := range preds {
		out = append(out, RowPredicate{
			Field: p.Field, Op: p.Op, Value: p.Value,
			Values: append([]string(nil), p.Values...),
		})
	}
	return out
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
		NoQuery:      fd.NoQuery,
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
