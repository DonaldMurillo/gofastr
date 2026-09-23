package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderMenu(p MenuProps) string { return string(Menu(p, nil)) }

func TestMenuRendersTheDisclosureContract(t *testing.T) {
	h := renderMenu(MenuProps{ID: "acct", Label: "Account", Items: []MenuItem{
		{Label: "Profile", Href: "/me"},
		{Label: "Delete", RPC: "/api/del", RPCMethod: "DELETE", Confirm: "Really?", Danger: true},
	}})
	for _, want := range []string{
		`<details data-hui-disclosure="" data-hui-menu="acct">`,
		`<summary aria-controls="acct-panel" aria-haspopup="menu">`,
		`<div data-hui-menu-panel="" id="acct-panel" role="menu">`,
		`<a href="/me" role="menuitem" tabindex="-1">`,
		`<button data-fui-confirm="Really?" data-fui-rpc="/api/del" data-fui-rpc-method="DELETE" role="menuitem" tabindex="-1" type="button">`,
		`aria-hidden="true">▾<`, // the caret says the activation opens a list
	} {
		if !strings.Contains(h, want) {
			t.Errorf("menu missing %q:\n%s", want, h)
		}
	}
	// Every row starts out of the tab order: the module's roving focus
	// owns which one is in it.
	if strings.Contains(h, `tabindex="0"`) {
		t.Errorf("a menu row joined the document tab order:\n%s", h)
	}
}

func TestMenuSubmenuAndRadioRows(t *testing.T) {
	h := renderMenu(MenuProps{ID: "theme", Label: "Theme", Items: []MenuItem{
		{Label: "Palette", ID: "palette-row", Children: []MenuItem{
			{Label: "Light", Radio: "theme"},
			{Label: "Dark", Radio: "theme", Checked: true},
		}},
	}})
	for _, want := range []string{
		`<details data-hui-disclosure="" data-hui-menu="theme-panel-sub-0">`,
		`<summary aria-controls="theme-panel-sub-0-panel" aria-haspopup="menu" id="palette-row" role="menuitem" tabindex="-1">`,
		`aria-checked="false" data-hui-menu-radio="theme" role="menuitemradio"`,
		`aria-checked="true" data-hui-menu-radio="theme" role="menuitemradio"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("submenu missing %q:\n%s", want, h)
		}
	}
}

func TestMenuActionRowsRenderForms(t *testing.T) {
	h := renderMenu(MenuProps{Label: "Admin", Items: []MenuItem{
		{Label: "Stop", Action: &MenuAction{Path: "/stop", Fields: map[string]string{"csrf": "t"}}},
	}})
	for _, want := range []string{
		`<form action="/stop" method="POST" role="none">`,
		`<input name="csrf" type="hidden" value="t">`,
		`role="menuitem" tabindex="-1" type="submit"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("action row missing %q:\n%s", want, h)
		}
	}
	// A rejected action path degrades to the inert "#".
	evil := renderMenu(MenuProps{Label: "x", Items: []MenuItem{
		{Label: "Go", Action: &MenuAction{Path: "javascript:alert(1)", Unsafe: true}},
	}})
	if strings.Contains(evil, `action="javascript:`) {
		t.Errorf("an unsafe action path reached the form:\n%s", evil)
	}
	if !strings.Contains(evil, `action="#"`) {
		t.Errorf("the rejected path did not degrade to #:\n%s", evil)
	}
}

