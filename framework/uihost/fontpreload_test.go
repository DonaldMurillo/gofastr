package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// WithFontPreload links each web font's woff2 at the URL FontFaceCSS
// serves it from, so the fetch starts with the page (#711).
func TestWithFontPreload(t *testing.T) {
	page := renderHeadPage(t, WithFontPreload("", style.WebFont{Family: "Bricolage Grotesque"}, style.WebFont{}, style.WebFont{Family: "'\\"}))
	want := `<link rel="preload" as="font" type="font/woff2" href="/fonts/bricolage-grotesque.woff2" crossorigin>`
	if !strings.Contains(page, want) {
		t.Fatalf("missing %s in:\n%s", want, page)
	}
	if strings.Count(page, `as="font"`) != 1 {
		t.Errorf("a font FontFaceCSS declares no rule for was preloaded:\n%s", page)
	}
	page = renderHeadPage(t, WithFontPreload("", style.WebFont{Family: "Inter", File: "inter"}, style.WebFont{Family: "Inter", File: "unused"}, style.WebFont{Family: "Mono"}))
	if strings.Contains(page, "unused.woff2") || !strings.Contains(page, "/fonts/inter.woff2") || !strings.Contains(page, "/fonts/mono.woff2") {
		t.Errorf("preload must follow FontFaceCSS's first-per-family rule:\n%s", page)
	}
	if page := renderHeadPage(t, WithFontPreload("javascript:alert(1)//", style.WebFont{Family: "X"})); strings.Contains(page, "javascript:") {
		t.Errorf("SECURITY: an unsafe font dir reached the head:\n%s", page)
	}
}
