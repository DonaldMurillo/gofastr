package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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
	mustContain(t, render.HTML(h), `data-fui-pane-host=""`)
	mustContain(t, render.HTML(h), `data-fui-pane="primary"`)
	mustContain(t, render.HTML(h), ">P<")
}

func TestPaneHostSSROpenPaneVisible(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:        render.Text("P"),
		Secondary:      render.Text("S"),
		SecondaryOpen:  true,
		SecondaryLabel: "Details",
	}))
	mustContain(t, render.HTML(h), `ui-pane-host--secondary-open`)
	// The open secondary pane must NOT carry hidden.
	secIdx := strings.Index(h, `data-fui-pane="secondary"`)
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
	mustContain(t, render.HTML(h), `role="region"`)
	mustContain(t, render.HTML(h), `aria-label="Details"`)
}

func TestPaneHostClosedOptionalPaneHidden(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:   render.Text("P"),
		Secondary: render.Text("S"),
		// SecondaryOpen defaults to false.
	}))
	if !strings.Contains(h, `data-fui-pane="secondary" hidden`) {
		t.Errorf("closed optional pane should carry hidden:\n%s", h)
	}
	if strings.Contains(h, "ui-pane-host--secondary-open") {
		t.Errorf("closed pane should not add the open modifier:\n%s", h)
	}
}

func TestPaneHostTertiaryLabelDefault(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:      render.Text("P"),
		Tertiary:     render.Text("T"),
		TertiaryOpen: true,
	}))
	mustContain(t, render.HTML(h), `aria-label="Tertiary"`)
	mustContain(t, render.HTML(h), `ui-pane-host--tertiary-open`)
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
	// Both panes open → both modifiers present, neither optional hidden.
	mustContain(t, render.HTML(h), `ui-pane-host--secondary-open ui-pane-host--tertiary-open`)
}

func TestPaneHostCSSHasBreakpoint(t *testing.T) {
	css := paneHostCSS(style.Theme{})
	if !strings.Contains(css, "max-width: 768px") {
		t.Fatal("pane-host CSS missing its 768px collapse breakpoint")
	}
	if !strings.Contains(css, `data-fui-pane-mode="overlay"`) {
		t.Fatal("pane-host CSS missing overlay-mode drawer rules")
	}
}
