package ui_test

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestIMEKeepsEscapeConsumers(t *testing.T) {
	for _, fixture := range []struct {
		name, body, module, selector, setup, kept, closed string
	}{
		{
			name: "section menu", body: string(interactive.SectionMenu(interactive.SectionMenuConfig{
				AriaLabel: "Trees", DrawerName: "trees", Filter: true,
				Groups: []interactive.SectionGroup{{Label: "Trees", Items: []interactive.SectionItem{{Label: "しらかば", Href: "/birch"}}}},
			})), module: "sectionmenu", selector: ".cui-section-menu__rail .cui-section-menu__filter-input",
			setup: `document.activeElement.value = 'しらかば'; document.activeElement.dispatchEvent(new Event('input', {bubbles: true}))`,
			kept:  `document.activeElement.value === 'しらかば'`, closed: `document.querySelector('.cui-section-menu__rail .cui-section-menu__filter-input').value === ''`,
		},
		{
			name: "search input", body: string(ui.SearchInput(ui.SearchInputConfig{
				ID: "ime", Name: "q", ExtraAttrs: map[string]string{"value": "しらかば"},
			})), module: "searchinput", selector: "#ime", setup: "true",
			kept: `document.getElementById('ime').value === 'しらかば'`, closed: `document.getElementById('ime').value === ''`,
		},
		{
			name: "clear helper", body: `<input id="ime" data-cui-clear-on-esc value="しらかば">`,
			module: "widgethelpers", selector: "#ime", setup: "true",
			kept: `document.getElementById('ime').value === 'しらかば'`, closed: `document.getElementById('ime').value === ''`,
		},
		{
			name: "dropdown", body: `<div data-cui-dropdown-wrap data-cui-dropdown-open><button data-cui-dropdown aria-expanded="true">Options</button><div id="panel" data-cui-dropdown-panel><input id="ime"></div></div>`,
			module: "dropdown", selector: "#ime", setup: "true",
			kept: `!document.getElementById('panel').hidden`, closed: `document.getElementById('panel').hidden`,
		},
		{
			name: "pane host", body: string(headless.PaneHost(headless.PaneHostProps{
				Primary: render.Text("List"), Secondary: render.Raw(`<input id="ime">`), SecondaryOpen: true,
			}, nil)), module: "headless-panehost", selector: "#ime", setup: "true",
			kept: `!document.querySelector('[data-hui-pane="secondary"]').hidden`, closed: `document.querySelector('[data-hui-pane="secondary"]').hidden`,
		},
		{
			name: "sortable", body: string(headless.SortableList(headless.SortableListProps{
				Label: "Trees", RPCPath: "/commit", Items: []headless.SortableItem{{Key: "birch", Label: "Birch"}},
			}, nil)), module: "headless-sortablelist", selector: `[data-hui-sort-key="birch"]`,
			setup: `document.activeElement.dispatchEvent(new KeyboardEvent('keydown', {key: ' ', bubbles: true, cancelable: true}))`,
			kept:  `document.activeElement.getAttribute('aria-grabbed') === 'true'`, closed: `document.querySelector('[data-hui-sort-key="birch"]').getAttribute('aria-grabbed') !== 'true'`,
		},
	} {
		for _, composition := range []string{`isComposing: true`, `isComposing: false, keyCode: 229`} {
			t.Run(fixture.name+"/"+composition, func(t *testing.T) {
				server := menuTriggerAxeServer(t, fixture.body)
				ctx := chromedptest.Context(t)
				width := int64(390)
				if fixture.name == "section menu" {
					width = 1280
				}
				var preserved bool
				if err := chromedp.Run(ctx,
					emulation.SetDeviceMetricsOverride(width, 844, 1, false),
					chromedp.Navigate(server.URL),
					chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.loadModule(%q)`, fixture.module), nil, chromedp.EvalAsValue),
					chromedp.Poll(fmt.Sprintf(`!!window.__gofastr.loadedModules[%q]`, fixture.module), nil),
					chromedp.Focus(fixture.selector), chromedp.Evaluate(fixture.setup, nil),
					chromedp.Poll(fixture.kept, nil),
					chromedp.Evaluate(fmt.Sprintf(`(() => {
						const event = new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true, %s});
						document.activeElement.dispatchEvent(event);
						return !event.defaultPrevented && (%s);
					})()`, composition, fixture.kept), &preserved),
				); err != nil {
					t.Fatal(err)
				}
				if !preserved {
					t.Fatal("Escape consumer answered an IME key")
				}
				if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape), chromedp.Poll(fixture.closed, nil)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
