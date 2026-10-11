package headless

import (
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

// carouselLayout lays a bare carousel out the way a styled one is: a
// 600px scroll-snap track whose slides are --w wide. The kit's sheet
// is not loaded here, so the test owns the geometry it measures.
const carouselLayout = `<style>
[data-hui-carousel-track]{display:flex;width:600px;overflow-x:auto;scroll-snap-type:x mandatory}
[data-hui-carousel-track]>*{flex:0 0 var(--w,600px);scroll-snap-align:start}
</style>`

// dotsState reports how many dots show and which one is current.
const dotsState = `(id) => {
	const dots = [...document.querySelectorAll('#' + id + ' [data-hui-carousel-goto]')];
	return {shown: dots.filter(d => !d.hidden).length,
		current: dots.findIndex(d => d.getAttribute('aria-current') === 'true')};
}`

// A swipe or a trackpad scroll moves the track without a click, and
// the dots stayed on the slide the last click chose (caught on a
// testimonial carousel: scrolled to the third card, the first dot
// still lit). Settling the scroll marks the slide at the track's start.
func TestE2E_CarouselScrollMovesTheDots(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+string(carouselHTML("swipe", 5, false)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('#swipe [data-hui-carousel-track]').scrollLeft = 1200`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('swipe').current === 2`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('swipe')`, &s))
		t.Fatalf("scrolled to the third slide, current dot = %v, want 2", s["current"])
	}
	if got := carouselCurrent(ctx, "swipe"); got != 2 {
		t.Errorf("scrolled to the third slide, aria-current slide = %d, want 2", got)
	}
}

// Three slides in view: six slides have four scroll positions, and a
// dot per slide left the last two dots naming places the track cannot
// reach. The dots count positions, Next stops at the last one, and a
// wider slide (one in view) brings every dot back.
func TestE2E_CarouselDotsCountPositions(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div id="wrap" style="--w:200px">`+string(carouselHTML("pos", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('pos').shown === 4`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('pos')`, &s))
		t.Fatalf("three in view of six: %v dots shown, want 4", s["shown"])
	}
	for range 5 {
		if err := chromedp.Run(ctx, chromedp.Click(`#pos [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	if !pollTrue(ctx, `(`+dotsState+`)('pos').current === 3`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('pos')`, &s))
		t.Fatalf("Next past the last position: current dot = %v, want 3", s["current"])
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('wrap').style.setProperty('--w', '600px')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('pos').shown === 6`) {
		t.Fatal("one slide in view did not bring back a dot per slide")
	}
}

// Next at the last position, once the scroll has settled there, must
// stay put: stepping on marked a slide whose dot is hidden. Rapid
// clicks cannot show it, because the settle handler re-marks the end.
func TestE2E_CarouselNextStopsAtLast(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:200px">`+string(carouselHTML("last", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	atEnd := `(() => { const tr = document.querySelector('#last [data-hui-carousel-track]');
		return tr.scrollLeft >= tr.scrollWidth - tr.clientWidth - 1; })()`
	for i := 1; i <= 3; i++ {
		if err := chromedp.Run(ctx, chromedp.Click(`#last [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		if !pollTrue(ctx, `(`+dotsState+`)('last').current === `+string(rune('0'+i))) {
			t.Fatalf("Next %d did not reach position %d", i, i)
		}
	}
	if !pollTrue(ctx, atEnd) {
		t.Fatal("the track never settled at its end")
	}
	// Let the 120ms scroll-settle handler run first: a click it follows
	// is re-marked by it and would hide a Next that overshot.
	time.Sleep(400 * time.Millisecond)
	if err := chromedp.Run(ctx, chromedp.Click(`#last [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // a refusal changes nothing to poll for
	var s map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('last')`, &s)); err != nil {
		t.Fatal(err)
	}
	if s["current"] != float64(3) || s["shown"] != float64(4) {
		t.Errorf("Next at the last position: %v, want current 3 of 4 shown", s)
	}
}

// Rotation counted every slide as a position, so with three in view it
// reached the last position and kept stepping onto hidden dots instead
// of starting over. It wraps to the first position after the last.
func TestE2E_CarouselRotationWraps(t *testing.T) {
	slides := make([]CarouselSlide, 6)
	for i := range slides {
		slides[i] = CarouselSlide{Content: render.Text("Slide")}
	}
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:200px">`+
		string(Carousel(CarouselProps{Label: "Rotating", ID: "rot", AutoRotateMS: 300, Slides: slides}, nil))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__seen = [];
		setInterval(() => window.__seen.push((`+dotsState+`)('rot').current), 50)`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__seen.includes(3) && window.__seen.lastIndexOf(0) > window.__seen.indexOf(3)`) {
		var seen []any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.__seen`, &seen))
		t.Fatalf("rotation never wrapped from the last position to the first: %v", seen)
	}
	var past bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__seen.some(c => c > 3)`, &past)); err != nil {
		t.Fatal(err)
	}
	if past {
		t.Error("rotation marked a position past the last one")
	}
}

// A track showing two and a half slides counted three in view, so the
// positions stopped one short and the last slide stayed half clipped
// with no control left to reveal it. Only whole slides count as in view.
func TestE2E_CarouselPeekReachesLastSlide(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:240px">`+string(carouselHTML("peek", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('peek').shown === 5`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('peek')`, &s))
		t.Fatalf("two and a half in view of six: %v dots shown, want 5", s["shown"])
	}
	for range 5 {
		if err := chromedp.Run(ctx, chromedp.Click(`#peek [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	reached := `(() => {
		const tr = document.querySelector('#peek [data-hui-carousel-track]');
		return tr.lastElementChild.getBoundingClientRect().right <= tr.getBoundingClientRect().right + 1;
	})()`
	if !pollTrue(ctx, reached) {
		t.Error("Next to the last position left the last slide clipped")
	}
}

// Moving to a slide scrolled it into view with scrollIntoView, which
// also scrolls every ancestor: an auto-rotating carousel below the fold
// pulled the page down to it (caught in review). Only the track moves.
func TestE2E_CarouselRotationKeepsPage(t *testing.T) {
	slides := make([]CarouselSlide, 3)
	for i := range slides {
		slides[i] = CarouselSlide{Content: render.Text("Slide")}
	}
	b := startBehaviorServer(t, carouselLayout+`<div style="block-size:3000px"></div>`+
		string(Carousel(CarouselProps{Label: "Rotating", ID: "fold", AutoRotateMS: 300, Slides: slides}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('fold').current === 2`) {
		t.Fatal("rotation never reached the third slide")
	}
	var y float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollY`, &y)); err != nil {
		t.Fatal(err)
	}
	if y != 0 {
		t.Errorf("rotation below the fold scrolled the page to %.0fpx", y)
	}
}

// With three slides in view, Loop wrapped only off the last slide, so
// Next at the last position stepped onto a hidden dot's slide and the
// setActive clamp held it at the end. Next at the last position wraps
// to the first.
func TestE2E_CarouselLoopWrapsPositions(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:200px">`+string(carouselHTML("lw", 6, true))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	for i := 1; i <= 3; i++ {
		if err := chromedp.Run(ctx, chromedp.Click(`#lw [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		if !pollTrue(ctx, `(`+dotsState+`)('lw').current === `+string(rune('0'+i))) {
			t.Fatalf("Next %d did not reach position %d", i, i)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#lw [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('lw').current === 0`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('lw')`, &s))
		t.Fatalf("Next at the last position under Loop: %v, want current 0", s)
	}
}

// End jumped to the last slide, whose dot is hidden when several slides
// are in view. It jumps to the last position.
func TestE2E_CarouselEndKeyStopsAtLast(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:200px">`+string(carouselHTML("end", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	// Every slide ever marked is recorded: the scroll settle would
	// re-mark the end within 120ms and hide a jump onto slide 5.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__marked = [];
			new MutationObserver(ms => ms.forEach(m => m.target.getAttribute('aria-current') === 'true' &&
				window.__marked.push([...m.target.parentNode.children].indexOf(m.target))))
			.observe(document.querySelector('#end [data-hui-carousel-track]'), {subtree: true, attributeFilter: ['aria-current']})`, nil),
		chromedp.Evaluate(`document.querySelector('#end [data-hui-carousel-track]').focus()`, nil),
		chromedp.Evaluate(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'End',bubbles:true,cancelable:true}))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('end').current === 3`) || carouselCurrent(ctx, "end") != 3 {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('end')`, &s))
		t.Fatalf("End with three in view: %v, slide %d current; want position 3", s, carouselCurrent(ctx, "end"))
	}
	var marked []int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__marked`, &marked)); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(marked, func(i int) bool { return i > 3 }) {
		t.Errorf("End marked slides %v on the way; a slide past the last position has a hidden dot", marked)
	}
}

// A styled track spaces its slides, and the gap is not part of a slide:
// 190px slides with a 20px gap fit two in 600px, not three, so six
// slides have five positions.
func TestE2E_CarouselCountsSlideGaps(t *testing.T) {
	layout := `<style>[data-hui-carousel-track]{display:flex;gap:20px;width:600px;overflow-x:auto;scroll-snap-type:x mandatory}
[data-hui-carousel-track]>*{flex:0 0 190px;scroll-snap-align:start}</style>`
	b := startBehaviorServer(t, layout+string(carouselHTML("gap", 6, false)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('gap').shown === 5`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('gap')`, &s))
		t.Fatalf("two whole slides in view of six: %v dots shown, want 5", s["shown"])
	}
}

// A track scrolled to its end marks the last position even when another
// slide's start is nearer the offset: with 220px slides the end (720px)
// sits nearer slide 3's start (660px) than slide 4's (880px).
func TestE2E_CarouselScrollEndMarksLast(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:220px">`+string(carouselHTML("tail", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('tail').shown === 5`) {
		t.Fatal("two in view of six did not count five positions")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const tr = document.querySelector('#tail [data-hui-carousel-track]'); tr.scrollLeft = tr.scrollWidth; })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('tail').current === 4`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('tail')`, &s))
		t.Fatalf("scrolled to the end: %v, want the last position 4", s)
	}
}

// Under dir=rtl the track scrolls to negative offsets and a slide's
// leading edge is its right one: scrolled two slides in with three in
// view, the left edge would name slide 4.
func TestE2E_CarouselRTLScrollMovesDots(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div dir="rtl" style="--w:200px">`+string(carouselHTML("rtl", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('#rtl [data-hui-carousel-track]').scrollLeft = -400`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('rtl').current === 2`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('rtl')`, &s))
		t.Fatalf("scrolled two slides into an RTL track: %v, want current 2", s)
	}
}

