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

// CarouselSlide is one entry.
type CarouselSlide struct {
	// Content is the slide body (required).
	Content render.HTML
	// Label is the slide's accessible label. Default "Slide {n} of {total}".
	Label string
}

// CarouselConfig configures a Carousel.
type CarouselConfig struct {
	// Slides are the entries (≥1).
	Slides []CarouselSlide
	// Label names the carousel region (required).
	Label string
	// NoDots hides the pagination dots.
	NoDots bool
	// NoArrows hides the Prev/Next buttons.
	NoArrows bool
	// AutoRotateMs, when > 0, auto-advances every N ms and draws a pause
	// button after the dots. The rotation holds while the button says
	// paused, on hover, focus and a hidden tab, and starts paused under
	// prefers-reduced-motion until the reader presses play.
	AutoRotateMs int
	// Loop wraps Next on the last slide to the first, and Prev back.
	Loop bool
	// VisiblePerView (1 to 8, default 1) is the most slides shown side by side.
	VisiblePerView int
	ID             string
	Class          string
	// ExtraAttrs lands on the root; class, id, data-hui-*, role, aria-roledescription and aria-label are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the carousel's sentences are said in.
	Ctx context.Context
}

// Carousel renders the slider. It panics without Label or a slide, or
// with a slide that has no Content.
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
		headless.PartRoot:           "fui-carousel",
		headless.PartCarouselStage:  "fui-carousel__stage",
		headless.PartCarouselTrack:  "fui-carousel__track",
		headless.PartCarouselSlide:  "fui-carousel__slide",
		headless.PartCarouselDot:    "fui-carousel__dot",
		headless.PartCarouselPrev:   "fui-carousel__prev",
		headless.PartCarouselNext:   "fui-carousel__next",
		headless.PartCarouselPause:  "fui-carousel__pause",
		headless.PartMarker:         "fui-carousel__dots",
		headless.PartVisuallyHidden: "fui-visually-hidden",
	}
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
	if id == "" && !render.HasIDScope(ctx) {
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
		Ctx:            ctx,
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
  /* The stage spans the row; under it the dots sit centred and the
     rotation button at the end, in the order they come in the markup. */
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
}
[data-cui-comp="ui-carousel"] > .fui-carousel__stage { grid-column: 1 / -1; }
[data-cui-comp="ui-carousel"] > .fui-carousel__dots { grid-column: 2; }
[data-cui-comp="ui-carousel"] > .fui-carousel__pause { grid-column: 3; justify-self: end; }
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
  /* Knob: --ui-carousel-gap, the room between slides, over the md
     layout gap step. The slide width below subtracts the same value. */
  gap: var(--ui-carousel-gap, var(--ui-layout-gap-md, var(--spacing-md, 8px)));
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  scrollbar-width: none;
  touch-action: manipulation;
}
[data-cui-comp="ui-carousel"] .fui-carousel__track::-webkit-scrollbar { display: none; }
[data-cui-comp="ui-carousel"] .fui-carousel__slide {
  --_carousel-cols: var(--ui-carousel-cols, 1);
  flex: 0 0 calc((100% - (var(--_carousel-cols) - 1) * var(--ui-carousel-gap, var(--ui-layout-gap-md, var(--spacing-md, 8px)))) / var(--_carousel-cols));
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
[data-cui-comp="ui-carousel"] .fui-carousel__track > .fui-carousel__slide > :focus-visible { outline-offset: calc(-2 * var(--stroke-focus-offset, 2px)); }
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
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
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
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  color: var(--color-text, #09090B);
  cursor: pointer;
  touch-action: manipulation;
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), box-shadow var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), translate var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
  padding: 0;
  font: inherit;
  /* The primitive's word ("Previous"/"Next") stays the accessible
     name via aria-label; visually the control is its chevron, so the
     word collapses and the ::before glyph draws the arrow. */
  font-size: 0;
}
/* Knobs (beside --ui-carousel-cols): --ui-carousel-arrow-size (9px)
   is the prev/next chevron glyph's square; --ui-carousel-dot-size
   (10px) is the visible dot pip inside its 24px hit area. */
