package gallery

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/retired"
)

// The runtime retired-markup check fails an app's test on every name
// the upgrade registry retired. A name the kit itself still emits would
// fail every app that renders the component, so these gates render the
// kit against the live registry and refuse any finding: every catalog
// demo, a widget's chrome at every position (the panel, backdrop and
// position classes), and every registered stylesheet's class
// selectors. v0.13.0's note once retired fui-pos-center, which every
// centered widget still carries; this is the gate that would have
// refused it.

func TestCatalogEmitsNoRetiredMarkup(t *testing.T) {
	set := retired.Current()
	for _, e := range Catalog {
		for _, f := range set.Check([]byte(e.Demo())) {
			t.Errorf("catalog entry %q: %s", e.Slug, f.Message())
		}
	}
}

type gateSlot struct{}

func (gateSlot) Render() render.HTML { return render.HTML(`<p>body</p>`) }

func TestWidgetChromeEmitsNoRetiredMarkup(t *testing.T) {
	set := retired.Current()
	positions := []widget.Position{
		widget.BottomRight, widget.BottomCenter, widget.BottomLeft,
		widget.TopRight, widget.TopCenter, widget.TopLeft,
		widget.Center, widget.Top, widget.Bottom, widget.Edge, widget.EdgeRight,
	}
	for _, pos := range positions {
		def := &widget.Definition{
			Name:     "retired-gate-" + string(pos),
			Position: pos,
			Slots:    []widget.Slot{{Name: "body", Component: gateSlot{}}},
		}
		chrome := widget.RenderChrome(def)
		if chrome == "" {
			t.Fatalf("position %s rendered no chrome: the gate is reading nothing", pos)
		}
		for _, f := range set.Check([]byte(chrome)) {
			t.Errorf("widget chrome at %s: %s", pos, f.Message())
		}
	}
}

// classSelectorRe reads a .name class selector out of CSS.
var classSelectorRe = regexp.MustCompile(`\.(-?[A-Za-z_][A-Za-z0-9_-]*)`)

func TestSheetsSelectNoRetiredClasses(t *testing.T) {
	set := retired.Current()
	th := style.DefaultTheme()
	checked := 0
	for _, e := range registry.All() {
		if e.StyleFn == nil {
			continue
		}
		checked++
		seen := map[string]bool{}
		for _, m := range classSelectorRe.FindAllStringSubmatch(e.CSSFor(th), -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			for _, f := range set.Check([]byte(`<i class="` + m[1] + `">`)) {
				t.Errorf("sheet %q selects .%s: %s", e.Name, m[1], f.Message())
			}
		}
	}
	if checked == 0 {
		t.Fatal("no registered sheet was found: the walk is broken, not the tree clean")
	}
}
