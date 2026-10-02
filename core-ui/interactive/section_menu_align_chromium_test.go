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
			const rail = document.querySelector('.fui-section-menu__rail') || document;
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
				lead: left('.fui-section-menu__lead'),
				group: left('.fui-section-menu__group-summary'),
				link: left('.fui-section-menu__link'),
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
		const a = document.querySelector('.fui-section-menu__rail a[href="/docs/hidden"]');
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
		chromedp.Click(`.fui-section-menu__rail .fui-section-menu__group:last-child > summary`, chromedp.ByQuery),
		chromedp.Evaluate(`!document.querySelector('.fui-section-menu__rail .fui-section-menu__group:last-child').open`, &closed),
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
