package appspec

import (
	"strings"
)

// Casing converters: the single in-package set. Before this file,
// generate.go and pack.go each carried private converters that disagreed
// on acronyms: toSnakeCase smashed them ("HTTPServer" → "httpserver")
// while camelToSnake split every letter ("HTTPServer" →
// "h_t_t_p_server"). Both directions now share one boundary rule: a
// boundary is inserted before an uppercase run when the previous rune is
// a lowercase letter or digit, or when the run is followed by a
// lowercase letter (the acronym-to-word edge: "HTTPServer" →
// "http_server", "APIKey" → "api_key").

// ToCamelCase converts a separator-delimited name ("_", "-", " ") to
// PascalCase. It never splits inside CamelCase input: there are no
// case-based boundaries, only separator-based ones.
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

// ToCamelJSON converts a field name to lowerCamelCase for JSON tags:
// toCamelCase with the first rune lowered.
func ToCamelJSON(value string) string {
	camel := ToCamelCase(value)
	if camel == "" {
		return ""
	}
	return strings.ToLower(camel[:1]) + camel[1:]
}

// WireKey is the JSON key the server puts a column or relation under in
// its default camel JSONCase (framework/internal/casing.ToCamel, which
// crud.CrudHandler.WireKey applies): split on "_", keep the first word as
// written, capitalize the first letter of each later word. It is
// ToCamelJSON for a name that starts with a lower-case letter, and differs
// for one that does not: Title stays Title and _note becomes Note, where
// ToCamelJSON gives title and note, keys the server never reads. Generated
// code that sends or reads a field's value spells its key with WireKey;
// ToCamelJSON stays for names that are only identifiers (a method, a
// parameter, a JS property).
func WireKey(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		if p := parts[i]; p != "" && p[0] >= 'a' && p[0] <= 'z' {
			parts[i] = string(p[0]-'a'+'A') + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// startsLower reports whether name starts with a lower-case ASCII letter.
func startsLower(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// wireCaseWhy is the reason a field or relation name must start lower:
// the generated typed repo writes through framework.MarshalEntity, which
// maps a JSON key back to its column through snake_case.
const wireCaseWhy = "the server keeps the first letter as written in the JSON key (Title stays Title, _note becomes Note), and the generated typed repo maps that key back to its column through snake_case, which lowers it, so a create or update would drop the value; rename it in snake_case (title, http_code, note)"

// ToSnakeCase converts a name to snake_case for generated file names,
// routes, and endpoints. It handles already-snake input ("blog_posts"),
// CamelCase including acronyms ("HTTPServer" → "http_server",
// "BlogPOST" → "blog_post"), and hyphenated or spaced input, collapsing
// repeated separators.
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
