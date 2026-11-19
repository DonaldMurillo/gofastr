package schema

// FieldTypeLabel returns the prose name of a field type; wire adds the
// wire-format note an SDK author needs. An unknown type labels as "string".
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
