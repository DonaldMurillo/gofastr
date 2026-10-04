package schema

// FieldTypeLabel returns the human-readable label for a field type.
// It is the one table behind both renderings that name field types in
// prose: the llm.md entity reference (framework/crud) and the SDK
// reference screens (framework/sdkdocs), which used to carry two
// copies that could drift. wire selects the audience: true appends the
// wire-format notes an SDK author needs (decimal arrives as a string,
// image and file fields arrive as URLs); false returns the bare name
// the llm.md table uses. An unknown FieldType labels as "string",
// matching what both tables previously answered.
func FieldTypeLabel(t FieldType, wire bool) string {
	switch t {
	case String:
		return "string"
	case Text:
		return "text"
	case Int:
		return "integer"
	case Float:
		return "float"
	case Decimal:
		if wire {
			return "decimal (string on the wire)"
		}
		return "decimal"
	case Bool:
		return "boolean"
	case Enum:
		return "enum"
	case UUID:
		return "uuid"
	case Timestamp:
		return "timestamp"
	case Date:
		return "date"
	case JSON:
		return "json"
	case Relation:
		return "relation"
	case Image:
		if wire {
			return "image (url)"
		}
		return "image"
	case File:
		if wire {
			return "file (url)"
		}
		return "file"
	default:
		return "string"
	}
}
