//go:build chromium

package interactive

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A group label started 12px left of the lead link and flush with the
// rail's edge, so on a rail with no padding of its own (the docs
// site's components page) the labels touched the viewport edge while
// the lead and the links sat inset. The group label now starts where
// the lead does, and the group's links sit indented under it.
func TestSectionMenuGroupLabelAlignsWithLead(t *testing.T) {
	menu := SectionMenu(sampleMenu())
	css := sectionMenuStyle.Entry().CSSFor(style.Theme{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;font-family:sans-serif}</style>
<style>%s</style>%s`, css, string(menu))
	}))
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const rail = document.querySelector('.cui-section-menu__rail') || document;
			// Where an element's first painted text starts: padding,
			// borders and an eyebrow ahead of the label all count.
			const left = sel => {
				const el = rail.querySelector(sel);
				if (!el) return -1;
				const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT,
					{acceptNode: n => n.textContent.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP});
				const text = walk.nextNode();
				if (!text) return -1;
				const r = document.createRange();
				r.selectNodeContents(text);
				return r.getBoundingClientRect().left;
			};
			return {
				lead: left('.cui-section-menu__lead'),
				group: left('.cui-section-menu__group-summary'),
				link: left('.cui-section-menu__link'),
			};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["lead"] < 0 || m["group"] < 0 || m["link"] < 0 {
		t.Fatalf("rail parts missing: %v", m)
	}
	if m["group"] < m["lead"]-0.5 {
		t.Errorf("group label starts at %.1fpx, left of the lead at %.1fpx", m["group"], m["lead"])
	}
	if m["link"] <= m["group"] {
		t.Errorf("group links start at %.1fpx, not indented under the label at %.1fpx", m["link"], m["group"])
	}
}

// The desktop rail shows every group expanded; collapse is a drawer
// behaviour. It relied on display:block over a closed <details>, which
// stopped working once browsers hid closed content through
// ::details-content: a Collapsed group's links kept their space but
// painted nothing (the components page showed labels with empty gaps).
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
	var shown, shownAfterClick, closed bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(painted, &shown),
		// A reader clicking the rail group's label shuts the <details>;
		// the rail still shows its links.
		chromedp.Click(`.cui-section-menu__rail .cui-section-menu__group:last-child > summary`, chromedp.ByQuery),
		chromedp.Evaluate(`!document.querySelector('.cui-section-menu__rail .cui-section-menu__group:last-child').open`, &closed),
		chromedp.Evaluate(painted, &shownAfterClick),
	); err != nil {
		t.Fatal(err)
	}
	if !shown {
		t.Error("a Collapsed group's link is not painted in the desktop rail")
	}
	if !closed {
		t.Fatal("clicking the rail group's summary did not close it; the second check proves nothing")
	}
	if !shownAfterClick {
		t.Error("after a click shut the rail group, its link is no longer painted")
	}
}

// The hairline rule under a group hung off physical left properties, so
// in a right-to-left page it drew at the far left of the rail, a whole
// column away from the right-aligned links it groups (caught in review).
// The rule sits on the inline-start side, beside the links.
func TestSectionMenuRuleFollowsRTL(t *testing.T) {
	menu := SectionMenu(sampleMenu())
	css := sectionMenuStyle.Entry().CSSFor(style.Theme{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;font-family:sans-serif}</style>
<style>%s</style><div dir="rtl" style="width:280px">%s</div>`, css, string(menu))
	}))
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const s = getComputedStyle(document.querySelector('.cui-section-menu__list'));
			return {right: parseFloat(s.borderRightWidth), left: parseFloat(s.borderLeftWidth),
				indent: parseFloat(s.marginRight)};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["right"] != 1 || m["left"] != 0 {
		t.Errorf("rtl rule borders: right %.0fpx, left %.0fpx; want it on the right, the inline start", m["right"], m["left"])
	}
	if m["indent"] != 12 {
		t.Errorf("rtl list indent is %.0fpx on the right, want 12px", m["indent"])
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
