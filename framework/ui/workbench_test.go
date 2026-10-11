package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The rail must scroll on its own. Without it the document grows to the length
// of the rail's contents and the pane scrolls off the screen, which is exactly
// what the theme editor looked like before this component existed: a 2300px
// page beside a preview nobody could see.
func TestWorkbenchRailScrollsIndependently(t *testing.T) {
	css := workbenchCSS(style.Theme{})
	rail := sectionOf(t, css, `[data-cui-comp="ui-workbench"] .fui-workbench__rail`)
	if !strings.Contains(rail, "overflow-y: auto") {
		t.Fatalf("the rail does not scroll on its own:\n%s", rail)
	}
	root := sectionOf(t, css, `[data-cui-comp="ui-workbench"] {`)
	if !strings.Contains(root, "block-size: 100dvh") {
		t.Fatalf("the shell is not viewport-height, so the rail has nothing to scroll within:\n%s", root)
	}
	if !strings.Contains(root, "overflow: hidden") {
		t.Fatalf("the shell scrolls as a page, which defeats the rail's own scroll:\n%s", root)
	}
}

// An <iframe> is the pane's motivating occupant and defaults to a ~300x150
// bordered box. If the component does not fill it, every caller has to, and the
// first caller that forgets ships a postage-stamp preview.
func TestWorkbenchPaneFillsAnIframe(t *testing.T) {
	rule := sectionOf(t, workbenchCSS(style.Theme{}),
		`[data-cui-comp="ui-workbench"] .fui-workbench__pane > iframe`)
	for _, want := range []string{"inline-size: 100%", "block-size: 100%", "border: 0"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("iframe rule is missing %q:\n%s", want, rule)
		}
	}
}

// The rail width is a named modifier class, never an inline style: a
// strict-CSP host strips style attributes, which left the old free-form
// length inert.
func TestWorkbenchRailWidthIsANamedModifier(t *testing.T) {
	for _, tc := range []struct {
		width WorkbenchRailWidth
		class string
	}{
		{WorkbenchRailDefault, `class="fui-workbench"`},
		{WorkbenchRailNarrow, `class="fui-workbench fui-workbench--rail-narrow"`},
		{WorkbenchRailWide, `class="fui-workbench fui-workbench--rail-wide"`},
	} {
		out := string(Workbench(WorkbenchConfig{
			RailWidth: tc.width,
			Rail:      render.Text("rail"),
			Pane:      render.Text("pane"),
		}))
		root := out[:strings.Index(out, ">")+1]
		if !strings.Contains(root, tc.class) {
			t.Errorf("RailWidth %q: root missing %s:\n%s", tc.width, tc.class, root)
		}
		if strings.Contains(root, "style=") {
			t.Errorf("RailWidth %q wrote an inline style:\n%s", tc.width, root)
		}
	}
	css := workbenchCSS(style.Theme{})
	for _, want := range []string{"var(--ui-workbench-rail-narrow, 240px)", "var(--ui-workbench-rail-wide, 480px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("workbench CSS missing %q", want)
		}
	}
}

// An unknown width is a programming error, refused loudly rather than
// rendered at the default.
func TestWorkbenchUnknownRailWidthPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unknown RailWidth rendered instead of panicking")
		}
	}()
	Workbench(WorkbenchConfig{RailWidth: "480px"})
}

// Both regions render, in rail-then-pane order, with the component marker the
// stylesheet keys on.
func TestWorkbenchRendersBothRegions(t *testing.T) {
	out := string(Workbench(WorkbenchConfig{
		Rail:       render.Text("RAILCONTENT"),
		Pane:       render.Text("PANECONTENT"),
		ExtraAttrs: html.Attrs{"aria-label": "Inspector"},
	}))
	if !strings.Contains(out, `data-cui-comp="ui-workbench"`) {
		t.Fatalf("missing the component marker the stylesheet keys on:\n%s", out)
	}
	if !strings.Contains(out, `aria-label="Inspector"`) {
		t.Fatalf("ExtraAttrs did not reach the root:\n%s", out)
	}
	ri, pi := strings.Index(out, "RAILCONTENT"), strings.Index(out, "PANECONTENT")
	if ri < 0 || pi < 0 {
		t.Fatalf("a region did not render:\n%s", out)
	}
	if ri > pi {
		t.Fatalf("pane rendered before rail — reading and tab order would start in the pane:\n%s", out)
	}
}

// A 320px rail beside anything is unusable on a phone, so the split has to
// collapse.
func TestWorkbenchStacksOnNarrowViewports(t *testing.T) {
	css := workbenchCSS(style.Theme{})
	if !strings.Contains(css, "@media (max-width: 720px)") {
		t.Fatal("no narrow-viewport rule — the 320px rail would sit beside the pane on a phone")
	}
	tail := css[strings.Index(css, "@media (max-width: 720px)"):]
	if !strings.Contains(tail, "display: block") {
		t.Fatalf("the split does not collapse below the breakpoint:\n%s", tail)
	}
}

// sectionOf returns the declaration block introduced by selector.
func sectionOf(t *testing.T, css, selector string) string {
	t.Helper()
	i := strings.Index(css, selector)
	if i < 0 {
		t.Fatalf("selector %q not found in the component stylesheet", selector)
	}
	rest := css[i:]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("selector %q has no closing brace", selector)
	}
	return rest[:end]
}

// ExtraAttrs land on the root element but never override what the
// component owns (#262): class, id, data-cui-* and style.
func TestWorkbenchExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Workbench(WorkbenchConfig{
		RailWidth: WorkbenchRailWide, Class: "mine",
		Rail: render.Text("rail"), Pane: render.Text("pane"),
		ExtraAttrs: map[string]string{
			"data-test": "hook", "style": "evil", "Class": "evil", "data-cui-comp": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof", "style="} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `class="fui-workbench fui-workbench--rail-wide mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root missing %q:\n%s", want, root)
		}
	}
}
