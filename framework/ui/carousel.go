package ui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
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
	// VirtualScroll, when true, server-renders only the first
	// VirtualWindow slides as visible content; the remaining slides
	// emit as same-width placeholder divs paired with a JSON manifest
	// of the deferred HTML. The runtime hydrates each placeholder via
	// IntersectionObserver as it scrolls into the viewport (plus a
	// one-window read-ahead buffer). Use for image-heavy archive views
	// (>50 slides) where rendering every slide upfront is wasteful.
	//
	// Once hydrated, slides stay hydrated for the lifetime of the
	// page — browsers manage the image cache on their own and
	// re-hydrating on scroll-back would feel laggier than the original
	// problem.
	VirtualScroll bool
	// VirtualWindow is the initial render window when VirtualScroll
	// is enabled. Default 5. Slides 0..VirtualWindow-1 ship hydrated;
	// the rest are placeholders.
	VirtualWindow int
	// VirtualPlaceholderHeight is an optional CSS length applied to
	// each unhydrated placeholder slide. Required when slides have
	// no intrinsic flex height (e.g. raw <img> with no fixed aspect)
	// — otherwise placeholders collapse to 0 and IntersectionObserver
	// fires every placeholder at once. Image-only carousels typically
	// pick "240px" or whatever matches the typical slide aspect.
	VirtualPlaceholderHeight string
	ID                       string
	Class                    string
	ExtraAttrs               html.Attrs
	// Ctx carries the per-request context used to resolve the Previous/Next,
	// Go-to-slide and pagination aria labels. When nil, English fallbacks apply.
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
		// autoID is process-global + atomic, so concurrent renders
		// across goroutines (or HTTP requests) never collide.
		id = autoID("ui-carousel")
	}

	cls := "ui-carousel"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	cls += " ui-carousel--cols-" + strconv.Itoa(visible)

	attrs := html.Attrs{
		"class":                cls,
		"role":                 "region",
		"aria-roledescription": "carousel",
		"aria-label":           cfg.Label,
		"id":                   id,
		"data-fui-carousel":    "true",
	}
	if cfg.AutoRotateMs > 0 {
		attrs["data-fui-carousel-autorotate"] = strconv.Itoa(cfg.AutoRotateMs)
	}
	if cfg.Loop {
		attrs["data-fui-carousel-loop"] = "true"
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	// VirtualScroll prep — figure out the inline window.
	virtualWindow := 0
	if cfg.VirtualScroll {
		virtualWindow = cfg.VirtualWindow
		if virtualWindow <= 0 {
			virtualWindow = 5
		}
		if virtualWindow > len(cfg.Slides) {
			virtualWindow = len(cfg.Slides)
		}
	}

	// Slides container. WAI-ARIA Carousel pattern: each slide is
	// role=group + aria-roledescription=slide. Using <ul>/<li> is
	// semantically misleading (slides aren't list items) and trips
	// axe's `list` rule (a <ul> with role=group children isn't a real
	// list). The track itself carries tabindex=0 so axe's
	// scrollable-region-focusable is satisfied even though the visual
	// Prev/Next buttons + Arrow-key nav are the primary affordances.
	slideEls := make([]render.HTML, 0, len(cfg.Slides))
	deferred := map[string]string{}
	for i, s := range cfg.Slides {
		if s.Content == "" {
			panic("ui: Carousel slide requires Content")
		}
		label := s.Label
		if label == "" {
			label = "Slide " + strconv.Itoa(i+1) + " of " + strconv.Itoa(len(cfg.Slides))
		}
		slideAttrs := map[string]string{
			"class":                   "ui-carousel__slide",
			"role":                    "group",
			"aria-roledescription":    "slide",
			"aria-label":              label,
			"data-fui-carousel-slide": strconv.Itoa(i),
		}
		if cfg.VirtualScroll && i >= virtualWindow {
			// Placeholder slide — content is deferred to the JSON
			// manifest and hydrated lazily via IntersectionObserver.
			slideAttrs["data-fui-carousel-defer"] = strconv.Itoa(i)
			if cfg.VirtualPlaceholderHeight != "" {
				slideAttrs["style"] = "min-block-size:" + cfg.VirtualPlaceholderHeight + ";"
			}
			deferred[strconv.Itoa(i)] = string(s.Content)
			slideEls = append(slideEls, render.Tag("div", slideAttrs))
			continue
		}
		slideEls = append(slideEls, render.Tag("div", slideAttrs, s.Content))
	}
	track := render.Tag("div", map[string]string{
		"class":                   "ui-carousel__track",
		"data-fui-carousel-track": "true",
		"tabindex":                "0",
		"aria-label":              cfg.Label + " — slides",
	}, slideEls...)

	// The track + nav arrows live in a positioned "stage" wrapper so the
	// absolute nav buttons center on the TRACK, not the whole carousel
	// (which includes the dot row). Without it the 44px arrows overlap the
	// pagination dots and partially obscure them — a WCAG 2.2 target-size
	// failure (the last dot's unobscured area drops below 24px). The dots
	// stay a sibling grid row below the stage, clear of the arrows.
	stageChildren := []render.HTML{track}

	// Emit the deferred-content manifest so the runtime can hydrate
	// placeholders. JSON keyed by slide index → HTML string. Escapes
	// `</` to neutralise embedded scripts that might prematurely
	// terminate the inline <script>.
	if len(deferred) > 0 {
		if buf, err := json.Marshal(deferred); err == nil {
			s := strings.ReplaceAll(string(buf), `</`, `<\/`)
			stageChildren = append(stageChildren, render.Tag("script", map[string]string{
				"type":                           "application/json",
				"data-fui-carousel-deferred-for": id,
			}, render.HTML(s)))
		}
	}

	// Prev / Next buttons.
	if !cfg.NoArrows {
		prev := render.Tag("button", map[string]string{
			"type":                   "button",
			"class":                  "ui-carousel__nav ui-carousel__nav--prev",
			"aria-label":             i18nui.T(ctx, i18nui.KeyCarouselPrevious),
			"aria-controls":          id,
			"data-fui-carousel-prev": "true",
		}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15 18l-6-6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
		next := render.Tag("button", map[string]string{
			"type":                   "button",
			"class":                  "ui-carousel__nav ui-carousel__nav--next",
			"aria-label":             i18nui.T(ctx, i18nui.KeyCarouselNext),
			"aria-controls":          id,
			"data-fui-carousel-next": "true",
		}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
		stageChildren = append(stageChildren, prev, next)
	}
	children := []render.HTML{render.Tag("div", map[string]string{"class": "ui-carousel__stage"}, stageChildren...)}

	// Pagination dots.
	if !cfg.NoDots && len(cfg.Slides) > 1 {
		dots := make([]render.HTML, 0, len(cfg.Slides))
		for i := range cfg.Slides {
			dotAttrs := map[string]string{
				"type":                  "button",
				"class":                 "ui-carousel__dot",
				"aria-label":            i18nui.TVars(ctx, i18nui.KeyCarouselGoTo, map[string]string{"slide": strconv.Itoa(i + 1)}),
				"aria-controls":         id,
				"data-fui-carousel-dot": strconv.Itoa(i),
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
			"aria-label": i18nui.T(ctx, i18nui.KeyCarouselPagination),
		}, dots...))
	}

	return carouselStyle.WrapHTML(render.Tag("div", attrs, children...))
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

[data-fui-comp="ui-carousel"] .ui-carousel__stage {
  /* Positioning context for the absolute nav arrows, so they center on the
     track and cannot overlap the dot row below (WCAG 2.2 target-size). */
  position: relative;
}

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
[data-fui-comp="ui-carousel"] .ui-carousel__nav--prev { inset-inline-start: var(--spacing-md, 8px); }
[data-fui-comp="ui-carousel"] .ui-carousel__nav--next { inset-inline-end: var(--spacing-md, 8px); }
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
  /* Target area meets WCAG 2.2 target-size (24px minimum). The visible pip
     stays a 10px dot rendered via ::after so the hit area grows without
     visually bloating the indicator row. */
  position: relative;
  inline-size: var(--spacing-xl, 24px);
  block-size: var(--spacing-xl, 24px);
  padding: 0;
  border: 0;
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
}
[data-fui-comp="ui-carousel"] .ui-carousel__dot::after {
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
[data-fui-comp="ui-carousel"] .ui-carousel__dot[aria-current="true"]::after {
  background: var(--color-primary, #4F46E5);
  transform: translate(-50%, -50%) scale(1.2);
}
[data-fui-comp="ui-carousel"] .ui-carousel__dot:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
