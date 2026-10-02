package headless

// Browser coverage for headless-carousel: the real registered module
// loads on the root marker, prev/next step the active slide (with the
// ends refusing without the loop flag, and wrapping with it), the dots
// jump, the arrows step on the focused track (RTL-aware), the status
// sentence is re-said through the server's words, and markup inserted
// after load is armed by the arrival pass.

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

const carouselLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-carousel'])`

func carouselHTML(id string, n int, loop bool) render.HTML {
	slides := make([]CarouselSlide, 0, n)
	for i := 0; i < n; i++ {
		slides = append(slides, CarouselSlide{Content: render.Text("Slide " + string(rune('0'+i+1)))})
	}
	return Carousel(CarouselProps{Label: "Demo", ID: id, Loop: loop, Slides: slides}, nil)
}

func carouselCurrent(ctx context.Context, id string) int {
	var cur int
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const root = document.getElementById('`+id+`');
			const slides = root.querySelectorAll('[data-hui-carousel-track] > [aria-label]');
			for (let i = 0; i < slides.length; i++) if (slides[i].getAttribute('aria-current') === 'true') return i;
			return -1; })()`, &cur))
	return cur
}

// Prev/Next step the active slide; without the loop flag the ends
// refuse; the dots jump; the status sentence follows.
func TestE2E_CarouselControlsAndStatus(t *testing.T) {
	b := startBehaviorServer(t, string(carouselHTML("ctl", 3, false)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the carousel marker never loaded headless-carousel")
	}
	click := func(sel string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	if got := carouselCurrent(ctx, "ctl"); got != 0 {
		t.Fatalf("initial current = %d, want 0", got)
	}
	click(`#ctl [data-hui-carousel-next]`)
	if !pollTrue(ctx, `(() => { const s = document.querySelectorAll('#ctl [data-hui-carousel-track] > [aria-label]'); return s[1].getAttribute('aria-current') === 'true'; })()`) {
		t.Fatal("Next did not step to the second slide")
	}
	var status string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('#ctl [data-hui-carousel-track]').getAttribute('data-hui-carousel-status')`, &status)); err != nil {
		t.Fatal(err)
	}
	if status != "Slide 2 of 3" {
		t.Fatalf("status = %q, want the server's sentence re-said for slide 2", status)
	}
	// The dot mirror: dot 1 is current.
	if !pollTrue(ctx, `document.querySelector('#ctl [data-hui-carousel-goto="1"]').getAttribute('aria-current') === 'true'`) {
		t.Fatal("the active dot did not mirror the active slide")
	}
	click(`#ctl [data-hui-carousel-goto="2"]`)
	if !pollTrue(ctx, `document.querySelectorAll('#ctl [data-hui-carousel-track] > [aria-label]')[2].getAttribute('aria-current') === 'true'`) {
		t.Fatal("the dot did not jump to the third slide")
	}
	// At the end without loop: Next refuses.
	click(`#ctl [data-hui-carousel-next]`)
	if !pollTrue(ctx, `document.querySelectorAll('#ctl [data-hui-carousel-track] > [aria-label]')[2].getAttribute('aria-current') === 'true'`) {
		t.Fatal("Next past the end stepped without the loop flag")
	}
	// Prev steps back.
	click(`#ctl [data-hui-carousel-prev]`)
	if !pollTrue(ctx, `document.querySelectorAll('#ctl [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("Prev did not step back")
	}
}

// Looping: Next on the last wraps to the first.
func TestE2E_CarouselLoopsAtTheEnds(t *testing.T) {
	b := startBehaviorServer(t, string(carouselHTML("loop", 2, true)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#loop [data-hui-carousel-next]`, chromedp.ByQuery),
		chromedp.Click(`#loop [data-hui-carousel-next]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#loop [data-hui-carousel-track] > [aria-label]')[0].getAttribute('aria-current') === 'true'`) {
		t.Fatal("Next on the last did not wrap to the first under the loop flag")
	}
}

