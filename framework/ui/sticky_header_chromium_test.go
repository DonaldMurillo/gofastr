package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
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
	wrap := func(id string, h render.HTML) render.HTML { return render.Tag("div", map[string]string{"id": id}, h) }
	parts := []render.HTML{
		wrap("plain", rail), knob, menu,
		wrap("toc", ui.TableOfContents(ui.TOCConfig{Sticky: true, Items: []ui.TOCItem{{ID: "a", Label: "A"}}})),
		wrap("anchored", ui.AnchoredRail(ui.AnchoredRailConfig{Label: "Rail", Items: []ui.RailItem{{Anchor: "a", Text: "A"}}})),
		wrap("search", ui.GlobalSearch(ui.GlobalSearchConfig{ID: "gs", Name: "q", Label: "Search", RPCPath: "/search", SignalName: "results", NoScriptAction: "/search", Sticky: true})),
		wrap("retry", ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{HealthEndpoint: "/health"})),
		wrap("sticky", ui.Sticky(ui.StickyConfig{}, render.Text("Pinned"))),
		wrap("stickylg", ui.Sticky(ui.StickyConfig{Offset: ui.StickyOffsetLg}, render.Text("Pinned lg"))),
		wrap("bounded", ui.Box(ui.BoxConfig{Bounded: true}, render.Text("Window"))),
		ui.Stack(ui.StackConfig{}, paras...),
	}
	page := ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true, Gap: ui.GapNone},
		render.Tag("div", map[string]string{"id": "strip"}, render.Text("Announcement")),
		html.Header(html.HeaderConfig{Banner: true, Class: "bar"}, render.Text("Header")),
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, Sidebar: render.Text("Nav"), Toolbar: render.Text("Trail")},
			html.Main(html.MainConfig{}, ui.Grid(ui.GridConfig{Min: "4rem"}, parts...))))
	srv := themeTestPageWithHead(t, head, string(page))
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1280, 800))
	var result string
	probe := `(() => {
		const q = s => document.querySelector(s), top = e => Math.round(e.getBoundingClientRect().top);
		const px = v => { const d = document.createElement('div'); d.style.height = v; document.body.appendChild(d); const h = d.getBoundingClientRect().height; d.remove(); return Math.round(h); };
		const hh = px('var(--size-header-height)'), gap = px('var(--spacing-xl)');
		q('#retry [data-cui-comp="ui-network-retry-banner"]').hidden = false;
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
			'table of contents': [q('#toc nav'), bar + px('var(--spacing-lg)')],
			'anchored rail': [q('#anchored [data-cui-comp="ui-anchored-rail"]'), bar + px('var(--spacing-lg)')],
			'global search': [q('#search [data-cui-comp="ui-global-search"]'), bar + px('var(--spacing-md)')],
			'network retry banner': [q('#retry [data-cui-comp="ui-network-retry-banner"]'), bar],
			'sticky': [q('#sticky [data-cui-comp="ui-sticky"]'), bar],
			'sticky with an offset': [q('#stickylg [data-cui-comp="ui-sticky"]'), bar + px('var(--spacing-lg)')],
		};
		for (const [name, [el, at]] of Object.entries(want)) {
			if (top(el) !== at) return name + ' sticks at ' + top(el) + 'px; want ' + at + 'px under the ' + hh + 'px header';
		}
		// A region that scrolls on its own pins its sticky parts to its
		// own top, not under the page header.
		for (const [name, el] of [['bounded box', q('#bounded [data-cui-comp="ui-layout"]')], ['pinned nav column', q('.fui-content-row__nav')]]) {
			const v = getComputedStyle(el).getPropertyValue('--ui-sticky-top').trim();
			if (v !== '0px') return name + ' passes --ui-sticky-top ' + v + ' to its sticky parts; want 0px';
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
