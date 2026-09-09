package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Glass renders its children inside the marker with the thick modifier
// only when asked, and never emits an inline style.
func TestGlassRendersMarkerAndVariant(t *testing.T) {
	out := string(desktopui.Glass(desktopui.GlassConfig{}, plain("<p>hi</p>")))
	if !strings.Contains(out, `data-fui-comp="desktopui-glass"`) {
		t.Errorf("missing glass marker:\n%s", out)
	}
	if strings.Contains(out, "desktopui-glass--thick") {
		t.Errorf("thin glass carries the thick modifier:\n%s", out)
	}
	if strings.Contains(out, "style=") {
		t.Errorf("glass must not emit an inline style:\n%s", out)
	}

	thick := string(desktopui.Glass(desktopui.GlassConfig{Thick: true}, plain("<p>hi</p>")))
	if !strings.Contains(thick, `data-fui-comp="desktopui-glass"`) || !strings.Contains(thick, "desktopui-glass--thick") {
		t.Errorf("thick glass missing marker or modifier:\n%s", thick)
	}
}

// The glass CSS is the honest recipe from the plan: blur + saturate
// (never an SVG reference filter, WebKit bug 245510), a translucent
// fill, and a 1px inset rim highlight. Both the prefixed and
// unprefixed backdrop-filter spellings ship (unprefixed since Safari
// 18, prefixed since 9).
func TestGlassCSSRecipe(t *testing.T) {
	css := componentCSS(t, "desktopui-glass")
	for _, w := range []string{
		"backdrop-filter:",
		"-webkit-backdrop-filter:",
		"saturate(",
		"blur(",
		"box-shadow: inset 0 0 0 1px",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("glass CSS missing %q:\n%s", w, css)
		}
	}
	if strings.Contains(css, "url(#") {
		t.Errorf("glass CSS must not reference an SVG filter (WebKit bug 245510):\n%s", css)
	}
	if strings.Contains(css, "feDisplacementMap") {
		t.Errorf("glass CSS must not use displacement:\n%s", css)
	}
}

// Under html.desktop-reduce-transparency the surface is opaque Surface
// with no filter; under html.desktop-inactive the fill flattens and the
// accent dims to TextMuted. The runtime module sets those classes; the
// CSS only reads them.
func TestGlassAccessibilityAndInactiveRules(t *testing.T) {
	css := componentCSS(t, "desktopui-glass")
	for _, w := range []string{
		"html.desktop-reduce-transparency",
		"backdrop-filter: none",
		"html.desktop-inactive",
		"--color-accent: var(--color-text-muted",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("glass CSS missing %q:\n%s", w, css)
		}
	}
}

// componentCSS builds one registered component's stylesheet under the
// desktop theme, the same way the host's /__gofastr/comp/<name>.css
// endpoint does.
func componentCSS(t *testing.T, name string) string {
	t.Helper()
	e, ok := registry.Lookup(name)
	if !ok {
		t.Fatalf("component %q is not registered", name)
	}
	return e.CSSFor(desktopui.Theme())
}

func plain(s string) render.HTML { return render.HTML(s) }
