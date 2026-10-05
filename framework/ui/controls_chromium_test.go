//go:build chromium

package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: the neutral palette made the dark-mode primary near
// white, and the toggle's literal white thumb vanished into a checked
// track (caught in review). The checked thumb takes the primary's
// foreground, so it reads on the track in either mode.
func TestToggleThumbShowsOnDarkTrack(t *testing.T) {
	dark := `*{transition:none!important}:root{--color-primary:#FAFAFA;--color-primary-fg:#18181B}`
	m := geometryOf(t, dark+signalToggleStyle.Entry().CSSFor(theme.Default()),
		SignalToggle(SignalToggleConfig{SignalName: "on", Label: "On"}), `(() => {
		const el = document.querySelector('[data-cui-comp="fui-toggle"]');
		el.setAttribute('aria-checked', 'true');
		const lum = node => {
			const [r, g, b] = getComputedStyle(node).backgroundColor.match(/[0-9.]+/g).map(Number)
				.map(c => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; });
			return 0.2126 * r + 0.7152 * g + 0.0722 * b;
		};
		const a = lum(el.querySelector('.fui-toggle__thumb')), b = lum(el.querySelector('.fui-toggle__track'));
		return {ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)};
	})()`)
	if m["ratio"] < 3 {
		t.Errorf("checked thumb on a dark-mode track is %.2f:1, want at least 3:1", m["ratio"])
	}
}

// Hard rule 9: a segmented control's per-count minimum width beat its
// 100% cap, so three options were 352px wide in a 300px phone column
// and scrolled the page sideways (caught in review). The minimum gives
// way to the column.
func TestSegmentedFitsPhoneColumn(t *testing.T) {
	page := render.HTML(`<div style="width:300px">`) + SegmentedControl(SegmentedControlConfig{Name: "p", Options: []SegmentedOption{
		{Label: "Day", Value: "d"}, {Label: "Week", Value: "w"}, {Label: "Month", Value: "m"}}}) + render.HTML(`</div>`)
	m := geometryOf(t, segmentedStyle.Entry().CSSFor(theme.Default()), page,
		`({w: document.querySelector('[data-cui-comp="ui-segmented"]').getBoundingClientRect().width})`)
	if m["w"] == 0 || m["w"] > 300.5 {
		t.Errorf("three-option control is %.0fpx wide in a 300px column", m["w"])
	}
}

