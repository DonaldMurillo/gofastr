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
func (c Classes) ToAttr() map[string]string {
	s := c.String()
	if s == "" {
		return map[string]string{}
	}
	return map[string]string{"class": s}
}

// String returns the space-separated class list from all true keys,
// sorted for determinism.
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

// GenerateUtilityCSS generates CSS rules for a set of utility class
// names. Each class resolves to one CSS declaration referencing
// theme variables via var(--*).
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

// resolveUtilityClass maps a utility class name to its CSS property
// declarations. Theme tokens always resolve to `var(--…)` so cascade
// overrides keep working.
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
	if strings.HasPrefix(class, "w-") {
		token := strings.TrimPrefix(class, "w-")
		return fmt.Sprintf("width: var(--spacing-%s);", token)
	}
	if strings.HasPrefix(class, "h-") {
		token := strings.TrimPrefix(class, "h-")
		return fmt.Sprintf("height: var(--spacing-%s);", token)
	}

	// Padding utilities
	if props := paddingCSS(class); props != "" {
		return props
	}
	// Margin utilities
	if props := marginCSS(class); props != "" {
		return props
	}
	// Gap utilities
	if props := gapCSS(class); props != "" {
		return props
	}

	// Font size utilities — `text-base`, `text-lg`, etc. → font-size token.
	if strings.HasPrefix(class, "text-") {
		token := strings.TrimPrefix(class, "text-")
		if isFontSizeToken(token) {
			return fmt.Sprintf("font-size: var(--text-%s);", token)
		}
		// Otherwise treat as color
		return fmt.Sprintf("color: var(--color-%s);", token)
	}

	// Background color
	if strings.HasPrefix(class, "bg-") {
		token := strings.TrimPrefix(class, "bg-")
		return fmt.Sprintf("background-color: var(--color-%s);", token)
	}

	// Border color / width
	if strings.HasPrefix(class, "border-") {
		token := strings.TrimPrefix(class, "border-")
		if _, err := strconv.Atoi(token); err == nil {
			return fmt.Sprintf("border-width: %spx;", token)
		}
		if isSpacingToken(token) {
			return fmt.Sprintf("border-width: var(--spacing-%s);", token)
		}
		return fmt.Sprintf("border-color: var(--color-%s);", token)
	}
	if class == "border" {
		return "border-width: 1px;"
	}

	// Border radius
	if strings.HasPrefix(class, "rounded-") {
		token := strings.TrimPrefix(class, "rounded-")
		return fmt.Sprintf("border-radius: var(--radii-%s);", token)
	}

	// Font weight
	if strings.HasPrefix(class, "font-") {
		weight := strings.TrimPrefix(class, "font-")
		if w, ok := fontWeightMap()[weight]; ok {
			return fmt.Sprintf("font-weight: %s;", w)
		}
		return fmt.Sprintf("font-family: var(--font-%s);", weight)
	}

	return ""
}

func paddingCSS(class string) string {
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
		if !strings.HasPrefix(class, prefix) {
			continue
		}
		token := strings.TrimPrefix(class, prefix)
		val := tokenOrPx(token, "spacing")
		switch prefix {
		case "px-":
			return fmt.Sprintf("padding-left: %s; padding-right: %s;", val, val)
		case "py-":
			return fmt.Sprintf("padding-top: %s; padding-bottom: %s;", val, val)
		}
		return fmt.Sprintf("%s: %s;", prop, val)
	}
	return ""
}

func marginCSS(class string) string {
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
		if !strings.HasPrefix(class, prefix) {
			continue
		}
		token := strings.TrimPrefix(class, prefix)
		val := tokenOrPx(token, "spacing")
		switch prefix {
		case "mx-":
			return fmt.Sprintf("margin-left: %s; margin-right: %s;", val, val)
		case "my-":
			return fmt.Sprintf("margin-top: %s; margin-bottom: %s;", val, val)
		}
		return fmt.Sprintf("%s: %s;", prop, val)
	}
	return ""
}

func gapCSS(class string) string {
	if strings.HasPrefix(class, "gap-x-") {
		token := strings.TrimPrefix(class, "gap-x-")
		return fmt.Sprintf("column-gap: var(--spacing-%s);", token)
	}
	if strings.HasPrefix(class, "gap-y-") {
		token := strings.TrimPrefix(class, "gap-y-")
		return fmt.Sprintf("row-gap: var(--spacing-%s);", token)
	}
	if strings.HasPrefix(class, "gap-") {
		token := strings.TrimPrefix(class, "gap-")
		return fmt.Sprintf("gap: var(--spacing-%s);", token)
	}
	return ""
}

// tokenOrPx returns either a CSS var reference (when token is a
// named scale value) or a literal pixel size (when token parses as
// a plain int).
func tokenOrPx(token, category string) string {
	if n, err := strconv.Atoi(token); err == nil {
		return fmt.Sprintf("%dpx", n)
	}
	return fmt.Sprintf("var(--%s-%s)", category, token)
}

// fontWeightMap — keyword → numeric weight.
func fontWeightMap() map[string]string {
	return map[string]string{
		"normal":   "400",
		"medium":   "500",
		"semibold": "600",
		"bold":     "700",
	}
}

// isFontSizeToken — known typography-scale names.
func isFontSizeToken(token string) bool {
	switch token {
	case "xs", "sm", "base", "lg", "xl", "2xl", "3xl":
		return true
	}
	return false
}

// isSpacingToken — known spacing-scale names.
func isSpacingToken(token string) bool {
	switch token {
	case "xs", "sm", "md", "lg", "xl", "2xl", "3xl":
		return true
	}
	return false
}

func alignValue(val string) string {
	m := map[string]string{
		"start":   "flex-start",
		"center":  "center",
		"end":     "flex-end",
		"stretch": "stretch",
	}
	return m[val]
}

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
