package c

import "c/schema"

// Partial coverage with no default: a new type falls through silently.
func ddl(t schema.FieldType) string {
	switch t { // want `switch on FieldType handles 2 of 3 constants and has no default: Bool would fall through silently`
	case schema.String:
		return "TEXT"
	case schema.Int:
		return "INTEGER"
	}
	return ""
}

// A default decides what an unknown type means. Fine.
func ddlDefaulted(t schema.FieldType) string {
	switch t {
	case schema.String:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Full coverage. Fine.
func ddlComplete(t schema.FieldType) string {
	switch t {
	case schema.String:
		return "TEXT"
	case schema.Int:
		return "INTEGER"
	case schema.Bool:
		return "BOOLEAN"
	}
	return ""
}

// Several constants in one case still count as covered.
func grouped(t schema.FieldType) bool {
	switch t {
	case schema.String, schema.Int:
		return true
	case schema.Bool:
		return false
	}
	return false
}

// A switch on something else is not this check's business.
func other(s string) bool {
	switch s {
	case "a":
		return true
	}
	return false
}
