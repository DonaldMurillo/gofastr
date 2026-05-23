package ui

import (
	"strconv"
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

func TestCarouselVirtualScrollPlaceholdersAndManifest(t *testing.T) {
	slides := make([]CarouselSlide, 0, 12)
	for i := 0; i < 12; i++ {
		slides = append(slides, CarouselSlide{Content: render.HTML("<img src='img" + strconv.Itoa(i) + ".jpg' alt=''>")})
	}
	h := string(Carousel(CarouselConfig{
		Label:                    "x",
		VirtualScroll:            true,
		VirtualWindow:            3,
		VirtualPlaceholderHeight: "240px",
		Slides:                   slides,
	}))
	// First 3 slides render content; the rest are placeholders.
	// The literal "<img" sequence appears only in hydrated slides;
	// the manifest body has escaped "<img" instead.
	if !strings.Contains(h, "<img src='img2.jpg'") {
		t.Errorf("first 3 slides should ship hydrated; <img2 missing:\n%s", h)
	}
	if strings.Contains(h, "<img src='img11.jpg'") {
		t.Errorf("slides outside window should be deferred; found <img11 inline:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-carousel-defer="3"`) {
		t.Errorf("slide 3 should be a placeholder:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-carousel-defer="11"`) {
		t.Errorf("slide 11 should be a placeholder:\n%s", h)
	}
	if !strings.Contains(h, "min-block-size:240px") {
		t.Errorf("VirtualPlaceholderHeight should apply to placeholders:\n%s", h)
	}
	if !strings.Contains(h, "data-fui-carousel-deferred-for=") {
		t.Errorf("expected deferred-content manifest script:\n%s", h)
	}
	// Manifest JSON should contain the deferred slide HTML escaped.
	if !strings.Contains(h, "img11.jpg") {
		t.Errorf("manifest should carry deferred slide HTML (img11):\n%s", h)
	}
}

func TestCarouselVirtualScrollClampsWindow(t *testing.T) {
	h := string(Carousel(CarouselConfig{
		Label:         "x",
		VirtualScroll: true,
		VirtualWindow: 50, // > slide count
		Slides:        []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	// No slides should be deferred when window exceeds slide count.
	if strings.Contains(h, "data-fui-carousel-defer=") {
		t.Errorf("window > slide count should hydrate everything; got defer attr:\n%s", h)
	}
	if strings.Contains(h, "data-fui-carousel-deferred-for=") {
		t.Errorf("no deferred slides → no manifest script:\n%s", h)
	}
}

func TestCarouselConcurrentRenderUniqueIDs(t *testing.T) {
	// carouselSeqCounter was a plain int — racy under concurrent renders
	// (`go test -race`). It also collided with autoID's namespace. Run
	// N parallel renders and assert every emitted id="ui-carousel-…" is
	// unique.
	const N = 32
	ids := make([]string, N)
	done := make(chan int, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			h := string(Carousel(CarouselConfig{
				Label:  "x",
				Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
			}))
			// Extract id="ui-carousel-…" substring.
			marker := `id="ui-carousel-`
			start := strings.Index(h, marker)
			if start < 0 {
				ids[i] = ""
			} else {
				rest := h[start+len(marker):]
				end := strings.Index(rest, `"`)
				ids[i] = rest[:end]
			}
			done <- i
		}(i)
	}
	for i := 0; i < N; i++ {
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

func TestCarouselVirtualScrollManifestEscapesScripts(t *testing.T) {
	slides := []CarouselSlide{
		{Content: render.HTML("a")},
		{Content: render.HTML("b")},
		{Content: render.HTML("<script>evil()</script>")},
	}
	h := string(Carousel(CarouselConfig{
		Label:         "x",
		VirtualScroll: true,
		VirtualWindow: 1,
		Slides:        slides,
	}))
	// The literal `</script>` sequence inside the JSON manifest must
	// be escaped so it doesn't prematurely terminate the <script> tag.
	// strings.Count of the un-escaped close tag = 1: the genuine
	// closing tag of the manifest's own <script> element. Two would
	// mean a script-injection footgun. (Go's encoding/json escapes
	// `<` and `>` to < / > by default, so the inner script
	// text never reaches the HTML parser as a literal close-tag.)
	if strings.Count(h, "</script>") != 1 {
		t.Errorf("manifest must escape inline </script> sequences (count > 1 = injection footgun):\n%s", h)
	}
	// Sanity: no literal "</scr"+"ipt>" sequence inside the manifest
	// body (the closing tag we count is the manifest's own).
	bodyStart := strings.Index(h, `data-fui-carousel-deferred-for=`)
	bodyEnd := strings.LastIndex(h, "</script>")
	if bodyStart < 0 || bodyEnd < 0 || bodyStart >= bodyEnd {
		t.Fatalf("could not locate manifest body in:\n%s", h)
	}
	if strings.Contains(h[bodyStart:bodyEnd], "</script>") {
		t.Errorf("manifest body contains an unescaped </script>:\n%s", h)
	}
}
