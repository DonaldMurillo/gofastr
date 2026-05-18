package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Carousel ───────────────────────────────────────────────────────
//
// Horizontal scroll-snap slider with Prev/Next buttons + pagination
// dots. One slide visible at a time by default; Multi mode shows N
// slides side-by-side and snaps one-at-a-time.
//
// Keyboard: ArrowLeft / ArrowRight while the carousel is focused
// shifts by one slide via the same Prev/Next mechanism.
//
// Opt-in AutoRotate: the runtime cycles slides every N ms. Pauses
// on hover, focus, and prefers-reduced-motion.

// CarouselSlide is one entry.
type CarouselSlide struct {
	// Content is the slide body (required) — caller decides the shape.
	// For an image-only carousel pass an <img>; for richer slides pass
	// a Card or any composition.
	Content render.HTML
	// Label is the slide's accessible label (announced when focused
	// via Tab nav within the carousel). Defaults to "Slide N of M".
	Label string
}

// CarouselConfig configures a Carousel.
type CarouselConfig struct {
	// Slides are the entries (≥1).
	Slides []CarouselSlide
	// Label is the accessible label for the carousel region (required,
	// becomes role=region + aria-label).
	Label string
	// ShowDots renders pagination dots under the slides (default on).
	// Set to false explicitly via NoDots if you want to hide them.
	NoDots bool
	// ShowArrows renders Prev/Next buttons (default on). Set NoArrows
	// to hide them.
	NoArrows bool
	// AutoRotateMs, when > 0, auto-advances every N ms. Paused on
	// hover, focus, and when prefers-reduced-motion is true.
	AutoRotateMs int
	// Loop, when true, makes Next-on-last wrap to first (and vice
	// versa). Default false — Prev/Next disable at the ends.
	Loop bool
	// VisiblePerView (default 1) shows N slides side-by-side; snap
	// still steps one slide at a time.
	VisiblePerView int
	ID             string
	Class          string
	Attrs          html.Attrs
}

