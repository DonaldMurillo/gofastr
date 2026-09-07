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
	rail := sectionOf(t, css, `[data-fui-comp="ui-workbench"] .ui-workbench__rail`)
	if !strings.Contains(rail, "overflow-y: auto") {
		t.Fatalf("the rail does not scroll on its own:\n%s", rail)
	}
	root := sectionOf(t, css, `[data-fui-comp="ui-workbench"] {`)
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
		`[data-fui-comp="ui-workbench"] .ui-workbench__pane > iframe`)
	for _, want := range []string{"inline-size: 100%", "block-size: 100%", "border: 0"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("iframe rule is missing %q:\n%s", want, rule)
		}
	}
}

// The rail width is configurable, and must arrive as a custom property rather
// than an inline width: a strict-CSP host rejects style attributes the design
// system did not account for.
func TestWorkbenchRailWidthIsACustomProperty(t *testing.T) {
	out := string(Workbench(WorkbenchConfig{
		RailWidth: "480px",
		Rail:      render.Text("rail"),
		Pane:      render.Text("pane"),
	}))
	if !strings.Contains(out, "--ui-workbench-rail: 480px") {
		t.Fatalf("RailWidth did not reach the markup as a custom property:\n%s", out)
	}
	if strings.Contains(out, "width:") || strings.Contains(out, "inline-size:") {
		t.Fatalf("RailWidth was written as a direct style property:\n%s", out)
	}
}

// Both regions render, in rail-then-pane order, with the component marker the
// stylesheet keys on.
func TestWorkbenchRendersBothRegions(t *testing.T) {
	out := string(Workbench(WorkbenchConfig{
		Rail:       render.Text("RAILCONTENT"),
		Pane:       render.Text("PANECONTENT"),
		ExtraAttrs: html.Attrs{"aria-label": "Inspector"},
	}))
	if !strings.Contains(out, `data-fui-comp="ui-workbench"`) {
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
// component owns (#262): the style attribute belongs to RailWidth
// (a CSP-safe custom property), so callers cannot swap in arbitrary
// inline styles.
func TestWorkbenchExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Workbench(WorkbenchConfig{
		RailWidth: "480px", Class: "mine",
		Rail: render.Text("rail"), Pane: render.Text("pane"),
		ExtraAttrs: map[string]string{
			"data-test": "hook", "style": "evil", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `style="--ui-workbench-rail: 480px"`, `class="ui-workbench mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root missing %q:\n%s", want, root)
		}
	}
}

// TestWorkbenchRailWidthIsOneCSSLength pins the sibling of the carousel
// placeholder-height finding: RailWidth lands inside the root's inline
// style attribute (as the --ui-workbench-rail custom property), so a
// declaration list in the config value must not ship as live page CSS —
// the same cssLengthOr gate carousel's VirtualPlaceholderHeight uses.
// A rejected value drops the style attribute and the CSS default (320px)
// applies.
func TestWorkbenchRailWidthIsOneCSSLength(t *testing.T) {
	malicious := "320px;background:url(//evil.example/x);position:fixed"
	for _, v := range []string{malicious, "var(--rail", "480px;color:red"} {
		h := string(Workbench(WorkbenchConfig{
			RailWidth: v,
			Rail:      render.Text("rail"), Pane: render.Text("pane"),
		}))
		root := h[:strings.Index(h, ">")+1]
		if strings.Contains(root, "background:url(") || strings.Contains(root, "color:red") ||
			strings.Contains(root, "style=") {
			t.Errorf("SECURITY: [workbench-rail-injection] RailWidth %q reached the page as inline CSS: %s", v, root)
		}
	}
	// Happy path: a plain length and a bare var() token still render.
	for _, ok := range []string{"480px", "32rem", "var(--app-rail)"} {
		h := string(Workbench(WorkbenchConfig{
			RailWidth: ok,
			Rail:      render.Text("rail"), Pane: render.Text("pane"),
		}))
		root := h[:strings.Index(h, ">")+1]
		if !strings.Contains(root, `style="--ui-workbench-rail: `+ok+`"`) {
			t.Errorf("plain CSS length %q must keep the custom property:\n%s", ok, root)
		}
	}
}