// A slide marked current past the last position, once the slides narrow
// into fewer positions, would leave no dot lit. The mark moves back to
// the last position.
func TestE2E_CarouselResizeKeepsADotLit(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div id="rw">`+string(carouselHTML("rs", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#rs [data-hui-carousel-goto="5"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('rs').current === 5`) {
		t.Fatal("the sixth dot did not mark the sixth slide")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('rw').style.setProperty('--w', '200px')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(`+dotsState+`)('rs').shown === 4 && (`+dotsState+`)('rs').current === 3`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('rs')`, &s))
		t.Fatalf("three in view after marking the sixth slide: %v, want current 3 of 4", s)
	}
}

// In a right-to-left track the slide's leading edge is its right one
// and scrollLeft runs negative. Measured from the left edge, as in LTR,
// Next moved the dot while the track never scrolled (caught in review).
func TestE2E_CarouselRTLNextScrollsTrack(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div dir="rtl" style="--w:200px">`+string(carouselHTML("rtln", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	for range 2 {
		if err := chromedp.Run(ctx, chromedp.Click(`#rtln [data-hui-carousel-next]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	scrolled := `document.querySelector('#rtln [data-hui-carousel-track]').scrollLeft`
	if !pollTrue(ctx, scrolled+` <= -399`) {
		var x float64
		_ = chromedp.Run(ctx, chromedp.Evaluate(scrolled, &x))
		t.Fatalf("two Nexts in an RTL track left scrollLeft at %.0f, want -400", x)
	}
}

