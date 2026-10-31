package openapi

import (
	"math"
	"slices"

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

	case schema.Text:
		prop["type"] = "string"
		prop["format"] = "textarea"
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
		// Accept any JSON value, no type constraint.

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

// Nullable widens prop to accept null as well: a type T becomes
// [T, "null"] (OpenAPI 3.1), and an enum gains null, which it must
// list for a null to pass. A prop with no type, such as a JSON
// field's, accepts null already.
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

// FieldsToSchema converts a slice of Fields into a JSON Schema object
// suitable for use as an OpenAPI schema.
func FieldsToSchema(fields []schema.Field) map[string]any {
	properties := make(map[string]any, len(fields))
	required := make([]string, 0)

	for _, f := range fields {
		// A Hidden field is never part of a published schema. The CRUD
		// generators filter through VisibleFields before they get here,
		// but a custom entity.Endpoint hands its InputSchema straight in
		// -- and entity.Endpoint's own docs invite reusing the entity's
		// field slice for it. That published the hidden column's name to
		// every caller allowed to see the tool at all, and listed it as
		// required, which is worse: it tells the caller to send a value
		// for a column it is not supposed to know exists. Filtering here
		// means no future caller of this function can reintroduce it.
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