// The keyboard: the arrows step on the focused track, Home/End jump,
// and under dir=rtl the arrows swap.
func TestE2E_CarouselKeyboardAndRTL(t *testing.T) {
	b := startBehaviorServer(t, `<div dir="rtl">`+string(carouselHTML("kb", 3, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	key := func(k string) {
		t.Helper()
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('#kb [data-hui-carousel-track]').focus()`, nil),
			chromedp.Evaluate(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'`+k+`',bubbles:true,cancelable:true}))`, nil),
		); err != nil {
			t.Fatal(err)
		}
	}
	isCur := func(i int) bool {
		var ok bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelectorAll('#kb [data-hui-carousel-track] > [aria-label]')[`+string(rune('0'+i))+`].getAttribute('aria-current') === 'true'`, &ok))
		return ok
	}
	// RTL: ArrowLeft steps forward.
	key("ArrowLeft")
	if !pollTrue(ctx, `document.querySelectorAll('#kb [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("ArrowLeft (RTL) did not step forward")
	}
	key("End")
	if !isCur(2) {
		t.Fatal("End did not jump to the last slide")
	}
	key("Home")
	if !isCur(0) {
		t.Fatal("Home did not jump to the first slide")
	}
	_ = status
}

var status string

// Markup inserted after load is armed by the arrival pass: a carousel
// that arrives open answers the controls.
func TestE2E_CarouselInsertedAfterLoadIsArmed(t *testing.T) {
	b := startBehaviorServer(t, string(carouselHTML("first", 2, false)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const d = document.createElement('div'); d.innerHTML = `+jsCarousel("late", 2)+`; document.body.appendChild(d.firstElementChild); return true; })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#late [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#late [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("the inserted carousel was never armed by the arrival pass")
	}
}

// Removing a rotating carousel clears its timer. Observed through a
// spy on window.clearInterval — the module deliberately exposes
// nothing on window for this — with the disarm arriving in the
// MutationObserver callback after the removal. The surviving carousel
// keeps rotating: the sweep disarms only what left the tree.
func TestE2E_CarouselDetachClearsItsTimer(t *testing.T) {
	gone := Carousel(CarouselProps{Label: "Detached", ID: "rot-gone", AutoRotateMS: 300, Slides: []CarouselSlide{
		{Content: render.Text("g0")}, {Content: render.Text("g1")},
	}}, nil)
	stays := Carousel(CarouselProps{Label: "Stays", ID: "rot-stays", AutoRotateMS: 300, Slides: []CarouselSlide{
		{Content: render.Text("s0")}, {Content: render.Text("s1")},
	}}, nil)
	b := startBehaviorServer(t, string(gone)+string(stays))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	// The removed carousel had a live timer (it advanced) — otherwise
	// there is nothing for the sweep to clear and the test proves
	// nothing.
	if !pollTrue(ctx, `document.querySelectorAll('#rot-gone [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("the 300ms carousel did not advance — no live timer to clear")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__clearSpy = [];
		const orig = window.clearInterval;
		window.clearInterval = function (id) { window.__clearSpy.push(id); return orig.call(window, id); };
		document.getElementById('rot-gone').remove();
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__clearSpy.length >= 1`) {
		t.Fatal("removing a rotating carousel cleared no interval — the detach sweep never fired")
	}
	// The survivor still rotates: reset it to its first slide and wait
	// past a cadence. Only a live interval can advance it again.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('#rot-stays [data-hui-carousel-goto="0"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#rot-stays [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("the surviving carousel stopped rotating after its sibling was removed — the sweep disarmed too much")
	}
}

func jsCarousel(id string, n int) string {
	return "`" + string(carouselHTML(id, n, false)) + "`"
}

// The rotation contract: one timer per carousel at its own cadence,
// stepping only when not paused. A 300 ms carousel advances within a
// second; focus inside it pauses it for a second; reduced motion
// pauses it; and a second carousel at 4000 on the same page does not
// step when the first does.
func TestE2E_CarouselRotationAdvancesAndPauses(t *testing.T) {
	fast := Carousel(CarouselProps{Label: "Fast", ID: "rot-fast", AutoRotateMS: 300, Slides: []CarouselSlide{
		{Content: render.Text("f0")}, {Content: render.Text("f1")},
	}}, nil)
	slow := Carousel(CarouselProps{Label: "Slow", ID: "rot-slow", AutoRotateMS: 4000, Slides: []CarouselSlide{
		{Content: render.Text("s0")}, {Content: render.Text("s1")},
	}}, nil)
	b := startBehaviorServer(t, string(fast)+string(slow))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	cur := func(id string) func() bool {
		return func() bool {
			var ok bool
			_ = chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('#`+id+` [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`, &ok))
			return ok
		}
	}
	// The fast carousel advances within a second at 300 ms.
	if !pollTrue(ctx, `document.querySelectorAll('#rot-fast [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`) {
		t.Fatal("the 300ms carousel did not advance within a second")
	}
	// The slow carousel has not stepped (4000 ms cadence): one
	// immediate read, no polling — a poll would wait out the cadence
	// and see a legitimate tick.
	var slowStepped bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('#rot-slow [data-hui-carousel-track] > [aria-label]')[1].getAttribute('aria-current') === 'true'`, &slowStepped))
	if slowStepped {
		t.Fatal("the 4000ms carousel stepped when the 300ms one did — one timer per carousel, not a shared sweep")
	}
	// Focus inside the fast carousel pauses it: reset to slide 0 by
	// clicking its first dot, focus the track, wait a second, assert
	// it stayed.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#rot-fast [data-hui-carousel-goto="0"]').click()`, nil),
		chromedp.Evaluate(`document.querySelector('#rot-fast [data-hui-carousel-track]').focus()`, nil),
		chromedp.Sleep(1100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	var stayed bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('#rot-fast [data-hui-carousel-track] > [aria-label]')[0].getAttribute('aria-current') === 'true'`, &stayed))
	if !stayed {
		t.Fatal("the carousel rotated while focus was inside it — the focus pause is not honoured")
	}
	// Reduced motion pauses it too.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('rot-fast').blur && document.activeElement.blur()`, nil),
		emulateReducedMotion(),
		chromedp.Sleep(1100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	var stillFirst bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('#rot-fast [data-hui-carousel-track] > [aria-label]')[0].getAttribute('aria-current') === 'true'`, &stillFirst))
	if !stillFirst {
		t.Fatal("the carousel rotated under prefers-reduced-motion: reduce")
	}
	_ = cur
}

// emulateReducedMotion sets the media emulation for the tab.
func emulateReducedMotion() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		feat := &emulation.MediaFeature{Name: "prefers-reduced-motion", Value: "reduce"}
		return emulation.SetEmulatedMedia().
			WithFeatures([]*emulation.MediaFeature{feat}).
			Do(ctx)
	})
}