// Slide rects are in visual pixels and the track's widths and scroll
// offset in layout pixels, so under a scaled ancestor the count of
// slides in view came out wrong: half scale showed two dots of four
// and Next never reached the last slides (caught in review).
func TestE2E_CarouselScaledCountsPositions(t *testing.T) {
	b := startBehaviorServer(t, carouselLayout+`<div style="--w:200px;transform:scale(.5);transform-origin:0 0">`+
		string(carouselHTML("half", 6, false))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if !pollTrue(ctx, `(`+dotsState+`)('half').shown === 4`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('half')`, &s))
		t.Fatalf("three in view of six at half scale: %v, want 4 dots", s)
	}
	for range 3 {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#half [data-hui-carousel-next]').click()`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	atEnd := `(() => { const tr = document.querySelector('#half [data-hui-carousel-track]');
		return tr.scrollLeft >= tr.scrollWidth - tr.clientWidth - 1; })()`
	if !pollTrue(ctx, atEnd) || !pollTrue(ctx, `(`+dotsState+`)('half').current === 3`) {
		var s map[string]any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('half')`, &s))
		t.Fatalf("three Nexts at half scale: %v, want the track at its end on position 3", s)
	}
}

// A carousel inside another's slide: the outer one counted the inner
// one's slides and dots as its own, so its Next marked an inner slide
// and left its own track where it was (caught in review).
func TestE2E_CarouselNestedStaysOwn(t *testing.T) {
	slides := []CarouselSlide{{Content: carouselHTML("inner", 3, false)}}
	for range 4 {
		slides = append(slides, CarouselSlide{Content: render.Text("Outer")})
	}
	b := startBehaviorServer(t, carouselLayout+string(Carousel(CarouselProps{Label: "Outer", ID: "outer", Slides: slides}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, carouselLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('#outer [data-hui-carousel-next]')]
		.find(n => n.closest('[data-hui-carousel]').id === 'outer').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('#outer [data-hui-carousel-track]').scrollLeft >= 599`) {
		t.Fatal("the outer Next never scrolled the outer track")
	}
	if got := carouselCurrent(ctx, "inner"); got != 0 {
		t.Errorf("the outer Next moved the inner carousel to slide %d", got)
	}
	var inner map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(`+dotsState+`)('inner')`, &inner)); err != nil {
		t.Fatal(err)
	}
	if inner["current"] != float64(0) {
		t.Errorf("the outer Next moved the inner carousel's dot: %v, want current 0", inner)
	}
	var shown float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('#outer [data-hui-carousel-goto]')]
		.filter(d => d.closest('[data-hui-carousel]').id === 'outer' && d.getAttribute('aria-current') === 'true').length`, &shown)); err != nil {
		t.Fatal(err)
	}
	if shown != 1 {
		t.Errorf("outer carousel has %.0f current dots, want 1", shown)
	}
}
