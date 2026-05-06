package framework

import (
	"strings"
	"unicode"
)

// toCamelCase converts a snake_case string to camelCase.
// e.g. "author_id" → "authorId", "created_at" → "createdAt"
func toCamelCase(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = string(unicode.ToUpper(rune(parts[i][0]))) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// toSnakeCase converts a camelCase string to snake_case.
// e.g. "authorId" → "author_id", "createdAt" → "created_at"
func toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mapToCamelCase converts all snake_case keys in a map to camelCase.
func mapToCamelCase(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[toCamelCase(k)] = v
	}
	return result
}

// mapToSnakeCase converts all camelCase keys in a map to snake_case.
func mapToSnakeCase(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[toSnakeCase(k)] = v
	}
	return result
}
