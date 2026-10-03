package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// paneClassTokens returns the class attribute of the first opening tag
// containing marker, split on spaces. Assertions match WHOLE tokens so
func paneClassTokens(t *testing.T, h, marker string) []string {
	t.Helper()
	i := strings.Index(h, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in:\n%s", marker, h)
	}
	// The whole opening tag: attributes render in sorted order, so the
	// class attribute may sit BEFORE the marker.
	start := strings.LastIndex(h[:i], "<")
	end := strings.Index(h[i:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("could not bound the %q tag:\n%s", marker, h)
	}
	tag := h[start : i+end]
	c := `class="`
	k := strings.Index(tag, c)
	if k < 0 {
		t.Fatalf("no class attribute on the %q tag:\n%s", marker, tag)
	}
	val := tag[k+len(c):]
	if j := strings.Index(val, `"`); j >= 0 {
		val = val[:j]
	}
	return strings.Fields(val)
}

func paneHasClass(t *testing.T, h, marker, token string) {
	t.Helper()
	for _, tok := range paneClassTokens(t, h, marker) {
		if tok == token {
			return
		}
	}
	t.Errorf("token %q missing (marker %q):\n%s", token, marker, h)
}

func TestPaneHostPanicsWithoutPrimary(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("PaneHost with empty Primary should panic")
		}
	}()
	_ = PaneHost(PaneHostConfig{})
}

func TestPaneHostRootEmitsMarkerAttrs(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{Primary: render.Text("P")}))
	mustContain(t, render.HTML(h), `data-fui-comp="ui-pane-host"`)
	mustContain(t, render.HTML(h), `data-hui-panehost=""`)
	mustContain(t, render.HTML(h), `data-hui-pane="primary"`)
	mustContain(t, render.HTML(h), ">P<")
	paneHasClass(t, h, `data-hui-panehost`, `fui-pane-host`)
	paneHasClass(t, h, `data-hui-pane="primary"`, `fui-pane-host__pane--primary`)
}

func TestPaneHostSSROpenPaneVisible(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:        render.Text("P"),
		Secondary:      render.Text("S"),
		SecondaryOpen:  true,
		SecondaryLabel: "Details",
	}))
	paneHasClass(t, h, `data-hui-panehost`, `fui-pane-host--secondary-open`)
	// The open state also ships on the hook the module maintains.
	mustContain(t, render.HTML(h), `data-hui-pane-open="secondary"`)
	// The open secondary pane must NOT carry hidden.
	secIdx := strings.Index(h, `data-hui-pane="secondary"`)
	if secIdx < 0 {
		t.Fatalf("secondary pane missing:\n%s", h)
	}
	// Inspect just the opening tag (up to '>'); an open pane must not
	// carry hidden there.
	gt := strings.Index(h[secIdx:], ">")
	openTag := h[secIdx:]
	if gt >= 0 {
		openTag = h[secIdx : secIdx+gt+1]
	}
	if strings.Contains(openTag, "hidden") {
		t.Errorf("open secondary pane should not be hidden:\n%s", openTag)
	}
	paneHasClass(t, h, `data-hui-pane="secondary"`, `fui-pane-host__pane--secondary`)
	mustContain(t, render.HTML(h), `role="region"`)
	mustContain(t, render.HTML(h), `aria-label="Details"`)
}

func TestPaneHostClosedOptionalPaneHidden(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:   render.Text("P"),
		Secondary: render.Text("S"),
		// SecondaryOpen defaults to false.
	}))
	if !strings.Contains(h, `data-hui-pane="secondary" hidden`) {
		t.Errorf("closed optional pane should carry hidden:\n%s", h)
	}
	for _, tok := range paneClassTokens(t, h, `data-hui-panehost`) {
		if tok == "fui-pane-host--secondary-open" {
			t.Errorf("closed pane should not add the open modifier:\n%s", h)
		}
	}
	if strings.Contains(h, `data-hui-pane-open`) {
		t.Errorf("closed pane should not mark the host open:\n%s", h)
	}
}

func TestPaneHostTertiaryLabelDefault(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:      render.Text("P"),
		Tertiary:     render.Text("T"),
		TertiaryOpen: true,
	}))
	mustContain(t, render.HTML(h), `aria-label="Tertiary"`)
	paneHasClass(t, h, `data-hui-panehost`, `fui-pane-host--tertiary-open`)
	paneHasClass(t, h, `data-hui-pane="tertiary"`, `fui-pane-host__pane--tertiary`)
}

func TestPaneHostNoInlineStyle(t *testing.T) {
	// Hard Rule 9b: column state is driven by classes/attrs, never
	// inline style, so CSP stays strict.
	h := string(PaneHost(PaneHostConfig{
		Primary:        render.Text("P"),
		Secondary:      render.Text("S"),
		Tertiary:       render.Text("T"),
		SecondaryOpen:  true,
		TertiaryOpen:   true,
		SecondaryLabel: "A",
		TertiaryLabel:  "B",
	}))
	if strings.Contains(h, `style="`) {
		t.Errorf("PaneHost output must not contain inline style:\n%s", h)
	}
	// Both panes open → both modifiers present, neither optional hidden,
	// and the open list names BOTH panes (the sheet and the module's
	// topmost read that list).
	paneHasClass(t, h, `data-hui-panehost`, `fui-pane-host--secondary-open`)
	paneHasClass(t, h, `data-hui-panehost`, `fui-pane-host--tertiary-open`)
	mustContain(t, render.HTML(h), `data-hui-pane-open="secondary tertiary"`)
}

func TestPaneHostCSSHasBreakpoint(t *testing.T) {
	css := paneHostCSS(style.Theme{})
	if !strings.Contains(css, "max-width: 768px") {
		t.Fatal("pane-host CSS missing its 768px collapse breakpoint")
	}
	if !strings.Contains(css, `data-hui-pane-mode="overlay"`) {
		t.Fatal("pane-host CSS missing overlay-mode drawer rules")
	}
	// The column rules key off the hook the module maintains, so a
	// client-side open changes the columns, not just the first-paint
	// classes.
	if !strings.Contains(css, `[data-hui-pane-open~="secondary"]`) {
		t.Fatal("pane-host CSS column rules do not key off data-hui-pane-open")
	}
	// The drawer chrome addresses BOTH side panes by the live hook; the
	// retired data-fui-pane spelling matches nothing the module marks.
	if !strings.Contains(css, `[data-hui-pane="tertiary"]:not([hidden])`) {
		t.Fatal("pane-host CSS drawer chrome does not address the tertiary pane by data-hui-pane")
	}
	if strings.Contains(css, `[data-fui-pane="tertiary"]`) {
		t.Fatal("pane-host CSS still addresses the retired data-fui-pane spelling")
	}
}

func TestPaneHostExtraAttrsOnRoot(t *testing.T) {
	h := PaneHost(PaneHostConfig{
		Primary:    render.Text("p"),
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("PaneHost root missing data-test:\n%s", root)
	}
}
