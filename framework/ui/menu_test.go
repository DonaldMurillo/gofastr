package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestMenuRendersTriggerAndItems(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{
			{Label: "Edit"},
			{Separator: true},
			{Label: "Delete", Danger: true, RPC: "/delete", RPCMethod: "POST"},
		},
	}))
	for _, want := range []string{
		`data-fui-comp="ui-menu"`,
		`data-fui-disclosure`,
		`data-fui-menu="`,
		`<summary`,
		`aria-haspopup="menu"`,
		`role="menu"`,
		`role="menuitem"`,
		`>Edit<`,
		`<hr class="ui-menu__sep" role="separator">`,
		`ui-menu__item--danger`,
		`data-fui-rpc="/delete"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Menu html missing %q\n--\n%s", want, out)
		}
	}
}

func TestMenuHrefRendersAnchor(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Go",
		Items: []ui.MenuItem{{Label: "Home", Href: "/"}},
	}))
	if !strings.Contains(out, `<a class="ui-menu__item" href="/"`) {
		t.Errorf("Href item should render as <a>:\n%s", out)
	}
}

func TestMenuPositionClass(t *testing.T) {
	cases := map[ui.MenuPosition]string{
		ui.MenuBottomEnd: "ui-menu--bottom-end",
		ui.MenuTopStart:  "ui-menu--top-start",
		ui.MenuTopEnd:    "ui-menu--top-end",
	}
	for pos, cls := range cases {
		out := string(ui.Menu(ui.MenuConfig{
			Label:    "x",
			Position: pos,
			Items:    []ui.MenuItem{{Label: "a"}},
		}))
		if !strings.Contains(out, cls) {
			t.Errorf("position %s should emit class %q\n%s", pos, cls, out)
		}
	}
}

func TestMenuCustomTriggerHTML(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		TriggerHTML: render.HTML(`<svg class="icon"></svg>`),
		Items:       []ui.MenuItem{{Label: "Settings"}},
	}))
	if !strings.Contains(out, `<svg class="icon"></svg>`) {
		t.Error("custom TriggerHTML not rendered")
	}
	if strings.Contains(out, `ui-menu__caret`) {
		t.Error("custom TriggerHTML should suppress default caret")
	}
}

func TestMenuBlocksSmuggledAttrKeys(t *testing.T) {
	// ExtraAttrs values are escaped, but escAttr does not touch spaces
	// — a key like `x onclick` must not render a live onclick attribute.
	// Keys go through the same allow-list as every other ExtraAttrs
	// consumer (core/render.Attr): on* handlers and syntactically
	// invalid names are dropped.
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{{
			Label: "Edit",
			ExtraAttrs: map[string]string{
				`x onclick`:   "alert(1)",
				"onmouseover": "alert(2)",
				"data-ok":     "yes",
			},
		}},
	}))
	if strings.Contains(out, "onclick") || strings.Contains(out, "onmouseover") || strings.Contains(out, "alert(") {
		t.Errorf("smuggled event-handler attr leaked into Menu html:\n%s", out)
	}
	if !strings.Contains(out, `data-ok="yes"`) {
		t.Errorf("legitimate ExtraAttrs key dropped:\n%s", out)
	}
}

func TestMenuPanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty Items")
		}
	}()
	_ = ui.Menu(ui.MenuConfig{Label: "x"})
}
