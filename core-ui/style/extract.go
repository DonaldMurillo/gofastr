package style

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// classAttrRe matches class="..." attributes in HTML.
var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// CSSExtractor scans rendered HTML and extracts used utility classes
// to generate CSS for them.
//
// The legacy "Known map of named component styles" was retired
// alongside the typed-theme migration — registered styled components
// now own their own CSS via core-ui/registry. CSSExtractor is now
// purely the utility-class generator (Tailwind-style helpers).
type CSSExtractor struct {
	Theme Theme
}

// NewCSSExtractor creates an extractor with theme context.
func NewCSSExtractor(theme Theme) *CSSExtractor {
	return &CSSExtractor{Theme: theme}
}

// ExtractFromHTML scans HTML for class attributes and returns the
// list of unique classes used.
func (e *CSSExtractor) ExtractFromHTML(html string) []string {
	matches := classAttrRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for _, c := range strings.Fields(match[1]) {
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

// GenerateCSS generates utility CSS for the given class list.
func (e *CSSExtractor) GenerateCSS(classes []string) string {
	var b strings.Builder
	for _, class := range classes {
		props := resolveUtilityClass(class, e.Theme)
		if props != "" {
			fmt.Fprintf(&b, ".%s { %s }\n", class, props)
		}
	}
	return b.String()
}

// GenerateChunk extracts classes from HTML and generates utility CSS.
func (e *CSSExtractor) GenerateChunk(html string) string {
	classes := e.ExtractFromHTML(html)
	return e.GenerateCSS(classes)
}
