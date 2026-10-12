package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The drawer body has no column padding: each nav row pads itself, so
// the title, Prepend and footer, which do not, sat flush against the
// drawer's edge while the rows' text started an inset in. Their text
// must start where the rows' text does, in the default and compact
// spellings.
func TestDrawerBodyTitleAlignsWithRows(t *testing.T) {
	for _, compact := range []bool{false, true} {
		cfg := SidebarConfig{
			Title:   "Meridian",
			Compact: compact,
			Prepend: app.NewStaticComponent(`<span id="pre">Section</span>`),
			Items:   []SidebarItem{{Label: "Overview", Href: "/app"}},
			Footer:  render.HTML(`<span id="foot">Sign out</span>`),
		}
		body := string(sidebarDrawerSlot{cfg: cfg}.Render())
		css := theme.Default().CSSCustomProperties() + sidebarStyle.Entry().CSSFor(theme.Default())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><style>body{margin:0}</style><style>%s</style>`+
				`<div id="drawer" style="width:320px">%s</div>`, css, body)
		}))
		ctx := chromedptest.Context(t)
		// Content-box left edge: border-box left plus the inline-start
		// padding and border, where the element's own text begins.
		var got map[string]float64
		err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL),
			chromedp.Evaluate(`(() => {
				const left = (el) => { const s = getComputedStyle(el);
					return el.getBoundingClientRect().left + parseFloat(s.paddingLeft) + parseFloat(s.borderLeftWidth); };
				const q = (s) => document.querySelector('#drawer ' + s);
				return {drawer: q('.fui-sidebar__title').parentElement.getBoundingClientRect().left,
					title: left(q('.fui-sidebar__title')), prepend: left(q('.fui-sidebar__prepend')),
					row: left(q('.fui-sidebar__link')), footer: left(q('.fui-sidebar__footer'))};
			})()`, &got),
		)
		srv.Close()
		if err != nil {
			t.Fatalf("compact=%v: %v", compact, err)
		}
		if got["row"] <= got["drawer"] {
			t.Fatalf("compact=%v: the row text starts at %.1f, not inside the drawer (%.1f); the page did not lay out", compact, got["row"], got["drawer"])
		}
		for _, part := range []string{"title", "prepend", "footer"} {
			if d := got[part] - got["row"]; d < -0.5 || d > 0.5 {
				t.Errorf("compact=%v: %s text starts at %.1fpx, the rows' text at %.1fpx", compact, part, got[part], got["row"])
			}
		}
	}
}
