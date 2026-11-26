package headless

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderCarousel(p CarouselProps) string { return string(Carousel(p, nil)) }

// Two carousels sharing one slides slice each derive their own slide
// ids: a render never writes its ids into the caller's slice.
func TestCarouselSharedSlidesKeepOwnIDs(t *testing.T) {
	slides := []CarouselSlide{{Content: render.Text("One")}}
	a := renderCarousel(CarouselProps{Label: "A", ID: "a", Slides: slides})
	b := renderCarousel(CarouselProps{Label: "B", ID: "b", Slides: slides})
	if !strings.Contains(a, `id="a-track-slide-0"`) || !strings.Contains(b, `id="b-track-slide-0"`) {
		t.Errorf("a shared slides slice carried one render's ids into the next:\n%s\n%s", a, b)
	}
	if slides[0].ID != "" {
		t.Errorf("the render wrote %q into the caller's slides", slides[0].ID)
	}
}

func TestCarouselRendersTrackControlsAndDots(t *testing.T) {
	h := renderCarousel(CarouselProps{Label: "Featured work", ID: "feat", Slides: []CarouselSlide{
		{Content: render.Text("One")},
		{Content: render.Text("Two")},
	}})
	for _, want := range []string{
		`aria-label="Featured work" aria-roledescription="carousel"`,
		`<div data-hui-carousel-status="Slide 1 of 2" data-hui-carousel-status-fmt="Slide {{n}} of {{total}}|2" data-hui-carousel-track="" id="feat-track" tabindex="0">`,
		`<div aria-current="true" aria-label="Slide 1 of 2" aria-roledescription="slide" id="feat-track-slide-0" role="group">`,
		`<div aria-label="Slide 2 of 2" aria-roledescription="slide" id="feat-track-slide-1" role="group">`,
		`<button aria-current="true" aria-label="Go to slide 1" data-hui-carousel-goto="0" type="button">1</button>`,
		`<button aria-label="Go to slide 2" data-hui-carousel-goto="1" type="button">2</button>`,
		`<button aria-disabled="true" data-cui-internal="" data-hui-carousel-prev="" type="button">Previous</button>`,
		`<button data-cui-internal="" data-hui-carousel-next="" type="button">Next</button>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("carousel missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "href=") {
		t.Errorf("a carousel control is still a link:\n%s", h)
	}
	if strings.Contains(h, "data-hui-carousel-pause") {
		t.Errorf("a carousel that does not rotate drew a rotation button:\n%s", h)
	}
}

// The arrows come before the track and the dots after it, the order the
// guide asks for, so a reader tabs through the controls before the slides.
func TestCarouselControlsBeforeTheTrack(t *testing.T) {
	h := renderCarousel(CarouselProps{Label: "Order", ID: "ord", AutoRotateMS: 5000, Slides: []CarouselSlide{
		{Content: render.Text("One")}, {Content: render.Text("Two")},
	}})
	prev := strings.Index(h, "data-hui-carousel-prev")
	next := strings.Index(h, "data-hui-carousel-next")
	track := strings.Index(h, "data-hui-carousel-track")
	dots := strings.Index(h, "data-hui-carousel-goto")
	pause := strings.Index(h, "data-hui-carousel-pause")
	if prev < 0 || next < 0 || track < 0 || dots < 0 || pause < 0 {
		t.Fatalf("a part is missing:\n%s", h)
	}
	if !(prev < next && next < track && track < dots && dots < pause) {
		t.Errorf("order prev=%d next=%d track=%d dots=%d pause=%d, want prev, next, track, dots, pause:\n%s",
			prev, next, track, dots, pause, h)
	}
}

// With Loop the ends wrap, so neither arrow starts disabled; a lone slide
// without Loop has nowhere to go either way.
func TestCarouselArrowsStartDisabledAtTheEnds(t *testing.T) {
	loop := renderCarousel(CarouselProps{Label: "L", Loop: true, Slides: []CarouselSlide{
		{Content: render.Text("a")}, {Content: render.Text("b")},
	}})
	if strings.Contains(loop, "aria-disabled") {
		t.Errorf("a looping carousel rendered a disabled arrow:\n%s", loop)
	}
	one := renderCarousel(CarouselProps{Label: "L", Slides: []CarouselSlide{{Content: render.Text("a")}}})
	if n := strings.Count(one, `aria-disabled="true"`); n != 2 {
		t.Errorf("a lone slide without Loop rendered %d disabled arrows, want 2:\n%s", n, one)
	}
}

// A rotating carousel draws a pause button (WCAG 2.2.2) whose name is
// the Strings word and whose aria-pressed starts false.
func TestCarouselRotatingDrawsAPauseButton(t *testing.T) {
	h := string(Carousel(CarouselProps{Label: "Hero", ID: "hero", NoDots: true, NoArrows: true, AutoRotateMS: 5000,
		Slides:  []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
		Strings: &Strings{CarouselPause: "Pausar diapositivas"}}, nil))
	want := `<button aria-pressed="false" data-cui-internal="" data-hui-carousel-pause="" type="button">Pausar diapositivas</button>`
	if !strings.Contains(h, want) {
		t.Errorf("rotating carousel missing %q:\n%s", want, h)
	}
}

func TestCarouselBareRotating(t *testing.T) {
	h := renderCarousel(CarouselProps{Label: "Hero", ID: "hero", NoDots: true, NoArrows: true,
		Loop: true, AutoRotateMS: 6000, Slides: []CarouselSlide{{Content: render.Text("Only")}}})
	for _, want := range []string{
		`data-hui-carousel-loop=""`,
		`data-hui-carousel-rotate-ms="6000"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("bare carousel missing %q:\n%s", want, h)
		}
	}
	for _, refuse := range []string{"data-hui-carousel-goto", "data-hui-carousel-prev"} {
		if strings.Contains(h, refuse) {
			t.Errorf("a bare carousel rendered %q:\n%s", refuse, h)
		}
	}
}

func TestCarouselRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    CarouselProps
	}{
		{"whitespace-only label", CarouselProps{Label: "  \t", Slides: []CarouselSlide{{Content: render.Text("s")}}}},
		{"no slides", CarouselProps{Label: "L"}},
		{"negative rotate", CarouselProps{Label: "L", AutoRotateMS: -1, Slides: []CarouselSlide{{Content: render.Text("s")}}}},
		{"negative visible", CarouselProps{Label: "L", VisiblePerView: -1, Slides: []CarouselSlide{{Content: render.Text("s")}}}},
		{"duplicate slide ids", CarouselProps{Label: "L", Slides: []CarouselSlide{
			{ID: "dup", Content: render.Text("a")}, {ID: "dup", Content: render.Text("b")},
		}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Carousel(tc.p, nil)
		}()
	}
}

func TestCarouselVisiblePerViewClampsAtEight(t *testing.T) {
	h := renderCarousel(CarouselProps{Label: "L", VisiblePerView: 99, Slides: []CarouselSlide{{Content: render.Text("s")}}})
	if !strings.Contains(h, `data-hui-carousel`) {
		t.Fatalf("a wide VisiblePerView was refused instead of clamped:\n%s", h)
	}
}

func TestCarouselSlideStatusThroughStrings(t *testing.T) {
	h := string(Carousel(CarouselProps{Label: "Galerie", ID: "fr", Slides: []CarouselSlide{
		{Content: render.Text("Un")}, {Content: render.Text("Deux")},
	}, Strings: &Strings{CarouselSlide: "Diapositive {{n}} sur {{total}}", CarouselGoTo: "Aller à la diapositive {{n}}"}}, nil))
	for _, want := range []string{
		`data-hui-carousel-status="Diapositive 1 sur 2"`,
		`aria-label="Aller à la diapositive 2"`,
		`aria-label="Diapositive 1 sur 2"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("translated carousel words missing %q:\n%s", want, h)
		}
	}
}

// An explicit slide id is reserved: an auto "Features" section beside a
// slide named "section-features-title" moves its heading id, not the
// slide's.
func TestCarouselReservesSlideIDs(t *testing.T) {
	ids := render.NewRegionBatch(render.WithIDScope(context.Background()), 1)
	ctx := ids.Context(0)
	sec := Section(SectionProps{Title: "Features", Ctx: ctx}, nil)
	car := Carousel(CarouselProps{Label: "Tour", Ctx: ctx, Slides: []CarouselSlide{
		{Content: render.Text("One"), ID: "section-features-title"},
	}}, nil)
	out := string(ids.Settle(0)(sec + car))
	if n := strings.Count(out, `id="section-features-title"`); n != 1 {
		t.Errorf("id section-features-title appears %d times:\n%s", n, out)
	}
}
