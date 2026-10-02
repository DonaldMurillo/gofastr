package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
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

func TestCarouselCSSRestoresTheOverlaidChrome(t *testing.T) {
	css := carouselCSS(style.Theme{})
	// Item 18's contract: the stage is the arrows' positioning
	// context (so they centre on the track, not the dot row), the
	// arrows are round overlaid controls with a CSS chevron — not
	// underlined text links — and the dots are pip indicators with a
	// 24px hit area, not visible numbers.
	for _, want := range []string{
		// The stage must actually BE the positioning context — an
		// empty rule with the right selector positions the arrows
		// against an ancestor and the chrome quietly drifts.
		"[data-fui-comp=\"ui-carousel\"] .fui-carousel__stage {\n  /* Positioning context for the overlaid prev/next arrows, so they\n     centre on the track and cannot overlap the dot row below\n     (WCAG 2.2 target-size). The stage is also at least as tall as\n     its overlaid controls: a short track would otherwise let the\n     44px arrows poke into the dots row and clip the outer dots'\n     target envelopes. */\n  position: relative;\n  min-block-size: var(--spacing-touch-target, 44px);\n}",
		"border-radius: 999px;",
		".fui-carousel__prev::before,",
		".fui-carousel__dot::after {",
		"inline-size: 10px;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("carouselCSS lost %q — the overlaid chrome regressed:\n%s", want, css)
		}
	}
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
	// Match the dot CLASS literal: the container class
	// "fui-carousel__dots" shares the substring otherwise.
	if c := strings.Count(h, `class="fui-carousel__dot"`); c != 3 {
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
	if strings.Contains(h, `class="fui-carousel__dot"`) {
		t.Errorf("NoDots=true should not emit dots:\n%s", h)
	}
}

func TestCarouselArrowsByDefault(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:  "x",
		Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, "fui-carousel__prev") || !strings.Contains(h, "fui-carousel__next") {
		t.Errorf("Carousel should render Prev/Next by default:\n%s", h)
	}
}

func TestCarouselAutoRotateMarker(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:        "x",
		AutoRotateMs: 4000,
		Slides:       []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, `data-hui-carousel-rotate-ms="4000"`) {
		t.Errorf("AutoRotateMs should emit data-hui-carousel-rotate-ms:\n%s", h)
	}
}

func TestCarouselLoopMarker(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:  "x",
		Loop:   true,
		Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(h, `data-hui-carousel-loop`) {
		t.Errorf("Loop=true should emit data-hui-carousel-loop:\n%s", h)
	}
}

func TestCarouselVisiblePerViewClampedAndApplied(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:          "x",
		VisiblePerView: 99,
		Slides:         []CarouselSlide{{Content: render.Text("a")}},
	}))
	if !strings.Contains(h, "fui-carousel--cols-8") {
		t.Errorf("VisiblePerView clamps to 8:\n%s", h)
	}
}

func TestCarouselConcurrentRenderUniqueIDs(t *testing.T) {
	// carouselSeqCounter was a plain int, racy under concurrent renders
	// (`go test -race`). It also collided with autoID's namespace. Run
	// N parallel renders and assert every emitted id="ui-carousel-…" is
	// unique.
	const N = 32
	ids := make([]string, N)
	done := make(chan int, N)
	for i := range N {
		go func(i int) {
			h := string(Carousel(CarouselConfig{
				Label:  "x",
				Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
			}))
			// Extract id="ui-carousel-…" substring.
			marker := `id="ui-carousel-`
			_, after, ok := strings.Cut(h, marker)
			if !ok {
				ids[i] = ""
			} else {
				rest := after
				end := strings.Index(rest, `"`)
				ids[i] = rest[:end]
			}
			done <- i
		}(i)
	}
	for range N {
		<-done
	}
	seen := make(map[string]bool, N)
	for _, id := range ids {
		if id == "" {
			t.Fatalf("missing carousel id in render output")
		}
		if seen[id] {
			t.Fatalf("duplicate carousel id %q under concurrent render — counter is racy", id)
		}
		seen[id] = true
	}
}

func TestCarouselExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Carousel(CarouselConfig{
		Label:  "Featured",
		Slides: []CarouselSlide{{Content: render.Text("a")}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "aria-label": "evil", "Class": "evil",
		},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `aria-label="Featured"`) {
		t.Errorf("region label lost its framework value:\n%s", root)
	}
	for _, banned := range []string{"evil", `role="presentation"`} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	if !strings.Contains(root, `role="region"`) {
		t.Errorf("region role lost:\n%s", root)
	}
}
