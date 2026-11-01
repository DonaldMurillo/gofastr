package headless

// The carousel's keys belong to the track, not to a field inside a
// slide, and its rotation to the carousel, not to the page under it.

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// currentSlide reads the carousel's aria-current slide index.
func currentSlide(id string) string {
	return `(() => { const s = document.querySelectorAll('#` + id + ` [data-hui-carousel-track] > [aria-label]'); for (let i = 0; i < s.length; i++) if (s[i].getAttribute('aria-current') === 'true') return i; return -1; })()`
}

// A text input inside a slide (a newsletter signup, a search) owns its
// caret keys: ArrowRight moves the caret, End jumps to the end of the
// text, and a Shift chord selects — none of them moves the carousel.
// The track itself still steps.
func TestE2E_CarouselArrowsInAFieldInsideASlide(t *testing.T) {
	c := Carousel(CarouselProps{Label: "Promos", ID: "pc", Slides: []CarouselSlide{
		{Content: render.HTML(`<input id="ci" type="text" value="abc">`)},
		{Content: render.Text("two")}, {Content: render.Text("three")},
	}}, nil)
	b := startBehaviorServer(t, string(c))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#ci`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('ci').setSelectionRange(1,1)`, nil),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.KeyEvent(kb.End),
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	got := evalString(t, ctx, `JSON.stringify([document.getElementById('ci').selectionStart, `+currentSlide("pc")+`])`)
	if got != `[3,0]` {
		t.Fatalf("ArrowRight then End in a slide's text field gave [caret, current slide] = %s, want [3,0]: the carousel took the caret keys", got)
	}
	// A chord on the track is the browser's too; a plain arrow steps.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const tr = document.querySelector('#pc [data-hui-carousel-track]');
		tr.setAttribute('tabindex', '-1'); tr.focus();
		tr.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowRight', shiftKey: true, bubbles: true, cancelable: true}));
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := evalString(t, ctx, `String(`+currentSlide("pc")+`)`); got != "0" {
		t.Fatalf("Shift+ArrowRight on the track moved the carousel to slide %s", got)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#pc [data-hui-carousel-track]').dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowRight', bubbles: true, cancelable: true}))`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, currentSlide("pc")+` === 1`) {
		t.Fatal("a plain ArrowRight on the track no longer steps the carousel")
	}
}

// A rotating carousel in layout chrome survives a client navigation (it
// is not in the swapped region): it keeps rotating afterwards.
func TestE2E_CarouselInKeptChromeKeepsRotating(t *testing.T) {
	c := string(Carousel(CarouselProps{Label: "News", ID: "rc", AutoRotateMS: 250, Loop: true, Slides: []CarouselSlide{
		{Content: render.Text("a")}, {Content: render.Text("b")}, {Content: render.Text("c")},
	}}, nil))
	b := startBehaviorServer(t, "", func(mux *http.ServeMux) {
		mux.HandleFunc("/p1", func(w http.ResponseWriter, r *http.Request) {
			chromeShellPage(w, c, keptRoutes+`<a id="go" href="/p2">next</a>`)
		})
		mux.HandleFunc("/p2", func(w http.ResponseWriter, r *http.Request) {
			chromeShellPage(w, c, keptRoutes+`<p id="arrived">p2</p>`)
		})
	})
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(b.srv.URL+"/p1"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__stay = 1; window.__rc = document.getElementById('rc')`, nil), chromedp.Click(`#go`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('arrived') && window.__stay === 1 && document.getElementById('rc') === window.__rc`) {
		t.Fatal("the client navigation never landed with the chrome carousel kept")
	}
	var first int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement && document.activeElement.blur && document.activeElement.blur(); `+currentSlide("rc"), &first)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, currentSlide("rc")+` !== `+fmt.Sprint(first)) {
		t.Fatal("the carousel in the kept chrome stopped rotating after a client navigation")
	}
}