[data-cui-comp="ui-carousel"] .fui-carousel__prev::before,
[data-cui-comp="ui-carousel"] .fui-carousel__next::before {
  content: "";
  inline-size: var(--ui-carousel-arrow-size, 9px);
  block-size: var(--ui-carousel-arrow-size, 9px);
  border-inline-start: var(--stroke-thick, 2px) solid currentColor;
  border-block-start: var(--stroke-thick, 2px) solid currentColor;
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev::before { transform: rotate(-45deg); }
[data-cui-comp="ui-carousel"] .fui-carousel__next::before { transform: rotate(135deg); }
[data-cui-comp="ui-carousel"] .fui-carousel__prev { inset-inline-start: 0; }
[data-cui-comp="ui-carousel"] .fui-carousel__next { inset-inline-end: 0; }
/* Press knobs: --ui-carousel-arrow-{hover,active}-{translate,shadow}
   over the shared --ui-press-* knobs. The arrows centre themselves
   with transform, so the press moves them with translate, which
   composes with it. */
[data-cui-comp="ui-carousel"] .fui-carousel__prev:hover,
[data-cui-comp="ui-carousel"] .fui-carousel__next:hover {
  background: var(--color-surface-soft, #F4F4F5);
  box-shadow: var(--ui-carousel-arrow-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-xs)));
  translate: var(--ui-carousel-arrow-hover-translate, var(--ui-press-hover-translate, none));
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev:active,
[data-cui-comp="ui-carousel"] .fui-carousel__next:active {
  box-shadow: var(--ui-carousel-arrow-active-shadow, var(--ui-press-active-shadow, var(--ui-carousel-arrow-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-xs)))));
  translate: var(--ui-carousel-arrow-active-translate, var(--ui-press-active-translate, var(--ui-carousel-arrow-hover-translate, var(--ui-press-hover-translate, none))));
}
/* At an end of a carousel without Loop the module marks the arrow
   aria-disabled; it stays focusable and dims. Knob:
   --ui-carousel-arrow-disabled-opacity (0.4). */
[data-cui-comp="ui-carousel"] :is(.fui-carousel__prev, .fui-carousel__next)[aria-disabled="true"] {
  opacity: var(--ui-carousel-arrow-disabled-opacity, 0.4);
  cursor: default;
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  translate: none;
}
[data-cui-comp="ui-carousel"] .fui-carousel__prev:focus-visible,
[data-cui-comp="ui-carousel"] .fui-carousel__next:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* Scoped copy of the visually-hidden recipe: the slide status live
   region must not be seen on a page that loads only this sheet. */
[data-cui-comp="ui-carousel"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
[data-cui-comp="ui-carousel"] .fui-carousel__dots {
  display: flex;
  gap: calc(var(--spacing-sm, 4px) * 1.5);
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
  border-radius: var(--radii-full, 9999px);
  background: transparent;
  cursor: pointer;
  touch-action: manipulation;
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
  left: 50%;
  inline-size: var(--ui-carousel-dot-size, 10px);
  block-size: var(--ui-carousel-dot-size, 10px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-border, #E4E4E7);
  transform: translate(-50%, -50%);
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-carousel"] .fui-carousel__dot[aria-current="true"]::after {
  background: var(--color-primary, #18181B);
  transform: translate(-50%, -50%) scale(1.2);
}
[data-cui-comp="ui-carousel"] .fui-carousel__dot:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* The rotation button: a 24px target at the end of the dot row whose
   glyph is two bars while the slides rotate and a play triangle once
   aria-pressed says they are paused. The word stays its accessible
   name. Knob: --ui-carousel-pause-size (10px), the glyph's square. */
[data-cui-comp="ui-carousel"] .fui-carousel__pause {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: var(--spacing-xl, 24px);
  block-size: var(--spacing-xl, 24px);
  margin-block-start: var(--spacing-xs, 2px);
  padding: 0;
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: transparent;
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  touch-action: manipulation;
  font: inherit;
  font-size: 0;
}
[data-cui-comp="ui-carousel"] .fui-carousel__pause:hover { color: var(--color-text, #09090B); }
[data-cui-comp="ui-carousel"] .fui-carousel__pause:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-carousel"] .fui-carousel__pause::before {
  content: "";
  box-sizing: border-box;
  inline-size: calc(var(--ui-carousel-pause-size, 10px) * 0.8);
  block-size: var(--ui-carousel-pause-size, 10px);
  border-inline: calc(var(--ui-carousel-pause-size, 10px) * 0.3) solid currentColor;
}
[data-cui-comp="ui-carousel"] .fui-carousel__pause[aria-pressed="true"]::before {
  inline-size: var(--ui-carousel-pause-size, 10px);
  border: 0;
  background: currentColor;
  clip-path: polygon(10% 0, 100% 50%, 10% 100%);
}
/* With no script the track still scrolls by touch, trackpad and keys,
   but nothing would answer the arrows, the dots or the rotation
   button, so they are not drawn and the arrows' gutters close. */
@media (scripting: none) {
  [data-cui-comp="ui-carousel"] :is(.fui-carousel__prev, .fui-carousel__next, .fui-carousel__dots, .fui-carousel__pause) { display: none; }
  [data-cui-comp="ui-carousel"] .fui-carousel__stage:has(> .fui-carousel__prev) { padding-inline: 0; }
}
/* Forced colours drop backgrounds: the current dot and the play glyph
   draw in system colours. */
@media (forced-colors: active) {
  [data-cui-comp="ui-carousel"] .fui-carousel__dot::after { forced-color-adjust: none; background: GrayText; }
  [data-cui-comp="ui-carousel"] .fui-carousel__dot[aria-current="true"]::after { background: Highlight; }
  [data-cui-comp="ui-carousel"] .fui-carousel__pause[aria-pressed="true"]::before { forced-color-adjust: none; background: ButtonText; }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-carousel"] .fui-carousel__track { scroll-behavior: auto; }
  [data-cui-comp="ui-carousel"] .fui-carousel__dot::after { transition: none; }
}`
}
