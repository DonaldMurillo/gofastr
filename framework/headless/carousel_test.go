package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderCarousel(p CarouselProps) string { return string(Carousel(p, nil)) }

func TestCarouselRendersTrackControlsAndDots(t *testing.T) {
	h := renderCarousel(CarouselProps{Label: "Featured work", ID: "feat", Slides: []CarouselSlide{
		{Content: render.Text("One")},
		{Content: render.Text("Two")},
	}})
	for _, want := range []string{
		`aria-label="Featured work" aria-roledescription="carousel"`,
		`<div data-hui-carousel-status="Slide 1 of 2" data-hui-carousel-status-fmt="Slide {n} of {total}|2" data-hui-carousel-track="" id="feat-track" tabindex="0">`,
		`<div aria-current="true" aria-label="Slide 1 of 2" aria-roledescription="slide" id="feat-track-slide-0" role="group">`,
		`<div aria-label="Slide 2 of 2" aria-roledescription="slide" id="feat-track-slide-1" role="group">`,
		`<a aria-current="true" aria-label="Go to slide 1" data-hui-carousel-goto="0" href="#feat-track-slide-0">`,
		`<a aria-label="Go to slide 2" data-hui-carousel-goto="1" href="#feat-track-slide-1">`,
		`data-hui-carousel-prev=""`,
		`data-hui-carousel-next=""`,
		`>Previous</a>`,
		`>Next</a>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("carousel missing %q:\n%s", want, h)
		}
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
	}, Strings: &Strings{CarouselSlide: "Diapositive {n} sur {total}", CarouselGoTo: "Aller à la diapositive {n}"}}, nil))
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
