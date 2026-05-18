package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestToolbarRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Toolbar without Label should panic")
		}
	}()
	Toolbar(ToolbarConfig{Groups: []ToolbarGroup{{Children: []render.HTML{}}}}) // shouldn't matter — panics before
}

func TestToolbarRequiresAtLeastOneGroup(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Toolbar without Groups should panic")
		}
	}()
	Toolbar(ToolbarConfig{Label: "x"})
}

func TestToolbarEmitsRoleToolbar(t *testing.T) {
	h := string(Toolbar(ToolbarConfig{
		Label: "Format",
		Groups: []ToolbarGroup{
			{Label: "Text", Children: []render.HTML{Button(ButtonConfig{Label: "Bold"})}},
		},
	}))
	if !strings.Contains(h, `role="toolbar"`) {
		t.Errorf("Toolbar should have role=toolbar:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Format"`) {
		t.Errorf("Toolbar should have aria-label=Label:\n%s", h)
	}
}

func TestToolbarGroupRoleEmittedWhenLabeled(t *testing.T) {
	h := string(Toolbar(ToolbarConfig{
		Label: "x",
		Groups: []ToolbarGroup{
			{Label: "Inline", Children: []render.HTML{Button(ButtonConfig{Label: "B"})}},
			{Children: []render.HTML{Button(ButtonConfig{Label: "X"})}}, // no Label
		},
	}))
	if !strings.Contains(h, `role="group"`) {
		t.Errorf("labeled group should have role=group:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Inline"`) {
		t.Errorf("labeled group should have aria-label:\n%s", h)
	}
	// Unlabeled group should not have role=group (only one occurrence overall).
	if strings.Count(h, `role="group"`) != 1 {
		t.Errorf("only labeled groups should have role=group, got %d:\n%s",
			strings.Count(h, `role="group"`), h)
	}
}

func TestToolbarAlignVariantClass(t *testing.T) {
	for _, a := range []string{"center", "end", "between"} {
		h := string(Toolbar(ToolbarConfig{
			Label:  "x",
			Align:  a,
			Groups: []ToolbarGroup{{Children: []render.HTML{Button(ButtonConfig{Label: "Z"})}}},
		}))
		if !strings.Contains(h, "ui-toolbar--"+a) {
			t.Errorf("Align=%s should emit .ui-toolbar--%s:\n%s", a, a, h)
		}
	}
}

func TestToolbarRejectsUnknownAlign(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Toolbar with unknown Align should panic")
		}
	}()
	Toolbar(ToolbarConfig{
		Label: "x",
		Align: "bogus",
		Groups: []ToolbarGroup{{Children: []render.HTML{Button(ButtonConfig{Label: "Z"})}}},
	})
}
