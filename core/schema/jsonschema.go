package schema

import (
	"math"
	"slices"
)

// JSONSchema generates a JSON Schema draft-07 object from a slice of Field
// definitions. The returned map can be marshaled directly to JSON.
func JSONSchema(fields []Field) map[string]any {
	properties := make(map[string]any, len(fields))
	required := make([]string, 0)

	for _, f := range fields {
		prop := fieldToJSONSchema(f)
		name := f.Name
		if f.WireName != "" {
			name = f.WireName
		}
		properties[name] = prop
		if f.Required {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
	}

	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

func fieldToJSONSchema(f Field) map[string]any {
	prop := map[string]any{}

	switch f.Type {
	case String:
		prop["type"] = "string"
		if f.Min != nil {
			if n, ok := LengthBound(*f.Min, math.Ceil); ok {
				prop["minLength"] = n
			}
		}
		if f.Max != nil {
			if n, ok := LengthBound(*f.Max, math.Floor); ok {
				prop["maxLength"] = n
			}
		}
		if f.Pattern != "" {
			prop["pattern"] = f.Pattern
		}

	case Text:
		prop["type"] = "string"
		prop["format"] = "textarea"
		if f.Min != nil {
			if n, ok := LengthBound(*f.Min, math.Ceil); ok {
				prop["minLength"] = n
			}
		}
		if f.Max != nil {
			if n, ok := LengthBound(*f.Max, math.Floor); ok {
				prop["maxLength"] = n
			}
		}
		if f.Pattern != "" {
			prop["pattern"] = f.Pattern
		}

	case Int:
		prop["type"] = "integer"
		if f.Min != nil {
			prop["minimum"] = *f.Min
		}
		if f.Max != nil {
			prop["maximum"] = *f.Max
		}

	case Float:
		prop["type"] = "number"
		if f.Min != nil {
			prop["minimum"] = *f.Min
		}
		if f.Max != nil {
			prop["maximum"] = *f.Max
		}

	case Decimal:
		prop["type"] = "string"
		prop["format"] = "decimal"
		if f.Min != nil {
			prop["minimum"] = *f.Min
		}
		if f.Max != nil {
			prop["maximum"] = *f.Max
		}

	case Bool:
		prop["type"] = "boolean"

	case Enum:
		prop["type"] = "string"
		prop["enum"] = slices.Clone(f.Values)

	case UUID:
		prop["type"] = "string"
		prop["format"] = "uuid"
		prop["pattern"] = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"

	case Timestamp:
		prop["type"] = "string"
		prop["format"] = "date-time"

	case Date:
		prop["type"] = "string"
		prop["format"] = "date"

	case JSON:
		// JSON fields accept any valid JSON value
		// no "type" constraint, any JSON value is valid

	case Relation:
		prop["type"] = "string"
		prop["format"] = "relation"
		if f.To != "" {
			prop["x-relation"] = f.To
		}
		if f.Many {
			prop["x-many"] = true
		}

	case Image:
		prop["type"] = "string"
		prop["format"] = "uri"

	case File:
		prop["type"] = "string"
		prop["format"] = "uri"
	}

	if f.Default != nil {
		prop["default"] = f.Default
	}

	return prop
}
