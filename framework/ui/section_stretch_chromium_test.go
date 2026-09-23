//go:build chromium

package ui

import (
	"context"
	"fmt"
	"math"
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

// Hard rule 9: a Section is a two-row grid (head, body). Two Sections side
// by side in a Grid share one row, so the shorter one stretches, and with
// the grid's default align-content the spare height was shared between its
// rows: the head floated far above the body (caught screenshotting the
// headless dashboard, whose invoice section sat beside a taller chart).
// align-content:start is the guard; this renders a real Grid in Chrome and
// measures the gap between the short section's description and its first body line.
func TestSectionHeadStaysOnBodyWhenStretched(t *testing.T) {
	tall := html.Paragraph(html.TextConfig{}, render.Text(strings.Repeat(
		"Long body copy that wraps onto many lines so this section sets the row height. ", 12)))
	short := func(id string) render.HTML {
		return Section(SectionConfig{ID: id, Heading: "Short", Description: "Stretched."},
			html.Paragraph(html.TextConfig{}, render.Text("One line.")))
	}
	page := render.HTML(string(Grid(GridConfig{Min: "12rem"},
		Section(SectionConfig{ID: "tall", Heading: "Tall", Description: "Sets the row."}, tall),
		short("short"),
	)) + string(short("solo")))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + sectionStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}
%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(800, 600))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	// span is the distance from a section's heading to its first body
	// line. The head and body elements fill their grid tracks, so their
	// own edges prove nothing; the text positions are what a reader sees.
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const box = id => document.getElementById(id).getBoundingClientRect();
			const span = id => {
				const s = document.getElementById(id);
				const h = s.querySelector('.fui-section__head').firstElementChild.getBoundingClientRect();
				const b = s.querySelector('.fui-section__body').firstElementChild.getBoundingClientRect();
				return b.top - h.top;
			};
			return {tallTop: box('tall').top, shortTop: box('short').top,
				tallH: box('tall').height, shortH: box('short').height,
				stretched: span('short'), solo: span('solo')};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	// Anti-vacuity: both grid sections share one row and the short one is
	// stretched to the tall one's height, or nothing was exercised.
	if math.Abs(m["tallTop"]-m["shortTop"]) > 1 || math.Abs(m["tallH"]-m["shortH"]) > 1 {
		t.Fatalf("sections not stretched in one row (tops %.1f/%.1f, heights %.1f/%.1f); widen the viewport",
			m["tallTop"], m["shortTop"], m["tallH"], m["shortH"])
	}
	if math.Abs(m["stretched"]-m["solo"]) > 1 {
		t.Errorf("stretched section: heading to body is %.1fpx, want %.1fpx as in an unstretched one", m["stretched"], m["solo"])
	}
}
