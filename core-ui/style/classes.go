package style

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Classes is a map of class name to whether it should be included.
type Classes map[string]bool

// ToAttr converts Classes to an Attrs map with the "class" key.
func (c Classes) ToAttr() map[string]string {
	s := c.String()
	if s == "" {
		return map[string]string{}
	}
	return map[string]string{"class": s}
}

// String returns the space-separated class list from all true keys, sorted for
// determinism.
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

// GenerateUtilityCSS generates CSS rules for a set of utility class names.
func GenerateUtilityCSS(classes []string, theme Theme) string {
	var b strings.Builder
	for _, class := range classes {
		if !isCSSIdent(class) {
			continue
		}
		props := resolveUtilityClass(class, theme)
		if props != "" {
			fmt.Fprintf(&b, ".%s { %s }\n", class, props)
		}
	}
	return b.String()
}

func isCSSIdent(name string) bool {
	return validCSSPropName(name)
}

func resolveUtilityClass(class string, theme Theme) string {
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

	switch class {
	case "flex-row":
		return "flex-direction: row;"
	case "flex-col":
		return "flex-direction: column;"
	case "flex-wrap":
		return "flex-wrap: wrap;"
	}

	if after, ok := strings.CutPrefix(class, "overflow-"); ok {
		val := after
		switch val {
		case "auto", "hidden", "scroll", "visible":
			return fmt.Sprintf("overflow: %s;", val)
		}
	}

	if after, ok := strings.CutPrefix(class, "items-"); ok {
		val := after
		cssVal := alignValue(val)
		if cssVal != "" {
			return fmt.Sprintf("align-items: %s;", cssVal)
		}
	}

	if after, ok := strings.CutPrefix(class, "justify-"); ok {
		val := after
		cssVal := justifyValue(val)
		if cssVal != "" {
			return fmt.Sprintf("justify-content: %s;", cssVal)
		}
	}

	if class == "w-full" {
		return "width: 100%;"
	}
	if class == "h-full" {
		return "height: 100%;"
	}
	if after, ok := strings.CutPrefix(class, "w-"); ok {
		token := after
		return fmt.Sprintf("width: var(--spacing-%s);", token)
	}
	if after, ok := strings.CutPrefix(class, "h-"); ok {
		token := after
		return fmt.Sprintf("height: var(--spacing-%s);", token)
	}

	if props := paddingCSS(class); props != "" {
		return props
	}
	if props := marginCSS(class); props != "" {
		return props
	}
	if props := gapCSS(class); props != "" {
		return props
	}

	if after, ok := strings.CutPrefix(class, "text-"); ok {
		token := after
		if isFontSizeToken(token) {
			return fmt.Sprintf("font-size: var(--text-%s);", token)
		}
		return fmt.Sprintf("color: var(--color-%s);", token)
	}

	if after, ok := strings.CutPrefix(class, "bg-"); ok {
		token := after
		return fmt.Sprintf("background-color: var(--color-%s);", token)
	}

	if after, ok := strings.CutPrefix(class, "border-"); ok {
		token := after
		if _, err := strconv.Atoi(token); err == nil {
			return fmt.Sprintf("border-width: %spx;", token)
		}
		if isSpacingToken(token) {
			return fmt.Sprintf("border-width: var(--spacing-%s);", token)
		}
		return fmt.Sprintf("border-color: var(--color-%s);", token)
	}
	if class == "border" {
		return "border-width: var(--stroke-thin);"
	}

	if after, ok := strings.CutPrefix(class, "rounded-"); ok {
		token := after
		return fmt.Sprintf("border-radius: var(--radii-%s);", token)
	}

	if after, ok := strings.CutPrefix(class, "font-"); ok {
		weight := after
		if w, ok := fontWeightMap()[weight]; ok {
			return fmt.Sprintf("font-weight: %s;", w)
		}
		return fmt.Sprintf("font-family: var(--font-%s);", weight)
	}

	return ""
}

func spacingCSS(class, short, long string) string {
	prefixes := map[string]string{
		short + "-":  long,
		short + "x-": long + "-left",
		short + "y-": long + "-top",
		short + "t-": long + "-top",
		short + "r-": long + "-right",
		short + "b-": long + "-bottom",
		short + "l-": long + "-left",
	}
	for prefix, prop := range prefixes {
		if !strings.HasPrefix(class, prefix) {
			continue
		}
		token := strings.TrimPrefix(class, prefix)
		val := tokenOrPx(token, "spacing")
		switch prefix {
		case short + "x-":
			return fmt.Sprintf("%s-left: %s; %s-right: %s;", long, val, long, val)
		case short + "y-":
			return fmt.Sprintf("%s-top: %s; %s-bottom: %s;", long, val, long, val)
		}
		return fmt.Sprintf("%s: %s;", prop, val)
	}
	return ""
}

func paddingCSS(class string) string { return spacingCSS(class, "p", "padding") }

func marginCSS(class string) string { return spacingCSS(class, "m", "margin") }

func gapCSS(class string) string {
	if after, ok := strings.CutPrefix(class, "gap-x-"); ok {
		token := after
		return fmt.Sprintf("column-gap: var(--spacing-%s);", token)
	}
	if after, ok := strings.CutPrefix(class, "gap-y-"); ok {
		token := after
		return fmt.Sprintf("row-gap: var(--spacing-%s);", token)
	}
	if after, ok := strings.CutPrefix(class, "gap-"); ok {
		token := after
		return fmt.Sprintf("gap: var(--spacing-%s);", token)
	}
	return ""
}

func tokenOrPx(token, category string) string {
	if n, err := strconv.Atoi(token); err == nil {
		return fmt.Sprintf("%dpx", n)
	}
	return fmt.Sprintf("var(--%s-%s)", category, token)
}

func fontWeightMap() map[string]string {
	return map[string]string{
		"normal":   "400",
		"medium":   "500",
		"semibold": "600",
		"bold":     "700",
	}
}

func isFontSizeToken(token string) bool {
	switch token {
	case "xs", "sm", "base", "lg", "xl", "2xl", "3xl":
		return true
	}
	return false
}

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
