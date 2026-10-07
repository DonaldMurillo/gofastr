package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A dropdown is a dismissable details: the outside-click hook is on,
// the count badge and icon are in the trigger, and the panel holds the
// caller's content.
func TestDropdownIsADismissablePopup(t *testing.T) {
	out := string(Dropdown(DropdownConfig{
		ID: "f", Label: "Filters", Icon: "filter", Count: 2, Align: DropdownEnd,
		Content: render.HTML(`<input name="q" aria-label="q">`),
	}))
	for _, want := range []string{
		`data-cui-comp="ui-dropdown"`,
		`data-hui-disclosure-dismiss`,
		`fui-dropdown fui-dropdown--end`,
		`<span class="fui-dropdown__label" data-cui-internal="">Filters</span>`,
		`<span class="fui-dropdown__count" data-cui-internal="">2</span>`,
		`<input name="q" aria-label="q">`,
		`<svg`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dropdown missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " open") {
		t.Errorf("a dropdown rendered open without Open:\n%s", out)
	}
}

// No count badge for zero, and the start edge is the default.
func TestDropdownDefaults(t *testing.T) {
	out := string(Dropdown(DropdownConfig{Label: "Save view", Content: render.Text("x")}))
	if strings.Contains(out, "fui-dropdown__count") {
		t.Errorf("a zero count drew a badge:\n%s", out)
	}
	if !strings.Contains(out, "fui-dropdown--start") {
		t.Errorf("the default edge is not start:\n%s", out)
	}
}

// A caller cannot take the component's open or class through
// ExtraAttrs; Open and Class are the spellings.
func TestDropdownOwnsOpenAndClass(t *testing.T) {
	out := string(Dropdown(DropdownConfig{Label: "x", Content: render.Text("y"), Class: "mine",
		ExtraAttrs: html.Attrs{"open": "", "class": "theirs", "data-test": "d"}}))
	if strings.Contains(out, "theirs") || strings.Contains(out, " open") {
		t.Errorf("ExtraAttrs took an owned key:\n%s", out)
	}
	for _, want := range []string{"fui-dropdown--start mine", `data-test="d"`} {
		if !strings.Contains(out, want) {
			t.Errorf("dropdown missing %q:\n%s", want, out)
		}
	}
}

func TestDropdownRefusals(t *testing.T) {
	for name, cfg := range map[string]DropdownConfig{
		"no label":     {Content: render.Text("x")},
		"no content":   {Label: "x"},
		"bad align":    {Label: "x", Content: render.Text("y"), Align: "middle"},
		"unknown icon": {Label: "x", Content: render.Text("y"), Icon: "no-such-icon"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			Dropdown(cfg)
		})
	}
}

// The panel floats (absolute, above the page) and is bounded by the
// viewport, so it can never widen the page.
func TestDropdownCSSFloatsInsideTheViewport(t *testing.T) {
	css := dropdownStyle.Entry().CSSFor(style.DefaultTheme())
	for _, want := range []string{
		"position: absolute",
		"z-index: var(--z-dropdown, 100)",
		"calc(100vw - var(--spacing-2xl, 32px))",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("dropdown CSS missing %q", want)
		}
	}
}

func TestListToolbarIconsRegistered(t *testing.T) {
	for _, name := range []string{"filter", "columns", "bookmark", "arrow-up", "arrow-down", "chevrons-up-down", "download"} {
		if !IconRegistered(name) {
			t.Errorf("icon %q is not registered", name)
		}
	}
}
