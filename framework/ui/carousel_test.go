package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCarouselRequiresSlides(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Carousel without Slides should panic")
		}
	}()
	Carousel(CarouselConfig{Label: "x"})
}

func TestCarouselRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Carousel without Label should panic")
		}
	}()
	Carousel(CarouselConfig{Slides: []CarouselSlide{{Content: render.Text("x")}}})
}

func TestCarouselSlideRequiresContent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Carousel slide without Content should panic")
		}
	}()
	Carousel(CarouselConfig{Label: "x", Slides: []CarouselSlide{{}}})
}

func TestCarouselRendersRegionAndSlideRoles(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label: "Featured products",
		Slides: []CarouselSlide{
			{Content: render.Text("one")},
			{Content: render.Text("two")},
		},
	}))
	if !strings.Contains(h, `role="region"`) {
		t.Errorf("carousel root should be role=region:\n%s", h)
	}
	if !strings.Contains(h, `aria-roledescription="carousel"`) {
		t.Errorf("carousel root should declare roledescription=carousel:\n%s", h)
	}
	if c := strings.Count(h, `aria-roledescription="slide"`); c != 2 {
		t.Errorf("each slide should declare roledescription=slide; got %d:\n%s", c, h)
	}
}

func TestCarouselDotsByDefault(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label: "x",
		Slides: []CarouselSlide{
			{Content: render.Text("a")}, {Content: render.Text("b")}, {Content: render.Text("c")},
		},
	}))
	// Match the dot CLASS literal — the container class
	// "ui-carousel__dots" shares the substring otherwise.
	if c := strings.Count(h, `class="ui-carousel__dot"`); c != 3 {
		t.Errorf("expected 3 pagination dots, got %d:\n%s", c, h)
	}
	if !strings.Contains(h, `aria-current="true"`) {
		t.Errorf("first dot should be aria-current=true on initial render:\n%s", h)
	}
}

func TestCarouselNoDotsHidesPagination(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:  "x",
		NoDots: true,
		Slides: []CarouselSlide{
			{Content: render.Text("a")}, {Content: render.Text("b")},
		},
	}))
	if strings.Contains(h, `class="ui-carousel__dot"`) {
		t.Errorf("NoDots=true should not emit dots:\n%s", h)
	}
}

func TestCarouselArrowsByDefault(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:  "x",
		Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, "ui-carousel__nav--prev") || !strings.Contains(h, "ui-carousel__nav--next") {
		t.Errorf("Carousel should render Prev/Next by default:\n%s", h)
	}
}

func TestCarouselAutoRotateMarker(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:        "x",
		AutoRotateMs: 4000,
		Slides:       []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, `data-fui-carousel-autorotate="4000"`) {
		t.Errorf("AutoRotateMs should emit data-fui-carousel-autorotate:\n%s", h)
	}
}

func TestCarouselLoopMarker(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:  "x",
		Loop:   true,
		Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, `data-fui-carousel-loop="true"`) {
		t.Errorf("Loop=true should emit data-fui-carousel-loop:\n%s", h)
	}
}

func TestCarouselVisiblePerViewClampedAndApplied(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:          "x",
		VisiblePerView: 99,
		Slides:         []CarouselSlide{{Content: render.Text("a")}},
	}))
	if !strings.Contains(h, "ui-carousel--cols-8") {
		t.Errorf("VisiblePerView clamps to 8:\n%s", h)
	}
}
