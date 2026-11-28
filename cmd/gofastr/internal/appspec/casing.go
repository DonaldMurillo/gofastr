package appspec

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/casing"
)

// ToCamelCase joins a name's _, - or space separated parts into PascalCase, never splitting inside a part.
func ToCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}

// ToCamelJSON is ToCamelCase with its first letter lowered.
func ToCamelJSON(value string) string {
	camel := ToCamelCase(value)
	if camel == "" {
		return ""
	}
	return strings.ToLower(camel[:1]) + camel[1:]
}

// WireKey returns the JSON key crud.CrudHandler.WireKey sends for a column or relation.
func WireKey(name string) string {
	return casing.ToCamel(name)
}

// ToSnakeCase converts a name to snake_case, splitting CamelCase and acronyms (HTTPServer to http_server).
func ToSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') ||
				(prev >= 'A' && prev <= 'Z' && nextLower) {
				b.WriteByte('_')
			}
		}
		switch {
		case r == '-' || r == ' ':
			b.WriteByte('_')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 'a' - 'A')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}
