package style

import (
	"regexp"
	"sort"
	"strings"
)

var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// CSSExtractor scans rendered HTML and extracts used utility classes to
// generate CSS for them.
type CSSExtractor struct {
	Theme Theme
}

// NewCSSExtractor creates an extractor with theme context.
func NewCSSExtractor(theme Theme) *CSSExtractor {
	return &CSSExtractor{Theme: theme}
}

// ExtractFromHTML scans HTML for class attributes and returns the list of
// unique classes used.
func (e *CSSExtractor) ExtractFromHTML(html string) []string {
	matches := classAttrRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for c := range strings.FieldsSeq(match[1]) {
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
	return GenerateUtilityCSS(classes, e.Theme)
}

// GenerateChunk extracts classes from HTML and generates utility CSS.
func (e *CSSExtractor) GenerateChunk(html string) string {
	classes := e.ExtractFromHTML(html)
	return e.GenerateCSS(classes)
}