func TestMenuTriggerElementPairsByHook(t *testing.T) {
	h := renderMenu(MenuProps{ID: "um", TriggerElement: render.HTML(`<button type="button">U</button>`), Items: []MenuItem{
		{Label: "Row"},
	}})
	for _, want := range []string{
		`<div><div data-hui-menu-trigger="um" role="presentation"><button type="button">U</button></div>`,
		`<details data-hui-disclosure="" data-hui-menu="um">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("trigger menu missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "<summary") {
		t.Errorf("the trigger path rendered a summary — the caller's element is the controller:\n%s", h)
	}
}

func TestMenuAutoIDSeparatesDistinctMenus(t *testing.T) {
	items := []MenuItem{{Label: "x"}}
	a := renderMenu(MenuProps{TriggerElement: render.HTML(`<button type="button" class="a">A</button>`), Items: items})
	b := renderMenu(MenuProps{TriggerElement: render.HTML(`<button type="button" class="b">B</button>`), Items: items})
	idA := menuIDOf(t, a)
	idB := menuIDOf(t, b)
	if idA == "" || idB == "" || idA == idB {
		t.Fatalf("distinct triggers must not share a menu id: %q vs %q", idA, idB)
	}
}

func menuIDOf(t *testing.T, h string) string {
	t.Helper()
	i := strings.Index(h, `data-hui-menu-trigger="`)
	if i < 0 {
		return ""
	}
	rest := h[i+len(`data-hui-menu-trigger="`):]
	return rest[:strings.Index(rest, `"`)]
}

func TestMenuRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    MenuProps
	}{

		{"no items", MenuProps{Label: "x"}},
		{"whitespace-only trigger label", MenuProps{Label: "  ", Items: []MenuItem{{Label: "x"}}}},
		{"whitespace-only row label", MenuProps{Label: "x", Items: []MenuItem{{Label: "  "}}}},
		{"row without a label", MenuProps{Label: "x", Items: []MenuItem{{}}}},
		{"radio with children", MenuProps{Label: "x", Items: []MenuItem{{Label: "p", Radio: "g", Children: []MenuItem{{Label: "in"}}}}}},
		{"href with children", MenuProps{Label: "x", Items: []MenuItem{{Label: "p", Href: "/x", Children: []MenuItem{{Label: "in"}}}}}},
		{"action with radio", MenuProps{Label: "x", Items: []MenuItem{{Label: "p", Radio: "g", Action: &MenuAction{Path: "/x", Unsafe: true}}}}},
		{"action without fields", MenuProps{Label: "x", Items: []MenuItem{{Label: "p", Action: &MenuAction{Path: "/x"}}}}},
		{"duplicate row ids", MenuProps{Label: "x", Items: []MenuItem{
			{Label: "a", ID: "dup"}, {Label: "b", ID: "dup"},
		}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Menu(tc.p, nil)
		}()
	}
}

func TestMenuScrubbedCarriedLabels(t *testing.T) {
	h := renderMenu(MenuProps{Label: "x", Items: []MenuItem{{Label: "Pro\r\nfile", Href: "/p"}}})
	if !strings.Contains(h, ">Profile<") {
		t.Errorf("a CR LF in a carried label reached the reader:\n%s", h)
	}
}

func renderDisclosure(p DisclosureProps) string { return string(Disclosure(p, nil)) }

func TestDisclosureRendersNativeDetails(t *testing.T) {
	h := renderDisclosure(DisclosureProps{
		Summary: render.Text("Advanced"),
		Content: render.Text("The panel."),
	})
	for _, want := range []string{
		`<details data-hui-disclosure="">`,
		`<summary>Advanced</summary>`,
		`<div>The panel.</div>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("disclosure missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "aria-expanded") {
		t.Errorf("the server renders no expanded state — the module mirrors it on arrival:\n%s", h)
	}
}

func TestDisclosureOptInMarkers(t *testing.T) {
	h := renderDisclosure(DisclosureProps{Summary: render.Text("S"), Content: render.Text("C"),
		Open: true, Trap: true, PersistKey: "nav.section"})
	for _, want := range []string{
		`data-hui-disclosure-trap=""`,
		`data-hui-disclosure-persist="nav.section"`,
		` open=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("disclosure missing %q:\n%s", want, h)
		}
	}
}

func TestDisclosureRefusesBrokenConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    DisclosureProps
	}{
		{"no summary", DisclosureProps{Content: render.Text("c")}},
		{"blank summary", DisclosureProps{Summary: render.Text("   ")}},
		{"blank persist key", DisclosureProps{Summary: render.Text("s"), PersistKey: " "}},
		{"control bytes in persist key", DisclosureProps{Summary: render.Text("s"), PersistKey: "a\r\nb"}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Disclosure(tc.p, nil)
		}()
	}
}

func TestDisclosureNameMakesAnExclusiveGroup(t *testing.T) {
	h := renderDisclosure(DisclosureProps{Name: "faq", Summary: render.Text("Q1"), Content: render.Text("A1")})
	if !strings.Contains(h, `<details data-hui-disclosure="" name="faq">`) {
		t.Errorf("the group name never reached the details element:\n%s", h)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a control byte in Name was accepted")
			}
		}()
		Disclosure(DisclosureProps{Name: "a\r\nb", Summary: render.Text("s")}, nil)
	}()
	// A caller cannot smuggle a name through ExtraAttrs.
	smuggled := string(Disclosure(DisclosureProps{Summary: render.Text("s"),
		ExtraAttrs: html.Attrs{"name": "evil"}}, nil))
	if strings.Contains(smuggled, `name="evil"`) {
		t.Errorf("a caller forged the group name:\n%s", smuggled)
	}
}
