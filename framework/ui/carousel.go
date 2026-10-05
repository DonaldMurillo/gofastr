package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Carousel ───────────────────────────────────────────────────────
//
// Horizontal scroll-snap slider rendered through headless.Carousel:
// a labelled region, a native scrollable track, prev/next controls and
// dot anchors as fragment hrefs (the no-script contract), the active
// slide + status bound by the registered headless-carousel module.

// CarouselSlide is one entry.
type CarouselSlide struct {
	// Content is the slide body (required). Caller decides the shape.
	Content render.HTML
	// Label is the slide's accessible label. Defaults to the
	// Strings.CarouselSlide sentence ("Slide {n} of {total}").
	Label string
}

// CarouselConfig configures a Carousel.
type CarouselConfig struct {
	// Slides are the entries (≥1).
	Slides []CarouselSlide
	// Label is the accessible label for the carousel region (required,
	// becomes role=region + aria-label).
	Label string
	// NoDots hides the pagination dots.
	NoDots bool
	// NoArrows hides the Prev/Next buttons.
	NoArrows bool
	// AutoRotateMs, when > 0, auto-advances every N ms. Paused on
	// hover, focus, hidden tab and prefers-reduced-motion.
	AutoRotateMs int
	// Loop makes Next-on-last wrap to first (and vice versa). Default
	// false: Prev/Next refuse at the ends.
	Loop bool
	// VisiblePerView (default 1) is the most slides shown side by
	// side: a carousel narrower than 40rem shows at most two, and one
	// narrower than 26rem shows one, without its arrows when it has dots.
	VisiblePerView int
	ID             string
	Class          string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the carousel's root.
	// Keys the component owns are dropped: class and id, the data-hui-*
	// wiring, and the region contract (role, aria-roledescription,
	// aria-label).
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the
	// carousel's sentences. When nil, English fallbacks apply.
	Ctx context.Context
}

// Carousel renders the slider.
func Carousel(cfg CarouselConfig) render.HTML {
	if len(cfg.Slides) == 0 {
		panic("ui: Carousel requires ≥1 Slide")
	}
	if cfg.Label == "" {
		panic("ui: Carousel requires Label")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	classes := headless.Classes{
		headless.PartRoot:          "fui-carousel",
		headless.PartCarouselStage: "fui-carousel__stage",
		headless.PartCarouselTrack: "fui-carousel__track",
		headless.PartCarouselSlide: "fui-carousel__slide",
		headless.PartCarouselDot:   "fui-carousel__dot",
		headless.PartCarouselPrev:  "fui-carousel__prev",
		headless.PartCarouselNext:  "fui-carousel__next",
		headless.PartMarker:        "fui-carousel__dots",
	}
	// The visible-per-view variant rides the root as a class modifier;
	// the count clamps to the CSS ceiling (8) the way it always did.
	// The public contract clamps the WHOLE out-of-range band
	// (negative, zero, huge) into cols-1..cols-8 — the emitted class
	// must always have a matching CSS rule.
	v := cfg.VisiblePerView
	if v < 1 {
		v = 1
	}
	if v > 8 {
		v = 8
	}
	classes[headless.PartRoot] += " fui-carousel--cols-" + strconv.Itoa(v)
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}

	slides := make([]headless.CarouselSlide, len(cfg.Slides))
	for i, s := range cfg.Slides {
		if s.Content == "" {
			panic("ui: Carousel slide requires Content")
		}
		slides[i] = headless.CarouselSlide{Content: s.Content, Label: s.Label}
	}
	id := cfg.ID
	if id == "" {
		// autoID is process-global + atomic, so concurrent renders
		// across goroutines (or HTTP requests) never collide — the
		// primitive's content-hash fallback is deterministic by design
		// (identical content shares it), which is wrong for two
		// identical carousels on one page.
		id = autoID("ui-carousel")
	}
	out := headless.Carousel(headless.CarouselProps{
		Label:          cfg.Label,
		Slides:         slides,
		Loop:           cfg.Loop,
		AutoRotateMS:   cfg.AutoRotateMs,
		VisiblePerView: v,
		NoDots:         cfg.NoDots,
		NoArrows:       cfg.NoArrows,
		ID:             id,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs, "class", "role", "aria-roledescription", "aria-label"),
		Strings:        StringsFor(ctx),
	}, classes)
	return carouselStyle.WrapHTML(out)
}

