package style

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// tokenRefRe matches token references like {colors.primary}, {spacing.md}, etc.
var tokenRefRe = regexp.MustCompile(`\{([a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)\}`)

// ResolveToken resolves a token reference like "{spacing.md}" to its value.
// The input should be a token reference in the format "{category.name}".
// Returns the resolved string and true if found, or the original and false.
func (t Theme) ResolveToken(ref string) (string, bool) {
	// Strip braces if present
	inner := ref
	if strings.HasPrefix(ref, "{") && strings.HasSuffix(ref, "}") {
		inner = ref[1 : len(ref)-1]
	}

	parts := strings.SplitN(inner, ".", 2)
	if len(parts) != 2 {
		return ref, false
	}

	category, name := parts[0], parts[1]

	switch category {
	case "colors", "color":
		if v, ok := t.Colors[name]; ok {
			return v, true
		}
	case "spacing":
		if v, ok := t.Spacing[name]; ok {
			return fmt.Sprintf("%d", v), true
		}
	case "radii":
		if v, ok := t.Radii[name]; ok {
			return fmt.Sprintf("%d", v), true
		}
	case "fonts", "font":
		if v, ok := t.Fonts[name]; ok {
			return v, true
		}
	case "breakpoints":
		if v, ok := t.Breakpoints[name]; ok {
			return fmt.Sprintf("%d", v), true
		}
	}

	return ref, false
}

// ResolveAll replaces all {token.path} references in a string with their resolved values.
// Unresolved tokens are left as-is.
func (t Theme) ResolveAll(s string) string {
	return tokenRefRe.ReplaceAllStringFunc(s, func(match string) string {
		resolved, ok := t.ResolveToken(match)
		if !ok {
			return match
		}
		return resolved
	})
}

// ResolveSpacing returns the spacing value as a CSS value string (e.g., "8px").
// Returns "0px" for unknown tokens.
func (t Theme) ResolveSpacing(token string) string {
	if v, ok := t.Spacing[token]; ok {
		return fmt.Sprintf("%dpx", v)
	}
	return "0px"
}

// ResolveColor returns the color value for the given token name.
// Returns empty string for unknown tokens.
func (t Theme) ResolveColor(token string) string {
	if v, ok := t.Colors[token]; ok {
		return v
	}
	return ""
}

// ResolveRadius returns the radius value as CSS (e.g., "8px").
// Returns "0px" for unknown tokens.
func (t Theme) ResolveRadius(token string) string {
	if v, ok := t.Radii[token]; ok {
		return fmt.Sprintf("%dpx", v)
	}
	return "0px"
}

// CSSCustomProperties generates CSS custom property declarations from the theme.
// Output format: ":root { --color-primary: #4F46E5; --spacing-md: 8px; ... }"
func (t Theme) CSSCustomProperties() string {
	var b strings.Builder
	b.WriteString(":root {\n")

	// Colors
	keys := sortedKeys(t.Colors)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --color-%s: %s;\n", k, t.Colors[k])
	}

	// Spacing
	keys = sortedKeysInt(t.Spacing)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --spacing-%s: %dpx;\n", k, t.Spacing[k])
	}

	// Radii
	keys = sortedKeysInt(t.Radii)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --radii-%s: %dpx;\n", k, t.Radii[k])
	}

	// Fonts
	keys = sortedKeys(t.Fonts)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --font-%s: %s;\n", k, t.Fonts[k])
	}

	// Breakpoints
	keys = sortedKeysInt(t.Breakpoints)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --breakpoint-%s: %dpx;\n", k, t.Breakpoints[k])
	}

	b.WriteString("}")
	return b.String()
}

// sortedKeys returns sorted keys from a map[string]string.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeysInt returns sorted keys from a map[string]int.
func sortedKeysInt(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
