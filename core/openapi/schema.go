package openapi

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// FieldToSchema converts a single schema.Field into an OpenAPI-compatible
// JSON Schema object.
func FieldToSchema(field schema.Field) map[string]any {
	prop := map[string]any{}

	switch field.Type {
	case schema.String:
		prop["type"] = "string"
		if field.Min != nil {
			prop["minLength"] = int(*field.Min)
		}
		if field.Max != nil {
			prop["maxLength"] = int(*field.Max)
		}
		if field.Pattern != "" {
			prop["pattern"] = field.Pattern
		}

	case schema.Text:
		prop["type"] = "string"
		prop["format"] = "textarea"
		if field.Min != nil {
			prop["minLength"] = int(*field.Min)
		}
		if field.Max != nil {
			prop["maxLength"] = int(*field.Max)
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
			prop["enum"] = field.Values
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
		// Accept any JSON value — no type constraint.

	case schema.Relation:
		if field.Many {
			prop["type"] = "array"
			prop["items"] = map[string]any{
				"type":   "string",
				"format": "uuid",
			}
		} else {
			prop["type"] = "string"
			prop["format"] = "uuid"
		}
		if field.To != "" {
			prop["x-relation"] = field.To
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

	// Mirror the runtime contract into the spec: ReadOnly fields and any
	// AutoGenerate variant are server-managed; clients must not be told
	// they can write them via the generated SDK.
	if field.ReadOnly || field.AutoGenerate != schema.AutoNone {
		prop["readOnly"] = true
	}

	return prop
}

// FieldsToSchema converts a slice of Fields into a JSON Schema object
// suitable for use as an OpenAPI schema.
func FieldsToSchema(fields []schema.Field) map[string]any {
	properties := make(map[string]any, len(fields))
	required := make([]string, 0)

	for _, f := range fields {
		properties[f.Name] = FieldToSchema(f)
		if f.Required {
			required = append(required, f.Name)
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
