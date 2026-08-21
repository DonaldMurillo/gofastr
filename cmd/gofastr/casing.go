package main

import "strings"

// Casing converters: the single in-package set. Before this file,
// generate.go and pack.go each carried private converters that disagreed
// on acronyms: toSnakeCase smashed them ("HTTPServer" → "httpserver")
// while camelToSnake split every letter ("HTTPServer" →
// "h_t_t_p_server"). Both directions now share one boundary rule: a
// boundary is inserted before an uppercase run when the previous rune is
// a lowercase letter or digit, or when the run is followed by a
// lowercase letter (the acronym-to-word edge: "HTTPServer" →
// "http_server", "APIKey" → "api_key").

// toCamelCase converts a separator-delimited name ("_", "-", " ") to
// PascalCase. It never splits inside CamelCase input: there are no
// case-based boundaries, only separator-based ones.
func toCamelCase(s string) string {
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

// toCamelJSON converts a field name to lowerCamelCase for JSON tags:
// toCamelCase with the first rune lowered.
func toCamelJSON(value string) string {
	camel := toCamelCase(value)
	if camel == "" {
		return ""
	}
	return strings.ToLower(camel[:1]) + camel[1:]
}

// toSnakeCase converts a name to snake_case for generated file names,
// routes, and endpoints. It handles already-snake input ("blog_posts"),
// CamelCase including acronyms ("HTTPServer" → "http_server",
// "BlogPOST" → "blog_post"), and hyphenated or spaced input, collapsing
// repeated separators.
func toSnakeCase(s string) string {
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

// toKebabCase converts a name to kebab-case. It is toSnakeCase with
// hyphens instead of underscores, so the two share one boundary rule.
func toKebabCase(s string) string {
	return strings.ReplaceAll(toSnakeCase(s), "_", "-")
}

// lowerFirst lowers the first rune of s without touching the rest.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
