package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

type railPrepend struct{ html render.HTML }

func (p railPrepend) Render() render.HTML { return p.html }

// The collapsed rail keeps the brand's logo tile at its head, the way
// shadcn's icon sidebar keeps the team mark, and drops the name beside
// it; any other Prepend still gives way. Each nav group opens under a
// rule, so the rail reads as groups without their labels.
func TestCollapsedRailKeepsBrandTile(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>body{margin:0}" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	rail := func(id string, prepend render.HTML) string {
		return `<div id="` + id + `">` + string(component.RenderComponent(ui.Sidebar(ui.SidebarConfig{
			NavLabel: "Main",
			Variant:  ui.SidebarCollapsible,
			Collapse: ui.SidebarCollapseCollapsed,
			Prepend:  railPrepend{prepend},
			Items: []ui.SidebarItem{
				{Label: "Dashboard", Href: "/", Icon: ui.Icon("home", ui.IconConfig{})},
				{Label: "Billing", Open: true, Children: []ui.SidebarItem{
					{Label: "Invoices", Href: "/invoices", Icon: ui.Icon("file", ui.IconConfig{})},
				}},
			},
		}))) + `</div>`
	}
	body := rail("brand", ui.SidebarBrand(ui.SidebarBrandConfig{Name: "Meridian", Sub: "Back office"})) +
		rail("other", render.HTML(`<p class="other">Workspace switcher</p>`))
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1280, 800))
	var got map[string]any
	probe := `(() => {
		const q = s => document.querySelector(s);
		const tile = q("#brand .fui-sidebar__inline .fui-sidebar-brand__tile").getBoundingClientRect();
		const col = q("#brand .fui-sidebar__inline").getBoundingClientRect();
		const group = q("#brand .fui-sidebar__inline .fui-sidebar__group").closest(".fui-sidebar__item");
		return {
			tileW: tile.width, tileIn: tile.left >= col.left && tile.right <= col.right,
			textShown: q("#brand .fui-sidebar__inline .fui-sidebar-brand__text").getBoundingClientRect().width > 1,
			otherShown: q("#other .fui-sidebar__inline .other").checkVisibility(),
			rule: parseFloat(getComputedStyle(group).borderTopWidth),
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if w, _ := got["tileW"].(float64); w < 20 || got["tileIn"] != true {
		t.Errorf("the brand tile is not drawn inside the rail: %v", got)
	}
	if got["textShown"] == true {
		t.Errorf("the brand's name squeezes into the rail: %v", got)
	}
	if got["otherShown"] == true {
		t.Errorf("a Prepend that is not a brand stays in the rail: %v", got)
	}
	if r, _ := got["rule"].(float64); r < 1 {
		t.Errorf("a nav group opens without a rule in the rail: %v", got)
	}
}
