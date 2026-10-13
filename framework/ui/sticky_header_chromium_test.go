package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A page frame with StickyHeader pins its banner and tells every sticky
// part below it, however deep, where the header ends: the layout says
// the header is sticky once and no component takes a flag for it. A
// strip above the header scrolls away; the header pins at the top.
func TestStackStickyHeaderOffsetsStickyParts(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>body{margin:0}" + th.CSSCustomProperties() + "\n" + css.String() +
		"\n.bar{block-size:var(--size-header-height);background:#fff}</style>"
	paras := make([]render.HTML, 120)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d", i)))
	}
	rail := ui.StepRail(ui.StepRailConfig{Items: []ui.StepRailItem{{Number: "01", Anchor: "a", Label: "A"}}, ActiveIndex: -1})
	knob := render.Tag("div", map[string]string{"id": "knob", "style": "--ui-step-rail-top: 4px"}, rail)
	menu := interactive.SectionMenu(interactive.SectionMenuConfig{
		Groups: []interactive.SectionGroup{{Label: "All", Items: []interactive.SectionItem{{Label: "One", Href: "#one"}}}}})
	page := ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true, Gap: ui.GapNone},
		render.Tag("div", map[string]string{"id": "strip"}, render.Text("Announcement")),
		html.Header(html.HeaderConfig{Banner: true, Class: "bar"}, render.Text("Header")),
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, Sidebar: render.Text("Nav"), Toolbar: render.Text("Trail")},
			html.Main(html.MainConfig{}, ui.Grid(ui.GridConfig{Min: "10rem"},
				render.Tag("div", map[string]string{"id": "plain"}, rail), knob, menu,
				ui.Stack(ui.StackConfig{}, paras...)))))
	srv := themeTestPageWithHead(t, head, string(page))
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1280, 800))
	var result string
	probe := `(() => {
		const q = s => document.querySelector(s), top = e => Math.round(e.getBoundingClientRect().top);
		const px = v => { const d = document.createElement('div'); d.style.height = v; document.body.appendChild(d); const h = d.getBoundingClientRect().height; d.remove(); return Math.round(h); };
		const hh = px('var(--size-header-height)'), gap = px('var(--spacing-xl)');
		scrollTo(0, 2000);
		if (scrollY < 1000) return 'the window did not scroll';
		if (top(q('#strip')) >= 0) return 'the strip above the header did not scroll away';
		const bar = Math.round(q('.fui-content-row__toolbar').getBoundingClientRect().bottom);
		const want = {
			'header': [q('header'), 0],
			'nav column': [q('.fui-content-row__nav'), hh],
			'toolbar': [q('.fui-content-row__toolbar'), hh],
			// Under the row's sticky toolbar, the parts below clear it too.
			'step rail': [q('#plain [data-cui-comp="ui-step-rail"]'), bar + gap],
			'step rail with its knob': [q('#knob [data-cui-comp="ui-step-rail"]'), bar + 4],
			'section menu rail': [q('.cui-section-menu__rail'), bar + 16],
		};
		for (const [name, [el, at]] of Object.entries(want)) {
			if (top(el) !== at) return name + ' sticks at ' + top(el) + 'px; want ' + at + 'px under the ' + hh + 'px header';
		}
		return '';
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &result)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result != "" {
		t.Error(result)
	}
}
