package style

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// classAttrRe matches class="..." attributes in HTML.
var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// CSSExtractor scans rendered HTML and extracts used CSS classes.
type CSSExtractor struct {
	Theme Theme
	Known map[string]StyleDef // known class → CSS properties
}

// NewCSSExtractor creates an extractor with theme context.
func NewCSSExtractor(theme Theme) *CSSExtractor {
	return &CSSExtractor{
		Theme: theme,
		Known: make(map[string]StyleDef),
	}
}

// ExtractFromHTML scans HTML for class attributes and returns the list of unique classes used.
func (e *CSSExtractor) ExtractFromHTML(html string) []string {
	matches := classAttrRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		classes := strings.Fields(match[1])
		for _, c := range classes {
			if c != "" {
				seen[c] = true
			}
		}
	}

	result := make([]string, 0, len(seen))
	for c := range seen {
		result = append(result, c)
	}
	sort.Strings(result)
	return result
}

// GenerateCSS generates minimal CSS for the given class list.
// It checks Known styles first, then falls back to utility class generation.
func (e *CSSExtractor) GenerateCSS(classes []string) string {
	var b strings.Builder

	for _, class := range classes {
		// Check known/component styles first
		if styleDef, ok := e.Known[class]; ok {
			fmt.Fprintf(&b, ".%s {\n", class)
			props := make([]string, 0, len(styleDef))
			for prop := range styleDef {
				props = append(props, prop)
			}
			sort.Strings(props)
			for _, prop := range props {
				val := e.Theme.ResolveAll(styleDef[prop])
				fmt.Fprintf(&b, "  %s: %s;\n", prop, val)
			}
			fmt.Fprintf(&b, "}\n")
			continue
		}

		// Fall back to utility class generation
		props := resolveUtilityClass(class, e.Theme)
		if props != "" {
			fmt.Fprintf(&b, ".%s { %s }\n", class, props)
		}
	}

	return b.String()
}

// GenerateChunk generates a CSS chunk for a specific screen/page by extracting
// classes from HTML and generating CSS for them.
func (e *CSSExtractor) GenerateChunk(html string) string {
	classes := e.ExtractFromHTML(html)
	return e.GenerateCSS(classes)
}
