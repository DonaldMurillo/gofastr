//go:build chromium

package interactive

import (
	"fmt"
	"github.com/chromedp/cdproto/runtime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// menuPage serves body with the menu's CSS and, when js is set, the
// sectionmenu behaviour evaluated after the markup the way the kernel
// would load it.
func menuPage(t *testing.T, body string, js bool) string {
	t.Helper()
	css := sectionMenuStyle.Entry().CSSFor(style.Theme{})
	script := ""
	if js {
		script = "<script>" + sectionMenuJS + "</script>"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;font-family:sans-serif}</style>
<style>%s</style>%s%s`, css, body, script)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// longMenu is a menu whose groups overflow a short column.
func longMenu() SectionMenuConfig {
	cfg := sampleMenu()
	cfg.Groups = nil
	for _, g := range []string{"Alpha", "Beta", "Gamma"} {
		var items []SectionItem
		for i := range 15 {
			items = append(items, SectionItem{Label: fmt.Sprintf("%s %02d", g, i), Href: fmt.Sprintf("/%s/%d", strings.ToLower(g), i)})
		}
		cfg.Groups = append(cfg.Groups, SectionGroup{Label: g, Items: items})
	}
	return cfg
}

// The desktop rail shows every group expanded; collapse is a sheet
// behaviour. The rail once forced closed <details> open with CSS, which
// broke when browsers hid closed content through ::details-content: a
// Collapsed group's links kept their space but painted nothing. The rail
// now renders no disclosures at all.
func TestSectionMenuRailShowsCollapsedGroups(t *testing.T) {
	cfg := sampleMenu()
	cfg.Groups = append(cfg.Groups, SectionGroup{Label: "Tucked", Collapsed: true, Items: []SectionItem{
		{Label: "Hidden one", Href: "/docs/hidden"},
	}})
	menu := SectionMenu(cfg)
	css := sectionMenuStyle.Entry().CSSFor(style.Theme{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;font-family:sans-serif}</style>
<style>%s</style>%s`, css, string(menu))
	}))
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	const painted = `(() => {
		const a = document.querySelector('.cui-section-menu__rail a[href="/docs/hidden"]');
		const r = a.getBoundingClientRect();
		if (r.height === 0) return false;
		return document.elementFromPoint(r.left + 30, r.top + r.height / 2) === a;
	})()`
	var shown, shownAfterClick bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(painted, &shown),
		// Clicking the rail group's header changes nothing.
		chromedp.Click(`.cui-section-menu__rail .cui-section-menu__group:last-child > .cui-section-menu__group-summary`, chromedp.ByQuery),
		chromedp.Evaluate(painted, &shownAfterClick),
	); err != nil {
		t.Fatal(err)
	}
	if !shown {
		t.Error("a Collapsed group's link is not painted in the desktop rail")
	}
	if !shownAfterClick {
		t.Error("after a click on the rail group's header, its link is no longer painted")
	}
}