// Carousel renders the slider.
func Carousel(cfg CarouselConfig) render.HTML {
	if len(cfg.Slides) == 0 {
		panic("ui: Carousel requires ≥1 Slide")
	}
	if cfg.Label == "" {
		panic("ui: Carousel requires Label")
	}
	visible := cfg.VisiblePerView
	if visible == 0 {
		visible = 1
	}
	if visible < 1 {
		visible = 1
	}
	if visible > 8 {
		visible = 8
	}

	id := cfg.ID
	if id == "" {
		// Stable-ish auto id so per-instance dots / aria-controls work.
		id = "ui-carousel-" + strconv.Itoa(autoCarouselSeq())
	}

	cls := "ui-carousel"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	cls += " ui-carousel--cols-" + strconv.Itoa(visible)

	attrs := html.Attrs{
		"class":             cls,
		"role":              "region",
		"aria-roledescription": "carousel",
		"aria-label":        cfg.Label,
		"id":                id,
		"data-fui-carousel": "true",
	}
	if cfg.AutoRotateMs > 0 {
		attrs["data-fui-carousel-autorotate"] = strconv.Itoa(cfg.AutoRotateMs)
	}
	if cfg.Loop {
		attrs["data-fui-carousel-loop"] = "true"
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}

	// Slides container. WAI-ARIA Carousel pattern: each slide is
	// role=group + aria-roledescription=slide. Using <ul>/<li> is
	// semantically misleading (slides aren't list items) and trips
	// axe's `list` rule (a <ul> with role=group children isn't a real
	// list). The track itself carries tabindex=0 so axe's
	// scrollable-region-focusable is satisfied even though the visual
	// Prev/Next buttons + Arrow-key nav are the primary affordances.
	slideEls := make([]render.HTML, 0, len(cfg.Slides))
	for i, s := range cfg.Slides {
		if s.Content == "" {
			panic("ui: Carousel slide requires Content")
		}
		label := s.Label
		if label == "" {
			label = "Slide " + strconv.Itoa(i+1) + " of " + strconv.Itoa(len(cfg.Slides))
		}
		slideEls = append(slideEls, render.Tag("div", map[string]string{
			"class":                   "ui-carousel__slide",
			"role":                    "group",
			"aria-roledescription":    "slide",
			"aria-label":              label,
			"data-fui-carousel-slide": strconv.Itoa(i),
		}, s.Content))
	}
	track := render.Tag("div", map[string]string{
		"class":                   "ui-carousel__track",
		"data-fui-carousel-track": "true",
		"tabindex":                "0",
		"aria-label":              cfg.Label + " — slides",
	}, slideEls...)

	children := []render.HTML{track}

	// Prev / Next buttons.
	if !cfg.NoArrows {
		prev := render.Tag("button", map[string]string{
			"type":                   "button",
			"class":                  "ui-carousel__nav ui-carousel__nav--prev",
			"aria-label":             "Previous slide",
			"aria-controls":          id,
			"data-fui-carousel-prev": "true",
		}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15 18l-6-6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
		next := render.Tag("button", map[string]string{
			"type":                   "button",
			"class":                  "ui-carousel__nav ui-carousel__nav--next",
			"aria-label":             "Next slide",
			"aria-controls":          id,
			"data-fui-carousel-next": "true",
		}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
		children = append(children, prev, next)
	}

	// Pagination dots.
	if !cfg.NoDots && len(cfg.Slides) > 1 {
		dots := make([]render.HTML, 0, len(cfg.Slides))
		for i := range cfg.Slides {
			dotAttrs := map[string]string{
				"type":                    "button",
				"class":                   "ui-carousel__dot",
				"aria-label":              "Go to slide " + strconv.Itoa(i+1),
				"aria-controls":           id,
				"data-fui-carousel-dot":   strconv.Itoa(i),
			}
			if i == 0 {
				dotAttrs["aria-current"] = "true"
			}
			dots = append(dots, render.Tag("button", dotAttrs))
		}
		// Plain <div> wrapper (no role="tablist" — dots aren't real
		// tabs, and tablist would require role="tab" children which
		// axe enforces via aria-required-children).
		children = append(children, render.Tag("div", map[string]string{
			"class":      "ui-carousel__dots",
			"aria-label": "Slide pagination",
		}, dots...))
	}

	return carouselStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// autoCarouselSeq returns a tiny page-unique counter for default IDs.
// Globals are cheap in single-process Go; concurrent renders in the
// same request are serialized through render.HTML construction.
var carouselSeqCounter int

func autoCarouselSeq() int {
	carouselSeqCounter++
	return carouselSeqCounter
}

var carouselStyle = registry.RegisterStyle("ui-carousel", carouselCSS)

func carouselCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-carousel"] {
  position: relative;
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--spacing-sm, 8px);
  --ui-carousel-cols: 1;
}
[data-fui-comp="ui-carousel"].ui-carousel--cols-1 { --ui-carousel-cols: 1; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-2 { --ui-carousel-cols: 2; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-3 { --ui-carousel-cols: 3; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-4 { --ui-carousel-cols: 4; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-5 { --ui-carousel-cols: 5; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-6 { --ui-carousel-cols: 6; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-7 { --ui-carousel-cols: 7; }
[data-fui-comp="ui-carousel"].ui-carousel--cols-8 { --ui-carousel-cols: 8; }

[data-fui-comp="ui-carousel"] .ui-carousel__track {
  display: flex;
  gap: var(--spacing-md, 12px);
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  scrollbar-width: none;
}
[data-fui-comp="ui-carousel"] .ui-carousel__track:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-carousel"] .ui-carousel__track::-webkit-scrollbar { display: none; }

[data-fui-comp="ui-carousel"] .ui-carousel__slide {
  flex: 0 0 calc((100% - (var(--ui-carousel-cols) - 1) * var(--spacing-md, 12px)) / var(--ui-carousel-cols));
  scroll-snap-align: start;
  border-radius: var(--radii-md, 8px);
  overflow: hidden;
}

[data-fui-comp="ui-carousel"] .ui-carousel__nav {
  position: absolute;
  inset-block-start: 50%;
  transform: translateY(-50%);
  z-index: 2;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  border: 0;
  border-radius: 999px;
  background: var(--color-surface, #FFFFFF);
  box-shadow: 0 4px 12px rgba(0,0,0,0.12);
  color: var(--color-text, #18181B);
  cursor: pointer;
}
[data-fui-comp="ui-carousel"] .ui-carousel__nav--prev { inset-inline-start: 8px; }
[data-fui-comp="ui-carousel"] .ui-carousel__nav--next { inset-inline-end: 8px; }
[data-fui-comp="ui-carousel"] .ui-carousel__nav:hover { background: var(--color-surface-soft, #F4F4F5); }
[data-fui-comp="ui-carousel"] .ui-carousel__nav:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-carousel"] .ui-carousel__nav:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

[data-fui-comp="ui-carousel"] .ui-carousel__dots {
  display: flex;
  gap: 6px;
  justify-content: center;
  padding-block-start: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-carousel"] .ui-carousel__dot {
  inline-size: 10px;
  block-size: 10px;
  padding: 0;
  border: 0;
  border-radius: 999px;
  background: var(--color-border, #E4E4E7);
  cursor: pointer;
  transition: background 120ms ease, transform 120ms ease;
}
[data-fui-comp="ui-carousel"] .ui-carousel__dot[aria-current="true"] {
  background: var(--color-primary, #4F46E5);
  transform: scale(1.2);
}
[data-fui-comp="ui-carousel"] .ui-carousel__dot:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
