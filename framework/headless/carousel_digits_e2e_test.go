package headless

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCarouselUsesLocalizedTotal(t *testing.T) {
	locale := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	words := (&Strings{CarouselSlide: "الشريحة {{n}} من {{total}}"}).ForRequest(locale)
	carousel := Carousel(CarouselProps{ID: "slides", Label: "Slides", Strings: words,
		Slides: []CarouselSlide{{Content: render.Text("One")}, {Content: render.Text("Two")}, {Content: render.Text("Three")}},
	}, nil)
	if !strings.Contains(string(carousel), "الشريحة ١ من ٣") {
		t.Fatal("server did not localize both slide numbers")
	}
	server := startBehaviorServer(t, string(carousel))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("carousel module did not load")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.lang = 'ar-EG'`, nil),
		chromedp.Click(`#slides [data-hui-carousel-next]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	want := "الشريحة ٢ من ٣"
	for _, expression := range []string{
		`document.querySelector('#slides [data-hui-carousel-live]').textContent`,
		`document.querySelector('#slides [data-hui-carousel-status]').getAttribute('data-hui-carousel-status')`,
	} {
		if got := evalString(t, ctx, expression); got != want {
			t.Errorf("slide announcement = %q, want %q", got, want)
		}
	}
}
