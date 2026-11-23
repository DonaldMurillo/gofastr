package headless

// The rotation button (WCAG 2.2.2): Pause holds the rotation until the
// reader presses it again, reduced motion starts it held, and pressing
// play rotates even with the pointer and focus left on the button. The
// arrows say aria-disabled at the ends of a carousel without Loop.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func rotatingCarousel(id string) string {
	return string(Carousel(CarouselProps{Label: "Rotating", ID: id, AutoRotateMS: 200, Loop: true, Slides: []CarouselSlide{
		{Content: render.Text("a")}, {Content: render.Text("b")}, {Content: render.Text("c")},
	}}, nil))
}

// countStepsJS resets window.__steps and counts each slide of #id that
// becomes current from here on.
func countStepsJS(id string) string {
	return `(() => {
		if (window.__stepObs) window.__stepObs.disconnect();
		window.__steps = 0;
		window.__stepObs = new MutationObserver((ms) => {
			for (const m of ms) if (m.target.getAttribute('aria-current') === 'true') window.__steps++;
		});
		window.__stepObs.observe(document.querySelector('#` + id + ` [data-hui-carousel-track]'),
			{ attributes: true, subtree: true, attributeFilter: ['aria-current'] });
	})();`
}

func TestE2E_CarouselPauseButtonHolds(t *testing.T) {
	b := startBehaviorServer(t, rotatingCarousel("pz"))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("module never loaded")
	}
	pressed := `document.querySelector('#pz [data-hui-carousel-pause]').getAttribute('aria-pressed')`
	if got := evalString(t, ctx, pressed); got != "false" {
		t.Fatalf("a rotating carousel's button starts aria-pressed=%q, want false", got)
	}
	// Pressed by script and blurred: no hover or focus pause is left, so
	// only the button's choice can hold the rotation.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#pz [data-hui-carousel-pause]').click(); document.activeElement.blur();`+countStepsJS("pz"), nil),
		chromedp.Sleep(900*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, pressed); got != "true" {
		t.Fatalf("after Pause the button says aria-pressed=%q, want true", got)
	}
	if got := evalString(t, ctx, `String(window.__steps)`); got != "0" {
		t.Fatalf("the carousel stepped %s times after Pause", got)
	}
	if got := evalString(t, ctx, `document.querySelector('#pz [data-hui-carousel-live]').getAttribute('aria-live')`); got != "polite" {
		t.Fatalf("a paused carousel's live region is %q, want polite", got)
	}
	// A real click on play leaves the pointer and the focus on the
	// button: the rotation still runs.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(countStepsJS("pz"), nil),
		chromedp.Click(`#pz [data-hui-carousel-pause]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, pressed); got != "false" {
		t.Fatalf("after Play the button says aria-pressed=%q, want false", got)
	}
	if !pollTrue(ctx, `window.__steps > 0`) {
		t.Fatal("pressing play with the pointer and focus on the button did not restart the rotation")
	}
}

func TestE2E_CarouselReducedMotionStartsHeld(t *testing.T) {
	b := startBehaviorServer(t, rotatingCarousel("rm"))
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		emulateReducedMotion(),
		chromedp.Navigate(b.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("module never loaded")
	}
	if !pollTrue(ctx, `document.querySelector('#rm [data-hui-carousel-pause]').getAttribute('aria-pressed') === 'true'`) {
		t.Fatal("under reduced motion the rotation button does not say the slides are held")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(countStepsJS("rm"), nil),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `String(window.__steps)`); got != "0" {
		t.Fatalf("the carousel stepped %s times under reduced motion", got)
	}
	// The reader's own play wins over the default.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#rm [data-hui-carousel-pause]').click(); document.activeElement.blur();`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__steps > 0`) {
		t.Fatal("pressing play under reduced motion did not start the rotation")
	}
}

func TestE2E_CarouselArrowsDisableAtTheEnds(t *testing.T) {
	b := startBehaviorServer(t, string(carouselHTML("ends", 3, false)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("module never loaded")
	}
	state := `JSON.stringify(['prev', 'next'].map(k => document.querySelector('#ends [data-hui-carousel-' + k + ']').getAttribute('aria-disabled')))`
	if got := evalString(t, ctx, state); got != `["true",null]` {
		t.Fatalf("at the first slide [prev, next] aria-disabled = %s, want [\"true\",null]", got)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#ends [data-hui-carousel-goto="2"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, state+` === '[null,"true"]'`) {
		t.Fatalf("at the last slide [prev, next] aria-disabled = %s, want [null,\"true\"]", evalString(t, ctx, state))
	}
}
