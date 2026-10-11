//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// GridConfig.Min rides on data-min, and for a long time nothing read
// it: every grid laid out at the 16rem default, so a dashboard's four
// 12rem stat cards wrapped to three columns. This renders grids in
// Chrome and counts the painted columns through each path alone (typed
// attr() with the ladder stripped, the whole-rem ladder that serves
// the other browsers with the @supports block stripped) and both.
func TestGridMinSetsColumnCount(t *testing.T) {
	grid := func(id, min string) string {
		kids := make([]render.HTML, 8)
		for i := range kids {
			kids[i] = html.Div(html.DivConfig{}, render.Text("cell"))
		}
		return string(Grid(GridConfig{Min: min, ExtraAttrs: html.Attrs{"data-grid": id}}, kids...))
	}
	body := `<div style="width:1280px">` + grid("min12", "12rem") + grid("min30", "30rem") + grid("default", "") + `</div>` +
		`<div style="width:375px">` + grid("phone30", "30rem") + `</div>`

	full := layoutStyle.Entry().CSSFor(theme.Default())
	i := strings.Index(full, "@supports (width: attr(data-min")
	if i < 0 {
		t.Fatal("layout sheet lost its typed attr() block")
	}
	j := i + strings.Index(full[i:], "}\n}") + len("}\n}")
	ladderOnly := full[:i] + full[j:]
	var attrOnly strings.Builder
	for _, line := range strings.SplitAfter(full, "\n") {
		if !strings.Contains(line, `.fui-grid[data-min="`) {
			attrOnly.WriteString(line)
		}
	}

	for _, c := range []struct{ name, css string }{{"attr", attrOnly.String()}, {"ladder", ladderOnly}, {"both", full}} {
		t.Run(c.name, func(t *testing.T) {
			cols := renderGridColumns(t, c.css, body)
			// 1280px with an 8px gap: 12rem (192px) fits 6, 30rem (480px)
			// fits 2, the 16rem default fits 4, and a 30rem minimum on a
			// 375px column shrinks to one full-width track.
			want := map[string]float64{"min12": 6, "min30": 2, "default": 4, "phone30": 1}
			for id, n := range want {
				if cols[id] != n {
					t.Errorf("grid %s: %v columns, want %v", id, cols[id], n)
				}
			}
			if cols["phone30Overflow"] > 0.5 {
				t.Errorf("30rem grid overflows a 375px column by %.1fpx", cols["phone30Overflow"])
			}
		})
	}
}

func renderGridColumns(t *testing.T, css, body string) map[string]float64 {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}</style>
<style>%s</style>%s`, css, body)
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1400, 900))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const out = {};
			for (const g of document.querySelectorAll('[data-grid]')) {
				const top = g.children[0].getBoundingClientRect().top;
				out[g.dataset.grid] = [...g.children].filter(k => Math.abs(k.getBoundingClientRect().top - top) < 1).length;
			}
			const phone = document.querySelector('[data-grid="phone30"]');
			out.phone30Overflow = phone.scrollWidth - phone.clientWidth;
			return out;
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	return m
}