var carouselStyle = registry.RegisterStyle("ui-carousel", carouselCSS)

func carouselCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-carousel"]).fui-carousel {
  position: relative;
  /* The carousel's own width picks how many slides fit (see the
     @container rules below), so a three-up carousel in a phone column
     shows one readable slide, not three slivers. */
  container-type: inline-size;
  /* Containment gives the carousel no width of its own, so as a flex
     item with an auto basis it collapsed to 0px. It fills its row. */
  inline-size: 100%;
}
[data-cui-comp="ui-carousel"] .fui-carousel__stage {
  /* Positioning context for the overlaid prev/next arrows, so they
     centre on the track and cannot overlap the dot row below
     (WCAG 2.2 target-size). The stage is also at least as tall as
     its overlaid controls: a short track would otherwise let the
     44px arrows poke into the dots row and clip the outer dots'
     target envelopes. */
  position: relative;
  min-block-size: var(--spacing-touch-target, 44px);
}
/* The arrows sit in a gutter beside the track rather than over it, so
   they never cover a slide's text: a card slide lost its first letters
   under an overlaid arrow. NoArrows renders no prev control and keeps
   the full width. */
[data-cui-comp="ui-carousel"] .fui-carousel__stage:has(> .fui-carousel__prev) {
  padding-inline: calc(var(--spacing-touch-target, 44px) + var(--spacing-md, 8px));
}
[data-cui-comp="ui-carousel"] .fui-carousel__track {
  display: flex;
  gap: var(--spacing-md, 8px);
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  scrollbar-width: none;
}
[data-cui-comp="ui-carousel"] .fui-carousel__track::-webkit-scrollbar { display: none; }
[data-cui-comp="ui-carousel"] .fui-carousel__slide {
  --_carousel-cols: var(--ui-carousel-cols, 1);
  flex: 0 0 calc((100% - (var(--_carousel-cols) - 1) * var(--spacing-md, 8px)) / var(--_carousel-cols));
  scroll-snap-align: start;
  border-radius: var(--radii-md, 8px);
  overflow: hidden;
  /* The track stretches every slide to the tallest; a slide's lone
     child takes that height, so cards in a row end on one line. Several
     children (an image and its caption) stack from the top instead of
     spreading over it. An image keeps its ratio: a replaced item does
     not stretch. */
  display: grid;
  align-content: start;
}
[data-cui-comp="ui-carousel"] .fui-carousel__slide:has(> :only-child) { align-content: stretch; }
/* A child that fills the slide would have its focus ring clipped by
   the slide's overflow, so the ring draws inside its edge. The extra
   track step outweighs a component's own :focus-visible rule. */
