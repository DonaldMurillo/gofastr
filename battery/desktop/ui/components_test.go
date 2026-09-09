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
		`data-fui-comp="desktopui-floating-toolbar"`,
		`data-fui-comp="ui-toolbar"`,
		`role="toolbar"`,
		`aria-label="Timer actions"`,
		`data-fui-comp="desktopui-glass"`,
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
		`[data-fui-comp="desktopui-floating-toolbar"] {`,
		"position: sticky",
		"var(--z-sticky",
		`[data-fui-comp="desktopui-floating-toolbar"] [data-fui-comp="ui-toolbar"]`,
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
		`data-fui-comp="desktopui-inspector"`,
		`data-fui-comp="desktopui-glass"`,
		`role="region"`,
		`aria-label="Task details"`,
		`>Write the brief</h2>`,
		`data-fui-comp="ui-detail-list"`,
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
		`data-fui-comp="desktopui-sheet"`,
		`data-fui-comp="desktopui-glass"`,
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
		`data-fui-comp="desktopui-popover"`,
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
