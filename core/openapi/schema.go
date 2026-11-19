package openapi

import (
	"math"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// FieldToSchema returns the OpenAPI 3.1 schema object for one field.
func FieldToSchema(field schema.Field) map[string]any {
	prop := map[string]any{}

	switch field.Type {
	case schema.String, schema.Text:
		prop["type"] = "string"
		if field.Type == schema.Text {
			prop["format"] = "textarea"
		}
		if field.Min != nil {
			if n, ok := schema.LengthBound(*field.Min, math.Ceil); ok {
				prop["minLength"] = n
			}
		}
		if field.Max != nil {
			if n, ok := schema.LengthBound(*field.Max, math.Floor); ok {
				prop["maxLength"] = n
			}
		}
		if field.Pattern != "" {
			prop["pattern"] = field.Pattern
		}

	case schema.Int:
		prop["type"] = "integer"
		if field.Min != nil {
			prop["minimum"] = *field.Min
		}
		if field.Max != nil {
			prop["maximum"] = *field.Max
		}

	case schema.Float:
		prop["type"] = "number"
		if field.Min != nil {
			prop["minimum"] = *field.Min
		}
		if field.Max != nil {
			prop["maximum"] = *field.Max
		}

	case schema.Decimal:
		prop["type"] = "string"
		prop["format"] = "decimal"

	case schema.Bool:
		prop["type"] = "boolean"

	case schema.Enum:
		prop["type"] = "string"
		if len(field.Values) > 0 {
			prop["enum"] = slices.Clone(field.Values)
		}

	case schema.UUID:
		prop["type"] = "string"
		prop["format"] = "uuid"

	case schema.Timestamp:
		prop["type"] = "string"
		prop["format"] = "date-time"

	case schema.Date:
		prop["type"] = "string"
		prop["format"] = "date"

	case schema.JSON:

	case schema.Relation:
		prop["type"] = "string"
		prop["format"] = "uuid"
		if field.To != "" {
			prop["x-relation"] = field.To
		}
		if field.Many {
			prop["x-many"] = true
		}

	case schema.Image:
		prop["type"] = "string"
		prop["format"] = "uri"

	case schema.File:
		prop["type"] = "string"
		prop["format"] = "binary"
	}

	if field.Default != nil {
		prop["default"] = field.Default
	}

	if field.ReadOnly || field.AutoGenerate != schema.AutoNone {
		prop["readOnly"] = true
	}

	return prop
}

// Nullable widens prop to accept null: a type T becomes [T, "null"] and
// an enum gains null.
func Nullable(prop map[string]any) {
	if t, ok := prop["type"].(string); ok {
		prop["type"] = []any{t, "null"}
	}
	if values, ok := prop["enum"].([]string); ok {
		enum := make([]any, 0, len(values)+1)
		for _, v := range values {
			enum = append(enum, v)
		}
		prop["enum"] = append(enum, nil)
	}
}

// FieldsToSchema returns the object schema for fields, keyed by wire name,
// leaving out every Hidden field.
func FieldsToSchema(fields []schema.Field) map[string]any {
	properties := make(map[string]any, len(fields))
	required := make([]string, 0)

	for _, f := range fields {
		if f.Hidden {
			continue
		}
		name := f.Name
		if f.WireName != "" {
			name = f.WireName
		}
		properties[name] = FieldToSchema(f)
		if f.Required {
			required = append(required, name)
		}
	}

	obj := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		obj["required"] = required
	}

	return obj
}
