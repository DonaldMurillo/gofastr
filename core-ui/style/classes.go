package style

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Classes is a map of class name to whether it should be included.
// Generates the "class" attribute value from all true keys.
type Classes map[string]bool

// ToAttr converts Classes to an Attrs map with the "class" key.
// If no classes are active, returns an empty map.
func (c Classes) ToAttr() map[string]string {
	s := c.String()
	if s == "" {
		return map[string]string{}
	}
	return map[string]string{"class": s}
}

// String returns the space-separated class list from all true keys, sorted for determinism.
func (c Classes) String() string {
	active := make([]string, 0, len(c))
	for k, v := range c {
		if v {
			active = append(active, k)
		}
	}
	sort.Strings(active)
	return strings.Join(active, " ")
}

// Use returns an Attrs (map[string]string) with a "class" attribute set to the
// component style name. This is resolved at render time using the theme's
// ComponentStyles map. The generated class name follows the pattern "comp-{name}".
//
// Example: Use("card") → Attrs{"class": "comp-card"}
func Use(name string) map[string]string {
	return map[string]string{"class": "comp-" + name}
}

// UseWith merges a component style class with additional classes.
// Example: UseWith("card", Classes{"highlighted": true}) → Attrs{"class": "comp-card highlighted"}
func UseWith(name string, extra Classes) map[string]string {
	cls := "comp-" + name
	for c, include := range extra {
		if include {
			cls += " " + c
		}
	}
	return map[string]string{"class": cls}
}

// ComponentCSS generates the CSS rules for a named component style.
// It resolves all token references in the style definition.
// Returns empty string if the component style is not defined.
func (t Theme) ComponentCSS(name string) string {
	def, ok := t.Components[name]
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, ".comp-%s {\n", name)
	props := make([]string, 0, len(def))
	for prop := range def {
		props = append(props, prop)
	}
	sort.Strings(props)
	for _, prop := range props {
		fmt.Fprintf(&b, "  %s: %s;\n", prop, t.ResolveAll(def[prop]))
	}
	b.WriteString("}")
	return b.String()
}

// AllComponentCSS generates CSS for all defined component styles.
// Component names are emitted in sorted order so output is byte-stable
// across process restarts — load-bearing for content-addressed CSS URLs
// (see core-ui/registry).
func (t Theme) AllComponentCSS() string {
	names := make([]string, 0, len(t.Components))
	for name := range t.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, t.ComponentCSS(name))
	}
	return strings.Join(parts, "\n\n")
}

// UtilityClass generates a utility class name from a property and token.
// For spacing tokens: UtilityClass("p", "md") → "p-8" (resolved from spacing.md = 8).
// For color tokens: the raw token value is used.
func (t Theme) UtilityClass(property, token string) string {
	// Check if this is a spacing-based property
	if isSpacingProperty(property) {
		if v, ok := t.Spacing[token]; ok {
			return fmt.Sprintf("%s-%d", property, v)
		}
	}
	// Check if this is a radius property
	if isRadiusProperty(property) {
		if v, ok := t.Radii[token]; ok {
			return fmt.Sprintf("%s-%d", property, v)
		}
	}
	// Fallback: use raw token
	return fmt.Sprintf("%s-%s", property, token)
}

// GenerateUtilityCSS generates CSS rules for a set of utility class names using the theme.
// Each class name is parsed and mapped to its CSS properties.
func GenerateUtilityCSS(classes []string, theme Theme) string {
	var b strings.Builder

	for _, class := range classes {
		props := resolveUtilityClass(class, theme)
		if props != "" {
			fmt.Fprintf(&b, ".%s { %s }\n", class, props)
		}
	}

	return b.String()
}

