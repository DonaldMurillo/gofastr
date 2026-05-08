package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
)

// EntityDeclaration is the JSON shape accepted by EntityFromFile and the CLI
// code generator. It mirrors EntityConfig while keeping field types readable.
type EntityDeclaration struct {
	Name        string             `json:"name"`
	Table       string             `json:"table,omitempty"`
	Fields      []FieldDeclaration `json:"fields"`
	Relations   []Relation         `json:"relations,omitempty"`
	Endpoints   []Endpoint         `json:"endpoints,omitempty"`
	SoftDelete  bool               `json:"soft_delete,omitempty"`
	MultiTenant bool               `json:"multi_tenant,omitempty"`
	Timestamps  *bool              `json:"timestamps,omitempty"`
	CRUD        *bool              `json:"crud,omitempty"`
	MCP         bool               `json:"mcp,omitempty"`
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

// LoadEntityDeclaration reads and validates one entity declaration file.
func LoadEntityDeclaration(path string) (EntityDeclaration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EntityDeclaration{}, err
	}
	var decl EntityDeclaration
	if err := json.Unmarshal(data, &decl); err != nil {
		return EntityDeclaration{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if decl.Name == "" {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		decl.Name = base
	}
	for _, endpoint := range decl.Endpoints {
		if endpoint.MCP {
			return EntityDeclaration{}, fmt.Errorf("invalid %s: endpoint %q has mcp=true but JSON declarations cannot supply Go handlers — wire endpoints in code via app.Entity(...)", path, endpoint.Path)
		}
	}
	if _, err := decl.Config(); err != nil {
		return EntityDeclaration{}, fmt.Errorf("invalid %s: %w", path, err)
	}
	return decl, nil
}

// LoadEntityDeclarations reads all *.json declarations in dir in stable order.
func LoadEntityDeclarations(dir string) ([]EntityDeclaration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)

	decls := make([]EntityDeclaration, 0, len(paths))
	for _, path := range paths {
		decl, err := LoadEntityDeclaration(path)
		if err != nil {
			return nil, err
		}
		decls = append(decls, decl)
	}
	return decls, nil
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
		Name:        d.Name,
		Table:       d.Table,
		Fields:      fields,
		Relations:   d.Relations,
		Endpoints:   d.Endpoints,
		SoftDelete:  d.SoftDelete,
		MultiTenant: d.MultiTenant,
		CRUD:        d.CRUD,
		MCP:         d.MCP,
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
