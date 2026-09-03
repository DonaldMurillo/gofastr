package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestMenuLazyPanelWrapsRowsInTemplate: LazyPanel wraps the panel's
// rows in an inert <template data-fui-menu-lazy> as the panel's ONLY
// child, on the summary path and the TriggerElement path alike. The
// panel <div> itself must stay (aria-controls resolves while closed),
// and nothing may sit between the panel open tag and the template or
// between the template close and the panel close. Nested submenu
// markup renders INSIDE the template, so it mounts with the rest.
func TestMenuLazyPanelWrapsRowsInTemplate(t *testing.T) {
	items := []ui.MenuItem{
		{Label: "Profile", Href: "/me"},
		{Label: "Palette", Children: []ui.MenuItem{
			{Label: "Light", Radio: "theme"},
			{Label: "Dark", Radio: "theme", Checked: true},
		}},
	}

	summary := string(ui.Menu(ui.MenuConfig{ID: "lm", Label: "View", LazyPanel: true, Items: items}))
	for _, want := range []string{
		// Panel div intact: id, role, marker, and the template as its
		// only child — bytes on both sides of the rows.
		`<div class="ui-menu__panel" id="lm-panel" role="menu" data-fui-menu-panel><template data-fui-menu-lazy>`,
		`</template></div></details>`,
		// Nested submenu lives inside the template.
		`<template data-fui-menu-lazy><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1">`,
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary path missing %q\n--\n%s", want, summary)
		}
	}
	if n := strings.Count(summary, "<template"); n != 1 {
		t.Errorf("summary path rendered %d templates, want exactly 1 (only the top panel is lazy)\n--\n%s", n, summary)
	}

	trigger := string(ui.Menu(ui.MenuConfig{
		ID:             "lmt",
		TriggerElement: render.HTML(`<button type="button" class="rounded-full">Open</button>`),
		LazyPanel:      true,
		Items:          items,
	}))
	for _, want := range []string{
		`<div class="ui-menu__panel" id="lmt-panel" role="menu" data-fui-menu-panel><template data-fui-menu-lazy>`,
		`</template></div></details></div>`,
	} {
		if !strings.Contains(trigger, want) {
			t.Errorf("trigger path missing %q\n--\n%s", want, trigger)
		}
	}
}

// TestMenuLazyPanelFalseIsByteIdentical: LazyPanel's zero value and an
// explicit false must render the exact bytes the component rendered
// before the field existed. The golden fixtures in menu_test.go are
// main's capture; this re-renders every one with an explicit false and
// re-asserts against them (no re-pinning), plus a direct no-template
// check.
func TestMenuLazyPanelFalseIsByteIdentical(t *testing.T) {
	for _, g := range goldenMenus {
		t.Run(g.name, func(t *testing.T) {
			cfg := g.cfg
			cfg.LazyPanel = false
			if got := string(ui.Menu(cfg)); got != g.want {
				t.Errorf("LazyPanel: false drifted from main bytes:\n--got--\n%s\n--want--\n%s", got, g.want)
			}
			if got := string(ui.Menu(cfg)); strings.Contains(got, "<template") {
				t.Errorf("LazyPanel: false emitted a template:\n%s", got)
			}
		})
	}
}