// resolveUtilityClass maps a utility class name to its CSS properties string.
func resolveUtilityClass(class string, theme Theme) string {
	// Display utilities
	switch class {
	case "flex":
		return "display: flex;"
	case "grid":
		return "display: grid;"
	case "block":
		return "display: block;"
	case "inline":
		return "display: inline;"
	case "hidden":
		return "display: none;"
	}

	// Flex direction
	switch class {
	case "flex-row":
		return "flex-direction: row;"
	case "flex-col":
		return "flex-direction: column;"
	case "flex-wrap":
		return "flex-wrap: wrap;"
	}

	// Overflow
	if strings.HasPrefix(class, "overflow-") {
		val := strings.TrimPrefix(class, "overflow-")
		switch val {
		case "auto", "hidden", "scroll", "visible":
			return fmt.Sprintf("overflow: %s;", val)
		}
	}

	// Align items
	if strings.HasPrefix(class, "items-") {
		val := strings.TrimPrefix(class, "items-")
		cssVal := alignValue(val)
		if cssVal != "" {
			return fmt.Sprintf("align-items: %s;", cssVal)
		}
	}

	// Justify content
	if strings.HasPrefix(class, "justify-") {
		val := strings.TrimPrefix(class, "justify-")
		cssVal := justifyValue(val)
		if cssVal != "" {
			return fmt.Sprintf("justify-content: %s;", cssVal)
		}
	}

	// Width / Height
	if class == "w-full" {
		return "width: 100%;"
	}
	if class == "h-full" {
		return "height: 100%;"
	}

	// Width with token
	if strings.HasPrefix(class, "w-") {
		token := strings.TrimPrefix(class, "w-")
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("width: %dpx;", v)
		}
	}

	// Height with token
	if strings.HasPrefix(class, "h-") {
		token := strings.TrimPrefix(class, "h-")
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("height: %dpx;", v)
		}
	}

	// Padding utilities
	if props := paddingCSS(class, theme); props != "" {
		return props
	}

	// Margin utilities
	if props := marginCSS(class, theme); props != "" {
		return props
	}

	// Gap utilities
	if props := gapCSS(class, theme); props != "" {
		return props
	}

	// Font size utilities
	if strings.HasPrefix(class, "text-") {
		token := strings.TrimPrefix(class, "text-")
		if size, ok := fontSizeMap()[token]; ok {
			return fmt.Sprintf("font-size: %s;", size)
		}
		// Otherwise treat as color
		if color := theme.ResolveColor(token); color != "" {
			return fmt.Sprintf("color: %s;", color)
		}
	}

	// Background color
	if strings.HasPrefix(class, "bg-") {
		token := strings.TrimPrefix(class, "bg-")
		if color := theme.ResolveColor(token); color != "" {
			return fmt.Sprintf("background-color: %s;", color)
		}
	}

	// Border color
	if strings.HasPrefix(class, "border-") {
		token := strings.TrimPrefix(class, "border-")
		// Check for color
		if color := theme.ResolveColor(token); color != "" {
			return fmt.Sprintf("border-color: %s;", color)
		}
		// Check for width (spacing token)
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("border-width: %dpx;", v)
		}
	}

	// Plain border
	if class == "border" {
		return "border-width: 1px;"
	}

	// Border radius
	if strings.HasPrefix(class, "rounded-") {
		token := strings.TrimPrefix(class, "rounded-")
		if v, ok := theme.Radii[token]; ok {
			return fmt.Sprintf("border-radius: %dpx;", v)
		}
	}

	// Font weight
	if strings.HasPrefix(class, "font-") {
		weight := strings.TrimPrefix(class, "font-")
		if w, ok := fontWeightMap()[weight]; ok {
			return fmt.Sprintf("font-weight: %s;", w)
		}
	}

	return ""
}

// paddingCSS resolves padding utility classes.
func paddingCSS(class string, theme Theme) string {
	prefixes := map[string]string{
		"p-":  "padding",
		"px-": "padding-left",
		"py-": "padding-top",
		"pt-": "padding-top",
		"pr-": "padding-right",
		"pb-": "padding-bottom",
		"pl-": "padding-left",
	}

	for prefix, prop := range prefixes {
		if strings.HasPrefix(class, prefix) {
			token := strings.TrimPrefix(class, prefix)
			v, ok := theme.Spacing[token]
			if !ok {
				// Try parsing as raw number
				if n, err := strconv.Atoi(token); err == nil {
					v = n
				} else {
					return ""
				}
			}
			if prefix == "px-" {
				return fmt.Sprintf("padding-left: %dpx; padding-right: %dpx;", v, v)
			}
			if prefix == "py-" {
				return fmt.Sprintf("padding-top: %dpx; padding-bottom: %dpx;", v, v)
			}
			return fmt.Sprintf("%s: %dpx;", prop, v)
		}
	}
	return ""
}

