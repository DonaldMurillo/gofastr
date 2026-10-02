package main

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every custom property the site's stylesheet reads with no fallback is
// declared in the same app.css (theme tokens and the site's own :root
// vars both land there). A var() naming nothing resolves to the
// property's initial value: renaming the spacing slots from xxl/xxxl to
// 2xl/3xl left 28 rules reading --spacing-xxl, and the docs index lost
// its top padding and its rail gap without a single error.
func TestAppCSSReadsOnlyDeclaredProps(t *testing.T) {
	css := body(t, "/__gofastr/app.css")
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--[a-zA-Z0-9_-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		declared[m[1]] = true
	}
	if len(declared) < 20 {
		t.Fatalf("app.css declares only %d custom properties; the theme's :root block is missing", len(declared))
	}
	var missing []string
	for _, m := range regexp.MustCompile(`var\(\s*(--[a-zA-Z0-9_-]+)\s*\)`).FindAllStringSubmatch(css, -1) {
		if !declared[m[1]] && !slices.Contains(missing, m[1]) {
			missing = append(missing, m[1])
		}
	}
	if len(missing) > 0 {
		t.Fatalf("app.css reads undeclared custom properties: %s", strings.Join(missing, ", "))
	}
}