[data-cui-comp="ui-carousel"] .fui-carousel__track > .fui-carousel__slide > :focus-visible { outline-offset: -4px; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-1 { --ui-carousel-cols: 1; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-2 { --ui-carousel-cols: 2; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-3 { --ui-carousel-cols: 3; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-4 { --ui-carousel-cols: 4; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-5 { --ui-carousel-cols: 5; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-6 { --ui-carousel-cols: 6; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-7 { --ui-carousel-cols: 7; }
[data-cui-comp="ui-carousel"].fui-carousel--cols-8 { --ui-carousel-cols: 8; }
/* VisiblePerView is the most a wide carousel shows. A tablet-wide one
   shows at most two, a phone-wide one shows one. */
@container (max-width: 40rem) {
  [data-cui-comp="ui-carousel"] .fui-carousel__slide { --_carousel-cols: min(var(--ui-carousel-cols, 1), 2); }
}
/* A phone-wide carousel with dots drops its arrows and their gutters,
   which took a third of a 320px column: a swipe and the dots still move
   it. Without dots the arrows are the only pointer control, so they
   stay. The child chain keeps a nested carousel's arrows its own. */
@container (max-width: 26rem) {
  [data-cui-comp="ui-carousel"] .fui-carousel__slide { --_carousel-cols: 1; }
  [data-cui-comp="ui-carousel"]:has(> .fui-carousel__dots) > .fui-carousel__stage:has(> .fui-carousel__prev) { padding-inline: 0; }
  [data-cui-comp="ui-carousel"]:has(> .fui-carousel__dots) > .fui-carousel__stage > :is(.fui-carousel__prev, .fui-carousel__next) { display: none; }
}
[data-cui-comp="ui-carousel"] .fui-carousel__track:focus-visible {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: 2px;
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev,
[data-cui-comp="ui-carousel"] .fui-carousel__next {
  position: absolute;
  inset-block-start: 50%;
  transform: translateY(-50%);
  z-index: 2;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: 999px;
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  color: var(--color-text, #18181B);
  cursor: pointer;
  text-decoration: none;
  font: inherit;
  /* The primitive's word ("Previous"/"Next") stays the accessible
     name via aria-label; visually the control is its chevron, so the
     word collapses and the ::before glyph draws the arrow. */
  font-size: 0;
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev::before,
[data-cui-comp="ui-carousel"] .fui-carousel__next::before {
  content: "";
  inline-size: 9px;
  block-size: 9px;
  border-inline-start: 2px solid currentColor;
  border-block-start: 2px solid currentColor;
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev::before { transform: rotate(-45deg); }
[data-cui-comp="ui-carousel"] .fui-carousel__next::before { transform: rotate(135deg); }
[data-cui-comp="ui-carousel"] .fui-carousel__prev { inset-inline-start: 0; }
[data-cui-comp="ui-carousel"] .fui-carousel__next { inset-inline-end: 0; }
[data-cui-comp="ui-carousel"] .fui-carousel__prev:hover,
[data-cui-comp="ui-carousel"] .fui-carousel__next:hover { background: var(--color-surface-soft, #F4F4F5); }
[data-cui-comp="ui-carousel"] .fui-carousel__prev:focus-visible,
[data-cui-comp="ui-carousel"] .fui-carousel__next:focus-visible {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: 2px;
}
[data-cui-comp="ui-carousel"] .fui-carousel__dots {
  display: flex;
  gap: 6px;
  justify-content: center;
  margin-block-start: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-carousel"] .fui-carousel__dot {
  /* Target area meets WCAG 2.2 target-size (24px minimum). The
     visible pip stays a 10px dot rendered via ::after so the hit area
     grows without visually bloating the indicator row; the number the
     primitive renders collapses (aria-label names the dot). */
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: var(--spacing-xl, 24px);
  block-size: var(--spacing-xl, 24px);
  padding: 0;
  border: 0;
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  text-decoration: none;
  font-size: 0;
  color: transparent;
}
/* The module hides the dots past the last reachable position; the
   display above would beat the UA's [hidden] rule on a page without a
   global reset. */
[data-cui-comp="ui-carousel"] .fui-carousel__dot[hidden] { display: none; }
[data-cui-comp="ui-carousel"] .fui-carousel__dot::after {
  content: "";
  position: absolute;
  inset-block-start: 50%;
  inset-inline-start: 50%;
  inline-size: 10px;
  block-size: 10px;
  border-radius: 999px;
  background: var(--color-border, #E4E4E7);
  transform: translate(-50%, -50%);
  transition: background 120ms ease, transform 120ms ease;
}
[data-cui-comp="ui-carousel"] .fui-carousel__dot[aria-current="true"]::after {
  background: var(--color-primary, #4F46E5);
  transform: translate(-50%, -50%) scale(1.2);
}
[data-cui-comp="ui-carousel"] .fui-carousel__dot:focus-visible {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-carousel"] .fui-carousel__track { scroll-behavior: auto; }
  [data-cui-comp="ui-carousel"] .fui-carousel__dot::after { transition: none; }
}`
}