// marginCSS resolves margin utility classes.
func marginCSS(class string, theme Theme) string {
	prefixes := map[string]string{
		"m-":  "margin",
		"mx-": "margin-left",
		"my-": "margin-top",
		"mt-": "margin-top",
		"mr-": "margin-right",
		"mb-": "margin-bottom",
		"ml-": "margin-left",
	}

	for prefix, prop := range prefixes {
		if strings.HasPrefix(class, prefix) {
			token := strings.TrimPrefix(class, prefix)
			v, ok := theme.Spacing[token]
			if !ok {
				if n, err := strconv.Atoi(token); err == nil {
					v = n
				} else {
					return ""
				}
			}
			if prefix == "mx-" {
				return fmt.Sprintf("margin-left: %dpx; margin-right: %dpx;", v, v)
			}
			if prefix == "my-" {
				return fmt.Sprintf("margin-top: %dpx; margin-bottom: %dpx;", v, v)
			}
			return fmt.Sprintf("%s: %dpx;", prop, v)
		}
	}
	return ""
}

// gapCSS resolves gap utility classes.
func gapCSS(class string, theme Theme) string {
	if strings.HasPrefix(class, "gap-x-") {
		token := strings.TrimPrefix(class, "gap-x-")
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("column-gap: %dpx;", v)
		}
	}
	if strings.HasPrefix(class, "gap-y-") {
		token := strings.TrimPrefix(class, "gap-y-")
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("row-gap: %dpx;", v)
		}
	}
	if strings.HasPrefix(class, "gap-") {
		token := strings.TrimPrefix(class, "gap-")
		if v, ok := theme.Spacing[token]; ok {
			return fmt.Sprintf("gap: %dpx;", v)
		}
	}
	return ""
}

// fontSizeMap returns a mapping of font size tokens to CSS values.
func fontSizeMap() map[string]string {
	return map[string]string{
		"xs":   "0.75rem",
		"sm":   "0.875rem",
		"base": "1rem",
		"lg":   "1.125rem",
		"xl":   "1.25rem",
		"2xl":  "1.5rem",
		"3xl":  "1.875rem",
	}
}

// fontWeightMap returns a mapping of font weight tokens to CSS values.
func fontWeightMap() map[string]string {
	return map[string]string{
		"normal":   "400",
		"medium":   "500",
		"semibold": "600",
		"bold":     "700",
	}
}

// alignValue maps shorthand alignment values to CSS.
func alignValue(val string) string {
	m := map[string]string{
		"start":   "flex-start",
		"center":  "center",
		"end":     "flex-end",
		"stretch": "stretch",
	}
	return m[val]
}

// justifyValue maps shorthand justify values to CSS.
func justifyValue(val string) string {
	m := map[string]string{
		"start":   "flex-start",
		"center":  "center",
		"end":     "flex-end",
		"between": "space-between",
		"around":  "space-around",
	}
	return m[val]
}

// isSpacingProperty returns true if the property uses spacing tokens.
func isSpacingProperty(prop string) bool {
	spacing := map[string]bool{
		"p": true, "px": true, "py": true, "pt": true, "pr": true, "pb": true, "pl": true,
		"m": true, "mx": true, "my": true, "mt": true, "mr": true, "mb": true, "ml": true,
		"gap": true, "gap-x": true, "gap-y": true,
		"w": true, "h": true,
	}
	return spacing[prop]
}

// isRadiusProperty returns true if the property uses radius tokens.
func isRadiusProperty(prop string) bool {
	return prop == "rounded"
}
