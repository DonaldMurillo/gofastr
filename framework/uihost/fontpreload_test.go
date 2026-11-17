package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// WithFontPreload links each web font's woff2 at the URL FontFaceCSS
// serves it from, so the fetch starts with the page (#711).
func TestWithFontPreload(t *testing.T) {
	page := renderHeadPage(t, WithFontPreload("", style.WebFont{Family: "Bricolage Grotesque"}, style.WebFont{}))
	want := `<link rel="preload" as="font" type="font/woff2" href="/fonts/bricolage-grotesque.woff2" crossorigin>`
	if !strings.Contains(page, want) {
		t.Fatalf("missing %s in:\n%s", want, page)
	}
	if strings.Count(page, `as="font"`) != 1 {
		t.Errorf("a font with no family was preloaded:\n%s", page)
	}
	if page := renderHeadPage(t, WithFontPreload("javascript:alert(1)//", style.WebFont{Family: "X"})); strings.Contains(page, "javascript:") {
		t.Errorf("SECURITY: an unsafe font dir reached the head:\n%s", page)
	}
}
