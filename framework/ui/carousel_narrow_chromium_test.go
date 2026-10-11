//go:build chromium

package ui

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: a three-up testimonial carousel stayed three-up on a
// 375px phone, so each card was about 70px wide and its quote wrapped
// one word per line (caught screenshotting a landing page at phone
// width). VisiblePerView is the most a wide container shows: a narrow
// one drops to two, then one. This renders the same carousel in a
// phone-wide, a tablet-wide and a desktop-wide column and counts how
// many slides fit the track.
func TestCarouselDropsSlidesWhenNarrow(t *testing.T) {
	carousel := func(id string) render.HTML {
		slides := make([]CarouselSlide, 6)
		for i := range slides {
			slides[i] = CarouselSlide{Content: html.Paragraph(html.TextConfig{}, render.Text("Quote"))}
		}
		return Carousel(CarouselConfig{ID: id, Label: "Quotes", Slides: slides, VisiblePerView: 3})
	}
	page := `<div style="width:343px">` + string(carousel("phone")) + `</div>` +
		`<div style="width:560px">` + string(carousel("tablet")) + `</div>` +
		`<div style="width:1000px">` + string(carousel("desktop")) + `</div>`
	css := carouselStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}
%s</style>%s`, css, page)
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1200, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	// perView is the track's width over one slide's pitch (slide plus
	// gap), so a gap does not count as a sliver of another slide.
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const perView = id => {
				const track = document.querySelector('#' + id + ' .fui-carousel__track');
				const [a, b] = track.querySelectorAll('.fui-carousel__slide');
				const pitch = b.getBoundingClientRect().left - a.getBoundingClientRect().left;
				const gap = pitch - a.getBoundingClientRect().width;
				return (track.clientWidth + gap) / pitch;
			};
			return {phone: perView('phone'), tablet: perView('tablet'), desktop: perView('desktop')};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		want float64
	}{{"phone", 1}, {"tablet", 2}, {"desktop", 3}} {
		if math.Abs(m[c.name]-c.want) > 0.05 {
			t.Errorf("%s column shows %.2f slides per view, want %v", c.name, m[c.name], c.want)
		}
	}
}
