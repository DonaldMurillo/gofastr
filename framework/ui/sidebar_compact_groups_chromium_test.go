//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A compact docs rail with several groups read as one flat list: the
// group's summary wore the link style, and the compact sublist dropped
// its indent, so "Getting started" looked like a sibling of the
// articles under it (a help-center agent in the third layout eval hit
// it with one group per section). A group label now reads as a label
// (heavier than its links) and its links sit indented under it.
func TestCompactSidebarGroupsShowHierarchy(t *testing.T) {
	nav, err := component.SafeRenderCtx(context.Background(), Sidebar(SidebarConfig{
		NavLabel: "Help", Compact: true, CurrentPath: "/help/b",
		Items: []SidebarItem{
			{Label: "Getting started", Open: true, Children: []SidebarItem{
				{Label: "Article A", Href: "/help/a"},
				{Label: "Article B", Href: "/help/b"},
			}},
			{Label: "Account", Open: true, Children: []SidebarItem{
				{Label: "Article C", Href: "/help/c"},
			}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	th := theme.Default()
	css := th.CSSCustomProperties() + sidebarStyle.Entry().CSSFor(th)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;font-family:sans-serif}.rail{width:260px}</style>
<style>%s</style><div class="rail">%s</div>`, css, string(nav))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1280, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const inline = document.querySelector('.fui-sidebar__inline');
			const sum = inline.querySelector('.fui-sidebar__group > summary');
			const label = el => el.querySelector('.fui-sidebar__label');
			const child = inline.querySelector('.fui-sidebar__sublist a[href="/help/a"]');
			const cur = inline.querySelector('.fui-sidebar__sublist a[aria-current="page"]');
			return {
				sumWeight: parseFloat(getComputedStyle(sum).fontWeight),
				childWeight: parseFloat(getComputedStyle(child).fontWeight),
				sumLeft: label(sum).getBoundingClientRect().left,
				childLeft: label(child).getBoundingClientRect().left,
				childWidth: child.getBoundingClientRect().width,
				curFound: cur ? 1 : 0,
			};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["childWidth"] < 20 || m["curFound"] != 1 {
		t.Fatalf("rail did not lay out (child width %.0f, current link found %.0f)", m["childWidth"], m["curFound"])
	}
	if m["sumWeight"] <= m["childWeight"] {
		t.Errorf("group label weight %.0f is not heavier than its links (%.0f); groups read as links", m["sumWeight"], m["childWeight"])
	}
	if m["childLeft"]-m["sumLeft"] < 8 {
		t.Errorf("a group's link text starts %.1fpx right of the group label; want an indent of at least 8px", m["childLeft"]-m["sumLeft"])
	}
}