// The rail swaps for the trigger at 900px unless SwapAt names a
// ContentRow breakpoint. In a row that stacks at 48rem, a 900px swap
// left a nav column holding only the trigger between 768 and 900px.
func TestSectionMenuSwapAtMatchesRow(t *testing.T) {
	for _, tc := range []struct {
		swap  SectionMenuSwap
		width int64
		rail  bool
	}{
		{SectionMenuSwapDefault, 840, false},
		{SectionMenuSwapDefault, 920, true},
		{SectionMenuSwapMD, 760, false},
		{SectionMenuSwapMD, 840, true},
		{SectionMenuSwapLG, 920, false},
		{SectionMenuSwapLG, 1040, true},
	} {
		cfg := sampleMenu()
		cfg.DrawerName = "sections"
		cfg.FillColumn = true
		cfg.SwapAt = tc.swap
		menu := SectionMenu(cfg)
		css := sectionMenuStyle.Entry().CSSFor(style.Theme{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><style>body{margin:0}</style><style>%s</style>%s`, css, string(menu))
		}))
		ctx := chromedptest.Context(t, chromedptest.WindowSize(int(tc.width), 800))
		var got map[string]bool
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL),
			chromedp.Evaluate(`(() => {
				const shown = sel => getComputedStyle(document.querySelector(sel)).display !== 'none';
				return {rail: shown('.cui-section-menu__rail'), trigger: shown('.cui-section-menu__trigger')};
			})()`, &got),
		); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		if got["rail"] != tc.rail || got["trigger"] == tc.rail {
			t.Errorf("SwapAt %q at %dpx: rail shown=%v, trigger shown=%v; want rail=%v", tc.swap, tc.width, got["rail"], got["trigger"], tc.rail)
		}
	}
}

// Headers and links sit inset from the rail's edge: a header 12px in
// (16px in a filled column, drawn edge to edge), and the lead and links
// 14px deeper, where the current page's dot sits.
func TestSectionMenuRailInsetsHeadsAndLinks(t *testing.T) {
	for _, tc := range []struct {
		fill bool
		head float64
	}{{false, 12}, {true, 16}} {
		cfg := sampleMenu()
		cfg.FillColumn = tc.fill
		url := menuPage(t, string(SectionMenu(cfg)), false)
		ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
		var m map[string]float64
		if err := chromedp.Run(ctx,
			chromedp.Navigate(url),
			chromedp.Evaluate(`(() => {
				const rail = document.querySelector('.cui-section-menu__rail');
				const edge = rail.getBoundingClientRect().left;
				const left = sel => {
					const el = rail.querySelector(sel);
					const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT,
						{acceptNode: n => n.textContent.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP});
					const r = document.createRange();
					r.selectNodeContents(walk.nextNode());
					return r.getBoundingClientRect().left - edge;
				};
				return {
					lead: left('.cui-section-menu__lead'),
					head: left('.cui-section-menu__group-summary'),
					link: left('.cui-section-menu__link'),
				};
			})()`, &m),
		); err != nil {
			t.Fatal(err)
		}
		if m["head"] != tc.head {
			t.Errorf("fill=%v: group header starts %.1fpx in, want %.0fpx", tc.fill, m["head"], tc.head)
		}
		if m["link"] != tc.head+14 || m["lead"] != m["link"] {
			t.Errorf("fill=%v: lead at %.1fpx and links at %.1fpx; want both %.0fpx, 14px under the header", tc.fill, m["lead"], m["link"], tc.head+14)
		}
	}
}

// The current page's dot sits on the inline-start side of its label, so
// a right-to-left rail draws it on the right, beside the text.
func TestSectionMenuDotFollowsRTL(t *testing.T) {
	url := menuPage(t, `<div dir="rtl" style="width:280px">`+string(SectionMenu(sampleMenu()))+`</div>`, false)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`(() => {
			const a = document.querySelector('.cui-section-menu__rail [aria-current="page"]');
			const s = getComputedStyle(a, '::before');
			return {right: parseFloat(s.right), left: parseFloat(s.left), w: parseFloat(s.width)};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["w"] != 6 || m["right"] != 13 || m["left"] < m["right"] {
		t.Errorf("rtl dot: width %.0fpx, right %.0fpx, left %.0fpx; want a 6px dot 13px from the right edge", m["w"], m["right"], m["left"])
	}
}

// A group's header pins to the top of the scrolling rail while its links
// pass under it, and the group above has let go of its own.
func TestSectionMenuGroupHeaderPins(t *testing.T) {
	cfg := longMenu()
	cfg.FillColumn = true
	url := menuPage(t, `<div style="height:420px;display:flex;flex-direction:column">`+string(SectionMenu(cfg))+`</div>`, false)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`(() => {
			const body = document.querySelector('.cui-section-menu__rail > .cui-section-menu__body');
			const groups = body.querySelectorAll('.cui-section-menu__group');
			body.scrollTop = groups[1].offsetTop - body.offsetTop + 200;
			const top = body.getBoundingClientRect().top;
			return {
				overflow: body.scrollHeight - body.clientHeight,
				beta: groups[1].querySelector('.cui-section-menu__group-summary').getBoundingClientRect().top - top,
				alpha: groups[0].querySelector('.cui-section-menu__group-summary').getBoundingClientRect().bottom - top,
			};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["overflow"] <= 0 {
		t.Fatalf("the rail body does not scroll (overflow %.0fpx); the check proves nothing", m["overflow"])
	}
	if m["beta"] < -0.5 || m["beta"] > 0.5 {
		t.Errorf("scrolled into Beta, its header sits %.1fpx below the rail's top; want it pinned at 0", m["beta"])
	}
	if m["alpha"] > 0.5 {
		t.Errorf("Alpha's header still shows (bottom %.1fpx below the top) after its group scrolled away", m["alpha"])
	}
}

// Below the swap the trigger is a pill floating at the bottom centre of
// the viewport; InlineTrigger keeps it in flow.
func TestSectionMenuPillFloats(t *testing.T) {
	for _, inline := range []bool{false, true} {
		cfg := sampleMenu()
		cfg.InlineTrigger = inline
		url := menuPage(t, `<p style="height:60px;margin:0">top</p>`+string(SectionMenu(cfg))+
			`<div style="height:1500px"></div><p id="end" style="margin:0">end</p>`, false)
		ctx := chromedptest.Context(t, chromedptest.WindowSize(600, 800))
		var m map[string]float64
		if err := chromedp.Run(ctx,
			chromedp.Navigate(url),
			chromedp.Evaluate(`(() => {
				const r = document.querySelector('.cui-section-menu__trigger').getBoundingClientRect();
				document.documentElement.dir = 'rtl';
				const rr = document.querySelector('.cui-section-menu__trigger').getBoundingClientRect();
				const rtl = rr.left + rr.width / 2 - innerWidth / 2;
				document.documentElement.dir = '';
				const pad = parseFloat(getComputedStyle(document.body).paddingBottom);
				scrollTo(0, document.documentElement.scrollHeight);
				const end = document.getElementById('end').getBoundingClientRect().bottom;
				const pill = document.querySelector('.cui-section-menu__trigger').getBoundingClientRect().top;
				return {top: r.top, bottom: innerHeight - r.bottom, centre: r.left + r.width / 2 - innerWidth / 2, rtl: rtl, pad: pad, clear: pill - end};
			})()`, &m),
		); err != nil {
			t.Fatal(err)
		}
		if inline {
			if m["top"] != 60 || m["pad"] != 0 {
				t.Errorf("inline trigger sits at %.1fpx, page padded %.1fpx; want 60px, in flow under the paragraph, no padding", m["top"], m["pad"])
			}
			continue
		}
		if m["bottom"] < 15.5 || m["bottom"] > 16.5 || m["centre"] < -1 || m["centre"] > 1 {
			t.Errorf("pill sits %.1fpx above the bottom, %.1fpx off centre; want 16px, centred", m["bottom"], m["centre"])
		}
		if m["rtl"] < -1 || m["rtl"] > 1 {
			t.Errorf("right-to-left pill sits %.1fpx off centre; want centred", m["rtl"])
		}
		// Scrolled to the end, the page's last line clears the pill.
		if m["clear"] < 8 {
			t.Errorf("at the page end the last line sits %.1fpx above the pill; want it clear by 8px or more", m["clear"])
		}
	}
}

// Typing in the filter hides every link whose label lacks the text and
// every group left empty, marks the match, shows the empty line when
// nothing is left, and Escape restores the menu.
func TestSectionMenuFilterHidesNonMatches(t *testing.T) {
	cfg := sampleMenu()
	cfg.Filter = true
	url := menuPage(t, string(SectionMenu(cfg)), true)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	const state = `(() => {
		const rail = document.querySelector('.cui-section-menu__rail');
		const shown = sel => [...rail.querySelectorAll(sel)].filter(e => e.getClientRects().length).map(e => e.textContent.trim());
		return {
			links: shown('.cui-section-menu__lead, .cui-section-menu__link').join('|'),
			groups: shown('.cui-section-menu__group-label').join('|'),
			marks: shown('.cui-section-menu__mark').join('|'),
			counts: shown('.cui-section-menu__count').join('|'),
			empty: !rail.querySelector('.cui-section-menu__empty').hidden,
		};
	})()`
	type view struct {
		Links, Groups, Marks, Counts string
		Empty                        bool
	}
	var typed, none, cleared view
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.SendKeys(`.cui-section-menu__rail .cui-section-menu__filter-input`, "dsl", chromedp.ByQuery),
		chromedp.Evaluate(state, &typed),
		chromedp.Evaluate(`(() => { const i = document.querySelector('.cui-section-menu__rail .cui-section-menu__filter-input'); i.value = ''; i.dispatchEvent(new Event('input')); })()`, nil),
		chromedp.SendKeys(`.cui-section-menu__rail .cui-section-menu__filter-input`, "zzz", chromedp.ByQuery),
		chromedp.Evaluate(state, &none),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(state, &cleared),
	); err != nil {
		t.Fatal(err)
	}
	if typed.Links != "Filter DSL" || typed.Groups != "Modeling" || typed.Marks != "DSL" || typed.Counts != "1/2" || typed.Empty {
		t.Errorf(`"dsl": %+v; want only Filter DSL under Modeling (1/2), DSL marked, no empty line`, typed)
	}
	if none.Links != "" || !none.Empty {
		t.Errorf(`"zzz": %+v; want no links and the empty line`, none)
	}
	if cleared.Links != "Overview|Entities|Filter DSL|HTTP" || cleared.Marks != "" || cleared.Counts != "2|1" || cleared.Empty {
		t.Errorf("after Escape: %+v; want every link back, unmarked", cleared)
	}
}

// The pill names the page the reader lands on after a client-side
// navigation, and falls back to the trigger label off the menu's pages.
func TestSectionMenuPillFollowsNavigation(t *testing.T) {
	url := menuPage(t, string(SectionMenu(sampleMenu())), true)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(600, 800))
	const nav = `((path, fresh) => {
		const root = fresh ? document.body : undefined;
		window.dispatchEvent(new CustomEvent('gofastr:navigate', {detail: {path, root}}));
		const b = document.querySelector('.cui-section-menu__trigger');
		return b.querySelector('.cui-section-menu__trigger-label').textContent + '|' + b.querySelector('.cui-section-menu__trigger-group').textContent;
	})`
	var first, http, off, fresh string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(nav+`('/docs/dsl')`, &first),
		chromedp.Evaluate(nav+`('/docs/http')`, &http),
		chromedp.Evaluate(nav+`('/elsewhere')`, &off),
		// A menu the navigation swapped in carries the server's mark
		// (Entities, Active on a URL its href does not spell).
		chromedp.Evaluate(nav+`('/docs/entities/42', true)`, &fresh),
	); err != nil {
		t.Fatal(err)
	}
	if first != "Filter DSL|Modeling" || http != "HTTP|Serving" {
		t.Errorf("pill after navigations: %q then %q; want Filter DSL|Modeling then HTTP|Serving", first, http)
	}
	if off != "Sections|" {
		t.Errorf("pill off the menu's pages: %q; want the trigger label alone", off)
	}
	if fresh != "Entities|Modeling" {
		t.Errorf("pill on a swapped-in menu: %q; want the server's mark, Entities|Modeling", fresh)
	}
}

// A menu the client cache restores comes back as HTML: the last
// filter's hidden items, marks and "n/total" counts, with an empty box.
// Wiring it settles the list to the empty box.
func TestSectionMenuCacheRestoreClearsFilter(t *testing.T) {
	cfg := sampleMenu()
	cfg.Filter = true
	url := menuPage(t, `<div id="slot">`+string(SectionMenu(cfg))+`</div>`, true)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var got map[string]string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.SendKeys(`.cui-section-menu__filter-input`, "dsl", chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const slot = document.getElementById('slot');
			slot.innerHTML = slot.innerHTML;
			window.__gofastr._moduleScanners.sectionmenu(slot);
			const rail = slot.querySelector('.cui-section-menu__rail');
			const shown = sel => [...rail.querySelectorAll(sel)].filter(e => e.getClientRects().length).map(e => e.textContent.trim()).join('|');
			const before = shown('.cui-section-menu__link');
			const i = rail.querySelector('.cui-section-menu__filter-input');
			i.value = 'http';
			i.dispatchEvent(new Event('input'));
			return {
				restored: before,
				marks: String(rail.querySelectorAll('.cui-section-menu__mark').length),
				counts: shown('.cui-section-menu__count'),
			};
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got["restored"] != "Entities|Filter DSL|HTTP" {
		t.Errorf("restored menu shows %q; want every link back", got["restored"])
	}
	if got["counts"] != "1/1" || got["marks"] != "1" {
		t.Errorf(`after filtering the restored menu for "http": counts %q, %s marks; want 1/1 and one mark`, got["counts"], got["marks"])
	}
}

// In the sheet, a filter opens the groups it matches in and Escape puts
// them back as they were. The first Escape clears the box and stops
// there; the next one reaches the sheet, which closes on it.
func TestSectionMenuSheetFilterRestoresGroups(t *testing.T) {
	cfg := sampleMenu()
	cfg.Filter = true
	url := menuPage(t, string(sectionMenuDrawerSlot{cfg: cfg}.Render())+
		`<script>window.escapes = 0; document.addEventListener('keydown', e => { if (e.key === 'Escape') window.escapes++; });</script>`, true)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	const open = `[...document.querySelectorAll('details.cui-section-menu__group')].map(d => d.open ? 'open' : 'shut').join('|')`
	var before, typed, cleared string
	var first, second int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`window.__gofastr._moduleScanners.sectionmenu(document.body)`, nil),
		chromedp.Evaluate(open, &before),
		chromedp.SendKeys(`.cui-section-menu__filter-input`, "http", chromedp.ByQuery),
		chromedp.Evaluate(open, &typed),
		chromedp.KeyEvent(""),
		chromedp.Evaluate(open, &cleared),
		chromedp.Evaluate(`window.escapes`, &first),
		chromedp.KeyEvent(""),
		chromedp.Evaluate(`window.escapes`, &second),
	); err != nil {
		t.Fatal(err)
	}
	if before != "open|shut" || typed != "open|open" || cleared != "open|shut" {
		t.Errorf("groups before/typing/after Escape: %s, %s, %s; want open|shut, open|open, open|shut", before, typed, cleared)
	}
	if first != 0 || second != 1 {
		t.Errorf("Escapes reaching the page: %d after clearing, %d after the next; want 0 then 1", first, second)
	}
}

// The sheet brings the current page into view below its pinned top bar
// and group header, and waits for the sheet's stylesheet to make it a
// scroller (here, three frames after it opens).
func TestSectionMenuSheetRevealsBelowChrome(t *testing.T) {
	cfg := longMenu()
	cfg.Filter = true
	// Mid-list, so the scroll lands where reveal puts it, not where the
	// list's end clamps it.
	cfg.Groups[1].Items[7].Href = "/here"
	url := menuPage(t, `<div id="box" style="height:320px;overflow:visible">`+string(sectionMenuDrawerSlot{cfg: cfg}.Render())+`</div>`, true)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/here"),
		chromedp.Evaluate(`new Promise(done => {
			const box = document.getElementById('box');
			document.dispatchEvent(new CustomEvent('fui:widget-open', {detail: {root: box}}));
			let n = 0;
			const tick = () => {
				if (++n === 3) box.style.overflow = 'auto';
				if (n < 40) return requestAnimationFrame(tick);
				const a = box.querySelector('a[href="/here"]').getBoundingClientRect();
				const bar = box.querySelector('.cui-section-menu__sheet-top').getBoundingClientRect();
				const head = box.querySelector('a[href="/here"]').closest('details').querySelector('summary').getBoundingClientRect();
				done({scrolled: box.scrollTop, gap: a.top - Math.max(bar.bottom, head.bottom), below: box.getBoundingClientRect().bottom - a.bottom});
			};
			requestAnimationFrame(tick);
		})`, &m, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatal(err)
	}
	if m["scrolled"] <= 0 {
		t.Fatalf("the sheet never scrolled to the current page (scrollTop %.0f)", m["scrolled"])
	}
	if m["gap"] < 0 || m["below"] < 8 {
		t.Errorf("current page sits %.1fpx below the pinned chrome and %.1fpx above the sheet's bottom; want it clear of both", m["gap"], m["below"])
	}
}
