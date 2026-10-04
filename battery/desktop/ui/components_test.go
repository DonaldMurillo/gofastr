package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// FloatingToolbar composes ui.Toolbar (semantics, groups, runtime
// behavior unchanged) inside the glass capsule. The framework toolbar
// keeps its own marker; the wrapper only adds the surface.
func TestFloatingToolbarComposesUIToolbar(t *testing.T) {
	out := string(desktopui.FloatingToolbar(ui.ToolbarConfig{
		Label:  "Timer actions",
		Groups: []ui.ToolbarGroup{{Children: []render.HTML{plain(`<button>Start</button>`)}}},
	}))
	for _, w := range []string{
		`data-cui-comp="desktopui-floating-toolbar"`,
		`data-cui-comp="ui-toolbar"`,
		`role="toolbar"`,
		`aria-label="Timer actions"`,
		`data-cui-comp="desktopui-glass"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("floating toolbar missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("floating toolbar must not emit an inline style:\n%s", out)
	}
}

// The capsule override beats ui-toolbar's own chrome at higher
// specificity, and the wrapper floats at the sticky layer.
func TestFloatingToolbarCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-floating-toolbar")
	for _, w := range []string{
		`[data-cui-comp="desktopui-floating-toolbar"] {`,
		"position: sticky",
		"var(--z-sticky",
		`[data-cui-comp="desktopui-floating-toolbar"] [data-cui-comp="ui-toolbar"]`,
		"background: transparent",
		"border: none",
		"border-radius: var(--radii-full",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("floating toolbar CSS missing %q:\n%s", w, css)
		}
	}
}

// Inspector is a labelled glass side panel composing ui.DetailList.
func TestInspectorComposesDetailList(t *testing.T) {
	out := string(desktopui.Inspector(desktopui.InspectorConfig{
		Label: "Task details",
		Title: "Write the brief",
		Items: []ui.DetailItem{
			{Label: "Phase", Value: render.Text("Work")},
		},
	}))
	for _, w := range []string{
		`data-cui-comp="desktopui-inspector"`,
		`data-cui-comp="desktopui-glass"`,
		`role="region"`,
		`aria-label="Task details"`,
		`>Write the brief</h2>`,
		`data-cui-comp="ui-detail-list"`,
		`<dt`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("inspector missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("inspector must not emit an inline style:\n%s", out)
	}
}

// Inspector requires a Label: an unnamed region is a landmark nobody
// can navigate.
func TestInspectorRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Inspector with empty Label must panic")
		}
	}()
	_ = desktopui.Inspector(desktopui.InspectorConfig{})
}

// Sheet renders the thick-glass dialog surface: one glass element, a
// labelled header, the body, an optional footer. The preset widget
// chrome paints its own opaque panel, so hosts wanting the glass look
// pass Sheet as the widget Skeleton, not as slot content under the
// default chrome.
func TestSheetSurface(t *testing.T) {
	out := string(desktopui.Sheet(desktopui.SheetConfig{
		Title:  "Discard draft?",
		Footer: plain(`<button>Discard</button>`),
	}, plain(`<p>The draft is unsaved.</p>`)))
	for _, w := range []string{
		`data-cui-comp="desktopui-sheet"`,
		`data-cui-comp="desktopui-glass"`,
		`desktopui-glass--thick`,
		`role="dialog"`,
		`aria-label="Discard draft?"`,
		`>Discard draft?</h2>`,
		`The draft is unsaved.`,
		`desktopui-sheet__footer`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("sheet missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("sheet must not emit an inline style:\n%s", out)
	}
}

func TestSheetRequiresTitle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Sheet with empty Title must panic")
		}
	}()
	_ = desktopui.Sheet(desktopui.SheetConfig{}, plain("x"))
}

// Popover is the compact thick-glass surface: dialog role, tighter
// padding, capped width.
func TestPopoverSurface(t *testing.T) {
	out := string(desktopui.Popover(desktopui.PopoverConfig{
		Title: "Quick actions",
	}, plain(`<p>Pick one.</p>`)))
	for _, w := range []string{
		`data-cui-comp="desktopui-popover"`,
		`desktopui-glass--thick`,
		`role="dialog"`,
		`aria-label="Quick actions"`,
		`Pick one.`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("popover missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("popover must not emit an inline style:\n%s", out)
	}
}

// Both overlay surfaces inherit the glass accessibility rules (opaque
// under reduce-transparency) through the shared glass class; their own
// sheets add only structure and sizing.
func TestOverlayCSSAddsStructure(t *testing.T) {
	for _, name := range []string{"desktopui-sheet", "desktopui-popover"} {
		css := componentCSS(t, name)
		if !strings.Contains(css, "max-inline-size") {
			t.Errorf("%s CSS missing the width cap:\n%s", name, css)
		}
	}
	popoverCSS := componentCSS(t, "desktopui-popover")
	if !strings.Contains(popoverCSS, "--desktop-popover-width") {
		t.Errorf("popover CSS missing its width knob:\n%s", popoverCSS)
	}
}

// InspectorSplit puts the inspector in the trailing column at its own
// width beside a content column that takes the rest, wrapping below
// only when the content would drop under 20rem.
func TestInspectorSplitTrailingColumn(t *testing.T) {
	out := string(desktopui.InspectorSplit(plain(`<p>body</p>`), plain(`<aside>facts</aside>`)))
	body := strings.Index(out, "<p>body</p>")
	facts := strings.Index(out, "<aside>facts</aside>")
	if !strings.Contains(out, `data-cui-comp="desktopui-inspector-split"`) || body < 0 || facts < body {
		t.Fatalf("split must render content then inspector under its marker:\n%s", out)
	}
	if strings.Contains(out, "style=") {
		t.Errorf("split must not emit an inline style:\n%s", out)
	}
	css := componentCSS(t, "desktopui-inspector-split")
	for _, w := range []string{
		"flex: 1 1 20rem;",
		"flex: 0 0 var(--desktop-inspector-width, 260px);",
		"flex-wrap: wrap;",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("split CSS missing %q:\n%s", w, css)
		}
	}
}

// Each glass panel's rule must match the element WrapHTML marks: the
// marker lands on the panel itself, so a descendant selector left the
// inspector (and the sheet and popover) unpadded, its rows running
// into the glass edge.
func TestPanelRulesMatchMarkedElement(t *testing.T) {
	cases := []struct{ name, out string }{
		{"inspector", string(desktopui.Inspector(desktopui.InspectorConfig{Label: "Facts"}))},
		{"sheet", string(desktopui.Sheet(desktopui.SheetConfig{Title: "Discard?"}))},
		{"popover", string(desktopui.Popover(desktopui.PopoverConfig{Title: "More"}))},
	}
	for _, c := range cases {
		marker := `data-cui-comp="desktopui-` + c.name + `"`
		i := strings.Index(c.out, marker)
		if i < 0 {
			t.Fatalf("%s: no marker:\n%s", c.name, c.out)
		}
		tag := c.out[strings.LastIndex(c.out[:i], "<"):i]
		if !strings.Contains(tag, `class="desktopui-`+c.name+`"`) {
			t.Fatalf("%s: marker is not on the panel element:\n%s", c.name, c.out)
		}
		css := componentCSS(t, "desktopui-"+c.name)
		if !strings.Contains(css, `[data-cui-comp="desktopui-`+c.name+`"].desktopui-`+c.name+` {`) {
			t.Errorf("%s: panel rule does not target the marked element:\n%s", c.name, css)
		}
	}
}

// The inspector caps the DetailList label column: the page default
// grows it to 13rem, which wrapped every value in a 260px panel.
func TestInspectorCapsDetailLabelColumn(t *testing.T) {
	css := componentCSS(t, "desktopui-inspector")
	if !strings.Contains(css, "--ui-detail-list-label-track: minmax(4rem, 6rem);") {
		t.Errorf("inspector does not cap the detail label column:\n%s", css)
	}
}