// Hard rule 9: the checked option sets its label semibold and every
// column is as wide as the widest label, so a pricing toggle grew and
// shrank as the selection moved between Monthly and its long Annual
// label (caught on a neo-brutalist theme, semibold 800 over 500). The
// control holds one width in every state.
func TestSegmentedWidthHoldsOnSelect(t *testing.T) {
	heavy := `*{transition:none!important}:root{--font-weight-semibold:800}body{font-weight:400;font-size:16px}`
	page := SegmentedControl(SegmentedControlConfig{Name: "billing", Selected: "m", Options: []SegmentedOption{
		{Label: "Monthly", Value: "m"}, {Label: "Annual billing (save 20 percent)", Value: "y"}}})
	m := geometryOf(t, heavy+segmentedStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const c = document.querySelector('[data-cui-comp="ui-segmented"]');
		const monthly = c.getBoundingClientRect().width;
		c.querySelectorAll('input')[1].checked = true;
		return {monthly, annual: c.getBoundingClientRect().width};
	})()`)
	if m["monthly"] == 0 || m["monthly"] != m["annual"] {
		t.Errorf("control is %.1fpx with Monthly checked and %.1fpx with Annual checked; want one width", m["monthly"], m["annual"])
	}
}

// Hard rule 9: the range slider drew its neutral halo on the thumb but
// never cleared the input's own focus outline, so a keyboard user saw
// the browser's blue box around the whole track beside it (caught in
// review). Slider clears it; RangeSlider does too.
func TestRangeSliderDropsInputOutline(t *testing.T) {
	page := RangeSlider(RangeSliderConfig{Name: "price", Label: "Price"})
	css := rangeSliderStyle.Entry().CSSFor(theme.Default())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1000, 600))
	var style string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.KeyEvent("\t"),
		chromedp.Evaluate(`(() => {
			const el = document.activeElement;
			return el && el.classList.contains('fui-range-slider__input') ? getComputedStyle(el).outlineStyle : 'not focused';
		})()`, &style),
	); err != nil {
		t.Fatal(err)
	}
	if style != "none" {
		t.Errorf("keyboard-focused range input outline is %q, want none (the thumb carries the ring)", style)
	}
}

// Hard rule 9: a slide's lone child fills it, and the slide clips its
// overflow, so a focusable card's outer ring was clipped away and a
// keyboard user saw no focus at all (caught in review). The ring of a
// slide's own child draws inside its edge. The card sheet loads last
// here, the order in which it wins a tie.
func TestCarouselCardFocusRingShows(t *testing.T) {
	slide := Card(CardConfig{Heading: "Plan", Href: "/plan"})
	page := Carousel(CarouselConfig{Label: "Plans", Slides: []CarouselSlide{{Content: slide}, {Content: slide}}, NoArrows: true, NoDots: true})
	css := carouselStyle.Entry().CSSFor(theme.Default()) + cardStyle.Entry().CSSFor(theme.Default())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><style>*{box-sizing:border-box}body{margin:0;width:900px}%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1000, 600))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatal(err)
	}
	// Tab until the first card holds focus; the track takes a stop first.
	var ring map[string]float64
	for range 6 {
		if err := chromedp.Run(ctx, chromedp.KeyEvent("\t"), chromedp.Evaluate(`(() => {
			const el = document.activeElement;
			if (!el || !el.classList.contains('fui-card')) return {focused: 0};
			const s = getComputedStyle(el);
			return {focused: 1, outside: parseFloat(s.outlineOffset) + parseFloat(s.outlineWidth)};
		})()`, &ring)); err != nil {
			t.Fatal(err)
		}
		if ring["focused"] == 1 {
			break
		}
	}
	if ring["focused"] != 1 {
		t.Fatalf("no card took keyboard focus: %v", ring)
	}
	if ring["outside"] > 0 {
		t.Errorf("a slide's card draws its ring %.0fpx outside its edge, where the slide clips it", ring["outside"])
	}
}

// Hard rule 9: a phone-wide carousel drops its arrows for the dots, but
// one with NoDots lost every pointer control with them: a mouse with a
// vertical wheel could not move it (caught in review). Without dots the
// arrows stay.
func TestNarrowNoDotsCarouselKeepsArrows(t *testing.T) {
	slides := []CarouselSlide{{Content: render.Text("One")}, {Content: render.Text("Two")}}
	page := render.HTML(`<div style="width:300px">`) + Carousel(CarouselConfig{Label: "Tips", Slides: slides, NoDots: true}) + render.HTML(`</div>`)
	m := geometryOf(t, carouselStyle.Entry().CSSFor(theme.Default()), page,
		`({next: document.querySelector('.fui-carousel__next').getBoundingClientRect().width})`)
	if m["next"] == 0 {
		t.Error("a narrow carousel with no dots hides its arrows, leaving no pointer control")
	}
}

// Hard rule 9: the rating input's default colour sat on its root, so an
// ancestor's --ui-rating-color never reached it, and a shape's colour
// could outrank the knob. The same chain as the display: the knob from
// any ancestor, else the shape's colour, else amber.
func TestRatingInputColorFromAncestor(t *testing.T) {
	css := ratingStyle.Entry().CSSFor(theme.Default())
	probe := `(() => {
		const [r, g, b] = getComputedStyle(document.querySelector('.fui-rating__input:checked ~ .fui-rating__choice')).color.match(/\d+/g).map(Number);
		return {r, g, b};
	})()`
	knob := `<div style="--ui-rating-color: rgb(1, 2, 3)">`
	for _, tc := range []struct {
		wrap    string
		shape   RatingShape
		r, g, b float64
	}{
		{knob, RatingShapeStar, 1, 2, 3},
		{`<div>`, RatingShapeStar, 217, 119, 6},
		{knob, RatingShapeHeart, 1, 2, 3},
		{`<div>`, RatingShapeHeart, 220, 38, 38},
	} {
		page := render.HTML(tc.wrap) + RatingInput(RatingConfig{Name: "r", Label: "Rating", Value: 4, Shape: tc.shape}) + "</div>"
		m := geometryOf(t, css, page, probe)
		if m["r"] != tc.r || m["g"] != tc.g || m["b"] != tc.b {
			t.Errorf("%s %q: lit choice is rgb(%v, %v, %v), want rgb(%v, %v, %v)", tc.wrap, tc.shape, m["r"], m["g"], m["b"], tc.r, tc.g, tc.b)
		}
	}
}
