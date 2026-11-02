package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The carousel's slide status sentence was written only into the
// track's data-hui-carousel-status attribute. No element was a live
// region, so a step, a dot or an auto-rotation was announced to
// nobody. The live region renders polite, and quiet (aria-live="off")
// for a carousel that rotates on its own, the APG carousel pattern.
func TestCarouselHasALiveStatus(t *testing.T) {
	slides := []CarouselSlide{{Content: render.Text("One")}, {Content: render.Text("Two")}}
	live := regexp.MustCompile(`<span[^>]*data-hui-carousel-live=""[^>]*>([^<]*)</span>`)
	h := string(Carousel(CarouselProps{Label: "Featured", Slides: slides}, nil))
	m := live.FindStringSubmatch(h)
	if m == nil || !strings.Contains(m[0], `aria-live="polite"`) || !strings.Contains(m[0], `aria-atomic="true"`) {
		t.Fatalf("no polite live region carries the slide status:\n%s", h)
	}
	if m[1] != "Slide 1 of 2" {
		t.Fatalf("the live region starts with %q, want the server's sentence", m[1])
	}
	r := live.FindString(string(Carousel(CarouselProps{Label: "Featured", Slides: slides, AutoRotateMS: 5000}, nil)))
	if !strings.Contains(r, `aria-live="off"`) {
		t.Fatalf("a rotating carousel's live region is not quiet: %s", r)
	}
}

// The browser proof: the reader's own step is said through the polite
// region; an automatic step updates the sentence in a quiet one.
func TestE2E_CarouselSaysTheSlideThroughTheLiveRegion(t *testing.T) {
	slides := []CarouselSlide{{Content: render.Text("One")}, {Content: render.Text("Two")}, {Content: render.Text("Three")}}
	body := string(Carousel(CarouselProps{Label: "Steps", ID: "cs", Slides: slides}, nil)) +
		string(Carousel(CarouselProps{Label: "Auto", ID: "ca", Slides: slides, AutoRotateMS: 300, Loop: true}, nil)) +
		`<button id="away">away</button>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the carousel marker never loaded headless-carousel")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#cs [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(() => { const l = document.querySelector('#cs [data-hui-carousel-live]');
		return l.textContent === 'Slide 2 of 3' && l.getAttribute('aria-live') === 'polite'; })()`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#cs [data-hui-carousel-live]').outerHTML`, &got))
		t.Fatalf("Next did not say the new slide through a polite live region: %s", got)
	}
	// The rotating carousel steps on its own while nothing hovers or
	// focuses it: the sentence moves, the region stays quiet.
	if err := chromedp.Run(ctx, chromedp.Focus(`#away`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(() => { const l = document.querySelector('#ca [data-hui-carousel-live]');
		return l.textContent !== 'Slide 1 of 3' && l.getAttribute('aria-live') === 'off'; })()`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#ca [data-hui-carousel-live]').outerHTML`, &got))
		t.Fatalf("an automatic step was not written into a quiet live region: %s", got)
	}
	// The reader's own step on the rotating carousel (focus inside
	// pauses the rotation) is heard.
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#ca [data-hui-carousel-next]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#ca [data-hui-carousel-next]').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('#ca [data-hui-carousel-live]').getAttribute('aria-live') === 'polite'`) {
		t.Fatal("a step the reader took on a paused rotating carousel was said through a quiet region")
	}
}
