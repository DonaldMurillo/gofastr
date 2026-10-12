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

// A compact Sidebar fills the column it is placed in, however narrow.
// The docpage package puts one in a 13rem (208px) grid column; the
// inline column kept the full sidebar's 220px minimum, so it spilled
// 12px past the column, the column clipped it, and a long article
// title was cut mid-letter instead of wrapping.
func TestCompactSidebarFitsNarrowColumn(t *testing.T) {
	nav, err := component.SafeRenderCtx(context.Background(), Sidebar(SidebarConfig{
		NavLabel: "Help", Compact: true, CurrentPath: "/help/long",
		Items: []SidebarItem{
			{Label: "Articles", Open: true, Children: []SidebarItem{
				{Label: "Organizing work into projects and shared team spaces", Href: "/help/long"},
				{Label: "Short", Href: "/help/short"},
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
<style>body{margin:0;font-family:sans-serif}.rail{width:13rem;overflow:hidden}</style>
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
			const rail = document.querySelector('.rail').getBoundingClientRect();
			const inline = document.querySelector('.fui-sidebar__inline').getBoundingClientRect();
			const label = document.querySelector('a[href="/help/long"] .fui-sidebar__label');
			const r = label.getBoundingClientRect();
			const line = parseFloat(getComputedStyle(label).lineHeight) || 20;
			return {railRight: rail.right, inlineRight: inline.right, labelRight: r.right,
				labelWidth: r.width, lines: Math.round(r.height / line)};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	if m["labelWidth"] < 20 {
		t.Fatalf("rail did not lay out (label width %.0f)", m["labelWidth"])
	}
	if m["inlineRight"] > m["railRight"]+0.5 {
		t.Errorf("the compact sidebar spills %.0fpx past its 13rem column", m["inlineRight"]-m["railRight"])
	}
	if m["labelRight"] > m["railRight"]+0.5 {
		t.Errorf("a long link label runs %.0fpx past the column and is clipped; it should wrap", m["labelRight"]-m["railRight"])
	}
	if m["lines"] < 2 {
		t.Errorf("the long label sits on %.0f line(s); at 13rem it should wrap", m["lines"])
	}
}
